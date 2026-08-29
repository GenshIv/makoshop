<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useToast } from '../composables/useToast';
import { useSeo } from '../composables/useSeo';
import { useSettings } from '../composables/useSettings';

const { defaultCurrency } = useSettings();
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

// Sanitize HTML description for safe rendering with v-html
function sanitizeHtml(html) {
  if (!html) return '';
  
  // Create a temporary element to parse and clean HTML
  try {
    const temp = document.createElement('div');
    temp.innerHTML = html;
    
    // Remove script tags
    temp.querySelectorAll('script').forEach(el => el.remove());
    
    // Remove style tags
    temp.querySelectorAll('style').forEach(el => el.remove());
    
    // Remove event handlers from all elements
    temp.querySelectorAll('[on*]').forEach(el => {
      Array.from(el.attributes).forEach(attr => {
        if (attr.name.startsWith('on')) {
          el.removeAttribute(attr.name);
        }
      });
    });
    
    // Remove javascript: URLs
    temp.querySelectorAll('[href^="javascript:"], [src^="javascript:"]').forEach(el => {
      el.removeAttribute('href');
      el.removeAttribute('src');
    });
    
    // Replace <pre> tags with <div> to allow text wrapping
    temp.querySelectorAll('pre').forEach(pre => {
      const div = document.createElement('div');
      div.className = 'whitespace-pre-wrap break-words';
      div.textContent = pre.textContent;
      pre.replaceWith(div);
    });
    
    return temp.innerHTML;
  } catch (e) {
    // Fallback: simple regex-based sanitization
    let clean = html;
    clean = clean.replace(/<script[\s\S]*?<\/script>/gi, '');
    clean = clean.replace(/<style[\s\S]*?<\/style>/gi, '');
    clean = clean.replace(/\s+on\w+="[^"]*"/gi, '');
    clean = clean.replace(/\s+on\w+='[^']*'/gi, '');
    clean = clean.replace(/\s+on\w+=\w+/gi, '');
    clean = clean.replace(/href\s*=\s*["']javascript:[^"']*["']/gi, '');
    clean = clean.replace(/src\s*=\s*["']javascript:[^"']*["']/gi, '');
    clean = clean.replace(/<pre>/gi, '<div class="whitespace-pre-wrap break-words">');
    clean = clean.replace(/<\/pre>/gi, '</div>');
    return clean;
  }
}



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
  title: computed(() => (page.value?.title ? `${page.value.title} — wszyst.pl` : t('pages.default_title'))),
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

