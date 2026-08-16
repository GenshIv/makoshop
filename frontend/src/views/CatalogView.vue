<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, watch, computed, defineAsyncComponent, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';

// Lazy-load SCUPageView to avoid circular imports
const SCUPageView = defineAsyncComponent(() => import('../views/SCUPageView.vue'));

const route = useRoute();
const router = useRouter();
const { t, locale } = useI18n();

const products = ref([]);
const categoryAttrs = ref([]);
const categoryPath = ref([]); // [{id, name}, ...]
const pagination = reactive({ page: 1, per_page: 50, total: 0, total_pages: 0 });

// Root categories for horizontal bar above products
const rootCategories = ref([]);
const rootCatsLoading = ref(false);

// Current browsing path inside categories bar: [{id, slug, name, description, ...}, ...]
const categoryBrowsePath = ref([]);

// Current category object from API (with description)
const currentCategory = ref(null);

const loading = ref(false);
const error = ref(null);
const maintenanceMode = ref(false);
const lastSearchMs = ref(null); // time in ms of last catalog request
const showMobileFilters = ref(false); // mobile filters panel

// If API returns SCUPage data, render SCUPageView instead
const scuPageData = ref(null);

// Current category path for tree/breadcrumbs (without SCUPage slug)
const currentCategoryPath = computed(() => {
  // If showing SCUPage, use its tree_path
  if (scuPageData.value && scuPageData.value.tree_path) {
    return scuPageData.value.tree_path;
  }
  // Otherwise derive from route path
  if (route.path.startsWith('/shop/')) {
    return route.path.slice(6).split('/').filter(Boolean);
  }
  return [];
});

// Adjust per_page based on screen width
const getPerPageForScreen = () => {
  if (window.innerWidth >= 1536) return 60; // xl: 6 columns
  if (window.innerWidth >= 1024) return 50; // lg: 5 columns
  if (window.innerWidth >= 768) return 30;  // md: 3-4 columns
  return 20; // sm: 2 columns
};

const updatePerPage = () => {
  const newPerPage = getPerPageForScreen();
  if (pagination.per_page !== newPerPage) {
    pagination.per_page = newPerPage;
    // Do not reset page if it is set in the URL
    if (!route.query.page) {
      pagination.page = 1;
    }
    fetchProducts();
  }
};

const filters = reactive({
  q: '',
  price_min: '',
  price_max: '',
  sort: 'relevance',
});

const attrFilters = reactive({});

const buildQueryParams = () => {
  const params = {};
  if (filters.q) params.q = filters.q;
  if (filters.price_min) params.price_min = filters.price_min;
  if (filters.price_max) params.price_max = filters.price_max;
  if (filters.sort && filters.sort !== 'relevance') params.sort = filters.sort;

  for (const [key, value] of Object.entries(attrFilters)) {
    if (Array.isArray(value) && value.length > 0) {
      params[`attr.${key}`] = value;
    } else if (typeof value === 'string' && value !== '') {
      params[`attr.${key}`] = value;
    }
  }

  return params;
};

