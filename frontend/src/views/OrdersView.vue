<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import api from '../api';

const router = useRouter();
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
  pending: 'text-yellow-600 bg-yellow-50',
  paid: 'text-blue-600 bg-blue-50',
  processing: 'text-indigo-600 bg-indigo-50',
  shipped: 'text-purple-600 bg-purple-50',
  delivered: 'text-green-600 bg-green-50',
  cancelled: 'text-red-600 bg-red-50',
  refunded: 'text-gray-600 bg-gray-50',
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const formatDate = (dateStr) => {
  return new Date(dateStr).toLocaleDateString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

const fetchOrders = async () => {
  loading.value = true;
  try {
    const response = await api.get('/orders');
    orders.value = response.data.items || response.data.orders || response.data || [];
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
  <div class="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">Мои заказы</h1>

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
      У вас пока нет заказов
      <router-link to="/" class="block mt-2 text-indigo-600 hover:underline">Перейти в каталог</router-link>
    </div>

    <!-- Orders list -->
    <div v-else class="space-y-4">
      <div
        v-for="order in orders"
        :key="order.id"
        class="bg-white rounded-lg shadow-sm p-4 hover:shadow-md transition cursor-pointer"
        @click="router.push({ name: 'order-detail', params: { id: order.id } })"
      >
        <div class="flex items-center justify-between">
          <div>
            <div class="font-medium">Заказ #{{ order.id }}</div>
            <div class="text-sm text-gray-500">{{ formatDate(order.created_at) }}</div>
          </div>
          <div class="flex items-center gap-4">
            <span class="text-sm font-medium">{{ formatPrice(order.total_amount || order.total) }}</span>
            <span
              class="px-2.5 py-1 rounded-full text-xs font-medium"
              :class="statusColors[order.status] || 'text-gray-600 bg-gray-50'"
            >
              {{ statusLabels[order.status] || order.status }}
            </span>
          </div>
        </div>
        <div class="mt-2 text-sm text-gray-500">
          {{ order.items?.length || 0 }} товар(ов)
        </div>
      </div>
    </div>
  </div>
</template>
