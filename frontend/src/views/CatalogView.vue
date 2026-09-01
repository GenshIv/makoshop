<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, watch, computed, defineAsyncComponent, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import ProductCard from '../components/ProductCard.vue';
import SkeletonCard from '../components/SkeletonCard.vue';
import ViewToggle from '../components/ViewToggle.vue';
import EmptyState from '../components/EmptyState.vue';
import BrandingSlot from '../components/BrandingSlot.vue';
import { useAnimation } from '../composables/useAnimation';
import { useBranding } from '../composables/useBranding';
import { useSettings } from '../composables/useSettings';
import { useBrandingStore } from '../stores/branding';

const { defaultCurrency } = useSettings();

// Lazy-load EANPageView to avoid circular imports
const EANPageView = defineAsyncComponent(() => import('../views/EANPageView.vue'));

// Preload the EANPageView chunk eagerly so the inline expansion transition is
// never interrupted by async loading on first use (fixes flaky enter animation).
// The same module specifier is deduped by the bundler, so this shares the
// exact chunk/promise used by defineAsyncComponent above.
import('../views/EANPageView.vue');

const { animationEnabled } = useAnimation();

const route = useRoute();
const router = useRouter();
const { t, locale } = useI18n();

// Track previous route path to distinguish navigation from filter changes
const prevRoutePath = ref(route.path);

const products = ref([]);
const categoryAttrs = ref([]);
const categoryPath = ref([]); // [{id, name}, ...]
const pagination = reactive({ page: 1, per_page: 50, total: 0, total_pages: 0 });

// Root categories for horizontal bar above products
const rootCategories = ref([]);
const rootCatsLoading = ref(false);

// Current browsing path inside categories bar: [{id, slug, name, description, ...}, ...]
const categoryBrowsePath = ref([]);

// Sync categoryBrowsePath with categoryPath (which comes from API)
watch(categoryPath, (newPath) => {
  if (newPath && newPath.length > 0) {
    categoryBrowsePath.value = newPath;
  }
});

// Branding: publish the current category chain (root -> current) so the
// resolution can apply per-section image overrides. Cleared on the home page
// and when leaving the catalog view (otherwise the stale chain would apply
// category overrides on product/cart pages).
const brandingStore = useBrandingStore();
watch(
  categoryBrowsePath,
  (path) => {
    brandingStore.setCategoryChain((path || []).map((c) => c.id));
  },
  { immediate: true }
);
onBeforeUnmount(() => {
  brandingStore.setCategoryChain([]);
});

// Branding banner elements for this view (re-evaluated on route/store changes).
const { useSlotElement } = useBranding();
const homeBannerEl = useSlotElement('home_banner');
const categoryBannerEl = useSlotElement('category_banner');

// Current category object from API (with description)
const currentCategory = ref(null);

// Breadcrumb children popup state
const hoveredBreadcrumbId = ref(null);
const breadcrumbHoverTimer = ref(null);
const showBreadcrumbChildren = (cat) => {
  if (!cat || !cat.children || cat.children.length === 0) return;
  clearTimeout(breadcrumbHoverTimer.value);
  hoveredBreadcrumbId.value = cat.id;
};
const hideBreadcrumbChildren = () => {
  breadcrumbHoverTimer.value = setTimeout(() => {
    hoveredBreadcrumbId.value = null;
  }, 200);
};
const getBreadcrumbChildren = (cat) => {
  if (!cat || !cat.children || cat.children.length === 0) return [];
  return cat.children;
};

// Breadcrumb popup animation
const breadcrumbPopupVisible = ref(false);
const breadcrumbPopupLeaving = ref(false);
watch(hoveredBreadcrumbId, (newId, oldId) => {
  if (newId) {
    // Show popup
    breadcrumbPopupLeaving.value = false;
    nextTick(() => {
      breadcrumbPopupVisible.value = true;
    });
  } else if (oldId) {
    // Hide popup with fade-out
    breadcrumbPopupVisible.value = false;
    breadcrumbPopupLeaving.value = true;
    setTimeout(() => {
      breadcrumbPopupLeaving.value = false;
    }, 150);
  }
});

const loading = ref(false);
const error = ref(null);
const maintenanceMode = ref(false);
const lastSearchMs = ref(null); // time in ms of last catalog request
const showMobileFilters = ref(false); // mobile filters panel

// If API returns EANPage data, render EANPageView instead
const eanPageData = ref(null);
// Flag to prevent route watch from re-fetching when we're about to set eanPageData from cache
const isInlineScuTransition = ref(false);
// Flag to prevent filters watch from triggering applyFilters when syncing from route
const isSyncingFiltersFromRoute = ref(false);

// EANPage cache: key -> data (to avoid re-fetching on hover/return)
// Uses sessionStorage so it survives SPA navigation but is cleared on page reload
const SCU_CACHE_KEY = 'makoshop_ean_page_cache';

const loadScuPageCacheFromStorage = () => {
  try {
    const raw = sessionStorage.getItem(SCU_CACHE_KEY);
    if (!raw) return new Map();
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return new Map();
    const map = new Map();
    for (const [k, v] of arr) {
      map.set(k, v);
    }
    return map;
  } catch {
    return new Map();
  }
};

