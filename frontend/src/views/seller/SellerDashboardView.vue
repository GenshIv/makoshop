<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';
import { useFormat } from '../../composables/useFormat';

const auth = useAuthStore();
const { t } = useI18n();
const { formatPrice } = useFormat();
const stats = ref({ products: 0, orders: 0, revenue: 0, campaigns: 0 });
const loading = ref(true);

const fetchStats = async () => {
  loading.value = true;
  try {
    // Get company info to find company_id
    const user = auth.user;
    if (!user?.profile?.company_id) {
      return;
    }
    const companyId = user.profile.company_id;

    // Fetch orders for this company
    const ordersRes = await api.get(`/companies/${companyId}/orders`);
    const orders = ordersRes.data.items || ordersRes.data.orders || [];

    let revenue = 0;
    orders.forEach(o => {
      if (o.status !== 'cancelled' && o.status !== 'refunded') {
        revenue += o.total_amount || o.total || 0;
      }
    });

    // Fetch campaigns count
    const campaignsRes = await api.get('/promo-campaigns');
    const campaigns = campaignsRes.data.items || campaignsRes.data || [];
    const myCampaigns = campaigns.filter(c => c.company_id === companyId);

    stats.value = {
      products: user.profile.product_count || 0,
      orders: orders.length,
      revenue,
      campaigns: myCampaigns.length,
    };
  } catch (e) {
    console.error('Seller dashboard stats error:', e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchStats);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">{{ t('seller.dashboard_title') }}</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else>
      <!-- Stats cards -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('seller.products') }}</div>
          <div class="text-2xl font-bold">{{ stats.products }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('seller.orders') }}</div>
          <div class="text-2xl font-bold">{{ stats.orders }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('seller.revenue') }}</div>
          <div class="text-2xl font-bold text-green-600">{{ formatPrice(stats.revenue) }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('seller.campaigns') }}</div>
          <div class="text-2xl font-bold">{{ stats.campaigns }}</div>
        </div>
      </div>

      <!-- Quick links -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <router-link to="/seller/products" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('seller.manage_products') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('seller.manage_products_desc') }}</div>
        </router-link>
        <router-link to="/seller/orders" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('seller.orders_title') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('seller.orders_desc') }}</div>
        </router-link>
        <router-link to="/seller/promo" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('seller.promotion_title') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('seller.promotion_desc') }}</div>
        </router-link>
        <a href="#" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition cursor-not-allowed opacity-60">
          <div class="font-medium">{{ t('seller.analytics_title') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('seller.analytics_desc') }}</div>
        </a>
      </div>
    </div>
  </div>
</template>
