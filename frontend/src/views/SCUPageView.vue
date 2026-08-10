<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useCartStore } from '../stores/cart';
import Breadcrumbs from '../components/Breadcrumbs.vue';

const { t } = useI18n();

const props = defineProps({
  // If provided, use this data directly instead of fetching from API
  data: { type: Object, default: null },
});

const route = useRoute();
const router = useRouter();
const cart = useCartStore();

const goBack = () => {
  // Go back to referrer, or to parent category, or to root catalog
  if (window.history.length > 1) {
    window.history.back();
    return;
  }
  // Fallback: go to parent category path
  if (treePath.value && treePath.value.length > 0) {
    router.push({ path: '/shop/' + treePath.value.join('/') });
    return;
  }
  router.push({ path: '/shop' });
};

const page = ref(null);
const products = ref([]);
const treePath = ref([]);
const loading = ref(true);
const error = ref(null);

const selectedProduct = ref(null);
const showProducts = ref(false);

const filterForm = reactive({
  sortBy: 'price_asc',
  companyFilter: '',
});

const fetchSCUPage = async () => {
  loading.value = true;
  error.value = null;
  try {
    // Use the full route path: /shop/{tree}/{slug}
    const response = await api.get(route.path);
    const data = response.data;
    page.value = data.page;
    products.value = data.products || [];
    treePath.value = data.tree_path || [];

    initFromData();
  } catch (e) {
    error.value = e.response?.data?.error?.message || 'Страница не найдена';
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const initFromData = () => {
  // Update page title
  if (page.value) {
    document.title = `${page.value.title} — MakoShop`;
  }

  // Select cheapest product by default (first supplier of first modification).
  // If no products available, create a "virtual" product from page data.
  if (modifications.value.length > 0 && modifications.value[0].suppliers.length > 0) {
    selectedProduct.value = modifications.value[0].suppliers[0];
  } else if (page.value && page.value.min_price > 0) {
    // No products yet — use page data as fallback
    selectedProduct.value = {
      id: 0,
      sku: page.value.sku || '',
      name: page.value.title || '',
      price: page.value.min_price,
      currency: page.value.currency || 'RUB',
      company_name: '',
      stock_qty: page.value.product_count || 0,
      status: page.value.product_count > 0 ? 'active' : 'inactive',
      images: page.value.images || [],
      is_virtual: true,
    };
  }
};

const addToCart = async () => {
  if (!selectedProduct.value) return;
  try {
    await cart.addItem(selectedProduct.value.id, 1);
    addToast('Товар добавлен в корзину', 'success');
  } catch (e) {
    addToast(e.response?.data?.message || 'Ошибка добавления в корзину', 'error');
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

const isInStock = (product) => {
  return product.status === 'active' && (product.stock_qty || 0) > 0;
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const currentImageIndex = ref(0);
const activeTab = ref(0); // index in modifications list
const descSupplierIndex = ref(0); // index in suppliers for active tab

// Strip company suffix from product name to get "pure" modification name.
// Example: "Rastar BMW I8 1:24 Серебристый — Magazilla" → "Rastar BMW I8 1:24 Серебристый"
const stripCompanyFromName = (name) => {
  if (!name) return '';
  // Remove " — CompanyName" suffix (Magazilla, DNS, Ситилинк, etc.)
  return name.replace(/\s*—\s*[^—]+$/, '').trim() || name;
};

// Extract company name from product name suffix.
// Example: "Rastar BMW I8 1:24 Серебристый — Magazilla" → "Magazilla"
const getCompanyName = (product) => {
  if (product.company_name) return product.company_name;
  const match = product.name?.match(/\s*—\s*([^—]+)$/);
  return match?.[1]?.trim() || 'Поставщик #' + product.company_id;
};

// Group products by modification (pure name without company). Each mod has multiple supplier offers.
const modifications = computed(() => {
  if (products.value.length === 0) return [];
  const groups = new Map();
  for (const p of products.value) {
    const pureName = stripCompanyFromName(p.name);
    const key = pureName || p.sku || 'Без названия';
    if (!groups.has(key)) {
      groups.set(key, { name: key, suppliers: [] });
    }
    groups.get(key).suppliers.push(p);
  }
  // Sort suppliers within each mod by price
  for (const mod of groups.values()) {
    mod.suppliers.sort((a, b) => a.price - b.price);
  }
  return Array.from(groups.values());
});

// Get all unique suppliers across all modifications
const allSuppliers = computed(() => {
  const seen = new Set();
  const result = [];
  for (const mod of modifications.value) {
    for (const s of mod.suppliers) {
      const key = getCompanyName(s);
      if (!seen.has(key)) {
        seen.add(key);
        result.push(key);
      }
    }
  }
  return result;
});

const currentImages = computed(() => {
  if (selectedProduct.value && selectedProduct.value.images?.length) {
    return selectedProduct.value.images;
  }
  return page.value?.images || [];
});

const currentPrice = computed(() => {
  return selectedProduct.value?.price || page.value?.min_price || 0;
});

const minPrice = computed(() => {
  if (products.value.length === 0) return page.value?.min_price || 0;
  return Math.min(...products.value.map(p => p.price));
});

// Select product and set active tab + supplier index
const selectProduct = (product) => {
  selectedProduct.value = product;
  // Find which modification tab this product belongs to
  for (let i = 0; i < modifications.value.length; i++) {
    const mod = modifications.value[i];
    const supplierIdx = mod.suppliers.findIndex(s => s.id === product.id);
    if (supplierIdx !== -1) {
      activeTab.value = i;
      descSupplierIndex.value = supplierIdx;
      break;
    }
  }
};

onMounted(() => {
  // If data provided via props (from CatalogView), use it directly
  if (props.data) {
    page.value = props.data.page;
    products.value = props.data.products || [];
    treePath.value = props.data.tree_path || [];
    loading.value = false;
    initFromData();
    return;
  }
  // Otherwise fetch from API
  fetchSCUPage();
});

// Watch for props.data changes (when rendered from CatalogView)
watch(
  () => props.data,
  (newData) => {
    if (newData) {
      page.value = newData.page;
      products.value = newData.products || [];
      treePath.value = newData.tree_path || [];
      loading.value = false;
      initFromData();
    }
  },
  { deep: true }
);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Breadcrumbs -->
    <Breadcrumbs :categories="treePath.map(s => ({ slug: s, name: s }))" />

    <!-- Back link -->
    <button
      @click="goBack"
      class="text-sm text-indigo-600 hover:underline mb-4 inline-block cursor-pointer"
    >
      ← Вернуться в каталог
    </button>

    <!-- Error -->
    <div v-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- SCU Page -->
    <div v-else-if="page" class="space-y-8">
      <!-- Title & Price -->
      <div>
        <h1 class="text-2xl font-bold">{{ page.title }}</h1>
        <div v-if="page.brand" class="text-sm text-gray-500 mt-1">{{ page.brand }}</div>
        <div class="mt-3 flex items-baseline gap-3 flex-wrap">
          <span class="text-3xl font-bold text-indigo-600">
            от {{ formatPrice(minPrice) }}
          </span>
          <span v-if="products.length > 1" class="text-sm text-gray-500">
            {{ products.length }} вариантов
          </span>
        </div>
      </div>

      <div class="grid grid-cols-1 lg:grid-cols-3 gap-8">
        <!-- Images (left) -->
        <div class="lg:col-span-1">
          <div class="bg-gray-100 rounded-lg overflow-hidden aspect-square">
            <img
              v-if="currentImages.length"
              :src="currentImages[currentImageIndex]"
              :alt="page.title"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center text-gray-400">
              Нет фото
            </div>
          </div>
          <!-- Thumbnails -->
          <div v-if="currentImages.length > 1" class="flex gap-2 mt-2 flex-wrap">
            <img
              v-for="(img, idx) in currentImages"
              :key="idx"
              :src="img"
              @click="currentImageIndex = idx"
              :class="[
                'w-14 h-14 object-cover rounded border-2 cursor-pointer transition',
                currentImageIndex === idx
                  ? 'border-indigo-600'
                  : 'border-transparent hover:border-indigo-400'
              ]"
            />
          </div>
        </div>

        <!-- Right column: prices by modification + description tabs -->
        <div class="lg:col-span-2 space-y-6">

          <!-- Prices grouped by modification -->
          <div v-if="modifications.length > 0" class="space-y-4">
            <div
              v-for="(mod, modIdx) in modifications"
              :key="modIdx"
              class="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden"
            >
              <!-- Modification header -->
              <div class="px-4 py-2 bg-gray-50 border-b border-gray-200">
                <h3 class="font-medium text-gray-800 text-sm">{{ mod.name }}</h3>
              </div>
              <!-- Supplier offers with radio buttons -->
              <div class="divide-y divide-gray-100">
                <label
                  v-for="product in mod.suppliers"
                  :key="product.id"
                  :class="[
                    'flex items-center gap-3 p-3 cursor-pointer transition',
                    selectedProduct?.id === product.id
                      ? 'bg-indigo-50'
                      : 'hover:bg-gray-50'
                  ]"
                >
                  <!-- Radio button -->
                  <input
                    type="radio"
                    name="scu-product"
                    :value="product.id"
                    :checked="selectedProduct?.id === product.id"
                    @change="selectProduct(product)"
                    class="w-4 h-4 text-indigo-600 border-gray-300 focus:ring-indigo-500 flex-shrink-0 cursor-pointer"
                  />

                  <!-- Supplier & stock -->
                  <div class="flex-1 min-w-0">
                    <div class="text-xs font-medium text-gray-600">
                      {{ getCompanyName(product) }}
                    </div>
                    <span :class="isInStock(product) ? 'text-green-600' : 'text-red-600'" class="text-xs">
                      {{ isInStock(product) ? t('catalog.in_stock') : t('catalog.out_of_stock') }}
                    </span>
                  </div>

                  <!-- Price -->
                  <div class="font-semibold text-indigo-600 whitespace-nowrap">
                    {{ formatPrice(product.price) }}
                  </div>
                </label>
              </div>
            </div>
          </div>

          <!-- Description tabs by modification -->
          <div v-if="modifications.length > 0" class="bg-white rounded-lg shadow-sm border border-gray-200">
            <!-- Tabs -->
            <div class="border-b border-gray-200">
              <div class="flex gap-1 px-4 pt-2 overflow-x-auto">
                <button
                  v-for="(mod, idx) in modifications"
                  :key="idx"
                  @click="activeTab = idx; descSupplierIndex = 0"
                  :class="[
                    'px-3 py-1.5 text-xs rounded-t-lg whitespace-nowrap transition',
                    activeTab === idx
                      ? 'bg-indigo-600 text-white'
                      : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
                  ]"
                >
                  {{ mod.name }}
                </button>
              </div>
            </div>

            <!-- Tab content: supplier carousel -->
            <div class="p-4">
              <template v-if="modifications[activeTab]">
                <!-- Supplier selector -->
                <div class="flex items-center justify-between mb-3">
                  <span class="inline-block px-2 py-0.5 bg-indigo-100 text-indigo-700 text-xs rounded">
                    {{ getCompanyName(modifications[activeTab].suppliers[descSupplierIndex]) }}
                  </span>
                  <div class="flex gap-1">
                    <button
                      @click="descSupplierIndex = (descSupplierIndex - 1 + modifications[activeTab].suppliers.length) % modifications[activeTab].suppliers.length"
                      class="px-2 py-1 text-xs border rounded hover:bg-gray-50"
                    >←</button>
                    <span class="px-2 py-1 text-xs">
                      {{ descSupplierIndex + 1 }} / {{ modifications[activeTab].suppliers.length }}
                    </span>
                    <button
                      @click="descSupplierIndex = (descSupplierIndex + 1) % modifications[activeTab].suppliers.length"
                      class="px-2 py-1 text-xs border rounded hover:bg-gray-50"
                    >→</button>
                  </div>
                </div>

                <!-- Description -->
                <div class="text-sm text-gray-700">
                  <p class="whitespace-pre-line">
                    {{ modifications[activeTab].suppliers[descSupplierIndex]?.description || t('product.no_description') }}
                  </p>
                </div>
              </template>
            </div>
          </div>

          <!-- Selected product add to cart -->
          <div v-if="selectedProduct && !selectedProduct.is_virtual" class="bg-white rounded-lg shadow-sm border border-gray-200 p-4">
            <div class="flex items-center justify-between gap-4">
              <div>
                <div class="text-2xl font-bold text-indigo-600">{{ formatPrice(selectedProduct.price) }}</div>
                <span :class="isInStock(selectedProduct) ? 'text-green-600' : 'text-red-600'" class="text-sm">
                  {{ isInStock(selectedProduct) ? t('catalog.in_stock') : t('catalog.out_of_stock') }}
                </span>
              </div>
              <button
                @click="addToCart"
                :disabled="!isInStock(selectedProduct)"
                class="px-6 py-3 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                {{ t('catalog.add_to_cart') }}
              </button>
            </div>
          </div>

          <!-- Virtual product notice -->
          <div v-if="selectedProduct?.is_virtual" class="p-3 bg-amber-50 border border-amber-200 rounded-lg text-sm text-amber-800">
            {{ t('catalog.virtual_product_notice') }}
          </div>

          <!-- Attributes -->
          <div v-if="page.attributes && Object.keys(page.attributes).length" class="bg-white rounded-lg shadow-sm border border-gray-200 p-4">
            <h3 class="font-medium text-gray-700 mb-3">Характеристики</h3>
            <dl class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-sm">
              <div v-for="(value, key) in page.attributes" :key="key" class="flex flex-col sm:flex-row sm:items-center">
                <dt class="text-gray-500 text-xs sm:text-sm capitalize">{{ key }}</dt>
                <dd class="sm:ml-2 text-sm">{{ value }}</dd>
              </div>
            </dl>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