const persistScuPageCache = () => {
  try {
    const arr = Array.from(eanPageCache.value.entries());
    sessionStorage.setItem(SCU_CACHE_KEY, JSON.stringify(arr));
  } catch {
    // ignore (quota exceeded or private mode)
  }
};

const eanPageCache = ref(loadScuPageCacheFromStorage());

// Companies cache: company_id -> { id, name }
// Uses sessionStorage so it survives SPA navigation but is cleared on page reload
const COMPANIES_CACHE_KEY = 'makoshop_companies_cache';
const COMPANIES_CACHE_TTL = 60 * 60 * 1000; // 1 hour in milliseconds

const loadCompaniesCacheFromStorage = () => {
  try {
    const raw = sessionStorage.getItem(COMPANIES_CACHE_KEY);
    if (!raw) return new Map();
    const parsed = JSON.parse(raw);
    // Check if cache has timestamp and is still valid
    if (parsed._ts && Date.now() - parsed._ts > COMPANIES_CACHE_TTL) {
      sessionStorage.removeItem(COMPANIES_CACHE_KEY);
      return new Map();
    }
    const arr = parsed.data || parsed;
    if (!Array.isArray(arr)) return new Map();
    const map = new Map();
    for (const [k, v] of arr) {
      map.set(k, v);
    }
    return map;
  } catch {
    return new Map();
  }
};

const persistCompaniesCache = () => {
  try {
    const arr = Array.from(companiesCache.value.entries());
    sessionStorage.setItem(COMPANIES_CACHE_KEY, JSON.stringify({
      _ts: Date.now(),
      data: arr
    }));
  } catch {
    // ignore
  }
};

const companiesCache = ref(loadCompaniesCacheFromStorage());
const companiesLoading = ref(false);

const fetchCompanies = async () => {
  if (companiesCache.value.size > 0 || companiesLoading.value) return;
  companiesLoading.value = true;
  try {
    const response = await api.get('/companies');
    const data = response.data || {};
    const companies = (Array.isArray(data) ? data : data.items || []);
    for (const c of companies) {
      companiesCache.value.set(c.id, { id: c.id, name: c.name });
    }
    persistCompaniesCache();
  } catch (e) {
    console.error('Failed to fetch companies:', e);
  } finally {
    companiesLoading.value = false;
  }
};

const getCompanyName = (companyId) => {
  if (!companyId) return 'Unknown';
  const company = companiesCache.value.get(companyId);
  return company?.name || `Supplier #${companyId}`;
};

// Hover preview state
const hoveredScuProduct = ref(null);
const scuPreviewData = ref(null);
const scuPreviewLoading = ref(false);
const scuPreviewTimer = ref(null); // delay timer for showing popup
const scuPreviewFetchTimer = ref(null); // delay timer for fetching data
const PREVIEW_DELAY_MS = 500; // must hover 500ms before showing popup / fetching

// Build the canonical cache/URL key for a product's EAN page.
// Must stay in sync with goToEANPage and fetchProducts (route.path based).
const getScuKey = (product) => {
  if (!product) return null;
  if (product.seo_url) return product.seo_url;
  if (product.slug) return '/shop/' + product.slug;
  return null;
};

const showScuPreview = (product) => {
  if (!product || (!product.seo_url && !product.slug)) return;
  if (product.product_count && product.product_count <= 1) return; // Only for EAN pages
  // Respect animation setting: don't show popup or fetch data when animations are off
  if (!animationEnabled.value) return;

  // Preview is already active for this product (e.g. mouse moved from the card
  // onto the popup itself) — keep it as-is to avoid a flicker/reset.
  if (
    hoveredScuProduct.value?.id === product.id &&
    (scuPreviewData.value || scuPreviewLoading.value)
  ) {
    // Cancel any pending hide so the popup stays open while on it
    clearTimeout(scuPreviewTimer.value);
    return;
  }

  clearTimeout(scuPreviewTimer.value);
  clearTimeout(scuPreviewFetchTimer.value);
  hoveredScuProduct.value = product;
  scuPreviewData.value = null;
  scuPreviewLoading.value = false;

  // Delay both popup and fetch by 500ms (only if mouse stays on the card)
  const cacheKey = getScuKey(product);
  scuPreviewTimer.value = setTimeout(async () => {
    // Check cache first
    if (eanPageCache.value.has(cacheKey)) {
      scuPreviewData.value = eanPageCache.value.get(cacheKey);
      return;
    }
    
    // Fetch from API
    scuPreviewLoading.value = true;
    try {
      const url = cacheKey;
      const response = await api.get(url);
      const data = response.data;
      
      if (data.ean_page && typeof data.ean_page === 'object' && data.ean_page.id) {
        if (!data.category && (data.ean_page.category || currentCategory.value)) {
          data.category = data.ean_page.category || currentCategory.value;
        }
        if (!data.subcategories && (data.ean_page.subcategories || rootCategories.value.length > 0)) {
          data.subcategories = data.ean_page.subcategories || [];
        }
        
        eanPageCache.value.set(cacheKey, data);
        persistScuPageCache();
        // Only show if this product is still hovered
        if (hoveredScuProduct.value?.id === product.id) {
          scuPreviewData.value = data;
        }
      }
    } catch (e) {
      console.error('Failed to fetch EAN preview:', e);
    } finally {
      scuPreviewLoading.value = false;
    }
  }, PREVIEW_DELAY_MS);
};

