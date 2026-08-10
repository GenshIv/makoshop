<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

const overview = ref(null);
const ordersAnalytics = ref(null);
const productsAnalytics = ref(null);
const searchQueries = ref([]);
const loading = ref(true);

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const fetchAnalytics = async () => {
  loading.value = true;
  try {
    const [ov, orders, products, queries] = await Promise.allSettled([
      api.get('/admin/analytics/overview'),
      api.get('/admin/analytics/orders'),
      api.get('/admin/analytics/products'),
      api.get('/admin/analytics/search-queries'),
    ]);

    if (ov.status === 'fulfilled') overview.value = ov.value.data;
    if (orders.status === 'fulfilled') ordersAnalytics.value = orders.value.data;
    if (products.status === 'fulfilled') productsAnalytics.value = products.value.data;
    if (queries.status === 'fulfilled') searchQueries.value = queries.value.data.items || queries.value.data || [];
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchAnalytics);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.analytics') }}</h1>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else class="space-y-6">
      <!-- Overview -->
      <div v-if="overview" class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-3">{{ t('admin.overview') }}</h2>
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div><div class="text-gray-500">{{ t('admin.users') }}</div><div class="font-bold">{{ overview.users || 0 }}</div></div>
          <div><div class="text-gray-500">{{ t('admin.products') }}</div><div class="font-bold">{{ overview.products || 0 }}</div></div>
          <div><div class="text-gray-500">{{ t('admin.orders') }}</div><div class="font-bold">{{ overview.orders || 0 }}</div></div>
          <div><div class="text-gray-500">{{ t('admin.revenue') }}</div><div class="font-bold text-green-600">{{ formatPrice(overview.revenue || 0) }}</div></div>
        </div>
      </div>

      <!-- Orders analytics -->
      <div v-if="ordersAnalytics" class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-3">{{ t('admin.orders') }}</h2>
        <div class="text-sm space-y-1">
          <div v-for="(count, status) in ordersAnalytics.by_status || {}" :key="status">
            <span class="text-gray-500">{{ status }}:</span> {{ count }}
          </div>
        </div>
      </div>

      <!-- Products analytics -->
      <div v-if="productsAnalytics" class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-3">{{ t('admin.products') }}</h2>
        <div class="text-sm">
          <div v-if="productsAnalytics.top_products?.length">
            <div class="font-medium mb-2">{{ t('admin.popular_products') }}</div>
            <div v-for="p in productsAnalytics.top_products" :key="p.id" class="flex justify-between py-1 border-b last:border-b-0">
              <span>{{ p.name }}</span>
              <span class="text-gray-500">{{ p.count }} {{ t('admin.orders_count') }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Search queries -->
      <div class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-3">{{ t('admin.popular_queries') }}</h2>
        <div v-if="searchQueries.length === 0" class="text-sm text-gray-500">
          {{ t('admin.no_data') }}
        </div>
        <div v-else class="text-sm">
          <div v-for="q in searchQueries.slice(0, 10)" :key="q.query" class="flex justify-between py-1 border-b last:border-b-0">
            <span>{{ q.query }}</span>
            <span class="text-gray-500">{{ q.count }} {{ t('admin.times') }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
