<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useCartStore } from '../stores/cart';
import { useToast } from '../composables/useToast';
import { useSeo } from '../composables/useSeo';
import Breadcrumbs from '../components/Breadcrumbs.vue';
import PriceSparkline from '../components/PriceSparkline.vue';

const { toast } = useToast();

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
const category = ref(null);
const subcategories = ref([]);
const products = ref([]);
const treePath = ref([]);
const treePathFull = ref([]);
const loading = ref(true);
const error = ref(null);


useSeo({
  title: computed(() => (page.value?.title ? `${page.value.title} — MakoShop` : t('pages.default_title'))),
  description: computed(() => page.value?.description || t('pages.default_description')),
  image: computed(() => page.value?.images?.[0] || null),
});

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
  if (props.data) {
    // Already initialized from props
    return;
  }
  loading.value = true;
  error.value = null;
  try {
    // Use the full route path: /shop/{tree}/{slug}
    const response = await api.get(route.path);
    const data = response.data;
    // console.log('SCUPageView fetched data:', data);
    page.value = data.scu_page;
    category.value = data.category || (data.scu_page ? data.scu_page.category : null);
    subcategories.value = data.subcategories || [];
    products.value = data.products || [];
    treePath.value = data.tree_path || [];
    treePathFull.value = data.tree_path_full || [];

    initFromData();
  } catch (e) {
    error.value = e.response?.data?.error?.message || t('scupage.not_found');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

function initFromData() {
  // Page title / meta are handled reactively by useSeo()

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
      currency: page.value.currency || 'EUR',
      company_name: '',
      stock_qty: page.value.product_count || 0,
      status: page.value.product_count > 0 ? 'active' : 'inactive',
      images: page.value.images || [],
      is_virtual: true,
    };
  }

  // Load company settings for badges
  // fetchCompanySettings(); // REMOVED: This causes a second request that overwrites data if not careful
};

