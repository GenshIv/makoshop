<script setup>
import { ref, reactive, onMounted, watch, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useCartStore } from '../stores/cart';
import { useToast } from '../composables/useToast';
import { useFormat } from '../composables/useFormat';
import { useSeo } from '../composables/useSeo';
import Breadcrumbs from '../components/Breadcrumbs.vue';
import PriceSparkline from '../components/PriceSparkline.vue';

const { toast } = useToast();
const { formatPrice, formatDate } = useFormat();

const route = useRoute();
const router = useRouter();
const cart = useCartStore();
const { t, locale } = useI18n();

const product = ref(null);

useSeo({
  title: computed(() => (product.value?.name ? `${product.value.name} — MakoShop` : t('pages.product_title'))),
  description: computed(() => product.value?.description || t('pages.default_description')),
  image: computed(() => product.value?.images?.[0] || null),
});
const reviews = ref([]);
const reviewsPagination = reactive({ page: 1, per_page: 10, total: 0, total_pages: 0 });
const loading = ref(true);
const error = ref(null);
const currentImageIndex = ref(0);
const categoryPath = ref([]); // [{id, name}, ...]

const reviewForm = reactive({ rating: 5, comment: '' });
const submittingReview = ref(false);

// Visual 5-star rating based on the average (0..5)
const starRating = computed(() => {
  const avg = Number(product.value?.avg_rating) || 0;
  const filled = Math.round(avg);
  return Array.from({ length: 5 }, (_, i) => i < filled);
});

// Mini price trend derived from the product's offer range (min/max/price)
const priceTrend = computed(() => {
  const p = product.value;
  if (!p) return [];
  const min = Number(p.min_price);
  const max = Number(p.max_price);
  const cur = Number(p.price);
  if (Number.isFinite(min) && Number.isFinite(max) && max > min && Number.isFinite(cur)) {
    return [max, (max + cur) / 2, cur, min];
  }
  return [];
});

