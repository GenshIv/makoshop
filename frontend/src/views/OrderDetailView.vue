<script setup>
import { ref, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import api from '../api';

const route = useRoute();
const router = useRouter();
const order = ref(null);
const loading = ref(true);
const error = ref(null);

const statusLabels = {
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

const fetchOrder = async () => {
  loading.value = true;
  try {
    const response = await api.get(`/orders/${route.params.id}`);
    order.value = response.data;
  } catch (e) {
    error.value = 'Заказ не найден';
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchOrder);
</script>

<template>
  <div class="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Back -->
    <router-link to="/orders" class="text-sm text-indigo-600 hover:underline mb-4 inline-block">
      ← Назад к заказам
    </router-link>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Order detail -->
    <div v-else-if="order" class="bg-white rounded-lg shadow-sm overflow-hidden">
      <!-- Header -->
      <div class="p-4 border-b flex items-center justify-between">
        <div>
          <h1 class="text-xl font-bold">Заказ #{{ order.id }}</h1>
          <div class="text-sm text-gray-500">{{ formatDate(order.created_at) }}</div>
        </div>
        <span
          class="px-3 py-1 rounded-full text-sm font-medium"
          :class="statusColors[order.status] || 'text-gray-600 bg-gray-50'"
        >
          {{ statusLabels[order.status] || order.status }}
        </span>
      </div>

      <!-- Items -->
      <div class="p-4">
        <h2 class="font-medium mb-3">Товары</h2>
        <div v-for="item in order.items" :key="item.product_id" class="flex items-center gap-4 py-3 border-b last:border-b-0">
          <div class="w-16 h-16 bg-gray-100 rounded-lg overflow-hidden flex-shrink-0">
            <img
              v-if="item.images?.length"
              :src="item.images[0]"
              :alt="item.name"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center text-gray-400 text-xs">Нет фото</div>
          </div>
          <div class="flex-1">
            <router-link
              :to="{ name: 'product', params: { id: item.product_id } }"
              class="font-medium hover:text-indigo-600"
            >
              {{ item.name }}
            </router-link>
            <div class="text-sm text-gray-500">
              {{ formatPrice(item.price) }} × {{ item.qty }}
            </div>
          </div>
          <div class="font-medium">{{ formatPrice(item.price * item.qty) }}</div>
        </div>
      </div>

      <!-- Shipping info -->
      <div v-if="order.shipping_info" class="p-4 border-t">
        <h2 class="font-medium mb-2">Доставка</h2>
        <div class="text-sm text-gray-700 space-y-1">
          <div>{{ order.shipping_info.name }}</div>
          <div>{{ order.shipping_info.phone }}</div>
          <div>{{ order.shipping_info.city }}, {{ order.shipping_info.address }}</div>
          <div v-if="order.shipping_info.zip">Индекс: {{ order.shipping_info.zip }}</div>
        </div>
      </div>

      <!-- Payment info -->
      <div class="p-4 border-t">
        <h2 class="font-medium mb-2">Оплата</h2>
        <div class="text-sm text-gray-700">
          <div>Статус: {{ statusLabels[order.payment_status] || order.payment_status }}</div>
        </div>
      </div>

      <!-- Total -->
      <div class="p-4 border-t bg-gray-50 flex items-center justify-between">
        <span class="font-medium">Итого:</span>
        <span class="text-xl font-bold">{{ formatPrice(order.total_amount || order.total) }}</span>
      </div>
    </div>
  </div>
</template>
