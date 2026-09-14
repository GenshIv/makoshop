<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useSeo } from '../composables/useSeo';
import ProductCard from '../components/ProductCard.vue';
import SkeletonCard from '../components/SkeletonCard.vue';
import EmptyState from '../components/EmptyState.vue';

const { t, locale } = useI18n();
const route = useRoute();
const router = useRouter();

const alias = ref(null);
const results = ref([]);
const loading = ref(true);
const error = ref(null);

const pagination = reactive({ page: 1, per_page: 50, total: 0, total_pages: 0 });

const defaultCurrency = ref('PLN');

const formatPrice = (price, currency) => {
  const cur = currency || defaultCurrency.value || 'PLN';
  const localeMap = { ru: 'ru-RU', en: 'en-US', ua: 'uk-UA', pl: 'pl-PL' };
  const loc = localeMap[locale.value] || 'en-US';
  return new Intl.NumberFormat(loc, { style: 'currency', currency: cur }).format(price);
};

// Parse JSON-LD if provided
const parsedJsonLd = computed(() => {
  if (!alias.value?.json_ld) return null;
  try {
    const parsed = JSON.parse(alias.value.json_ld);
    if (Array.isArray(parsed)) return parsed[0];
    return parsed;
  } catch (e) {
    console.error('Failed to parse JSON-LD:', e);
    return null;
  }
});

// Set SEO with alias data
useSeo({
  title: computed(() => alias.value?.seo_title || alias.value?.title || t('pages.search')),
  description: computed(() => alias.value?.seo_description || alias.value?.description || ''),
  image: computed(() => alias.value?.og_image || null),
  jsonLd: parsedJsonLd,
});

const loadProducts = async (page) => {
  loading.value = true;
  error.value = null;

  // Check for SSR initial data first (only on first load, page 1)
  if (typeof window !== 'undefined' && window.__INITIAL_DATA__ && page === 1) {
    const data = window.__INITIAL_DATA__;
    delete window.__INITIAL_DATA__; // consume once

    alias.value = data.alias || null;
    results.value = data.items || [];
    pagination.total = data.total || 0;
    const perPage = data.limit || pagination.per_page;
    pagination.total_pages = Math.ceil(pagination.total / perPage);
    loading.value = false;
    return;
  }

  // Fetch via API (SPA navigation or non-first page)
  try {
    if (!alias.value) {
      const aliasRes = await api.get(`/search-aliases/${route.params.slug}`);
      alias.value = aliasRes.data;
    }

    const searchParams = { page: page };
    // Pass user query params (sort, etc.) to backend
    if (route.query.sort) searchParams.sort = route.query.sort;

    let url = `/search/${route.params.slug}`;
    const response = await api.get(url, { params: searchParams });
    
    results.value = response.data.items || [];
    pagination.total = response.data.total || 0;
    const perPage = response.data.limit || pagination.per_page;
    pagination.total_pages = Math.ceil(pagination.total / perPage);
  } catch (e) {
    error.value = t('common.not_found');
    console.error('Failed to load search alias page:', e);
  } finally {
    loading.value = false;
  }
};

const goToPage = (page) => {
  if (page < 1 || page > pagination.total_pages) return;
  pagination.page = page;
  router.push({ path: route.path, query: { ...route.query, page: page.toString() } });
};

// Watch for page and sort query param changes
watch(
  () => [route.query.page, route.query.sort],
  ([newPage, newSort]) => {
    const page = newPage ? parseInt(newPage, 10) : 1;
    if (page !== pagination.page) {
      loadProducts(page);
    }
  }
);

onMounted(async () => {
  const page = route.query.page ? parseInt(route.query.page, 10) : 1;
  await loadProducts(page);
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 py-6">
    <!-- Loading -->
    <div v-if="loading" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      <SkeletonCard v-for="i in 8" :key="i" />
    </div>

    <!-- Error -->
    <div v-else-if="error" class="flex items-center justify-center py-20 text-ink-3">
      {{ error }}
    </div>

    <!-- Results -->
    <template v-else>
      <div v-if="pagination.total > 0" class="mb-4 text-sm text-ink-3">
        {{ t('catalog.found', { count: pagination.total }) }}
      </div>

      <div v-if="results.length === 0" class="bg-surface rounded-lg shadow-sm">
        <EmptyState icon="search" :title="t('catalog.no_results_title')" />
      </div>

      <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        <ProductCard
          v-for="item in results"
          :key="item.ean || item.id"
          :product="item"
          :format-price="formatPrice"
        />
      </div>

      <!-- Pagination -->
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
    </template>
  </div>
</template>