const fetchProduct = async () => {
  loading.value = true;
  try {
    const response = await api.get(`/products/${route.params.id}`);
    product.value = response.data;
    currentImageIndex.value = 0;
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
// Uses optimized API endpoint: /categories/tree_path/{id}
const fetchCategoryPath = async (categoryId) => {
  if (!categoryId) {
    categoryPath.value = [];
    return;
  }
  try {
    const response = await api.get(`/categories/tree_path/${categoryId}`);
    const path = response.data || [];
    categoryPath.value = Array.isArray(path) ? path : [];
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
    toast.success(t('cart.added_to_cart'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('cart.add_to_cart_error'));
  }
};

const isInStock = () => {
  return product.value?.status === 'active' && (product.value?.stock_qty || 0) > 0;
};

const submitReview = async () => {
  if (!reviewForm.comment.trim()) {
    toast.error(t('product.add_comment_prompt'));
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
    toast.success(t('product.review_saved'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('product.review_error'));
  } finally {
    submittingReview.value = false;
  }
};

// Get localized attribute label
const attrLabel = (code) => {
  if (!code) return '';
  // Try i18n attr_names[code]
  const key = `attr_names.${code}`;
  const translated = t(key);
  if (translated !== key) return translated;
  // Fallback: humanize code
  let s = code.replace(/_/g, ' ').replace(/-/g, ' ');
  return s.replace(/\b\w/g, c => c.toUpperCase());
};

onMounted(() => {
  fetchProduct();
  fetchReviews();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Breadcrumbs -->
    <Breadcrumbs :categories="categoryPath" :product-name="product?.name" />

    <!-- Back link -->
    <router-link to="/" class="text-sm text-accent hover:underline mb-4 inline-block transition-colors">
      {{ t('catalog.back_to_catalog') }}
    </router-link>

    <!-- Error -->
    <div v-if="error" class="p-4 bg-red-50 text-red-700 rounded-lg theme-dark:bg-red-900/30 theme-dark:text-red-300">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="grid grid-cols-1 md:grid-cols-2 gap-8" aria-hidden="true">
      <div>
        <div class="bg-surface-3 rounded-lg aspect-square animate-pulse"></div>
        <div class="flex gap-2 mt-2">
          <div v-for="i in 4" :key="i" class="w-16 h-16 bg-surface-3 rounded animate-pulse"></div>
        </div>
      </div>
      <div class="space-y-4">
        <div class="h-7 w-3/4 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-4 w-1/4 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-24 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-9 w-1/3 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-12 w-full sm:w-64 bg-surface-3 rounded animate-pulse"></div>
      </div>
    </div>

    <!-- Product -->
    <div v-else class="grid grid-cols-1 md:grid-cols-2 gap-8">
      <!-- Images -->
      <div>
        <div class="bg-surface-2 rounded-xl overflow-hidden aspect-square border border-line">
          <img
            v-if="product.images?.length"
            :src="product.images[currentImageIndex]"
            :alt="product.name"
            loading="lazy"
            decoding="async"
            class="w-full h-full object-cover"
          />
          <div v-else class="w-full h-full flex items-center justify-center text-ink-3">
            {{ t('common.no_photo') }}
          </div>
        </div>
        <!-- Thumbnails -->
        <div v-if="product.images?.length > 1" class="flex gap-2 mt-2 flex-wrap">
          <img
            v-for="(img, idx) in product.images"
            :key="idx"
            :src="img"
            loading="lazy"
            decoding="async"
            @click="currentImageIndex = idx"
            :class="[
              'w-16 h-16 object-cover rounded-lg border-2 cursor-pointer transition',
              currentImageIndex === idx
                ? 'border-indigo-600'
                : 'border-transparent hover:border-indigo-400'
            ]"
          />
        </div>
      </div>

      <!-- Info -->
      <div>
        <h1 class="text-2xl font-bold text-ink">{{ product.name }}</h1>
        <div v-if="product.sku" class="text-sm text-ink-3 mt-1">SKU: {{ product.sku }}</div>
        <div v-if="product.description" class="mt-4 text-ink-2 whitespace-pre-line text-sm">
          {{ product.description }}
        </div>

        <!-- Price & stock -->
        <div class="mt-6 flex items-center gap-4 flex-wrap">
          <span class="text-3xl font-bold text-accent">{{ formatPrice(product.price) }}</span>
          <span :class="isInStock() ? 'text-green-600' : 'text-red-600'" class="text-sm">
            {{ isInStock() ? t('catalog.in_stock') : t('catalog.out_of_stock') }}
          </span>
          <!-- Mini price trend (from min/max offers) -->
          <div v-if="priceTrend.length >= 2" class="flex items-center gap-2 text-accent">
            <PriceSparkline :values="priceTrend" :width="80" :height="28" />
          </div>
        </div>

        <!-- Rating -->
        <div v-if="product.avg_rating !== undefined && product.avg_rating !== null" class="mt-2 flex items-center gap-2">
          <span class="flex" role="img" :aria-label="t('catalog.rating_value', { value: product.avg_rating.toFixed(1) })">
            <span v-for="(isFilled, i) in starRating" :key="i" class="text-lg leading-none" :class="isFilled ? 'text-yellow-400' : 'text-ink-3'">★</span>
          </span>
          <span class="font-medium">{{ product.avg_rating.toFixed(1) }}</span>
          <span class="text-sm text-ink-3">{{ t('catalog.reviews_count', { count: product.review_count || 0 }) }}</span>
        </div>

        <!-- Add to cart -->
        <button
          @click="addToCart"
          :disabled="!isInStock()"
          class="mt-6 btn btn-primary btn-lg w-full sm:w-auto"
        >
          {{ t('catalog.add_to_cart') }}
        </button>

        <!-- Attributes -->
        <div v-if="product.attrs && Object.keys(product.attrs).length" class="mt-6 bg-surface rounded-xl border border-line p-4">
          <h3 class="font-medium text-ink-2">{{ t('catalog.characteristics') }}</h3>
          <dl class="mt-2 grid grid-cols-1 sm:grid-cols-2 gap-2 text-sm">
            <div v-for="(value, key) in product.attrs" :key="key" class="flex flex-col sm:flex-row sm:items-center">
              <dt class="text-ink-3 text-xs sm:text-sm">{{ attrLabel(key) }}</dt>
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
      <div class="bg-surface rounded-lg shadow-sm p-4 mb-6">
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
          class="w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
        ></textarea>
        <button
          @click="submitReview"
          :disabled="submittingReview"
          class="mt-2 btn btn-primary"
        >
          {{ submittingReview ? t('catalog.sending_review') : t('catalog.send_review') }}
        </button>
      </div>

      <!-- Reviews list -->
      <div v-if="reviews.length === 0" class="text-ink-3 text-sm">
        {{ t('catalog.no_reviews_yet') }}
      </div>
      <div v-else class="space-y-4">
        <div v-for="review in reviews" :key="review.id" class="bg-surface rounded-lg shadow-sm p-4">
          <div class="flex items-center justify-between flex-wrap gap-1">
            <div class="flex items-center gap-2">
              <span class="font-medium text-sm">{{ review.user_name || t('catalog.user') }}</span>
              <span class="text-yellow-500 text-sm">
                {{ '★'.repeat(review.rating) }}
              </span>
            </div>
            <span class="text-xs text-ink-3">{{ formatDate(review.created_at) }}</span>
          </div>
          <p class="mt-2 text-sm text-ink-2">{{ review.comment }}</p>
        </div>
      </div>

      <!-- Reviews pagination -->
      <div v-if="reviewsPagination.total_pages > 1" class="flex justify-center gap-2 mt-4">
        <button
          @click="() => { reviewsPagination.page--; fetchReviews(); }"
          :disabled="reviewsPagination.page <= 1"
          class="btn btn-secondary btn-sm"
        >
          {{ t('common.back') }}
        </button>
        <span class="px-3 py-1.5 text-sm">
          {{ reviewsPagination.page }} / {{ reviewsPagination.total_pages }}
        </span>
        <button
          @click="() => { reviewsPagination.page++; fetchReviews(); }"
          :disabled="reviewsPagination.page >= reviewsPagination.total_pages"
          class="btn btn-secondary btn-sm"
        >
          {{ t('common.next') }}
        </button>
      </div>
    </div>
  </div>
</template>
