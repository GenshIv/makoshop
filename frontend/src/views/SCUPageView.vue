<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useCartStore } from '../stores/cart';
import Breadcrumbs from '../components/Breadcrumbs.vue';

const { t, locale } = useI18n();

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
const treePathFull = ref([]);
const loading = ref(true);
const error = ref(null);

const selectedProduct = ref(null);
const showProducts = ref(false);

// Company settings: payment methods, delivery times, installment plans
const companySettingsMap = ref({}); // company_id -> { payment_methods: [], delivery_times: [], installment_plans: [] }

const fetchCompanySettings = async () => {
  try {
    // Get all lists
    const [pmRes, dtRes, ipRes] = await Promise.all([
      api.get('/admin/payment-methods'),
      api.get('/admin/delivery-times'),
      api.get('/admin/installment-plans'),
    ]);
    const allPM = (pmRes.data || []).reduce((m, pm) => { m[pm.id] = pm; return m; }, {});
    const allDT = (dtRes.data || []).reduce((m, dt) => { m[dt.id] = dt; return m; }, {});
    const allIP = (ipRes.data || []).reduce((m, ip) => { m[ip.id] = ip; return m; }, {});

    // Get unique company IDs from products
    const companyIds = [...new Set(products.value.map(p => p.company_id).filter(Boolean))];
    const map = {};
    for (const cid of companyIds) {
      try {
        const res = await api.get(`/admin/companies/${cid}/settings`);
        const c = res.data.company || {};
        map[cid] = {
          name: c.name || '',
          payment_methods: (c.payment_method_ids || [])
            .map(id => allPM[id])
            .filter(Boolean),
          delivery_times: (c.delivery_time_ids || [])
            .map(id => allDT[id])
            .filter(Boolean),
          installment_plans: (c.installment_plan_ids || [])
            .map(id => allIP[id])
            .filter(Boolean),
        };
      } catch {
        map[cid] = { name: '', payment_methods: [], delivery_times: [], installment_plans: [] };
      }
    }
    companySettingsMap.value = map;
  } catch (e) {
    console.warn('fetch company settings failed:', e);
    companySettingsMap.value = {};
  }
};

