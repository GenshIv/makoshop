<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';

const router = useRouter();
const auth = useAuthStore();
const orders = ref([]);
const loading = ref(true);
const error = ref(null);

const statusLabels = {
  new: 'Новый',
  pending: 'Ожидает оплаты',
  paid: 'Оплачен',
  processing: 'В обработке',
  shipped: 'Отправлен',
  delivered: 'Доставлен',
  cancelled: 'Отменён',
  refunded: 'Возврат',
};

const statusColors = {
  new: 'text-blue-600 bg-blue-50',
  pending: 'text-yellow-600 bg-yellow-50',
  paid: 'text-green-600 bg-green-50',
  processing: 'text-indigo-600 bg-indigo-50',
  shipped: 'text-purple-600 bg-purple-50',
  delivered: 'text-green-700 bg-green-100',
  cancelled: 'text-red-600 bg-red-50',
  refunded: 'text-gray-600 bg-gray-50',
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const formatDate = (dateStr) => {
  return new Date(dateStr).toLocaleDateString('ru-RU', {
    day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit',
  });
};

const fetchOrders = async () => {
  loading.value = true;
  error.value = null;
  try {
    const companyId = auth.user?.profile?.company_id;
    if (!companyId) {
      orders.value = [];
      return;
    }
    const response = await api.get(`/companies/${companyId}/orders`);
    orders.value = response.data.items || response.data.orders || [];
  } catch (e) {
    error.value = 'Ошибка загрузки заказов';
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchOrders);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">Заказы</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Empty -->
    <div v-else-if="orders.length === 0" class="text-center py-12 text-gray-500">
      Заказов пока нет
    </div>

    <!-- Orders table -->
    <div v-else class="bg-white rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-4 py-3 text-left">ID</th>
            <th class="px-4 py-3 text-left">Дата</th>
            <th class="px-4 py-3 text-left">Покупатель</th>
            <th class="px-4 py-3 text-left">Товаров</th>
            <th class="px-4 py-3 text-left">Сумма</th>
            <th class="px-4 py-3 text-left">Статус</th>
            <th class="px-4 py-3 text-right">Действия</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in orders" :key="order.id" class="border-t hover:bg-gray-50">
            <td class="px-4 py-3 font-medium">#{{ order.id }}</td>
            <td class="px-4 py-3 text-gray-500">{{ formatDate(order.created_at) }}</td>
            <td class="px-4 py-3">{{ order.shipping_info?.name || '—' }}</td>
            <td class="px-4 py-3">{{ order.items?.length || 0 }}</td>
            <td class="px-4 py-3 font-medium">{{ formatPrice(order.total_amount || order.total) }}</td>
            <td class="px-4 py-3">
              <span class="px-2.5 py-1 rounded-full text-xs font-medium" :class="statusColors[order.status] || 'text-gray-600 bg-gray-50'">
                {{ statusLabels[order.status] || order.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right">
              <router-link :to="{ name: 'order-detail', params: { id: order.id } }" class="text-indigo-600 hover:underline">
                Детали
              </router-link>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