const fetchEANPage = async () => {
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
    // console.log('EANPageView fetched data:', data);
    page.value = data.ean_page;
    category.value = data.category || (data.ean_page ? data.ean_page.category : null);
    subcategories.value = data.subcategories || [];
    products.value = data.products || [];
    treePath.value = data.tree_path || [];
    treePathFull.value = data.tree_path_full || [];

    await initFromData();
  } catch (e) {
    error.value = e.response?.data?.error?.message || t('eanpage.not_found');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

async function initFromData() {
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
  await fetchCompanySettings();
};

// Get the partner purchase URL from a product's purchase_url field
// Falls back to attributes for legacy data
const getPurchaseUrl = (product) => {
  if (!product) return '';
  // New format: separate field
  if (product.purchase_url) return product.purchase_url;
  // Legacy format: in attributes
  if (product.attributes && Array.isArray(product.attributes)) {
    const attr = product.attributes.find(a => a.key === 'purchase_url');
    if (attr?.value) return attr.value;
  }
  return '';
};

// Open the partner purchase link in a new tab
const goToPurchase = () => {
  const url = getPurchaseUrl(selectedProduct.value);
  if (url) {
    window.open(url, '_blank', 'noopener,noreferrer');
  }
};

const isInStock = (product) => {
  return product.status === 'active' && (product.stock_qty || 0) > 0;
};

const formatPrice = (price, currency) => {
  const cur = currency || defaultCurrency.value || 'PLN';
  const localeMap = { ru: 'ru-RU', en: 'en-US', ua: 'uk-UA', pl: 'pl-PL' };
  const loc = localeMap[locale.value] || 'en-US';
  return new Intl.NumberFormat(loc, { style: 'currency', currency: cur }).format(price);
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
    const key = pureName || p.sku || t('eanpage.no_name');
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

const currentCurrency = computed(() => {
  return selectedProduct.value?.currency || 'EUR';
});

// Previous (old) price for discount display: shown when higher than current price.
const previousPrice = computed(() => {
  const pp = Number(selectedProduct.value?.previous_price);
  const cur = Number(currentPrice.value);
  if (Number.isFinite(pp) && Number.isFinite(cur) && pp > cur) {
    return pp;
  }
  return null;
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
const INTERNAL_ATTRS = ['product_url', 'purchase_url', 'shop_category'];

const normalizeAttrs = (attrs) => {
  if (!attrs) return {};
  if (Array.isArray(attrs)) {
    const out = {};
    for (const kv of attrs) {
      if (kv.key && kv.value != null && !INTERNAL_ATTRS.includes(kv.key)) {
        out[kv.key] = kv.value;
      }
    }
    return out;
  }
  // Filter internal attrs from object format too
  const filtered = {};
  for (const [key, value] of Object.entries(attrs)) {
    if (!INTERNAL_ATTRS.includes(key)) {
      filtered[key] = value;
    }
  }
  return filtered;
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
  return pluralize(n, 'eanpage.variant_one', 'eanpage.variant_few', 'eanpage.variant_many');
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
  page.value = props.data.ean_page;
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
    fetchEANPage();
  }
});

// Watch for props.data changes (when rendered from CatalogView)
watch(
  () => props.data,
  (newData) => {
    if (newData) {
      page.value = newData.ean_page;
      category.value = newData.category || (newData.ean_page ? newData.ean_page.category : null);
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

    <!-- EAN Page -->
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
                · {{ uniqueCompanyCount }} {{ pluralize(uniqueCompanyCount, 'eanpage.store_one', 'eanpage.store_few', 'eanpage.store_many') }}
              </span>
              <span v-if="modifications.length > 1">
                · {{ modifications.length }} {{ pluralize(modifications.length, 'eanpage.mod_one', 'eanpage.mod_few', 'eanpage.mod_many') }}
              </span>
            </div>
            <!-- Tags -->
            <div v-if="hasAnyInStock" class="mt-2">
              <span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-700">
                {{ t('eanpage.available') }}
              </span>
            </div>
          </div>
          <button
            @click="goBack"
            class="link-btn text-sm text-orange-600 whitespace-nowrap cursor-pointer shrink-0"
          >
            {{ t('eanpage.to_catalog') }}
          </button>
        </div>
      </div>

      <!-- Top: photo left, description/specs/price right -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6">

        <!-- Left column (~5 cols): photo -->
        <div class="lg:col-span-5">
          <div class="sticky top-4 space-y-3">
            <div class="bg-white rounded-2xl overflow-hidden aspect-square">
              <img
                v-if="currentImages.length"
                :src="currentImages[currentImageIndex]"
                :alt="page.title"
                loading="lazy"
                decoding="async"
                class="w-full h-full object-contain"
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
                    ? 'border-orange-600'
                    : 'border-transparent hover:border-orange-400'
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
                      ? 'bg-orange-600 text-white'
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
                  <span class="inline-block px-2 py-0.5 bg-orange-100 text-orange-700 text-xs rounded">
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
                <div class="text-sm text-ink-2 prose prose-sm max-w-none max-h-[600px] overflow-y-auto [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-track]:bg-surface-2 [&::-webkit-scrollbar-thumb]:bg-line [&::-webkit-scrollbar-thumb]:rounded-full hover:[&::-webkit-scrollbar-thumb]:bg-orange-400">
                  <div v-if="modifications[activeTab].suppliers[descSupplierIndex]?.description && modifications[activeTab].suppliers[descSupplierIndex].description !== '<pre></pre>'"
                       v-html="sanitizeHtml(modifications[activeTab].suppliers[descSupplierIndex].description)"
                       class="prose-ul:list-disc prose-ul:pl-5 prose-ol:list-decimal prose-ol:pl-5 prose-p:my-1 prose-li:my-0.5 prose-strong:font-semibold prose-ul:my-2 prose-ol:my-2">
                  </div>
                  <div v-else-if="page.description"
                       class="whitespace-pre-line text-sm">
                    {{ page.description }}
                  </div>
                  <p v-else class="text-ink-3">{{ t('product.no_description') }}</p>
                </div>
              </template>
            </div>
          </div>

          <!-- Specifications -->
          <div v-if="displayAttributes && Object.keys(displayAttributes).length" class="bg-surface rounded-2xl shadow-sm border border-line p-4">
            <h3 class="font-semibold text-ink mb-3">{{ t('catalog.characteristics') }}</h3>
            <dl class="space-y-2 text-sm max-h-[300px] overflow-y-auto [&::-webkit-scrollbar]:w-1.5 [&::-webkit-scrollbar-track]:bg-surface-2 [&::-webkit-scrollbar-thumb]:bg-line [&::-webkit-scrollbar-thumb]:rounded-full hover:[&::-webkit-scrollbar-thumb]:bg-orange-400">
              <div
                v-for="(value, key) in displayAttributes"
                :key="key"
                class="flex items-start gap-2 border-b border-line pb-2 last:border-0 last:pb-0"
              >
                <dt class="text-ink-3 text-xs w-[180px] shrink-0 truncate" :title="attrLabel(key)">{{ attrLabel(key) }}</dt>
                <dd class="text-ink flex-1">{{ value }}</dd>
              </div>
            </dl>
          </div>

          <!-- "Best price" block -->
          <div
            v-if="selectedProduct && !selectedProduct.is_virtual"
            class="bg-gradient-to-br from-orange-50 to-amber-100 dark:from-orange-950/50 dark:to-orange-900/30 border border-orange-200 dark:border-orange-800/60 rounded-2xl shadow-sm p-5 text-orange-900 dark:text-orange-50"
          >
            <div class="flex items-end justify-between gap-4">
              <div>
                <div class="text-sm font-medium text-orange-700 dark:text-orange-300">{{ t('eanpage.best_price') }}</div>
                <div class="flex items-baseline gap-2 mt-0.5 flex-wrap">
                  <span v-if="previousPrice" class="text-sm text-orange-700/70 dark:text-orange-300/70 line-through">
                    {{ formatPrice(previousPrice, currentCurrency) }}
                  </span>
                  <span class="text-3xl font-bold text-orange-900 dark:text-white">{{ formatPrice(currentPrice, currentCurrency) }}</span>
                </div>
                <div class="text-xs text-orange-700 dark:text-orange-300 mt-1">
                  {{ isInStock(selectedProduct) ? t('eanpage.in_stock') : t('eanpage.out_of_stock') }}
                </div>
                <!-- Mini price trend across offers -->
                <div v-if="modifications.length > 0" class="mt-2 text-orange-600 dark:text-orange-400">
                  <PriceSparkline
                    :values="modifications.map(m => m.suppliers[0]?.price).filter(p => Number.isFinite(p))"
                    :width="120"
                    :height="28"
                  />
                </div>
              </div>
              <button
                @click="goToPurchase"
                :disabled="!getPurchaseUrl(selectedProduct)"
                class="px-6 py-3 bg-orange-600 text-white rounded-xl font-semibold text-sm hover:bg-orange-700 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
              >
                {{ t('catalog.go_to_purchase') }}
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
              {{ t('eanpage.where_to_buy_base') }} ({{ filteredOfferCount }} {{ offersPlural }})
            </h3>
          </div>
          <fieldset class="m-0 p-0 border-0 min-w-0">
            <legend class="sr-only">{{ t('eanpage.where_to_buy_base') }}</legend>
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
                    ? 'bg-orange-50 border-l-4 border-orange-600 pl-3'
                    : 'hover:bg-surface-2 border-l-4 border-transparent'
                ]"
              >
                <input
                  type="radio"
                  name="ean-product"
                  :value="product.id"
                  :checked="selectedProduct?.id === product.id"
                  @change="selectProduct(product)"
                  class="w-4 h-4 text-orange-600 border-line focus:ring-orange-500 flex-shrink-0 cursor-pointer mt-1"
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
                <!-- Middle: attributes in key-value columns -->
                <div class="flex-shrink-0 w-48 text-xs text-ink-3">
                  <template v-if="product.attributes && product.attributes.length">
                    <div class="grid grid-cols-2 gap-x-2 gap-y-0.5">
                      <template v-for="attr in product.attributes.filter(a => !INTERNAL_ATTRS.includes(a.key) && !a.key.toLowerCase().includes('url') && !a.value.toLowerCase().startsWith('http')).slice(0, 6)" :key="attr.key">
                        <div class="truncate font-medium text-ink-2">{{ attr.key }}:</div>
                        <div class="truncate">{{ attr.value }}</div>
                      </template>
                    </div>
                  </template>
                </div>
                <!-- Right: description (wide, multi-line) -->
                <div v-if="product.description" class="flex-1 min-w-0 text-xs text-ink-3 leading-relaxed break-words">
                  <div v-html="sanitizeHtml(product.description)" class="[&>ul]:list-disc [&>ul]:pl-4 [&>ol]:list-decimal [&>ol]:pl-4 [&>p]:my-0.5 [&>li]:my-0 [&>div]:my-0 [&>span]:my-0 [&>strong]:font-semibold"></div>
                </div>
                <!-- Far right: price -->
                <div class="font-semibold text-orange-600 whitespace-nowrap text-sm flex-shrink-0 mt-1">
                  {{ formatPrice(product.price, product.currency) }}
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
            <div class="font-semibold text-ink-2 mb-1">{{ t('eanpage.filter_by_company') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="company in allSuppliers" :key="company" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="company" v-model="filterForm.companyFilters" class="rounded text-orange-600 focus:ring-orange-500" />
                {{ company }}
              </label>
            </div>
          </div>

          <div v-if="allPaymentMethods.length > 0" class="border-t border-line pt-2">
            <div class="font-semibold text-ink-2 mb-1">{{ t('eanpage.filter_by_payment') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="pm in allPaymentMethods" :key="pm" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="pm" v-model="filterForm.paymentMethodFilters" class="rounded text-orange-600 focus:ring-orange-500" />
                {{ pm }}
              </label>
            </div>
          </div>

          <div v-if="allDeliveryTimes.length > 0" class="border-t border-line pt-2">
            <div class="font-semibold text-ink-2 mb-1">{{ t('eanpage.filter_by_delivery') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="dt in allDeliveryTimes" :key="dt" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="dt" v-model="filterForm.deliveryTimeFilters" class="rounded text-orange-600 focus:ring-orange-500" />
                {{ dt }}
              </label>
            </div>
          </div>

          <div v-if="allInstallmentPlans.length > 0" class="border-t border-line pt-2">
            <div class="font-semibold text-ink-2 mb-1">{{ t('eanpage.filter_by_installment') }}</div>
            <div class="flex flex-col gap-1">
              <label v-for="ip in allInstallmentPlans" :key="ip" class="inline-flex items-center gap-1.5 cursor-pointer text-ink-2">
                <input type="checkbox" :value="ip" v-model="filterForm.installmentPlanFilters" class="rounded text-orange-600 focus:ring-orange-500" />
                {{ ip }}
              </label>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
