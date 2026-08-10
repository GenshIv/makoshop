<script setup>
import { ref, reactive, onMounted, watch, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useCartStore } from '../stores/cart';
import Breadcrumbs from '../components/Breadcrumbs.vue';

const route = useRoute();
const router = useRouter();
const cart = useCartStore();
const { t } = useI18n();

const product = ref(null);
const reviews = ref([]);
const reviewsPagination = reactive({ page: 1, per_page: 10, total: 0, total_pages: 0 });
const loading = ref(true);
const error = ref(null);
const currentImageIndex = ref(0);
const categoryPath = ref([]); // [{id, name}, ...]

const reviewForm = reactive({ rating: 5, comment: '' });
const submittingReview = ref(false);

const fetchProduct = async () => {
  loading.value = true;
  try {
    const response = await api.get(`/products/${route.params.id}`);
    product.value = response.data;
    currentImageIndex.value = 0;
    // Update page title
    document.title = `${product.value.name} — MakoShop`;
    // Fetch category path if product has category_id
    if (product.value.category_id) {
      await fetchCategoryPath(product.value.category_id);
    } else {
      categoryPath.value = [];
    }
  } catch (e) {
    error.value = t('product.not_found');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

// Build category path: [{id, name}, ...] from root to current
const fetchCategoryPath = async (categoryId) => {
  if (!categoryId) {
    categoryPath.value = [];
    return;
  }
  try {
    const response = await api.get(`/admin/categories/${categoryId}`);
    const cat = response.data;
    if (!cat) {
      categoryPath.value = [];
      return;
    }
    const path = [{ id: cat.id, name: cat.name }];
    // Walk up the tree
    let currentId = cat.parent_id;
    while (currentId) {
      const parentResponse = await api.get(`/admin/categories/${currentId}`);
      const parent = parentResponse.data;
      if (!parent) break;
      path.unshift({ id: parent.id, name: parent.name });
      currentId = parent.parent_id;
    }
    categoryPath.value = path;
  } catch (e) {
    console.error('Failed to fetch category path:', e);
    categoryPath.value = [];
  }
};

const fetchReviews = async () => {
  try {
    const response = await api.get(`/products/${route.params.id}/reviews`, {
      params: { page: reviewsPagination.page, per_page: reviewsPagination.per_page },
    });
    const data = response.data;
    // API returns {items:[], limit, page, total} or {reviews:[], total, total_pages}
    reviews.value = data.items || data.reviews || [];
    reviewsPagination.total = data.total || 0;
    reviewsPagination.total_pages = data.total_pages || Math.ceil(reviewsPagination.total / reviewsPagination.per_page);
  } catch (e) {
    reviews.value = [];
  }
};

const addToCart = async () => {
  try {
    await cart.addItem(product.value.id, 1);
    addToast(t('cart.added_to_cart'), 'success');
  } catch (e) {
    addToast(e.response?.data?.message || t('cart.add_to_cart_error'), 'error');
  }
};

const addToast = (message, type = 'success') => {
  const toast = document.createElement('div');
  toast.className = `fixed bottom-4 right-4 px-4 py-2 rounded-lg shadow-lg text-sm z-50 transition-opacity ${
    type === 'success' ? 'bg-green-600 text-white' : 'bg-red-600 text-white'
  }`;
  toast.textContent = message;
  document.body.appendChild(toast);
  setTimeout(() => {
    toast.style.opacity = '0';
    setTimeout(() => toast.remove(), 300);
  }, 2000);
};

const isInStock = () => {
  return product.value?.status === 'active' && (product.value?.stock_qty || 0) > 0;
};

const submitReview = async () => {
  if (!reviewForm.comment.trim()) {
    alert(t('product.add_comment_prompt'));
    return;
  }
  submittingReview.value = true;
  try {
    await api.post(`/products/${route.params.id}/reviews`, {
      rating: reviewForm.rating,
      comment: reviewForm.comment,
    });
    reviewForm.comment = '';
    reviewForm.rating = 5;
    await fetchReviews();
    await fetchProduct();
  } catch (e) {
    alert(e.response?.data?.message || t('product.review_error'));
  } finally {
    submittingReview.value = false;
  }
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

onMounted(() => {
  fetchProduct();
  fetchReviews();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Breadcrumbs -->
    <Breadcrumbs :categories="categoryPath" :product-name="product?.name" />

    <!-- Back link -->
    <router-link to="/" class="text-sm text-indigo-600 hover:underline mb-4 inline-block">
      {{ t('catalog.back_to_catalog') }}
    </router-link>

    <!-- Error -->
    <div v-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Product -->
    <div v-else class="grid grid-cols-1 md:grid-cols-2 gap-8">
      <!-- Images -->
      <div>
        <div class="bg-gray-100 rounded-lg overflow-hidden aspect-square">
          <img
            v-if="product.images?.length"
            :src="product.images[currentImageIndex]"
            :alt="product.name"
            class="w-full h-full object-cover"
          />
          <div v-else class="w-full h-full flex items-center justify-center text-gray-400">
            {{ t('common.no_photo') }}
          </div>
        </div>
        <!-- Thumbnails -->
        <div v-if="product.images?.length > 1" class="flex gap-2 mt-2 flex-wrap">
          <img
            v-for="(img, idx) in product.images"
            :key="idx"
            :src="img"
            @click="currentImageIndex = idx"
            :class="[
              'w-16 h-16 object-cover rounded border-2 cursor-pointer transition',
              currentImageIndex === idx
                ? 'border-indigo-600'
                : 'border-transparent hover:border-indigo-400'
            ]"
          />
        </div>
      </div>

      <!-- Info -->
      <div>
        <h1 class="text-2xl font-bold">{{ product.name }}</h1>
        <div v-if="product.sku" class="text-sm text-gray-500 mt-1">SKU: {{ product.sku }}</div>
        <div v-if="product.description" class="mt-4 text-gray-700 whitespace-pre-line text-sm">
          {{ product.description }}
        </div>

        <!-- Price & stock -->
        <div class="mt-6 flex items-center gap-4 flex-wrap">
          <span class="text-3xl font-bold text-indigo-600">{{ formatPrice(product.price) }}</span>
          <span :class="isInStock() ? 'text-green-600' : 'text-red-600'" class="text-sm">
            {{ isInStock() ? t('catalog.in_stock') : t('catalog.out_of_stock') }}
          </span>
        </div>

        <!-- Rating -->
        <div v-if="product.avg_rating !== undefined && product.avg_rating !== null" class="mt-2 flex items-center gap-2">
          <span class="text-yellow-500">★</span>
          <span class="font-medium">{{ product.avg_rating.toFixed(1) }}</span>
          <span class="text-sm text-gray-500">{{ t('catalog.reviews_count', { count: product.review_count || 0 }) }}</span>
        </div>

        <!-- Add to cart -->
        <button
          @click="addToCart"
          :disabled="!isInStock()"
          class="mt-6 px-6 py-3 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition"
        >
          {{ t('catalog.add_to_cart') }}
        </button>

        <!-- Attributes -->
        <div v-if="product.attrs && Object.keys(product.attrs).length" class="mt-6">
          <h3 class="font-medium text-gray-700">{{ t('catalog.characteristics') }}</h3>
          <dl class="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-2 text-sm">
            <div v-for="(value, key) in product.attrs" :key="key" class="flex flex-col sm:flex-row sm:items-center">
              <dt class="text-gray-500 text-xs sm:text-sm capitalize">{{ key }}</dt>
              <dd class="sm:ml-2 text-sm">{{ value }}</dd>
            </div>
          </dl>
        </div>
      </div>
    </div>

    <!-- Reviews section -->
    <div class="mt-12">
      <h2 class="text-xl font-bold mb-4">{{ t('catalog.reviews') }} ({{ reviewsPagination.total }})</h2>

      <!-- Write review -->
      <div class="bg-white rounded-lg shadow-sm p-4 mb-6">
        <h3 class="font-medium mb-3">{{ t('catalog.write_review') }}</h3>
        <div class="flex items-center gap-2 mb-3">
          <span class="text-sm">{{ t('catalog.rating') }}:</span>
          <select v-model="reviewForm.rating" class="px-2 py-1 border rounded text-sm">
            <option v-for="n in 5" :key="n" :value="n">{{ n }} ★</option>
          </select>
        </div>
        <textarea
          v-model="reviewForm.comment"
          rows="3"
          :placeholder="t('catalog.review_placeholder')"
          class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
        ></textarea>
        <button
          @click="submitReview"
          :disabled="submittingReview"
          class="mt-2 px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm hover:bg-indigo-700 disabled:opacity-40"
        >
          {{ submittingReview ? t('catalog.sending_review') : t('catalog.send_review') }}
        </button>
      </div>

      <!-- Reviews list -->
      <div v-if="reviews.length === 0" class="text-gray-500 text-sm">
        {{ t('catalog.no_reviews_yet') }}
      </div>
      <div v-else class="space-y-4">
        <div v-for="review in reviews" :key="review.id" class="bg-white rounded-lg shadow-sm p-4">
          <div class="flex items-center justify-between flex-wrap gap-1">
            <div class="flex items-center gap-2">
              <span class="font-medium text-sm">{{ review.user_name || t('catalog.user') }}</span>
              <span class="text-yellow-500 text-sm">
                {{ '★'.repeat(review.rating) }}
              </span>
            </div>
            <span class="text-xs text-gray-400">{{ new Date(review.created_at).toLocaleDateString('ru-RU') }}</span>
          </div>
          <p class="mt-2 text-sm text-gray-700">{{ review.comment }}</p>
        </div>
      </div>

      <!-- Reviews pagination -->
      <div v-if="reviewsPagination.total_pages > 1" class="flex justify-center gap-2 mt-4">
        <button
          @click="() => { reviewsPagination.page--; fetchReviews(); }"
          :disabled="reviewsPagination.page <= 1"
          class="px-3 py-1.5 border rounded-lg text-sm disabled:opacity-40 hover:bg-gray-50"
        >
          {{ t('common.back') }}
        </button>
        <span class="px-3 py-1.5 text-sm">
          {{ reviewsPagination.page }} / {{ reviewsPagination.total_pages }}
        </span>
        <button
          @click="() => { reviewsPagination.page++; fetchReviews(); }"
          :disabled="reviewsPagination.page >= reviewsPagination.total_pages"
          class="px-3 py-1.5 border rounded-lg text-sm disabled:opacity-40 hover:bg-gray-50"
        >
          {{ t('common.next') }}
        </button>
      </div>
    </div>
  </div>
</template>