const hideScuPreview = () => {
  clearTimeout(scuPreviewTimer.value);
  clearTimeout(scuPreviewFetchTimer.value);
  scuPreviewTimer.value = setTimeout(() => {
    hoveredScuProduct.value = null;
    scuPreviewData.value = null;
    scuPreviewLoading.value = false;
  }, 200);
};

const getScuPreviewSuppliers = (data) => {
  if (!data || !data.products || data.products.length === 0) return [];
  
  // Group by modification (same logic as EANPageView)
  const groups = new Map();
  for (const p of data.products) {
    const pureName = (p.name || '').replace(/\s*—\s*[^—]+$/, '').trim() || p.sku || 'Unknown';
    if (!groups.has(pureName)) {
      groups.set(pureName, { name: pureName, suppliers: [] });
    }
    groups.get(pureName).suppliers.push(p);
  }
  
  // Sort suppliers by price and extract company names
  for (const mod of groups.values()) {
    mod.suppliers.sort((a, b) => (a.price || 0) - (b.price || 0));
    
    // Extract company names using companies cache
    mod.suppliers = mod.suppliers.map(s => ({
      ...s,
      company_name: getCompanyName(s.company_id)
    }));
  }
  
  return Array.from(groups.values());
};

// Current category path for tree/breadcrumbs (without EANPage slug)
const currentCategoryPath = computed(() => {
  // If showing EANPage, use its tree_path
  if (eanPageData.value && eanPageData.value.tree_path) {
    return eanPageData.value.tree_path;
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

// Grid / List view preference (persisted across pages)
const VIEW_KEY = 'makoshop_catalog_view';
const catalogView = ref(
  typeof localStorage !== 'undefined' ? localStorage.getItem(VIEW_KEY) || 'grid' : 'grid'
);
const setCatalogView = (v) => {
  catalogView.value = v;
  try { localStorage.setItem(VIEW_KEY, v); } catch {}
};

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
  // Do not reset eanPageData if already on an EAN page with data
  const isOnEANPage = eanPageData.value != null;

  // Check cache for EANPage data before making API call
  if (route.path && !route.query.page) {
    const cached = eanPageCache.value.get(route.path);
    if (cached && cached.ean_page && typeof cached.ean_page === 'object' && cached.ean_page.id) {
      // Use cached EANPage data
      if (!cached.category && (cached.ean_page.category || currentCategory.value)) {
        cached.category = cached.ean_page.category || currentCategory.value;
      }
      if (!cached.subcategories && (cached.ean_page.subcategories || rootCategories.value.length > 0)) {
        cached.subcategories = cached.ean_page.subcategories || [];
      }
      eanPageData.value = cached;
      if (cached.category_id || cached.ean_page.category_id) {
        const catId = cached.category_id || cached.ean_page.category_id;
        fetchCategoryPath(catId);
        currentCategory.value = cached.category || cached.ean_page.category || null;
      }
      loading.value = false;
      return;
    }
  }

  try {
    // Only read page from URL on navigation (path changed), not on filter changes
    const pathChanged = prevRoutePath.value !== route.path;
    if (pathChanged && route.query.page) {
      pagination.page = parseInt(route.query.page, 10);
    }
    prevRoutePath.value = route.path;

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

    function parseJSON(value) {
      if (typeof value !== "string") {
        return value;
      }

      try {
        // Strip invisible Unicode characters that break JSON parsing:
        // \u00A0 (non-breaking space), \u200B (zero-width space),
        // \uFEFF (BOM), \u2000-\u200A (various spaces), \u2028/\u2029 (line/paragraph sep)
        const cleaned = value
          .replace(/\u00A0/g, ' ')       // non-breaking space → regular space
          .replace(/\u200B/g, '')        // zero-width space
          .replace(/\uFEFF/g, '')        // BOM / zero-width no-break space
          .replace(/[\u2000-\u200A]/g, '') // various Unicode spaces
          .replace(/\u2028/g, '\n')      // line separator → newline
          .replace(/\u2029/g, '\n');     // paragraph separator → newline
        return JSON.parse(cleaned);
      } catch (e) {
        console.warn('[CatalogView] parseJSON failed even after cleanup:', e?.message?.slice(0, 200), 'raw preview:', String(value).slice(0, 200));
        return value;
      }
    }

    let data = parseJSON(response.data);
    console.log('[CatalogView] Fetched data:', data);
    console.log('[CatalogView] data.ean_page:', data.ean_page);

    // If the response is an EANPage, store it and render EANPageView
    if (data.ean_page && typeof data.ean_page === 'object' && data.ean_page.id) {
      console.log('[CatalogView] Detected EANPage, setting eanPageData for path:', route.path);
      // Ensure category info is present in the data passed to EANPageView
      if (!data.category && (data.ean_page.category || currentCategory.value)) {
        data.category = data.ean_page.category || currentCategory.value;
      }
      if (!data.subcategories && (data.ean_page.subcategories || rootCategories.value.length > 0)) {
        // subcategories might be in ean_page or we might have them from browsing
        data.subcategories = data.ean_page.subcategories || [];
      }
      
      eanPageData.value = data;
      // Cache the EANPage data for future hover previews and inline expansion
      const cacheKey = route.path;
      if (cacheKey) {
        eanPageCache.value.set(cacheKey, data);
        persistScuPageCache();
      }
      // Build category path via API for proper localized names
      if (data.category_id || data.ean_page.category_id) {
        const catId = data.category_id || data.ean_page.category_id;
        fetchCategoryPath(catId);
        // Store the current category with description
        currentCategory.value = data.category || data.ean_page.category || null;
      }
      return;
    }

    // If we were on an EANPage but the API returned a regular catalog, reset EANPage
    if (isOnEANPage) {
      console.log('[CatalogView] Resetting eanPageData (API returned catalog for path:', route.path, ')');
      eanPageData.value = null;
    }

    products.value = data.items || [];
    pagination.total = data.total || 0;
    const perPage = data.limit || pagination.per_page;
    pagination.total_pages = Math.ceil(pagination.total / perPage);
    categoryAttrs.value = data.category_attrs || [];
    
    // console.log('CatalogView processing regular products, data.category:', data.category);

    // Build category path via API for proper localized names
    if (data.category_id || (data.ean_page && data.ean_page.category_id)) {
      const catId = data.category_id || data.ean_page.category_id;
      fetchCategoryPath(catId);
    // Store the current category with description
    if (data.category) {
      currentCategory.value = data.category;
    } else if (data.ean_page && data.ean_page.category) {
      currentCategory.value = data.ean_page.category;
    }
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

// Navigate into a category — full reset like resetFilters
const navigateToCategory = (cat) => {
  filters.q = '';
  filters.price_min = '';
  filters.price_max = '';
  filters.sort = 'relevance';
  Object.keys(attrFilters).forEach(key => delete attrFilters[key]);
  pagination.page = 1;

  const slugs = buildCategoryPath(cat.id, rootCategories.value);
  let path = '/shop';
  if (slugs && slugs.length > 0) {
    path = '/shop/' + slugs.join('/');
  }

  router.push({ path, query: {} });
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
  console.log('[CatalogView] applyFilters called, current path:', route.path);
  pagination.page = 1;
  const query = { ...route.query };
  delete query.page;
  delete query.category_id;
  if (filters.q) query.q = filters.q; else delete query.q;
  if (filters.price_min) query.price_min = filters.price_min; else delete query.price_min;
  if (filters.price_max) query.price_max = filters.price_max; else delete query.price_max;
  if (filters.sort && filters.sort !== 'relevance') query.sort = filters.sort; else delete query.sort;

  // Save attribute filters to URL query
  for (const [key, values] of Object.entries(attrFilters)) {
    if (values.length > 0) {
      query[`attr_${key}`] = values.join(',');
    } else {
      delete query[`attr_${key}`];
    }
  }

  // Only replace route if query actually changed (prevents interfering with navigation)
  const oldQueryStr = JSON.stringify(route.query);
  const newQueryStr = JSON.stringify(query);
  if (oldQueryStr !== newQueryStr) {
    console.log('[CatalogView] applyFilters: query changed, replacing route');
    router.replace({ path: route.path, query });
  } else {
    console.log('[CatalogView] applyFilters: query unchanged, skipping router.replace');
  }
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

const formatPrice = (price, currency) => {
  const cur = currency || defaultCurrency.value || 'PLN';
  const localeMap = { ru: 'ru-RU', en: 'en-US', ua: 'uk-UA', pl: 'pl-PL' };
  const loc = localeMap[locale.value] || 'en-US';
  return new Intl.NumberFormat(loc, { style: 'currency', currency: cur }).format(price);
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

// Show the hero intro only on the clean home state:
// no search query, no selected category, and not on an EAN page.
const showHero = computed(() => {
  return !eanPageData.value && !filters.q && !currentCategory.value;
});



// Sync filters from route
const syncFiltersFromRoute = () => {
  isSyncingFiltersFromRoute.value = true;
  const oldQ = filters.q;
  filters.q = route.query.q || '';
  filters.price_min = route.query.price_min || '';
  filters.price_max = route.query.price_max || '';
  filters.sort = route.query.sort || 'relevance';

  // Only read page from URL on navigation (path changed), not on filter changes
  const pathChanged = prevRoutePath.value !== route.path;
  if (pathChanged) {
    if (route.query.page) {
      pagination.page = parseInt(route.query.page, 10);
    }
    // Reset attrFilters on navigation
    if (route.query.page || oldQ !== filters.q) {
      Object.keys(attrFilters).forEach(key => delete attrFilters[key]);
    }
    prevRoutePath.value = route.path;
  }

  if (oldQ !== filters.q) {
    console.log('[CatalogView] syncFiltersFromRoute: q changed from', oldQ, 'to', filters.q);
  }

  // Load attribute filters from URL (only when navigating)
  if (pathChanged) {
    Object.keys(route.query).forEach(key => {
      if (key.startsWith('attr_')) {
        const attrCode = key.slice(5);
        const values = route.query[key].split(',');
        attrFilters[attrCode] = values;
      }
    });
  }
  // Reset flag after next tick to allow Vue to process the changes
  setTimeout(() => {
    isSyncingFiltersFromRoute.value = false;
  }, 0);
};

onMounted(async () => {
  syncFiltersFromRoute();
  updatePerPage();

  // Load root categories for horizontal bar
  await fetchRootCategories();
  
  // Fetch companies for preview popups (only when animations are enabled)
  if (animationEnabled.value) {
    fetchCompanies();
  }
  
  buildPathFromUrl();

  // Use pre-rendered data if available (from SSR/proxy)
  if (typeof window !== 'undefined' && window.__INITIAL_DATA__) {
    const data = window.__INITIAL_DATA__;
    delete window.__INITIAL_DATA__; // consume once

    // If it's an EANPage
    if (data.ean_page && typeof data.ean_page === 'object' && data.ean_page.id) {
      eanPageData.value = data;
      // Build category path via API for proper localized names
      if (data.category_id || data.ean_page.category_id) {
        const catId = data.category_id || data.ean_page.category_id;
        fetchCategoryPath(catId);
        // Store the current category with description
        currentCategory.value = data.category || data.ean_page.category || null;
      }
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
    if (data.category_id || (data.ean_page && data.ean_page.category_id)) {
      const catId = data.category_id || data.ean_page.category_id;
      fetchCategoryPath(catId);
      // Store the current category with description
      currentCategory.value = data.category || (data.ean_page ? data.ean_page.category : null) || null;
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
    console.log('[CatalogView] Route watch fired, path:', route.path, 'eanPageData:', !!eanPageData.value);
    // If we're in the middle of an inline EAN transition, skip everything —
    // including filter syncing, which could trigger a competing navigation.
    if (isInlineScuTransition.value) {
      return;
    }

    syncFiltersFromRoute();
    buildPathFromUrl();

    // If we already have EANPage data for this path (from inline expansion), skip re-fetch
    if (eanPageData.value && route.path) {
      const cached = eanPageCache.value.get(route.path);
      if (cached && cached.ean_page && typeof cached.ean_page === 'object' && cached.ean_page.id) {
        // Already showing this EANPage inline — no need to re-fetch
        return;
      }
    }

    // Reset EANPage data when navigating to a new path that's not in cache
    // This ensures we don't show stale EANPage data when clicking on a new product
    if (eanPageData.value && route.path && !eanPageCache.value.has(route.path)) {
      console.log('[CatalogView] Route changed, resetting eanPageData for path:', route.path);
      eanPageData.value = null;
    }

    console.log('[CatalogView] Calling fetchProducts for path:', route.path);
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

// When animations get re-enabled, ensure companies are loaded for previews
watch(
  animationEnabled,
  (enabled) => {
    if (enabled) {
      fetchCompanies();
    }
  }
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

// Monotonic token so only the latest inline transition resets the guard flag
// (protects against rapid double-clicks racing each other).
let inlineScuNavToken = 0;

// Navigate to EAN page (landing page for product group)
// When animations are enabled and data is available, render EANPageView inline
// with a smooth transition instead of a full router navigation.
const goToEANPage = async (product) => {
  const cacheKey = getScuKey(product);
  const myToken = ++inlineScuNavToken;

  // Clear any pending preview timers when clicking
  clearTimeout(scuPreviewTimer.value);
  clearTimeout(scuPreviewFetchTimer.value);
  hoveredScuProduct.value = null;
  scuPreviewData.value = null;
  scuPreviewLoading.value = false;

  // Try to use cached data for inline expansion (animations enabled only)
  if (animationEnabled.value && cacheKey && eanPageCache.value.has(cacheKey)) {
    const data = eanPageCache.value.get(cacheKey);
    if (data && data.ean_page && typeof data.ean_page === 'object' && data.ean_page.id) {
      // Ensure category info is present
      if (!data.category && (data.ean_page.category || currentCategory.value)) {
        data.category = data.ean_page.category || currentCategory.value;
      }
      if (!data.subcategories && (data.ean_page.subcategories || rootCategories.value.length > 0)) {
        data.subcategories = data.ean_page.subcategories || [];
      }
      // Set flag to prevent route watch from re-fetching
      isInlineScuTransition.value = true;

      // Set EANPage data BEFORE router.push to ensure it's ready
      eanPageData.value = data;

      try {
        // Update URL (router.push is async, but we've already set the data).
        // Scroll to top is handled by the router scrollBehavior — no manual
        // scrollTo here (it raced the router and caused a visible jump).
        await router.push({ path: cacheKey });
      } catch (e) {
        console.error('Failed to navigate to EAN page:', e);
      } finally {
        // Only the latest navigation resets the guard flag
        if (myToken === inlineScuNavToken) {
          isInlineScuTransition.value = false;
        }
      }
      return;
    }
  }

  // Fallback: regular navigation (animations off or no cached data)
  if (product.seo_url) {
    console.log('[CatalogView] goToEANPage: navigating to seo_url:', product.seo_url);
    router.push({ path: product.seo_url });
    return;
  }
  if (product.slug) {
    router.push({ path: '/shop/' + product.slug });
    return;
  }
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

  <!-- Crossfade container: catalog <-> EAN page.
       The leaving element is taken out of flow (absolute inset-0) so the two
       views crossfade in place instead of stacking vertically. -->
  <div v-else class="relative">
    <!-- Render EANPageView if API returned an EANPage -->
    <Transition
      enter-active-class="transition duration-600 ease-out"
      enter-from-class="opacity-0 scale-95"
      enter-to-class="opacity-100 scale-100"
      leave-active-class="transition duration-400 ease-in absolute inset-0"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <EANPageView v-if="eanPageData" :data="eanPageData" key="ean-page" />
    </Transition>

    <Transition
      enter-active-class="transition duration-400 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-300 ease-in absolute inset-0"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div v-if="!eanPageData" class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6" key="catalog">

    <!-- Branding: main page banner (home only) -->
    <div v-if="showHero && homeBannerEl" class="mb-6">
      <BrandingSlot slot-name="home_banner" />
    </div>

    <!-- Hero intro (home only) -->
    <div
      v-if="showHero"
      class="hero-glow relative overflow-hidden rounded-2xl border border-line bg-gradient-to-br from-accent/10 via-surface to-surface-2 py-10 sm:py-14 px-6 text-center mb-6"
    >
      <h1 class="text-3xl sm:text-4xl lg:text-5xl font-extrabold tracking-tight text-ink mb-4 leading-tight">
        {{ t('catalog.hero_headline') }}
      </h1>
      <p class="text-lg sm:text-xl text-ink-2 max-w-2xl mx-auto mb-6 leading-relaxed">
        {{ t('catalog.hero_sub') }}
      </p>
      <p class="inline-flex items-center gap-2 text-sm font-semibold text-accent bg-accent/10 px-4 py-2 rounded-full">
        {{ t('catalog.hero_tagline') }}
      </p>
    </div>

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
              ? 'border-orange-500 ring-2 ring-orange-200 shadow-md'
              : 'border-line hover:border-orange-300 hover:shadow-md'
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
                  ? 'bg-orange-50 dark:bg-orange-900/30'
                  : 'bg-surface'
              ]"
            >
              <div
                :class="[
                  'text-xs font-medium text-left line-clamp-2',
                  isRootCategoryActive(cat)
                    ? 'text-orange-700 dark:text-orange-300 font-semibold'
                    : 'text-ink'
                ]"
              >
                {{ catName(cat) }}
              </div>
            </div>
          </div>
      </div>

      <!-- Categories panel with description and subcategories -->
      <div class="mt-2 bg-surface rounded-xl border border-line">
        <div class="p-4">
          <!-- Breadcrumbs-style path -->
          <div class="flex items-center flex-wrap gap-1 text-xs text-ink-3 mb-2">
            <span
              class="link-btn cursor-pointer"
              @click="navigateToCategory({ id: '', slug: '' })"
            >
              {{ t('catalog.all_products') }}
            </span>
            <template v-for="(cat, idx) in categoryBrowsePath" :key="cat.id">
              <span class="text-ink-3">/</span>
              <span class="relative">
                <span
                  class="link-btn cursor-pointer"
                  :class="idx === categoryBrowsePath.length - 1 ? 'font-semibold text-ink' : ''"
                  @click="navigateToCategory(cat)"
                  @mouseenter="showBreadcrumbChildren(cat)"
                  @mouseleave="hideBreadcrumbChildren()"
                >
                  {{ catName(cat) }}
                </span>
                <!-- Children popup -->
                <Transition
                  enter-active-class="transition duration-150 ease-out"
                  leave-active-class="transition duration-150 ease-in"
                  enter-from-class="opacity-0 -translate-y-1"
                  leave-to-class="opacity-0"
                >
                  <div
                    v-if="hoveredBreadcrumbId === cat.id && getBreadcrumbChildren(cat).length > 0"
                    class="absolute left-0 top-full mt-1 z-50 min-w-[180px] max-w-xs bg-surface border border-line rounded-lg shadow-lg"
                    @mouseenter="showBreadcrumbChildren(cat)"
                    @mouseleave="hideBreadcrumbChildren()"
                  >
                    <div class="py-1 max-h-60 overflow-y-auto">
                      <button
                        v-for="child in getBreadcrumbChildren(cat)"
                        :key="child.id"
                        @click="navigateToCategory(child)"
                        class="w-full text-left px-3 py-1.5 text-xs text-ink-2 hover:bg-surface-2 hover:text-ink transition-colors flex items-center justify-between gap-2"
                      >
                        <span class="truncate">{{ catName(child) }}</span>
                        <span
                          v-if="child.children && child.children.length > 0"
                          class="text-ink-3 text-[10px]"
                        >
                          ▸
                        </span>
                      </button>
                    </div>
                  </div>
                </Transition>
              </span>
            </template>
          </div>

          <!-- Branding: category banner -->
          <div v-if="categoryBannerEl" class="mb-3">
            <BrandingSlot slot-name="category_banner" />
          </div>

          <!-- Current category header with full description and image -->
          <div class="mb-3">
            <div class="relative overflow-hidden rounded-2xl border border-line bg-gradient-to-br from-accent/10 via-surface to-surface-2 px-5 py-4 sm:px-6 sm:py-5">
              <div class="flex flex-col lg:flex-row gap-4 items-start lg:items-center">
                <!-- Category name and full description -->
                <div class="flex-1 min-w-0">
                  <!-- Warm eyebrow: journey/destination cue -->
                  <span
                    v-if="currentBrowseCategory"
                    class="inline-flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.2em] text-accent mb-2"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <circle cx="12" cy="12" r="9"/>
                      <path d="M15.5 8.5l-2 5-5 2 2-5z" stroke-linejoin="round"/>
                    </svg>
                    {{ t('catalog.explore') }}
                  </span>
                  <h2 class="text-2xl sm:text-3xl font-bold tracking-tight text-ink leading-tight">
                    {{ currentBrowseCategory ? catName(currentBrowseCategory) : t('catalog.all_products') }}
                  </h2>
                  <p
                    v-if="currentBrowseCategory && catDescription(currentBrowseCategory)"
                    class="mt-2 text-sm sm:text-base text-ink-2 leading-relaxed max-w-prose"
                  >
                    {{ catDescription(currentBrowseCategory) }}
                  </p>
                </div>
                <!-- Category image on the right -->
                <div
                  v-if="currentBrowseCategory && (isValidImage(currentBrowseCategory.image_light_url) || isValidImage(currentBrowseCategory.image_dark_url))"
                  class="flex-shrink-0 w-36 sm:w-48 aspect-square relative rounded-xl overflow-hidden shadow-md border border-line"
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
                  ? 'border-orange-500 bg-orange-50 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300'
                  // Has children — slightly highlighted
                  : sub.children && sub.children.length > 0
                    ? 'border-line bg-surface-2 text-ink hover:border-orange-300 hover:text-orange-600 hover:bg-orange-50 dark:hover:bg-slate-600'
                    // Leaf category
                    : 'border-line text-ink-2 hover:border-orange-300 hover:text-orange-600 hover:bg-orange-50 dark:hover:bg-slate-700'
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
          class="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-[11px] font-semibold bg-amber-50 text-amber-700 border border-amber-200 theme-dark:bg-amber-900/30 theme-dark:text-amber-300 theme-dark:border-amber-700/60"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3 text-amber-500 theme-dark:text-amber-400" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
            <path fill-rule="evenodd" d="M11.3 1.046A1 1 0 0112 2v5h4a1 1 0 01.82 1.573l-7 10A1 1 0 018 18v-5H4a1 1 0 01-.82-1.573l7-10a1 1 0 011.12-.38z" clip-rule="evenodd" />
          </svg>
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
          class="search-field w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-orange-500"
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
              class="search-field w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-orange-500"
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
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-orange-600 text-white border-orange-600"
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
              class="mt-1 text-xs text-orange-600 hover:underline"
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
                class="w-full mb-1.5 px-2 py-0.5 border border-line rounded text-[11px] focus:outline-none focus:ring-1 focus:ring-orange-500"
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
          <div class="flex items-center gap-2">
            <ViewToggle v-model="catalogView" @update:model-value="setCatalogView" />
            <select
              v-model="filters.sort"
              :aria-label="t('catalog.sort_by')"
              class="px-3 py-1.5 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-accent bg-surface"
            >
              <option value="relevance">{{ t('catalog.sort_relevance') }}</option>
              <option value="price_asc">{{ t('catalog.sort_price_asc') }}</option>
              <option value="price_desc">{{ t('catalog.sort_price_desc') }}</option>
            </select>
          </div>
        </div>

        <!-- Skeleton while loading -->
        <div v-if="loading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3 sm:gap-4" :class="{ 'lg:grid-cols-4': hasAttrsOrBrands, 'lg:grid-cols-5 xl:grid-cols-6': !hasAttrsOrBrands }" aria-hidden="true">
          <SkeletonCard v-for="i in 12" :key="i" />
        </div>

        <div v-else-if="products.length === 0" class="bg-surface rounded-lg border border-line">
          <EmptyState
            icon="search"
            :title="t('catalog.no_results_title')"
            :message="t('catalog.no_results_message')"
          >
            <button @click="resetFilters" class="btn btn-secondary">
              {{ t('catalog.reset_filters') }}
            </button>
          </EmptyState>
        </div>

        <!-- Grid view -->
        <div
          v-else-if="catalogView === 'grid'"
          class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3 sm:gap-4"
          :class="{ 'lg:grid-cols-4': hasAttrsOrBrands, 'lg:grid-cols-5 xl:grid-cols-6': !hasAttrsOrBrands }"
        >
          <div
            v-for="product in products"
            :key="product.id"
            class="relative h-full"
            @mouseenter="showScuPreview(product)"
            @mouseleave="hideScuPreview()"
          >
            <ProductCard
              :product="product"
              :format-price="formatPrice"
              :view="'grid'"
              :enable-image-fade="animationEnabled"
              @click="goToEANPage(product)"
            />
            <!-- EAN Preview Popup (only when animations enabled) -->
            <Transition
              v-if="animationEnabled"
              enter-active-class="transition duration-400 ease-out"
              leave-active-class="transition duration-300 ease-in"
              enter-from-class="opacity-0 -translate-y-1"
              leave-to-class="opacity-0"
            >
              <div
                v-if="hoveredScuProduct?.id === product.id && (scuPreviewLoading || scuPreviewData)"
                class="absolute left-0 top-full z-50 w-72 bg-surface border border-line rounded-xl shadow-xl overflow-hidden"
                @mouseenter="showScuPreview(product)"
                @mouseleave="hideScuPreview()"
              >
                <!-- Loading skeleton -->
                <div v-if="scuPreviewLoading" class="p-4 space-y-3">
                  <div class="h-4 w-3/4 bg-surface-3 rounded animate-pulse"></div>
                  <div class="space-y-2">
                    <div v-for="i in 3" :key="i" class="h-8 bg-surface-3 rounded animate-pulse"></div>
                  </div>
                </div>
                <!-- Preview content -->
                <div v-else-if="scuPreviewData" class="p-4">
                  <h4 class="font-semibold text-sm text-ink mb-3 line-clamp-1">
                    {{ scuPreviewData.ean_page?.title || product.title || product.name }}
                  </h4>
                  <div class="space-y-2 max-h-48 overflow-y-auto">
                    <div
                      v-for="(mod, modIdx) in getScuPreviewSuppliers(scuPreviewData)"
                      :key="modIdx"
                      class="border-b border-line pb-2 last:border-0 last:pb-0"
                    >
                      <div v-if="getScuPreviewSuppliers(scuPreviewData).length > 1" class="text-xs font-medium text-ink-2 mb-1">
                        {{ mod.name }}
                      </div>
                      <div
                        v-for="supplier in mod.suppliers.slice(0, 3)"
                        :key="supplier.id"
                        class="flex items-center justify-between text-xs py-1"
                      >
                        <span class="text-ink-2 truncate flex-1">
                          {{ supplier.company_name || supplier.name?.split('—')?.pop()?.trim() || 'Supplier' }}
                        </span>
                        <span class="font-semibold text-accent ml-2">
                          {{ formatPrice(supplier.price) }}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </Transition>
          </div>
        </div>

        <!-- List view -->
        <div v-else class="space-y-3">
          <div
            v-for="product in products"
            :key="product.id"
            class="relative h-full"
            @mouseenter="showScuPreview(product)"
            @mouseleave="hideScuPreview()"
          >
            <ProductCard
              :product="product"
              :format-price="formatPrice"
              :view="'list'"
              :enable-image-fade="animationEnabled"
              @click="goToEANPage(product)"
            />
            <!-- EAN Preview Popup (List view, only when animations enabled) -->
            <Transition
              v-if="animationEnabled"
              enter-active-class="transition duration-400 ease-out"
              leave-active-class="transition duration-300 ease-in"
              enter-from-class="opacity-0 -translate-y-1"
              leave-to-class="opacity-0"
            >
              <div
                v-if="hoveredScuProduct?.id === product.id && (scuPreviewLoading || scuPreviewData)"
                class="absolute left-0 top-full z-50 w-72 bg-surface border border-line rounded-xl shadow-xl overflow-hidden"
                @mouseenter="showScuPreview(product)"
                @mouseleave="hideScuPreview()"
              >
                <!-- Loading skeleton -->
                <div v-if="scuPreviewLoading" class="p-4 space-y-3">
                  <div class="h-4 w-3/4 bg-surface-3 rounded animate-pulse"></div>
                  <div class="space-y-2">
                    <div v-for="i in 3" :key="i" class="h-8 bg-surface-3 rounded animate-pulse"></div>
                  </div>
                </div>
                <!-- Preview content -->
                <div v-else-if="scuPreviewData" class="p-4">
                  <h4 class="font-semibold text-sm text-ink mb-3 line-clamp-1">
                    {{ scuPreviewData.ean_page?.title || product.title || product.name }}
                  </h4>
                  <div class="space-y-2 max-h-48 overflow-y-auto">
                    <div
                      v-for="(mod, modIdx) in getScuPreviewSuppliers(scuPreviewData)"
                      :key="modIdx"
                      class="border-b border-line pb-2 last:border-0 last:pb-0"
                    >
                      <div v-if="getScuPreviewSuppliers(scuPreviewData).length > 1" class="text-xs font-medium text-ink-2 mb-1">
                        {{ mod.name }}
                      </div>
                      <div
                        v-for="supplier in mod.suppliers.slice(0, 3)"
                        :key="supplier.id"
                        class="flex items-center justify-between text-xs py-1"
                      >
                        <span class="text-ink-2 truncate flex-1">
                          {{ supplier.company_name || supplier.name?.split('—')?.pop()?.trim() || 'Supplier' }}
                        </span>
                        <span class="font-semibold text-accent ml-2">
                          {{ formatPrice(supplier.price) }}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </Transition>
          </div>
        </div>

        <div v-if="pagination.total_pages > 1" class="flex justify-center items-center gap-2 mt-6">
          <button
            @click="goToPage(pagination.page - 1)"
            :disabled="pagination.page <= 1"
            class="btn btn-secondary btn-sm disabled:opacity-40"
          >
            {{ t('catalog.back') }}
          </button>
          <span class="px-3 py-1.5 text-sm text-ink-2">
            {{ t('catalog.page_of', { page: pagination.page, total: pagination.total_pages }) }}
          </span>
          <button
            @click="goToPage(pagination.page + 1)"
            :disabled="pagination.page >= pagination.total_pages"
            class="btn btn-secondary btn-sm disabled:opacity-40"
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
              class="search-field w-full px-3 py-2 border border-line rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-orange-500"
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
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-orange-600 text-white border-orange-600"
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
                    ? 'bg-orange-600 text-white border-orange-600'
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
    </Transition>
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
