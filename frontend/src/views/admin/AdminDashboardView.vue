<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

const stats = ref({ users: 0, companies: 0, products: 0, orders: 0, revenue: 0 });
const loading = ref(true);
const maintenance = ref({ enabled: false, auto_disable: false });
const maintenanceLoading = ref(false);

const fetchMaintenance = async () => {
  try {
    const res = await api.get('/admin/maintenance');
    maintenance.value = res.data;
  } catch (e) {
    console.error('fetch maintenance:', e);
  }
};

const autoDisable = ref(false);

const setMaintenance = async (enable) => {
  maintenanceLoading.value = true;
  try {
    await api.post('/admin/maintenance', { enable, auto_disable: enable ? autoDisable.value : false });
    maintenance.value.enabled = enable;
    maintenance.value.auto_disable = enable ? autoDisable.value : false;
  } catch (e) {
    console.error('set maintenance:', e);
    alert('Failed to update maintenance mode');
  } finally {
    maintenanceLoading.value = false;
  }
};

const fetchStats = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/analytics/overview');
    const data = res.data;
    stats.value = {
      users: data.users || 0,
      companies: data.companies || 0,
      products: data.products || 0,
      orders: data.orders || 0,
      revenue: data.revenue || 0,
    };
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

onMounted(() => {
  fetchStats();
  fetchMaintenance();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.dashboard_title') }}</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else>
      <!-- Maintenance mode control -->
      <div class="mb-6 flex items-center gap-3">
        <span class="text-sm font-medium text-gray-700">{{ t('admin.maintenance_mode') }}:</span>
        <span
          :class="maintenance.enabled ? 'text-red-600' : 'text-green-600'"
          class="text-sm font-semibold"
        >
          {{ maintenance.enabled ? t('admin.maintenance_on') : t('admin.maintenance_off') }}
        </span>
        <button
          @click="setMaintenance(!maintenance.enabled)"
          :disabled="maintenanceLoading"
          class="px-3 py-1 text-xs rounded-md border border-gray-300 bg-white hover:bg-gray-50 disabled:opacity-50"
        >
          {{ maintenanceLoading ? '...' : (maintenance.enabled ? t('admin.maintenance_disable') : t('admin.maintenance_enable')) }}
        </button>
        <label class="inline-flex items-center gap-1 text-xs text-gray-600 cursor-pointer">
          <input
            v-model="autoDisable"
            type="checkbox"
            class="form-checkbox h-3 w-3"
          />
          {{ t('admin.maintenance_auto_disable') }}
        </label>
        <span class="text-xs text-gray-500">
          {{ t('admin.maintenance_hint') }}
        </span>
      </div>

      <!-- Stats -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-4 mb-8">
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">{{ t('admin.users') }}</div>
          <div class="text-2xl font-bold">{{ stats.users }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">{{ t('admin.companies') }}</div>
          <div class="text-2xl font-bold">{{ stats.companies }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">{{ t('admin.products') }}</div>
          <div class="text-2xl font-bold">{{ stats.products }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">{{ t('admin.orders') }}</div>
          <div class="text-2xl font-bold">{{ stats.orders }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">{{ t('admin.revenue') }}</div>
          <div class="text-2xl font-bold text-green-600">{{ formatPrice(stats.revenue) }}</div>
        </div>
      </div>

      <!-- Quick links -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <router-link to="/admin/users" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.users_manage') }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.users_manage_desc') }}</div>
        </router-link>
        <router-link to="/admin/companies" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.companies_manage') }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.companies_manage_desc') }}</div>
        </router-link>
        <router-link to="/admin/categories" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.categories_attrs') }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.categories_attrs_desc') }}</div>
        </router-link>
        <router-link to="/admin/analytics" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.analytics') }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.analytics_desc') }}</div>
        </router-link>
        <router-link to="/admin/promo" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.promo') }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.promo_desc') }}</div>
        </router-link>
        <router-link to="/admin/scupages" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.scupage_title') || 'SCU Pages' }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.scupage_manage_desc') || 'Manage SEO product pages' }}</div>
        </router-link>
        <router-link to="/admin/catalogizer" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.catalogizer_title') || 'Auto-Catalogizer' }}</div>
          <div class="text-sm text-gray-500 mt-1">{{ t('admin.catalogizer_desc_short') || 'Auto-assign products to categories' }}</div>
        </router-link>
        <router-link to="/admin/stats" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">Request Stats</div>
          <div class="text-sm text-gray-500 mt-1">Request metrics, latencies, top routes</div>
        </router-link>
      </div>
    </div>
  </div>
</template>