const addToCart = async () => {
  if (!selectedProduct.value) return;
  try {
    await cart.addItem(selectedProduct.value.id, 1);
    toast.success(t('cart.added_to_cart'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('cart.add_to_cart_error'));
  }
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
// Example: "Rastar BMW I8 1:24 Silver — Magazilla" → "Rastar BMW I8 1:24 Silver"
const stripCompanyFromName = (name) => {
  if (!name) return '';
  // Remove " — CompanyName" suffix (Magazilla, DNS, Citilink, etc.)
  return name.replace(/\s*—\s*[^—]+$/, '').trim() || name;
};

// Extract company name from product name suffix or company settings.
// Priority:
// 1) product.company_name (if provided by API)
// 2) company.name from companySettingsMap
// 3) suffix from product.name like " — Magazilla"
// 4) fallback "Supplier #id"
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
    const key = pureName || p.sku || t('scupage.no_name');
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

// Base product name (without company suffix)
const mainProductName = computed(() => {
  if (modifications.value.length > 0) {
    return modifications.value[0].name;
  }
  return page.value?.title || '';
});

// Number of unique companies
const uniqueCompanyCount = computed(() => {
  return allSuppliers.value.length;
});

// Total count of offers across all modifications (after filtering)
const filteredOfferCount = computed(() => {
  return modifications.value.reduce((acc, mod) => acc + mod.suppliers.length, 0);
});

// Whether at least one product is in stock
const hasAnyInStock = computed(() => {
  return products.value.some(p => isInStock(p));
});

// Convert []KeyValue to {key: value} map.
const normalizeAttrs = (attrs) => {
  if (!attrs) return {};
  if (Array.isArray(attrs)) {
    const out = {};
    for (const kv of attrs) {
      if (kv.key && kv.value != null) {
        out[kv.key] = kv.value;
      }
    }
    return out;
  }
  return attrs;
};

// Attributes to display (from selectedProduct if present, otherwise from page)
const displayAttributes = computed(() => {
  if (selectedProduct.value?.attributes) {
    const m = normalizeAttrs(selectedProduct.value.attributes);
    if (Object.keys(m).length) return m;
  }
  if (page.value?.attributes) {
    return normalizeAttrs(page.value.attributes);
  }
  return {};
});

// Get localized category name
const catName = (cat) => {
  if (!cat) return '';
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || '';
};

// Get localized description for category
const catDescription = (cat) => {
  if (!cat) return '';
  const langField = `description_${locale.value}`;
  return cat[langField] || cat.description || '';
};

const isValidImage = (url) => {
  if (!url) return false;
  return !url.includes('cdn.makoshop.com');
};

const navigateToCategory = (sub) => {
  if (!sub.slug) return;
  // If we have treePath for current category, prepend it
  if (treePath.value.length > 0) {
    router.push({ path: '/shop/' + treePath.value.join('/') + '/' + sub.slug });
  } else {
    router.push({ path: '/shop/' + sub.slug });
  }
};

// Pluralized word form for "Where to buy (N options)"
const offersPlural = computed(() => {
  const n = filteredOfferCount.value;
  return pluralize(n, 'scupage.variant_one', 'scupage.variant_few', 'scupage.variant_many');
});

// Pluralization helper using locale-specific i18n keys
const pluralize = (n, oneKey, fewKey, manyKey) => {
  const abs = Math.abs(n) % 100;
  const lastDigit = abs % 10;
  if (abs > 10 && abs < 20) return t(manyKey);
  if (lastDigit === 1) return t(oneKey);
  if (lastDigit >= 2 && lastDigit <= 4) return t(fewKey);
  return t(manyKey);
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

// Initialize from props.data if available
if (props.data) {
  page.value = props.data.scu_page;
  products.value = props.data.products || [];
  category.value = props.data.category;
  subcategories.value = props.data.subcategories || [];
  treePath.value = props.data.tree_path || [];
  treePathFull.value = props.data.tree_path_full || [];
  loading.value = false;
  initFromData();
}

onMounted(() => {
  // Otherwise fetch from API
  if (!props.data) {
    fetchSCUPage();
  }
});

// Watch for props.data changes (when rendered from CatalogView)
watch(
  () => props.data,
  (newData) => {
    if (newData) {
      page.value = newData.scu_page;
      category.value = newData.category || (newData.scu_page ? newData.scu_page.category : null);
      subcategories.value = newData.subcategories || [];
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
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Error -->
    <div v-if="error" class="p-4 bg-red-50 text-red-700 rounded-lg theme-dark:bg-red-900/30 theme-dark:text-red-300">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-else-if="loading" class="grid grid-cols-1 lg:grid-cols-12 gap-6" aria-hidden="true">
      <div class="lg:col-span-5">
        <div class="bg-surface-3 rounded-2xl aspect-square animate-pulse"></div>
        <div class="flex gap-2 mt-2">
          <div v-for="i in 4" :key="i" class="w-14 h-14 bg-surface-3 rounded-xl animate-pulse"></div>
        </div>
      </div>
      <div class="lg:col-span-7 space-y-4">
        <div class="h-8 w-2/3 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-4 w-1/3 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-40 bg-surface-3 rounded-2xl animate-pulse"></div>
        <div class="h-24 bg-surface-3 rounded-2xl animate-pulse"></div>
      </div>
    </div>

    <!-- SCU Page -->
    <div v-else-if="page" class="space-y-6">
      <!-- Top section: breadcrumbs + category info -->
      <div>
        <Breadcrumbs :categories="treePathFull" />

        <div class="mt-4 flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h1 class="text-2xl font-bold text-ink break-words">{{ mainProductName }}</h1>
            <div class="mt-1 flex items-center gap-2 text-sm text-ink-3 flex-wrap">
              <span v-if="page.brand">{{ page.brand }}</span>
              <span v-if="uniqueCompanyCount > 1">
                · {{ uniqueCompanyCount }} {{ pluralize(uniqueCompanyCount, 'scupage.store_one', 'scupage.store_few', 'scupage.store_many') }}
              </span>
              <span v-if="modifications.length > 1">
                · {{ modifications.length }} {{ pluralize(modifications.length, 'scupage.mod_one', 'scupage.mod_few', 'scupage.mod_many') }}
              </span>
            </div>
            <!-- Tags -->
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

      <!-- Top: photo left, description/specs/price right -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6">

        <!-- Left column (~5 cols): photo -->
        <div class="lg:col-span-5">
          <div class="sticky top-4 space-y-3">
            <div class="bg-surface-2 rounded-2xl overflow-hidden aspect-square">
              <img
                v-if="currentImages.length"
                :src="currentImages[currentImageIndex]"
                :alt="page.title"
                loading="lazy"
                decoding="async"
                class="w-full h-full object-cover"
              />
              <div v-else class="w-full h-full flex items-center justify-center text-ink-3">
                {{ t('common.no_photo') }}
              </div>
            </div>
            <!-- Thumbnails -->
            <div v-if="currentImages.length > 1" class="flex gap-2 flex-wrap">
              <img
                v-for="(img, idx) in currentImages"
                :key="idx"
                :src="img"
                loading="lazy"
                decoding="async"
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

        <!-- Right column (~7 cols): description, specs, price -->
        <div class="lg:col-span-7 space-y-4">

          <!-- Description -->
          <div v-if="modifications.length > 0" class="bg-surface rounded-2xl shadow-sm border border-line">
            <div class="border-b border-line">
              <div class="flex gap-1 px-4 pt-2 overflow-x-auto">
                <button
                  v-for="(mod, idx) in modifications"
                  :key="idx"
                  @click="activeTab = idx; descSupplierIndex = 0"
                  :class="[
                    'px-3 py-1.5 text-xs rounded-t-lg whitespace-nowrap transition',
                    activeTab === idx
                      ? 'bg-indigo-600 text-white'
                      : 'bg-surface-2 text-ink-2 hover:bg-surface-3'
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
                      class="px-2 py-1 text-xs border border-line rounded hover:bg-surface-2"
                    >←</button>
                    <span class="px-2 py-1 text-xs">
                      {{ descSupplierIndex + 1 }} / {{ modifications[activeTab].suppliers.length }}
                    </span>
                    <button
                      @click="descSupplierIndex = (descSupplierIndex + 1) % modifications[activeTab].suppliers.length"
                      class="px-2 py-1 text-xs border border-line rounded hover:bg-surface-2"
                    >→</button>
                  </div>
                </div>
                <div class="text-sm text-ink-2">
                  <p class="whitespace-pre-line">
                    {{ modifications[activeTab].suppliers[descSupplierIndex]?.description || t('product.no_description') }}
                  </p>
                </div>
              </template>
            </div>
          </div>

          <!-- Specifications -->
          <div v-if="displayAttributes && Object.keys(displayAttributes).length" class="bg-surface rounded-2xl shadow-sm border border-line p-4">
            <h3 class="font-semibold text-ink mb-3">{{ t('catalog.characteristics') }}</h3>
            <dl class="space-y-2 text-sm">
              <div
                v-for="(value, key) in displayAttributes"
                :key="key"
                class="flex items-start gap-2 border-b border-line pb-2 last:border-0 last:pb-0"
              >
                <dt class="text-ink-3 text-xs min-w-[100px] shrink-0">{{ attrLabel(key) }}</dt>
                <dd class="text-ink">{{ value }}</dd>
              </div>
            </dl>
          </div>

          <!-- "Best price" block -->
          <div
            v-if="selectedProduct && !selectedProduct.is_virtual"
            class="bg-gradient-to-br from-indigo-900 to-indigo-800 rounded-2xl shadow-sm p-5 text-white"
          >
            <div class="flex items-end justify-between gap-4">
              <div>
                <div class="text-sm text-indigo-200">{{ t('scupage.best_price') }}</div>
                <div class="text-3xl font-bold mt-0.5">{{ formatPrice(currentPrice) }}</div>
                <div class="text-xs text-indigo-200 mt-1">
                  {{ isInStock(selectedProduct) ? t('scupage.in_stock') : t('scupage.out_of_stock') }}
                </div>
                <!-- Mini price trend across offers -->
                <div v-if="modifications.length > 0" class="mt-2 text-indigo-200">
                  <PriceSparkline
                    :values="modifications.map(m => m.suppliers[0]?.price).filter(p => Number.isFinite(p))"
                    :width="120"
                    :height="28"
                  />
                </div>
              </div>
              <button
                @click="addToCart"
                :disabled="!isInStock(selectedProduct)"
                class="px-6 py-3 bg-surface text-indigo-900 dark:text-indigo-300 rounded-xl font-semibold text-sm hover:bg-indigo-50 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
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

      <!-- Bottom: offers wide + filters narrow on the right -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6">

        <!-- Where to buy (offers) — wide (~10 cols) -->
        <div v-if="modifications.length > 0" class="lg:col-span-10 bg-surface rounded-2xl shadow-sm border border-line overflow-hidden">
          <div class="px-4 py-3 border-b border-line">
            <h3 class="font-semibold text-ink">
              {{ t('scupage.where_to_buy_base') }} ({{ filteredOfferCount }} {{ offersPlural }})
            </h3>
          </div>
          <fieldset class="m-0 p-0 border-0 min-w-0">
            <legend class="sr-only">{{ t('scupage.where_to_buy_base') }}</legend>
            <div class="divide-y divide-line max-h-[400px] overflow-y-auto">
            <template v-for="(mod, modIdx) in modifications" :key="modIdx">
              <!-- Modification header -->
              <div v-if="modifications.length > 1" class="px-4 py-2 bg-surface-2 text-xs font-medium text-ink-2">
                {{ mod.name }}
              </div>
              <!-- Offers -->
              <label
                v-for="product in mod.suppliers"
                :key="product.id"
                :class="[
                  'flex items-start gap-4 px-4 py-3 cursor-pointer transition',
                  selectedProduct?.id === product.id
                    ? 'bg-indigo-50 border-l-4 border-indigo-600 pl-3'
                    : 'hover:bg-surface-2 border-l-4 border-transparent'
                ]"
              >
                <input
                  type="radio"
                  name="scu-product"
                  :value="product.id"
                  :checked="selectedProduct?.id === product.id"
                  @change="selectProduct(product)"
                  class="w-4 h-4 text-indigo-600 border-line focus:ring-indigo-500 flex-shrink-0 cursor-pointer mt-1"
                />
                <!-- Left: seller info -->
                <div class="flex-shrink-0 w-36">
                  <div class="text-xs font-medium text-ink">
                    {{ getCompanyName(product) }}
                  </div>
                  <span :class="isInStock(product) ? 'text-green-600' : 'text-red-600'" class="text-xs">
                    {{ isInStock(product) ? t('catalog.in_stock') : t('catalog.out_of_stock') }}
                  </span>
                </div>
                <!-- Middle: attributes -->
                <div class="flex-shrink-0 w-32 text-xs text-ink-3">
                  <template v-if="product.attributes && product.attributes.length">
                    <div v-for="attr in product.attributes.slice(0, 3)" :key="attr.key" class="truncate">
                      {{ attr.value }}
                    </div>
                  </template>
                </div>
                <!-- Right: description (wide, multi-line) -->
                <div v-if="product.description" class="flex-1 min-w-0 text-xs text-ink-3 leading-relaxed line-clamp-2 break-words">
                  {{ product.description }}
                </div>
                <!-- Far right: price -->
                <div class="font-semibold text-indigo-600 whitespace-nowrap text-sm flex-shrink-0 mt-1">
                  {{ formatPrice(product.price) }}
                </div>
              </label>
            </template>
            </div>
          </fieldset>
        </div>

        <!-- Filters — narrow (~2 cols) -->
        <div v-if="allSuppliers.length >= 1" class="lg:col-span-2 bg-surface rounded-2xl shadow-sm border border-line p-3 space-y-3 text-xs">
          <!-- Companies -->
          <div>
            <div class="font-semibold text-ink-2 mb-1">{{ t('scupage.filter_by_company') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="company in allSuppliers" :key="company" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="company" v-model="filterForm.companyFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ company }}
              </label>
            </div>
          </div>

          <div v-if="allPaymentMethods.length > 0" class="border-t border-line pt-2">
            <div class="font-semibold text-ink-2 mb-1">{{ t('scupage.filter_by_payment') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="pm in allPaymentMethods" :key="pm" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="pm" v-model="filterForm.paymentMethodFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ pm }}
              </label>
            </div>
          </div>

          <div v-if="allDeliveryTimes.length > 0" class="border-t border-line pt-2">
            <div class="font-semibold text-ink-2 mb-1">{{ t('scupage.filter_by_delivery') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="dt in allDeliveryTimes" :key="dt" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="dt" v-model="filterForm.deliveryTimeFilters" class="rounded text-indigo-600 focus:ring-indigo-500" />
                {{ dt }}
              </label>
            </div>
          </div>

          <div v-if="allInstallmentPlans.length > 0" class="border-t border-line pt-2">
            <div class="font-semibold text-ink-2 mb-1">{{ t('scupage.filter_by_installment') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="ip in allInstallmentPlans" :key="ip" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
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
