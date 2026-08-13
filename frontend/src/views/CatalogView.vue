<script setup>
import { ref, reactive, onMounted, watch, computed, defineAsyncComponent } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import Breadcrumbs from '../components/Breadcrumbs.vue';

// Lazy-load SCUPageView to avoid circular imports
const SCUPageView = defineAsyncComponent(() => import('../views/SCUPageView.vue'));

const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const products = ref([]);
const categoryAttrs = ref([]);
const categoryPath = ref([]); // [{id, name}, ...]
const pagination = reactive({ page: 1, per_page: 50, total: 0, total_pages: 0 });
const loading = ref(false);
const error = ref(null);
const maintenanceMode = ref(false);

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
    // Не сбрасываем page, если он задан в URL
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
  // Не сбрасываем scuPageData, если уже на SCU-странице и данные есть
  const isOnSCUPage = scuPageData.value != null;
  try {
    // Если page задан в URL — используем его и синхронизируем pagination
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
    const data = response.data;

    // Если ответ — SCUPage, запоминаем и рендерим SCUPageView
    if (data.page && typeof data.page === 'object' && data.page.scu) {
      scuPageData.value = data;
      return;
    }

    // Если мы были на SCUPage, а теперь API вернул обычный каталог — сбрасываем SCUPage
    if (isOnSCUPage) {
      scuPageData.value = null;
    }

    products.value = data.items || [];
    pagination.total = data.total || 0;
    const perPage = data.limit || pagination.per_page;
    pagination.total_pages = Math.ceil(pagination.total / perPage);
    categoryAttrs.value = data.category_attrs || [];

    // Use tree_path from API for breadcrumbs if available
    if (Array.isArray(data.tree_path) && data.tree_path.length > 0) {
      categoryPath.value = data.tree_path.map((slug) => ({ slug, name: slug }));
    } else {
      categoryPath.value = [];
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
    const path = [{ id: cat.id, name_ru: cat.name_ru, name_ua: cat.name_ua, name_pl: cat.name_pl, name_en: cat.name_en, slug: cat.slug }];
    // Walk up the tree
    let currentId = cat.parent_id;
    while (currentId) {
      const parentResponse = await api.get(`/admin/categories/${currentId}`);
      const parent = parentResponse.data;
      if (!parent) break;
      path.unshift({ id: parent.id, name_ru: parent.name_ru, name_ua: parent.name_ua, name_pl: parent.name_pl, name_en: parent.name_en, slug: parent.slug });
      currentId = parent.parent_id;
    }
    categoryPath.value = path;
  } catch (e) {
    console.error('Failed to fetch category path:', e);
    categoryPath.value = [];
  }
};

