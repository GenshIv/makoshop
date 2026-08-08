<script setup>
import { ref, onMounted } from 'vue';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';

const auth = useAuthStore();
const stats = ref({ products: 0, orders: 0, revenue: 0, campaigns: 0 });
const loading = ref(true);

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

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
    <h1 class="text-2xl font-bold mb-6">Кабинет продавца</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else>
      <!-- Stats cards -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">Товары</div>
          <div class="text-2xl font-bold">{{ stats.products }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">Заказы</div>
          <div class="text-2xl font-bold">{{ stats.orders }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">Выручка</div>
          <div class="text-2xl font-bold text-green-600">{{ formatPrice(stats.revenue) }}</div>
        </div>
        <div class="bg-white rounded-lg shadow-sm p-4">
          <div class="text-sm text-gray-500">Кампании</div>
          <div class="text-2xl font-bold">{{ stats.campaigns }}</div>
        </div>
      </div>

      <!-- Quick links -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <router-link to="/seller/products" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">Управление товарами</div>
          <div class="text-sm text-gray-500 mt-1">Создание и редактирование</div>
        </router-link>
        <router-link to="/seller/orders" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">Заказы</div>
          <div class="text-sm text-gray-500 mt-1">Просмотр и обработка</div>
        </router-link>
        <router-link to="/seller/promo" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">Продвижение</div>
          <div class="text-sm text-gray-500 mt-1">Рекламные кампании</div>
        </router-link>
        <a href="#" class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition cursor-not-allowed opacity-60">
          <div class="font-medium">Аналитика</div>
          <div class="text-sm text-gray-500 mt-1">Скоро</div>
        </a>
      </div>
    </div>
  </div>
</template>