const fetchProducts = async () => {
  loading.value = true;
  error.value = null;
  lastSearchMs.value = null;
  // Do not reset scuPageData if already on an SCU page with data
  const isOnSCUPage = scuPageData.value != null;
  try {
    // If page is set in the URL, use it and sync pagination
    if (route.query.page) {
      pagination.page = parseInt(route.query.page, 10);
    }

    const params = {
      ...buildQueryParams(),
      page: pagination.page,
      limit: pagination.per_page,
    };

    // Build URL from current route path (use /shop/{category_slugs} if in shop)
    let url = '/shop';
    if (route.path.startsWith('/shop/')) {
      url = route.path; // preserve category slugs from URL
    }

    const response = await api.get(url, { params });

    // Read server-side timing from header: X-Response-Time-Ms
    // Axios normalizes header names to lowercase, but we'll search defensively
    const headers = response.headers || {};
    const headerKeys = Object.keys(headers);
    const timingKey = headerKeys.find(k => 
      k.toLowerCase().includes('response-time') || k.toLowerCase().includes('response_time')
    );
    const raw = timingKey ? headers[timingKey] : null;

    if (raw != null) {
      const val = String(raw).replace('ms', '').trim();
      const parsed = parseFloat(val);
      if (!isNaN(parsed)) {
        const ms = Math.max(1, Math.round(parsed));
        lastSearchMs.value = ms;
        // Make available globally for header badge
        if (typeof window !== 'undefined') {
          window.__LAST_SEARCH_MS__ = ms;
        }
      }
    }

    const data = response.data;

    // If the response is an SCUPage, store it and render SCUPageView
    if (data.page && typeof data.page === 'object' && data.page.scu) {
      scuPageData.value = data;
      return;
    }

    // If we were on an SCUPage but the API returned a regular catalog, reset SCUPage
    if (isOnSCUPage) {
      scuPageData.value = null;
    }

    products.value = data.items || [];
    pagination.total = data.total || 0;
    const perPage = data.limit || pagination.per_page;
    pagination.total_pages = Math.ceil(pagination.total / perPage);
    categoryAttrs.value = data.category_attrs || [];

    // Build category path via API for proper localized names
    if (data.category_id) {
      fetchCategoryPath(data.category_id);
      // Store the current category with description
      currentCategory.value = data.category || null;
    } else {
      categoryPath.value = [];
      currentCategory.value = null;
    }
  } catch (e) {
    if (e.response?.status === 503) {
      maintenanceMode.value = true;
      error.value = null;
      return;
    }
    error.value = e.response?.data?.message || t('catalog.load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const fetchCategoryAttrs = async (categoryId) => {
  try {
    const response = await api.get(`/admin/categories/${categoryId}/attributes`);
    const items = response.data.attributes || response.data.items || response.data || [];
    categoryAttrs.value = Array.isArray(items)
      ? items.filter(a => a && a.code && a.is_filterable !== false)
      : [];
  } catch (e) {
    console.error('Failed to fetch category attributes:', e);
    categoryAttrs.value = [];
  }
};

// Build category path: [{id, name_ru/ua/pl/en, slug}, ...] from root to current
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

// Fetch root categories for horizontal bar
const fetchRootCategories = async () => {
  if (rootCategories.value.length > 0) return; // already loaded
  rootCatsLoading.value = true;
  try {
    const response = await api.get('/categories/tree');
    rootCategories.value = Array.isArray(response.data) ? response.data : [];
  } catch (e) {
    console.error('Failed to fetch root categories:', e);
    rootCategories.value = [];
  } finally {
    rootCatsLoading.value = false;
  }
};

// Get localized category name
// Check if image URL is valid (not a placeholder CDN URL)
const isValidImage = (url) => {
  if (!url) return false;
  return !url.includes('cdn.makoshop.com');
};

// Check if root category is active (we are browsing it or its subtree)
const isRootCategoryActive = (rootCat) => {
  if (!currentBrowseCategory.value) return false;
  // Direct match
  if (rootCat.id === currentBrowseCategory.value.id) return true;
  // Check if current category is in this root's subtree
  const find = (nodes) => {
    for (const node of nodes) {
      if (node.id === currentBrowseCategory.value.id) return true;
      if (node.children?.length && find(node.children)) return true;
    }
    return false;
  };
  return rootCat.children?.length && find(rootCat.children);
};

const catName = (cat) => {
  if (!cat) return '';
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || '';
};

// Navigate to category using slugs from tree
const buildCategoryPath = (targetId, nodes, path = []) => {
  for (const node of nodes) {
    path.push(node.slug);
    if (node.id === targetId) {
      return path;
    }
    if (node.children && node.children.length > 0) {
      const result = buildCategoryPath(targetId, node.children, path);
      if (result) return result;
    }
    path.pop();
  }
  return null;
};

// Build category path from URL slugs
const buildPathFromUrl = () => {
  if (!rootCategories.value.length || !route.path.startsWith('/shop')) {
    categoryBrowsePath.value = [];
    return;
  }

  const slugs = route.path.slice(6).split('/').filter(Boolean);
  const path = [];
  let nodes = rootCategories.value;

  for (const slug of slugs) {
    const cat = nodes.find(c => c.slug === slug);
    if (!cat) break;
    path.push(cat);
    nodes = cat.children || [];
  }

  categoryBrowsePath.value = path;
};

// Navigate into a category (updates products)
const navigateToCategory = (cat) => {
  const slugs = buildCategoryPath(cat.id, rootCategories.value);
  const query = { ...route.query };
  delete query.page;

  let path = '/shop';
  if (slugs && slugs.length > 0) {
    path = '/shop/' + slugs.join('/');
  }

  router.push({ path, query });
};

// Get localized description for category
const catDescription = (cat) => {
  if (!cat) return '';
  const langField = `description_${locale.value}`;
  return cat[langField] || cat.description || '';
};

// Current category in browse panel
const currentBrowseCategory = computed(() => {
  if (categoryBrowsePath.value.length === 0) return null;
  return categoryBrowsePath.value[categoryBrowsePath.value.length - 1];
});

// Computed: current context title and description
const contextTitle = computed(() => {
  if (currentCategory.value) {
    return catName(currentCategory.value);
  }
  return t('catalog.catalog_title'); // root catalog
});

const contextDescription = computed(() => {
  if (currentCategory.value) {
    return catDescription(currentCategory.value);
  }
  // Root catalog description (i18n)
  return t('catalog.root_description');
});



const toggleAttrFilter = (code, value, checked) => {
  if (!attrFilters[code]) attrFilters[code] = [];
  if (checked) {
    if (!attrFilters[code].includes(value)) attrFilters[code].push(value);
  } else {
    attrFilters[code] = attrFilters[code].filter(v => v !== value);
    if (attrFilters[code].length === 0) delete attrFilters[code];
  }
};

// Get localized attribute name based on current locale
const attrDisplayName = (attr) => {
  if (!attr) return '';
  // attr has name_ru/name_ua/name_pl/name_en from API
  const langField = `name_${locale.value}`;
  return attr[langField] || attr.name_en || attr.name_ru || attr.name_ua || attr.name_pl || humanizeAttrName(attr.code);
};

const humanizeAttrName = (code) => {
  if (!code) return '';
  let s = code.replace(/_/g, ' ').replace(/-/g, ' ');
  s = s.replace(/\b\w/g, c => c.toUpperCase());

  // Known prefixes
  if (s.startsWith('Komplektatsiya:')) {
    return s;
  }
  if (s.startsWith('V Komplekte:')) {
    return s;
  }

  // Try to detect "komplektatsiya <item>" pattern
  if (s.toLowerCase().startsWith('komplektatsiya ')) {
    const rest = s.slice('komplektatsiya '.length);
    return t('catalog.config_prefix') + rest.charAt(0).toUpperCase() + rest.slice(1);
  }
  if (s.toLowerCase().startsWith('v komplekte ')) {
    const rest = s.slice('v komplekte '.length);
    return t('catalog.bundle_prefix') + rest.charAt(0).toUpperCase() + rest.slice(1);
  }

  return s;
};

// Get localized label for enum attribute values
// Tries: attr_enum.{code}.{value} in i18n, then humanize value
const enumValueLabel = (attr, value) => {
  if (!attr || !value) return String(value);
  if (attr.type !== 'enum' && attr.type !== 'multi_enum') return String(value);
  // Try i18n key: attr_enum.{code}.{value}
  const key = `attr_enum.${attr.code}.${value}`;
  const translated = t(key);
  if (translated !== key) return translated;
  // Fallback: humanize the value string
  let s = String(value).replace(/_/g, ' ').replace(/-/g, ' ');
  return s.replace(/\b\w/g, c => c.toUpperCase());
};

const isAttrSelected = (code, value) => {
  return (attrFilters[code] || []).includes(value);
};

const getAttrOptions = (attr) => attr.options || attr.values || [];

const MAX_ATTR_TAGS = 7;

// Search filters per attribute (for scrollable tags)
const attrSearch = ref({});

// Expanded state per attribute
const attrExpanded = ref({});

const toggleAttrExpanded = (code) => {
  attrExpanded.value[code] = !attrExpanded.value[code];
};

const setAttrSearch = (code, value) => {
  attrSearch.value[code] = value;
};

// Selected tags for an attribute (always visible)
const selectedAttrTags = (attr) => {
  const selected = attrFilters[attr.code] || [];
  if (selected.length === 0) return [];
  return selected.filter(tag => getAttrOptions(attr).includes(tag));
};

// Unselected tags, optionally filtered by search
const unselectedAttrTags = (attr) => {
  const all = getAttrOptions(attr);
  const selected = attrFilters[attr.code] || [];
  const unselected = all.filter(tag => !selected.includes(tag));
  const search = (attrSearch.value[attr.code] || '').toLowerCase();
  if (!search) return unselected;
  return unselected.filter(tag => String(tag).toLowerCase().includes(search));
};

// When expanded: all unselected tags go into scrollable area
// When collapsed: only first MAX_ATTR_TAGS shown outside
const visibleUnselectedTags = (attr) => {
  if (attrExpanded.value[attr.code]) return [];
  return unselectedAttrTags(attr).slice(0, MAX_ATTR_TAGS);
};

// Hidden unselected tags (go into scrollable area)
const hiddenUnselectedTags = (attr) => {
  if (attrExpanded.value[attr.code]) return unselectedAttrTags(attr);
  return unselectedAttrTags(attr).slice(MAX_ATTR_TAGS);
};

const hasMoreUnselectedTags = (attr) => hiddenUnselectedTags(attr).length > 0;

const visibleAttrs = computed(() => {
  return categoryAttrs.value
    .filter(attr => attr.is_filterable !== false)
    .filter(attr => getAttrOptions(attr).length > 0);
});

const visibleBrands = computed(() => {
  return [];
});

const hasAttrsOrBrands = computed(() => {
  return visibleAttrs.value.length > 0 || visibleBrands.value.length > 0;
});

const applyFilters = () => {
  pagination.page = 1;
  const query = { ...route.query };
  delete query.category_id;
  if (filters.q) query.q = filters.q; else delete query.q;
  if (filters.price_min) query.price_min = filters.price_min; else delete query.price_min;
  if (filters.price_max) query.price_max = filters.price_max; else delete query.price_max;
  if (filters.sort && filters.sort !== 'relevance') query.sort = filters.sort; else delete query.sort;

  // Always preserve current route path (category slugs)
  router.replace({ path: route.path, query });
};

const resetFilters = () => {
  filters.q = '';
  filters.price_min = '';
  filters.price_max = '';
  filters.sort = 'relevance';
  Object.keys(attrFilters).forEach(key => delete attrFilters[key]);
  pagination.page = 1;
  applyFilters();
};

const formatPrice = (price) => {
  const currency = t('scupage.currency', 'EUR');
  const localeMap = { ru: 'ru-RU', en: 'en-US', ua: 'uk-UA', pl: 'pl-PL' };
  const loc = localeMap[locale.value] || 'en-US';
  return new Intl.NumberFormat(loc, { style: 'currency', currency }).format(price);
};

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

// Returns the parameters string on one line separated by ";"
const getAttributesString = (attrs) => {
  const m = normalizeAttrs(attrs);
  if (!m || Object.keys(m).length === 0) return '';
  const parts = [];
  const keys = Object.keys(m);
  for (const key of keys.slice(0, 4)) {
    const value = m[key];
    if (value != null && String(value).trim() !== '') {
      parts.push(`${key}: ${String(value).trim()}`);
    }
  }
  return parts.join(' ; ');
};

const pageTitle = computed(() => {
  if (filters.q) return t('catalog.search_results', { q: filters.q });
  return t('catalog.catalog_title');
});



// Sync filters from route
const syncFiltersFromRoute = () => {
  filters.q = route.query.q || '';
  filters.price_min = route.query.price_min || '';
  filters.price_max = route.query.price_max || '';
  filters.sort = route.query.sort || 'relevance';
  if (route.query.page) pagination.page = parseInt(route.query.page, 10);
};

onMounted(async () => {
  syncFiltersFromRoute();
  updatePerPage();

  // Load root categories for horizontal bar
  await fetchRootCategories();
  buildPathFromUrl();

  // Use pre-rendered data if available (from SSR/proxy)
  if (typeof window !== 'undefined' && window.__INITIAL_DATA__) {
    const data = window.__INITIAL_DATA__;
    delete window.__INITIAL_DATA__; // consume once

    // If it's an SCUPage
    if (data.page && typeof data.page === 'object' && data.page.scu) {
      scuPageData.value = data;
      loading.value = false;
      return;
    }

    // Catalog data
    products.value = data.items || [];
    pagination.total = data.total || 0;
    const perPage = data.limit || pagination.per_page;
    pagination.total_pages = Math.ceil(pagination.total / perPage);
    categoryAttrs.value = data.category_attrs || [];

    // Build category path via API for proper localized names
    if (data.category_id) {
      fetchCategoryPath(data.category_id);
      // Store the current category with description
      currentCategory.value = data.category || null;
    } else {
      categoryPath.value = [];
      currentCategory.value = null;
    }

    loading.value = false;
    return;
  }

  fetchProducts();
  window.addEventListener('resize', updatePerPage);
});

// Watch route changes
watch(
  () => [route.query, route.path],
  async () => {
    syncFiltersFromRoute();
    buildPathFromUrl();
    fetchProducts();
  },
  { deep: true }
);

// Watch filter changes — update URL only
watch(
  filters,
  () => {
    applyFilters();
  },
  { deep: true }
);

// Watch attr filter changes
watch(
  attrFilters,
  () => {
    pagination.page = 1;
    fetchProducts();
  },
  { deep: true }
);

// Lock body scroll while the mobile filters overlay is open
watch(
  showMobileFilters,
  (open) => {
    if (typeof document === 'undefined') return;
    document.body.style.overflow = open ? 'hidden' : '';
  },
  { immediate: true }
);
onBeforeUnmount(() => {
  if (typeof document !== 'undefined') document.body.style.overflow = '';
});

const goToPage = (page) => {
  if (page < 1 || page > pagination.total_pages) return;
  pagination.page = page;
  router.push({ path: route.path, query: { ...route.query, page: page.toString() } });
  // fetchProducts() will be called by watch(route.query)
};

// Navigate to SCU page (landing page for product group)
const goToSCUPage = (product) => {
  // Use canonical SEO URL if available (from API response)
  if (product.seo_url) {
    router.push({ path: product.seo_url });
    return;
  }
  
  // Fallback: build URL from slug
  if (product.slug) {
    router.push({ path: '/shop/' + product.slug });
    return;
  }
  
  // Fallback to product detail
  router.push({ name: 'product', params: { id: product.id } });
};

// Pluralization helper using locale-specific i18n keys
const pluralize = (n, oneKey, fewKey, manyKey) => {
  const abs = Math.abs(n) % 100;
  const lastDigit = abs % 10;
  if (abs > 10 && abs < 20) return t(manyKey);
  if (lastDigit === 1) return t(oneKey);
  if (lastDigit >= 2 && lastDigit <= 4) return t(fewKey);
  return t(manyKey);
};

defineOptions({ name: 'CatalogView' });
</script>

<template>
  <!-- Maintenance mode screen -->
  <div v-if="maintenanceMode" class="min-h-screen flex items-center justify-center bg-surface-2">
    <div class="max-w-md text-center p-6 bg-surface rounded-xl shadow-sm">
      <h1 class="text-2xl font-bold mb-3 text-ink">{{ t('maintenance.title') }}</h1>
      <p class="text-ink-2 mb-4">
        {{ t('maintenance.message') }}
      </p>
      <button @click="fetchProducts" class="btn btn-primary">
        {{ t('maintenance.try_again') }}
      </button>
    </div>
  </div>

  <!-- Render SCUPageView if API returned an SCUPage -->
  <SCUPageView v-else-if="scuPageData" :data="scuPageData" />

  <div v-else class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">

    <!-- Root categories grid -->
    <div class="mb-4">
      <!-- Loading state -->
      <div v-if="rootCatsLoading" class="flex gap-2">
        <div v-for="i in 12" :key="i" class="flex-1 aspect-square rounded-lg bg-surface-3 animate-pulse" />
      </div>

      <!-- Categories row (single line) -->
      <div v-else class="flex gap-2">
        <div
          v-for="cat in rootCategories"
          :key="cat.id"
          :class="[
            'flex-1 cursor-pointer group rounded-xl overflow-hidden border transition-all duration-200 flex flex-col',
            // Active category (we are browsing it or its subtree)
            isRootCategoryActive(cat)
              ? 'border-indigo-500 ring-2 ring-indigo-200 shadow-md'
              : 'border-line hover:border-indigo-300 hover:shadow-md'
          ]"
          @click="navigateToCategory(cat)"
        >
            <!-- Category image -->
            <div class="relative w-full pt-[100%] bg-surface-2 overflow-hidden">
              <!-- Light theme image -->
              <img
                v-if="isValidImage(cat.image_light_url)"
                :src="cat.image_light_url"
                :alt="catName(cat)"
                loading="lazy"
                decoding="async"
                class="absolute inset-1 w-full h-full object-cover dark:hidden"
              />
              <!-- Dark theme image (falls back to light if no dark variant) -->
              <img
                v-if="isValidImage(cat.image_dark_url) || isValidImage(cat.image_light_url)"
                :src="isValidImage(cat.image_dark_url) ? cat.image_dark_url : cat.image_light_url"
                :alt="catName(cat)"
                loading="lazy"
                decoding="async"
                class="absolute inset-1 w-full h-full object-cover hidden dark:block"
              />
              <!-- Fallback placeholder -->
              <div
                v-if="!isValidImage(cat.image_light_url) && !isValidImage(cat.image_dark_url)"
                class="absolute inset-1 flex items-center justify-center text-ink-3"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="h-8 w-8" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
                </svg>
              </div>
              <!-- Chevron indicator -->
              <div
                v-if="cat.children && cat.children.length > 0"
                class="absolute inset-4 flex items-center justify-center bg-black/10 opacity-0 group-hover:opacity-100 transition-opacity rounded-full"
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  class="h-5 w-5 text-white"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
                </svg>
              </div>
            </div>

            <!-- Category name -->
            <div
              :class="[
                'flex-1 flex items-center px-2 py-1.5',
                isRootCategoryActive(cat)
                  ? 'bg-indigo-50 dark:bg-indigo-900/30'
                  : 'bg-surface'
              ]"
            >
              <div
                :class="[
                  'text-xs font-medium text-left line-clamp-2',
                  isRootCategoryActive(cat)
                    ? 'text-indigo-700 dark:text-indigo-300 font-semibold'
                    : 'text-ink'
                ]"
              >
                {{ catName(cat) }}
              </div>
            </div>
          </div>
      </div>

      <!-- Categories panel with description and subcategories -->
      <div class="mt-2 bg-surface rounded-xl border border-line overflow-hidden">
        <div class="p-4">
          <!-- Breadcrumbs-style path -->
          <div class="flex items-center flex-wrap gap-1 text-xs text-ink-3 mb-2">
            <span
              class="cursor-pointer hover:text-indigo-600 hover:underline"
              @click="navigateToCategory({ id: '', slug: '' })"
            >
              {{ t('catalog.all_products') }}
            </span>
            <span v-for="(cat, idx) in categoryBrowsePath" :key="cat.id" class="flex items-center gap-1">
              <span class="text-ink-3">/</span>
              <span
                class="cursor-pointer hover:text-indigo-600 hover:underline"
                :class="idx === categoryBrowsePath.length - 1 ? 'font-semibold text-ink' : ''"
                @click="navigateToCategory(cat)"
              >
                {{ catName(cat) }}
              </span>
            </span>
          </div>

          <!-- Current category header with full description and image -->
          <div class="mb-3">
            <div class="flex flex-col lg:flex-row gap-4">
              <!-- Category name and full description -->
              <div class="flex-1 min-w-0">
                <h2 class="text-xl font-semibold text-ink">
                  {{ currentBrowseCategory ? catName(currentBrowseCategory) : t('catalog.all_products') }}
                </h2>
                <p
                  v-if="currentBrowseCategory && catDescription(currentBrowseCategory)"
                  class="mt-1 text-sm text-ink-2"
                >
                  {{ catDescription(currentBrowseCategory) }}
                </p>
              </div>
              <!-- Category image on the right -->
              <div
                v-if="currentBrowseCategory && (isValidImage(currentBrowseCategory.image_light_url) || isValidImage(currentBrowseCategory.image_dark_url))"
                class="flex-shrink-0 w-48 aspect-square relative rounded-xl overflow-hidden shadow-md border border-line"
              >
                <img
                  v-if="isValidImage(currentBrowseCategory.image_light_url)"
                  :src="currentBrowseCategory.image_light_url"
                  :alt="catName(currentBrowseCategory)"
                  loading="lazy"
                  decoding="async"
                  class="absolute inset-0 w-full h-full object-cover"
                />
                <img
                  v-if="isValidImage(currentBrowseCategory.image_dark_url) || isValidImage(currentBrowseCategory.image_light_url)"
                  :src="isValidImage(currentBrowseCategory.image_dark_url) ? currentBrowseCategory.image_dark_url : currentBrowseCategory.image_light_url"
                  :alt="catName(currentBrowseCategory)"
                  loading="lazy"
                  decoding="async"
                  class="absolute inset-0 w-full h-full object-cover hidden dark:block"
                />
              </div>
            </div>
          </div>

          <!-- Subcategories list as tags -->
          <div
            v-if="currentBrowseCategory && currentBrowseCategory.children && currentBrowseCategory.children.length > 0"
            class="flex flex-wrap gap-2"
          >
            <button
              v-for="sub in currentBrowseCategory.children"
              :key="sub.id"
              @click="navigateToCategory(sub)"
              :class="[
                'inline-flex items-center gap-1 px-3 py-1.5 text-xs font-medium rounded-full border transition-all duration-150 cursor-pointer',
                // Active category (we are inside it)
                currentCategory && currentCategory.id === sub.id
                  ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300'
                  // Has children — slightly highlighted
                  : sub.children && sub.children.length > 0
                    ? 'border-line bg-surface-2 text-ink hover:border-indigo-300 hover:text-indigo-600 hover:bg-indigo-50 dark:hover:bg-slate-600'
                    // Leaf category
                    : 'border-line text-ink-2 hover:border-indigo-300 hover:text-indigo-600 hover:bg-indigo-50 dark:hover:bg-slate-700'
              ]"
            >
              {{ catName(sub) }}
              <!-- Arrow for categories with children -->
              <svg
                v-if="sub.children && sub.children.length > 0"
                xmlns="http://www.w3.org/2000/svg"
                class="h-3 w-3 flex-shrink-0 opacity-60"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2.5"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
              </svg>
              <span
                v-if="sub.products_count != null && sub.products_count > 0"
                class="text-[11px] opacity-60"
              >
                · {{ Number(sub.products_count).toLocaleString() }}
              </span>
            </button>
          </div>
        </div>
      </div>
    </div>



    <div class="flex items-center justify-between mb-3">
      <div class="flex items-center gap-2">
        <!-- Mobile filters button -->
        <button
          @click="showMobileFilters = true"
          class="md:hidden inline-flex items-center gap-1 px-3 py-1.5 text-xs border border-line rounded-lg bg-surface text-ink-2 hover:bg-surface-2"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z" />
          </svg>
          {{ t('catalog.filters') }}
        </button>

        <span
          v-if="lastSearchMs != null"
          class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium bg-green-50 text-green-700 border border-green-200"
        >
          <span class="inline-block w-1.5 h-1.5 rounded-full bg-green-500"></span>
          {{ t('catalog.search_time', { ms: lastSearchMs }) }}
        </span>
      </div>
    </div>

    <div v-if="error" class="mb-4 p-3 bg-red-50 text-red-700 rounded-lg text-sm">
      {{ error }}
    </div>

    <!-- Inline filters (when no attributes — shown above products) -->
    <div
      v-if="!hasAttrsOrBrands"
      class="mb-4 flex flex-wrap items-center gap-3 bg-surface rounded-lg shadow-sm border border-line p-3"
    >
      <!-- Search -->
      <div class="flex-1 min-w-[200px]">
        <input
          v-model="filters.q"
          type="text"
          :placeholder="t('catalog.search_placeholder')"
          class="w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
        />
      </div>
      <!-- Price range -->
      <div class="flex items-center gap-2">
        <input
          v-model="filters.price_min"
          type="number"
          :placeholder="t('catalog.price_from')"
          class="w-24 px-2 py-2 border border-line rounded-lg text-sm"
        />
        <span class="text-ink-3">—</span>
        <input
          v-model="filters.price_max"
          type="number"
          :placeholder="t('catalog.price_to')"
          class="w-24 px-2 py-2 border border-line rounded-lg text-sm"
        />
      </div>
    </div>

    <div class="flex gap-6">
      <!-- Sidebar: Filters (only when attributes exist) -->
      <aside v-if="hasAttrsOrBrands" class="w-64 flex-shrink-0 hidden md:block">
        <div class="bg-surface rounded-lg shadow-sm border border-line p-4 space-y-4">
          <!-- Search -->
          <div>
            <label class="block text-xs font-semibold text-ink-3 uppercase tracking-wide mb-1">{{ t('catalog.search_label') }}</label>
            <input
              v-model="filters.q"
              type="text"
              :placeholder="t('catalog.search_placeholder')"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
            />
          </div>

          <!-- Price range -->
          <div>
            <label class="block text-xs font-semibold text-ink-3 uppercase tracking-wide mb-1">{{ t('catalog.price_label') }}</label>
            <div class="flex gap-2">
              <input
                v-model="filters.price_min"
                type="number"
                :placeholder="t('catalog.price_from')"
                class="w-1/2 px-2 py-1.5 border border-line rounded-lg text-sm"
              />
              <input
                v-model="filters.price_max"
                type="number"
                :placeholder="t('catalog.price_to')"
                class="w-1/2 px-2 py-1.5 border border-line rounded-lg text-sm"
              />
            </div>
          </div>

          <!-- Brands -->
          <div v-if="visibleBrands.length > 0" class="border-t pt-3">
            <label class="block text-xs font-semibold text-ink-3 uppercase tracking-wide mb-1">{{ t('catalog.brand_label') }}</label>
            <div class="space-y-1">
              <label
                v-for="brand in visibleBrands"
                :key="brand"
                class="flex items-center text-sm py-0.5 cursor-pointer hover:bg-surface-2 rounded px-1 -ml-1"
              >
                <span class="flex items-center gap-2">
                  <input
                    type="checkbox"
                    :checked="isAttrSelected('brand', brand)"
                    @change="toggleAttrFilter('brand', brand, $event.target.checked)"
                  />
                  {{ brand }}
                </span>
              </label>
            </div>
          </div>

          <!-- Attribute filters -->
          <div v-for="attr in visibleAttrs" :key="attr.code" class="border-t pt-3">
            <div class="flex items-center justify-between mb-1">
              <label class="text-xs font-semibold text-ink-3 uppercase tracking-wide">
                {{ attrDisplayName(attr) }}
              </label>
            </div>

            <!-- Selected tags (always visible) -->
            <div v-if="selectedAttrTags(attr).length > 0" class="flex flex-wrap gap-1 mb-1">
              <button
                v-for="tag in selectedAttrTags(attr)"
                :key="tag"
                @click="toggleAttrFilter(attr.code, tag, false)"
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-indigo-600 text-white border-indigo-600"
              >
                {{ enumValueLabel(attr, tag) }}
              </button>
            </div>

            <!-- Visible unselected tags (up to 7) -->
            <div class="flex flex-wrap gap-1">
              <button
                v-for="tag in visibleUnselectedTags(attr)"
                :key="tag"
                @click="toggleAttrFilter(attr.code, tag, true)"
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-surface-2 text-ink-2 border-line hover:bg-surface-3"
              >
                {{ enumValueLabel(attr, tag) }}
              </button>
            </div>

            <!-- Show more link -->
            <button
              v-if="hasMoreUnselectedTags(attr)"
              @click="toggleAttrExpanded(attr.code)"
              class="mt-1 text-xs text-indigo-600 hover:underline"
            >
              {{ attrExpanded[attr.code] ? t('catalog.hide') : t('catalog.show_more', { count: hiddenUnselectedTags(attr).length }) }}
            </button>

            <!-- Expanded area with search + scrollable tags -->
            <div
              v-if="hasMoreUnselectedTags(attr) && attrExpanded[attr.code]"
              class="mt-1 border border-line rounded p-1.5 max-h-48 overflow-y-auto bg-surface-2"
            >
              <!-- Search -->
              <input
                v-model="attrSearch[attr.code]"
                type="text"
                :placeholder="t('catalog.search_attr_placeholder')"
                class="w-full mb-1.5 px-2 py-0.5 border border-line rounded text-[11px] focus:outline-none focus:ring-1 focus:ring-indigo-500"
              />
              <!-- Tags -->
              <div class="flex flex-wrap gap-1">
                <button
                  v-for="tag in hiddenUnselectedTags(attr)"
                  :key="tag"
                  @click="toggleAttrFilter(attr.code, tag, true)"
                  class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-surface-2 text-ink-2 border-line hover:bg-surface-3"
                >
                  {{ enumValueLabel(attr, tag) }}
                </button>
              </div>
            </div>
          </div>

          <!-- Reset -->
          <button
            @click="resetFilters"
            class="w-full px-3 py-2 text-sm text-ink-2 border border-line rounded-lg hover:bg-surface-2 transition"
          >
            {{ t('catalog.reset_filters') }}
          </button>
        </div>
      </aside>

      <!-- Products grid -->
      <div class="flex-1">
        <div class="flex flex-col sm:flex-row items-start sm:items-center justify-between mb-4 gap-2">
          <div class="text-sm text-ink-2">
            {{ t('catalog.found', { count: pagination.total }) }}
          </div>
          <select
            v-model="filters.sort"
            class="px-3 py-1.5 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 bg-surface"
          >
            <option value="relevance">{{ t('catalog.sort_relevance') }}</option>
            <option value="price_asc">{{ t('catalog.sort_price_asc') }}</option>
            <option value="price_desc">{{ t('catalog.sort_price_desc') }}</option>
          </select>
        </div>

        <!-- Skeleton while loading -->
        <div v-if="loading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3 sm:gap-4" aria-hidden="true">
          <div v-for="i in 12" :key="i" class="bg-surface rounded-xl border border-line overflow-hidden">
            <div class="aspect-[4/3] bg-surface-3 animate-pulse"></div>
            <div class="p-3 space-y-2">
              <div class="h-3 bg-surface-3 rounded animate-pulse"></div>
              <div class="h-3 w-2/3 bg-surface-3 rounded animate-pulse"></div>
              <div class="h-4 w-1/2 bg-surface-3 rounded animate-pulse"></div>
            </div>
          </div>
        </div>

        <div v-else-if="products.length === 0" class="text-center py-12 text-ink-3">
          <p class="mb-2">{{ t('catalog.no_products') }}</p>
          <button @click="resetFilters" class="text-indigo-600 hover:underline text-sm">
            {{ t('catalog.reset_filters') }}
          </button>
        </div>

        <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3 sm:gap-4">
          <div
            v-for="product in products"
            :key="product.id"
            class="bg-surface rounded-xl border border-line overflow-hidden cursor-pointer relative
                   transition-all duration-200 ease-out
                   hover:shadow-lg hover:-translate-y-0.5 hover:border-indigo-300"
            @click="goToSCUPage(product)"
          >
            <!-- Badge: ad -->
            <span v-if="product.promoted" class="absolute top-2 left-2 z-10 bg-yellow-400 text-yellow-900 text-[11px] font-semibold px-1.5 py-0.5 rounded-full">
              {{ t('catalog.ad') }}
            </span>
            <!-- Badge: multiple variants -->
            <span v-if="product.product_count && product.product_count > 1" class="absolute top-2 right-2 z-10 bg-indigo-600 text-white text-[11px] font-semibold px-1.5 py-0.5 rounded-full">
              {{ product.product_count }} {{ pluralize(product.product_count, 'catalog.variant_one', 'catalog.variant_few', 'catalog.variant_many') }}
            </span>

            <!-- Image -->
            <div class="aspect-[4/3] bg-surface-2 flex items-center justify-center">
              <img
                v-if="product.images?.length"
                :src="product.images[0]"
                :alt="product.name"
                loading="lazy"
                decoding="async"
                class="w-full h-full object-cover"
              />
              <span v-else class="text-ink-3 text-xs">{{ t('catalog.no_photo') }}</span>
            </div>

            <!-- Info -->
            <div class="p-2.5 sm:p-3 space-y-1">
              <!-- Product name -->
              <h3 class="font-semibold text-[13px] sm:text-sm leading-tight line-clamp-2 text-ink">{{ product.title || product.name }}</h3>

              <!-- Brand/manufacturer -->
              <div v-if="product.brand" class="text-[11px] sm:text-xs text-ink-3">{{ product.brand }}</div>

              <!-- Attributes (if any) -->
              <div v-if="getAttributesString(product.attributes)" class="text-[11px] text-ink-3 truncate">
                {{ getAttributesString(product.attributes) }}
              </div>

              <!-- Price + sellers + rating -->
              <div class="flex items-end justify-between pt-1 gap-2">
                <div class="flex flex-col">
                  <span class="font-bold text-sm sm:text-base text-indigo-700">
                    {{ formatPrice(product.price || product.min_price) }}
                  </span>
                  <span
                    v-if="product.sellers_count && product.sellers_count > 1"
                    class="text-[11px] text-ink-3"
                  >
                    {{ t('catalog.from_price_sellers', { count: product.sellers_count }) }}
                  </span>
                </div>
                <span v-if="product.avg_rating" class="text-xs text-yellow-500 flex-shrink-0">
                  ★ {{ product.avg_rating.toFixed(1) }}
                </span>
              </div>
            </div>
          </div>
        </div>

        <div v-if="pagination.total_pages > 1" class="flex justify-center items-center gap-2 mt-6">
          <button
            @click="goToPage(pagination.page - 1)"
            :disabled="pagination.page <= 1"
            class="px-3 py-1.5 border border-line rounded-lg text-sm disabled:opacity-40 hover:bg-surface-2 transition"
          >
            {{ t('catalog.back') }}
          </button>
          <span class="px-3 py-1.5 text-sm text-ink-2">
            {{ t('catalog.page_of', { page: pagination.page, total: pagination.total_pages }) }}
          </span>
          <button
            @click="goToPage(pagination.page + 1)"
            :disabled="pagination.page >= pagination.total_pages"
            class="px-3 py-1.5 border border-line rounded-lg text-sm disabled:opacity-40 hover:bg-surface-2 transition"
          >
            {{ t('common.next') }} →
          </button>
        </div>
      </div>
    </div>

    <!-- Mobile filters overlay -->
    <div
      v-if="showMobileFilters"
      class="md:hidden fixed inset-0 z-40 bg-black/30 flex justify-end"
      @click="showMobileFilters = false"
    >
      <div
        role="dialog"
        aria-modal="true"
        :aria-label="t('catalog.filters')"
        class="w-[85vw] max-w-sm bg-surface h-full shadow-xl overflow-y-auto"
        @click.stop
      >
        <div class="sticky top-0 bg-surface border-b border-line px-4 py-3 flex items-center justify-between z-10">
          <span class="font-semibold">{{ t('catalog.filters') }}</span>
          <button @click="showMobileFilters = false" class="p-1 rounded hover:bg-surface-2" :aria-label="t('common.close')">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div class="p-4 space-y-4">
          <!-- Search -->
          <div>
            <label class="block text-xs font-semibold text-ink-3 uppercase tracking-wide mb-1">{{ t('catalog.search_label') }}</label>
            <input
              v-model="filters.q"
              type="text"
              :placeholder="t('catalog.search_placeholder')"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
            />
          </div>

          <!-- Price range -->
          <div>
            <label class="block text-xs font-semibold text-ink-3 uppercase tracking-wide mb-1">{{ t('catalog.price_label') }}</label>
            <div class="flex gap-2">
              <input
                v-model="filters.price_min"
                type="number"
                :placeholder="t('catalog.price_from')"
                class="w-1/2 px-2 py-1.5 border border-line rounded-lg text-sm"
              />
              <input
                v-model="filters.price_max"
                type="number"
                :placeholder="t('catalog.price_to')"
                class="w-1/2 px-2 py-1.5 border border-line rounded-lg text-sm"
              />
            </div>
          </div>

          <!-- Attribute filters -->
          <div v-for="attr in visibleAttrs" :key="attr.code" class="border-t pt-3">
            <label class="text-xs font-semibold text-ink-3 uppercase tracking-wide mb-1 block">
              {{ attrDisplayName(attr) }}
            </label>

            <!-- Selected tags -->
            <div v-if="selectedAttrTags(attr).length > 0" class="flex flex-wrap gap-1 mb-1">
              <button
                v-for="tag in selectedAttrTags(attr)"
                :key="tag"
                @click="toggleAttrFilter(attr.code, tag, false)"
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-indigo-600 text-white border-indigo-600"
              >
                {{ enumValueLabel(attr, tag) }}
              </button>
            </div>

            <!-- All tags (scrollable) -->
            <div class="max-h-40 overflow-y-auto border border-line rounded p-1.5 bg-surface-2">
              <div class="flex flex-wrap gap-1">
                <button
                  v-for="tag in getAttrOptions(attr)"
                  :key="tag"
                  @click="toggleAttrFilter(attr.code, tag, !isAttrSelected(attr.code, tag))"
                  class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer"
                  :class="isAttrSelected(attr.code, tag)
                    ? 'bg-indigo-600 text-white border-indigo-600'
                    : 'bg-surface-2 text-ink-2 border-line hover:bg-surface-3'"
                >
                  {{ enumValueLabel(attr, tag) }}
                </button>
              </div>
            </div>
          </div>

          <!-- Reset -->
          <button
            @click="resetFilters; showMobileFilters = false"
            class="w-full px-3 py-2 text-sm text-ink-2 border border-line rounded-lg hover:bg-surface-2 transition"
          >
            {{ t('catalog.reset_filters') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>


<style scoped>
/* Thin scrollbar for horizontal categories */
.scrollbar-thin::-webkit-scrollbar {
  height: 4px;
}
.scrollbar-thin::-webkit-scrollbar-track {
  background: transparent;
}
.scrollbar-thin::-webkit-scrollbar-thumb {
  background-color: rgba(156, 163, 175, 0.4);
  border-radius: 999px;
}
.scrollbar-thin::-webkit-scrollbar-thumb:hover {
  background-color: rgba(156, 163, 175, 0.7);
}
</style>