const toggleAttrFilter = (code, value, checked) => {
  if (!attrFilters[code]) attrFilters[code] = [];
  if (checked) {
    if (!attrFilters[code].includes(value)) attrFilters[code].push(value);
  } else {
    attrFilters[code] = attrFilters[code].filter(v => v !== value);
    if (attrFilters[code].length === 0) delete attrFilters[code];
  }
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
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

// Возвращает строку параметров в одну строку через ";"
const getAttributesString = (attrs) => {
  if (!attrs || typeof attrs !== 'object') return '';
  const parts = [];
  const keys = Object.keys(attrs);
  for (const key of keys.slice(0, 4)) {
    const value = attrs[key];
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

    // Use tree_path from API for breadcrumbs if available
    if (Array.isArray(data.tree_path) && data.tree_path.length > 0) {
      categoryPath.value = data.tree_path.map((slug) => ({ slug, name: slug }));
    } else {
      categoryPath.value = [];
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

const goToPage = (page) => {
  if (page < 1 || page > pagination.total_pages) return;
  pagination.page = page;
  router.push({ path: route.path, query: { ...route.query, page: page.toString() } });
  // fetchProducts() будет вызван watch(route.query)
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

// Russian pluralization: 1 вариант, 2-4 варианта, 5+ вариантов
const pluralize = (n, one, few, many) => {
  const abs = Math.abs(n) % 100;
  const lastDigit = abs % 10;
  if (abs > 10 && abs < 20) return many;
  if (lastDigit === 1) return one;
  if (lastDigit >= 2 && lastDigit <= 4) return few;
  return many;
};

defineOptions({ name: 'CatalogView' });
</script>

<template>
  <!-- Maintenance mode screen -->
  <div v-if="maintenanceMode" class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="max-w-md text-center p-6 bg-white rounded-xl shadow-sm">
      <h1 class="text-2xl font-bold mb-3 text-gray-800">{{ t('maintenance.title') }}</h1>
      <p class="text-gray-600 mb-4">
        {{ t('maintenance.message') }}
      </p>
      <button
        @click="fetchProducts"
        class="px-4 py-2 text-sm bg-indigo-600 text-white rounded-lg hover:bg-indigo-700"
      >
        {{ t('maintenance.try_again') }}
      </button>
    </div>
  </div>

  <!-- Render SCUPageView if API returned an SCUPage -->
  <SCUPageView v-else-if="scuPageData" :data="scuPageData" />

  <div v-else class="max-w-[1920px] mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Breadcrumbs -->
    <Breadcrumbs :categories="categoryPath" />

    <h1 class="text-2xl font-bold mb-4">{{ pageTitle }}</h1>

    <div v-if="error" class="mb-4 p-3 bg-red-50 text-red-700 rounded-lg text-sm">
      {{ error }}
    </div>

    <div class="flex gap-6">
      <!-- Sidebar: Filters -->
      <aside class="w-64 flex-shrink-0 hidden md:block">
        <div class="bg-white rounded-lg shadow-sm border border-gray-200 p-4 space-y-4">
          <!-- Search -->
          <div>
            <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wide mb-1">Поиск</label>
            <input
              v-model="filters.q"
              type="text"
              placeholder="Название товара..."
              class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
            />
          </div>

          <!-- Price range -->
          <div>
            <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wide mb-1">Цена</label>
            <div class="flex gap-2">
              <input
                v-model="filters.price_min"
                type="number"
                placeholder="От"
                class="w-1/2 px-2 py-1.5 border border-gray-300 rounded-lg text-sm"
              />
              <input
                v-model="filters.price_max"
                type="number"
                placeholder="До"
                class="w-1/2 px-2 py-1.5 border border-gray-300 rounded-lg text-sm"
              />
            </div>
          </div>

          <!-- Brands -->
          <div v-if="visibleBrands.length > 0" class="border-t pt-3">
            <label class="block text-xs font-semibold text-gray-500 uppercase tracking-wide mb-1">Бренд</label>
            <div class="space-y-1">
              <label
                v-for="brand in visibleBrands"
                :key="brand"
                class="flex items-center text-sm py-0.5 cursor-pointer hover:bg-gray-50 rounded px-1 -ml-1"
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
              <label class="text-xs font-semibold text-gray-500 uppercase tracking-wide">
                {{ humanizeAttrName(attr.name || attr.code) }}
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
                {{ tag }}
              </button>
            </div>

            <!-- Visible unselected tags (up to 7) -->
            <div class="flex flex-wrap gap-1">
              <button
                v-for="tag in visibleUnselectedTags(attr)"
                :key="tag"
                @click="toggleAttrFilter(attr.code, tag, true)"
                class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-gray-50 text-gray-700 border-gray-200 hover:bg-gray-100"
              >
                {{ tag }}
              </button>
            </div>

            <!-- Show more link -->
            <button
              v-if="hasMoreUnselectedTags(attr)"
              @click="toggleAttrExpanded(attr.code)"
              class="mt-1 text-xs text-indigo-600 hover:underline"
            >
              {{ attrExpanded[attr.code] ? 'Скрыть' : 'Показать ещё (' + hiddenUnselectedTags(attr).length + ')' }}
            </button>

            <!-- Expanded area with search + scrollable tags -->
            <div
              v-if="hasMoreUnselectedTags(attr) && attrExpanded[attr.code]"
              class="mt-1 border border-gray-200 rounded p-1.5 max-h-48 overflow-y-auto bg-gray-50"
            >
              <!-- Search -->
              <input
                v-model="attrSearch[attr.code]"
                type="text"
                placeholder="Поиск..."
                class="w-full mb-1.5 px-2 py-0.5 border border-gray-300 rounded text-[10px] focus:outline-none focus:ring-1 focus:ring-indigo-500"
              />
              <!-- Tags -->
              <div class="flex flex-wrap gap-1">
                <button
                  v-for="tag in hiddenUnselectedTags(attr)"
                  :key="tag"
                  @click="toggleAttrFilter(attr.code, tag, true)"
                  class="inline-flex items-center px-2 py-0.5 rounded-full text-xs border transition cursor-pointer bg-gray-50 text-gray-700 border-gray-200 hover:bg-gray-100"
                >
                  {{ tag }}
                </button>
              </div>
            </div>
          </div>

          <!-- Reset -->
          <button
            @click="resetFilters"
            class="w-full px-3 py-2 text-sm text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50 transition"
          >
            {{ t('catalog.reset_filters') }}
          </button>
        </div>
      </aside>

      <!-- Products grid -->
      <div class="flex-1">
        <div class="flex flex-col sm:flex-row items-start sm:items-center justify-between mb-4 gap-2">
          <div class="text-sm text-gray-600">
            Найдено: <span class="font-medium text-gray-800">{{ pagination.total }}</span> товаров
          </div>
          <select
            v-model="filters.sort"
            class="px-3 py-1.5 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 bg-white"
          >
            <option value="relevance">По релевантности</option>
            <option value="price_asc">Цена: по возрастанию</option>
            <option value="price_desc">Цена: по убыванию</option>
            <option value="name_asc">Название: А-Я</option>
            <option value="created_desc">Новинки</option>
          </select>
        </div>

        <div v-if="loading" class="flex justify-center py-12">
          <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
        </div>

        <div v-else-if="products.length === 0" class="text-center py-12 text-gray-500">
          <p class="mb-2">Товары не найдены</p>
          <button @click="resetFilters" class="text-indigo-600 hover:underline text-sm">
            {{ t('catalog.reset_filters') }}
          </button>
        </div>

        <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-3">
          <div
            v-for="product in products"
            :key="product.id"
            class="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden hover:shadow-md hover:border-indigo-200 transition cursor-pointer relative"
            @click="goToSCUPage(product)"
          >
            <span v-if="product.promoted" class="absolute top-1.5 left-1.5 z-10 bg-yellow-400 text-yellow-900 text-[10px] font-semibold px-1.5 py-0.5 rounded-full">
              Реклама
            </span>
            <span v-if="product.product_count && product.product_count > 1" class="absolute top-1.5 right-1.5 z-10 bg-indigo-600 text-white text-[10px] font-semibold px-1.5 py-0.5 rounded-full">
              {{ product.product_count }} {{ pluralize(product.product_count, 'вариант', 'варианта', 'вариантов') }}
            </span>

            <div class="aspect-[4/3] bg-gray-100 flex items-center justify-center">
              <img
                v-if="product.images?.length"
                :src="product.images[0]"
                :alt="product.name"
                class="w-full h-full object-cover"
              />
              <span v-else class="text-gray-400 text-xs">Нет фото</span>
            </div>

            <div class="p-2 space-y-0.5">
              <!-- Название товара -->
              <h3 class="font-semibold text-sm line-clamp-2 text-gray-900">{{ product.title || product.name }}</h3>

              <!-- Бренд/производитель -->
              <div v-if="product.brand" class="text-[11px] text-gray-500">{{ product.brand }}</div>

              <!-- Параметры (если есть) -->
              <div v-if="product.attributes && Object.keys(product.attributes).length" class="text-[10px] text-gray-500 truncate">
                {{ getAttributesString(product.attributes) }}
              </div>

              <!-- Цена и рейтинг -->
              <div class="flex items-center justify-between pt-0.5">
                <span class="font-bold text-sm text-indigo-600">{{ formatPrice(product.price || product.min_price) }}</span>
                <span v-if="product.avg_rating" class="text-xs text-yellow-500">
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
            class="px-3 py-1.5 border border-gray-300 rounded-lg text-sm disabled:opacity-40 hover:bg-gray-50 transition"
          >
            ← Назад
          </button>
          <span class="px-3 py-1.5 text-sm text-gray-600">
            Страница {{ pagination.page }} из {{ pagination.total_pages }}
          </span>
          <button
            @click="goToPage(pagination.page + 1)"
            :disabled="pagination.page >= pagination.total_pages"
            class="px-3 py-1.5 border border-gray-300 rounded-lg text-sm disabled:opacity-40 hover:bg-gray-50 transition"
          >
            Далее →
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