const filterForm = reactive({
  sortBy: 'price_asc',
  companyFilters: [], // array of company names
  paymentMethodFilters: [], // array of payment method names
  deliveryTimeFilters: [], // array of delivery time names
  installmentPlanFilters: [], // array of installment plan names
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
    treePathFull.value = data.tree_path_full || [];

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

  // Load company settings for badges
  fetchCompanySettings();
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
  const currency = t('scupage.currency', 'EUR');
  const localeMap = { ru: 'ru-RU', en: 'en-US', ua: 'uk-UA', pl: 'pl-PL' };
  const loc = localeMap[locale.value] || 'en-US';
  return new Intl.NumberFormat(loc, { style: 'currency', currency }).format(price);
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

// Extract company name from product name suffix or company settings.
// Priority:
// 1) product.company_name (if provided by API)
// 2) company.name from companySettingsMap
// 3) suffix from product.name like " — Magazilla"
// 4) fallback "Поставщик #id"
const getCompanyName = (product) => {
  if (!product) return '';
  if (product.company_name) return product.company_name;

  try {
    const settings = product.company_id ? companySettingsMap.value[product.company_id] : null;
    if (settings && settings.name) return settings.name;
  } catch {
    // ignore
  }

  const match = product.name?.match(/\s*—\s*([^—]+)$/);
  if (match?.[1]) return match[1].trim();

  return t('product.supplier', { id: product.company_id });
};

// Group products by modification (pure name without company). Each mod has multiple supplier offers.
const modifications = computed(() => {
  if (products.value.length === 0) return [];

  // Apply filters: AND between blocks, OR inside each block
  let filtered = products.value;

  // Company filter (OR inside)
  if (filterForm.companyFilters.length > 0) {
    filtered = filtered.filter(p => filterForm.companyFilters.includes(getCompanyName(p)));
  }

  // Payment method filter (OR inside)
  if (filterForm.paymentMethodFilters.length > 0) {
    filtered = filtered.filter(p => {
      try {
        const settings = p.company_id ? companySettingsMap.value[p.company_id] : null;
        if (!settings || !Array.isArray(settings.payment_methods)) return false;
        const available = settings.payment_methods.map(pm => pm.name);
        return filterForm.paymentMethodFilters.some(f => available.includes(f));
      } catch {
        return false;
      }
    });
  }

  // Delivery time filter (OR inside)
  if (filterForm.deliveryTimeFilters.length > 0) {
    filtered = filtered.filter(p => {
      try {
        const settings = p.company_id ? companySettingsMap.value[p.company_id] : null;
        if (!settings || !Array.isArray(settings.delivery_times)) return false;
        const available = settings.delivery_times.map(dt => dt.name);
        return filterForm.deliveryTimeFilters.some(f => available.includes(f));
      } catch {
        return false;
      }
    });
  }

  // Installment plan filter (OR inside)
  if (filterForm.installmentPlanFilters.length > 0) {
    filtered = filtered.filter(p => {
      try {
        const settings = p.company_id ? companySettingsMap.value[p.company_id] : null;
        if (!settings || !Array.isArray(settings.installment_plans)) return false;
        const available = settings.installment_plans.map(ip => ip.name);
        return filterForm.installmentPlanFilters.some(f => available.includes(f));
      } catch {
        return false;
      }
    });
  }

  const groups = new Map();
  for (const p of filtered) {
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

// Get all unique suppliers across ALL products (used for filter UI, not affected by filters)
const allSuppliers = computed(() => {
  const seen = new Set();
  const result = [];
  for (const p of products.value) {
    const key = getCompanyName(p);
    if (!seen.has(key)) {
      seen.add(key);
      result.push(key);
    }
  }
  return result;
});

// Collect all unique payment methods, delivery times, installment plans from companies on this page
const allPaymentMethods = computed(() => {
  const seen = new Set();
  const result = [];
  for (const p of products.value) {
    if (!p.company_id) continue;
    const settings = companySettingsMap.value[p.company_id];
    if (!settings) continue;
    for (const pm of settings.payment_methods) {
      if (!seen.has(pm.name)) {
        seen.add(pm.name);
        result.push(pm.name);
      }
    }
  }
  return result;
});

const allDeliveryTimes = computed(() => {
  const seen = new Set();
  const result = [];
  for (const p of products.value) {
    if (!p.company_id) continue;
    const settings = companySettingsMap.value[p.company_id];
    if (!settings) continue;
    for (const dt of settings.delivery_times) {
      if (!seen.has(dt.name)) {
        seen.add(dt.name);
        result.push(dt.name);
      }
    }
  }
  return result;
});

const allInstallmentPlans = computed(() => {
  const seen = new Set();
  const result = [];
  for (const p of products.value) {
    if (!p.company_id) continue;
    const settings = companySettingsMap.value[p.company_id];
    if (!settings) continue;
    for (const ip of settings.installment_plans) {
      if (!seen.has(ip.name)) {
        seen.add(ip.name);
        result.push(ip.name);
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

// Основное имя товара (без суффикса компании)
const mainProductName = computed(() => {
  if (modifications.value.length > 0) {
    return modifications.value[0].name;
  }
  return page.value?.title || '';
});

// Количество уникальных компаний
const uniqueCompanyCount = computed(() => {
  return allSuppliers.value.length;
});

// Есть ли хотя бы один товар в наличии
const hasAnyInStock = computed(() => {
  return products.value.some(p => isInStock(p));
});

// Атрибуты для отображения (из selectedProduct, если есть, иначе из page)
const displayAttributes = computed(() => {
  if (selectedProduct.value?.attributes && Object.keys(selectedProduct.value.attributes).length) {
    return selectedProduct.value.attributes;
  }
  return page.value?.attributes || {};
});

// Общее количество офферов (после фильтров)
const filteredOfferCount = computed(() => {
  return modifications.value.reduce((sum, mod) => sum + mod.suppliers.length, 0);
});

// Форма слова для "Где купить (N вариантов)"
const offersPlural = computed(() => {
  const n = filteredOfferCount.value;
  if (locale.value === 'ru') {
    return pluralize(n, 'вариант', 'варианта', 'вариантов');
  }
  if (locale.value === 'ua') {
    return pluralize(n, 'варіант', 'варіанти', 'варіантів');
  }
  if (locale.value === 'pl') {
    return pluralize(n, 'opcja', 'opcje', 'opcji');
  }
  // EN default
  return n === 1 ? 'option' : 'options';
});

// Склонение слов (1 магазин, 2 магазина, 5 магазинов)
const pluralize = (n, one, few, many) => {
  const abs = Math.abs(n) % 100;
  const last = abs % 10;
  if (abs > 10 && abs < 20) return many;
  if (last > 1 && last < 5) return few;
  if (last === 1) return one;
  return many;
};

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
    treePathFull.value = props.data.tree_path_full || [];
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
      treePathFull.value = newData.tree_path_full || [];
      loading.value = false;
      initFromData();
    }
  },
  { deep: true }
);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Error -->
    <div v-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- SCU Page -->
    <div v-else-if="page" class="space-y-6">
      <!-- Верхняя часть: хлебные крошки + заголовок -->
      <div>
        <Breadcrumbs :categories="treePathFull" />

        <div class="mt-3 flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h1 class="text-2xl font-bold text-gray-900 break-words">{{ mainProductName }}</h1>
            <div class="mt-1 flex items-center gap-2 text-sm text-gray-500 flex-wrap">
              <span v-if="page.brand">{{ page.brand }}</span>
              <span v-if="uniqueCompanyCount > 1">
                · {{ uniqueCompanyCount }} {{ pluralize(uniqueCompanyCount, t('scupage.store_one'), t('scupage.store_few'), t('scupage.store_many')) }}
              </span>
              <span v-if="modifications.length > 1">
                · {{ modifications.length }} {{ pluralize(modifications.length, t('scupage.mod_one'), t('scupage.mod_few'), t('scupage.mod_many')) }}
              </span>
            </div>
            <!-- Теги -->
            <div v-if="hasAnyInStock" class="mt-2">
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-700">
                {{ t('scupage.available') }}
              </span>
            </div>
          </div>
          <button
            @click="goBack"
            class="text-sm text-indigo-600 hover:underline whitespace-nowrap cursor-pointer shrink-0"
          >
            {{ t('scupage.to_catalog') }}
          </button>
        </div>
      </div>

      <!-- Верх: фото слева, описание/характеристики/цена справа -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6">

        <!-- Левая колонка (~5 cols): фото -->
        <div class="lg:col-span-5">
          <div class="sticky top-4 space-y-3">
            <div class="bg-gray-100 rounded-2xl overflow-hidden aspect-square">
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
            <!-- Миниатюры -->
            <div v-if="currentImages.length > 1" class="flex gap-2 flex-wrap">
              <img
                v-for="(img, idx) in currentImages"
                :key="idx"
                :src="img"
                @click="currentImageIndex = idx"
                :class="[
                  'w-14 h-14 object-cover rounded-xl border-2 cursor-pointer transition',
                  currentImageIndex === idx
                    ? 'border-indigo-600'
                    : 'border-transparent hover:border-indigo-400'
                ]"
              />
            </div>
          </div>
        </div>

        <!-- Правая колонка (~7 cols): описание, характеристики, цена -->
        <div class="lg:col-span-7 space-y-4">

          <!-- Описание -->
          <div v-if="modifications.length > 0" class="bg-white rounded-2xl shadow-sm border border-gray-100">
            <div class="border-b border-gray-100">
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
            <div class="p-4">
              <template v-if="modifications[activeTab]">
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
                <div class="text-sm text-gray-700">
                  <p class="whitespace-pre-line">
                    {{ modifications[activeTab].suppliers[descSupplierIndex]?.description || t('product.no_description') }}
                  </p>
                </div>
              </template>
            </div>
          </div>

          <!-- Характеристики -->
          <div v-if="displayAttributes && Object.keys(displayAttributes).length" class="bg-white rounded-2xl shadow-sm border border-gray-100 p-4">
            <h3 class="font-semibold text-gray-800 mb-3">{{ t('catalog.characteristics') }}</h3>
            <dl class="space-y-2 text-sm">
              <div
                v-for="(value, key) in displayAttributes"
                :key="key"
                class="flex items-start gap-2 border-b border-gray-50 pb-2 last:border-0 last:pb-0"
              >
                <dt class="text-gray-500 text-xs min-w-[100px] shrink-0">{{ attrLabel(key) }}</dt>
                <dd class="text-gray-800">{{ value }}</dd>
              </div>
            </dl>
          </div>

          <!-- Блок "Лучшая цена" -->
          <div
            v-if="selectedProduct && !selectedProduct.is_virtual"
            class="bg-gradient-to-br from-indigo-900 to-indigo-800 rounded-2xl shadow-sm p-5 text-white"
          >
            <div class="flex items-end justify-between gap-4">
              <div>
                <div class="text-sm text-indigo-200">{{ t('scupage.price') }}</div>
                <div class="text-3xl font-bold mt-0.5">{{ formatPrice(currentPrice) }}</div>
                <div class="text-xs text-indigo-200 mt-1">
                  {{ isInStock(selectedProduct) ? t('scupage.in_stock') : t('scupage.out_of_stock') }}
                </div>
              </div>
              <button
                @click="addToCart"
                :disabled="!isInStock(selectedProduct)"
                class="px-6 py-3 bg-white text-indigo-900 rounded-xl font-semibold text-sm hover:bg-indigo-50 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
              >
                {{ t('catalog.add_to_cart') }}
              </button>
            </div>
          </div>

          <!-- Virtual product notice -->
          <div v-if="selectedProduct?.is_virtual" class="p-3 bg-amber-50 border border-amber-200 rounded-2xl text-sm text-amber-800">
            {{ t('catalog.virtual_product_notice') }}
          </div>
        </div>
      </div>

      <!-- Низ: предложения широко + фильтры узко справа -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6">

        <!-- Где купить (офферы) — широко (~10 cols) -->
        <div v-if="modifications.length > 0" class="lg:col-span-10 bg-white rounded-2xl shadow-sm border border-gray-100 overflow-hidden">
          <div class="px-4 py-3 border-b border-gray-100">
            <h3 class="font-semibold text-gray-800">
              {{ t('scupage.where_to_buy_base') }} ({{ filteredOfferCount }} {{ offersPlural }})
            </h3>
          </div>
          <div class="divide-y divide-gray-50 max-h-[400px] overflow-y-auto">
            <template v-for="(mod, modIdx) in modifications" :key="modIdx">
              <!-- Модификация header -->
              <div v-if="modifications.length > 1" class="px-4 py-2 bg-gray-50 text-xs font-medium text-gray-600">
                {{ mod.name }}
              </div>
              <!-- Офферы -->
              <label
                v-for="product in mod.suppliers"
                :key="product.id"
                :class="[
                  'flex items-center gap-3 px-4 py-3 cursor-pointer transition',
                  selectedProduct?.id === product.id
                    ? 'bg-indigo-50 border-l-4 border-indigo-600 pl-3'
                    : 'hover:bg-gray-50 border-l-4 border-transparent'
                ]"
              >
                <input
                  type="radio"
                  name="scu-product"
                  :value="product.id"
                  :checked="selectedProduct?.id === product.id"
                  @change="selectProduct(product)"
                  class="w-4 h-4 text-indigo-600 border-gray-300 focus:ring-indigo-500 flex-shrink-0 cursor-pointer"
                />
                <div class="flex-1 min-w-0">
                  <div class="text-xs font-medium text-gray-800">
                    {{ getCompanyName(product) }}
                  </div>
                  <span :class="isInStock(product) ? 'text-green-600' : 'text-red-600'" class="text-xs">
                    {{ isInStock(product) ? t('catalog.in_stock') : t('catalog.out_of_stock') }}
                  </span>
                </div>
                <div class="font-semibold text-indigo-600 whitespace-nowrap text-sm">
                  {{ formatPrice(product.price) }}
                </div>
              </label>
            </template>
          </div>
        </div>

        <!-- Фильтры — узко (~2 cols) -->
        <div v-if="allSuppliers.length >= 1" class="lg:col-span-2 bg-white rounded-2xl shadow-sm border border-gray-100 p-3 space-y-3 text-xs">
          <!-- Компании -->
          <div>
            <div class="font-semibold text-gray-700 mb-1">{{ t('scupage_filter_by_company') || 'Компании' }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="company in allSuppliers" :key="company" class="inline-flex items-center gap-1.5 cursor-pointer text-gray-700">
                <input type="checkbox" :value="company" v-model="filterForm.companyFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ company }}
              </label>
            </div>
          </div>

          <div v-if="allPaymentMethods.length > 0" class="border-t border-gray-100 pt-2">
            <div class="font-semibold text-gray-700 mb-1">{{ t('scupage_filter_by_payment') || 'Оплата' }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="pm in allPaymentMethods" :key="pm" class="inline-flex items-center gap-1.5 cursor-pointer text-gray-700">
                <input type="checkbox" :value="pm" v-model="filterForm.paymentMethodFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ pm }}
              </label>
            </div>
          </div>

          <div v-if="allDeliveryTimes.length > 0" class="border-t border-gray-100 pt-2">
            <div class="font-semibold text-gray-700 mb-1">{{ t('scupage_filter_by_delivery') || 'Доставка' }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="dt in allDeliveryTimes" :key="dt" class="inline-flex items-center gap-1.5 cursor-pointer text-gray-700">
                <input type="checkbox" :value="dt" v-model="filterForm.deliveryTimeFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ dt }}
              </label>
            </div>
          </div>

          <div v-if="allInstallmentPlans.length > 0" class="border-t border-gray-100 pt-2">
            <div class="font-semibold text-gray-700 mb-1">{{ t('scupage_filter_by_installment') || 'Рассрочка' }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="ip in allInstallmentPlans" :key="ip" class="inline-flex items-center gap-1.5 cursor-pointer text-gray-700">
                <input type="checkbox" :value="ip" v-model="filterForm.installmentPlanFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ ip }}
              </label>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
