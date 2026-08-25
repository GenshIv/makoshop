<script setup>
import { ref, onMounted, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useFormat } from '../composables/useFormat';
import SkeletonList from '../components/SkeletonList.vue';

const route = useRoute();
const router = useRouter();
const order = ref(null);
const loading = ref(true);
const error = ref(null);
const { t } = useI18n();
const { formatPrice, formatDate } = useFormat();

const statusLabels = computed(() => ({
  pending: t('orders.statuses.pending'),
  paid: t('orders.statuses.paid'),
  processing: t('orders.statuses.processing'),
  shipped: t('orders.statuses.shipped'),
  delivered: t('orders.statuses.delivered'),
  cancelled: t('orders.statuses.cancelled'),
  refunded: t('orders.statuses.refunded'),
}));

const statusColors = {
  pending: 'text-yellow-600 bg-yellow-50',
  paid: 'text-blue-600 bg-blue-50',
  processing: 'text-orange-600 bg-orange-50',
  shipped: 'text-purple-600 bg-purple-50',
  delivered: 'text-green-600 bg-green-50',
  cancelled: 'text-red-600 bg-red-50',
  refunded: 'text-ink-2 bg-surface-2',
};

const fetchOrder = async () => {
  loading.value = true;
  try {
    const response = await api.get(`/orders/${route.params.id}`);
    order.value = response.data;
  } catch (e) {
    error.value = t('order_detail.not_found');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchOrder);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Back -->
    <router-link to="/orders" class="text-sm text-accent hover:underline mb-4 inline-block transition-colors">
      {{ t('order_detail.back_to_orders') }}
    </router-link>

    <!-- Loading -->
    <div v-if="loading">
      <p class="text-sm text-ink-3 mb-4">{{ t('order_detail.loading') }}</p>
      <SkeletonList :count="3" />
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-50 text-red-700 rounded-lg theme-dark:bg-red-900/30 theme-dark:text-red-300">
      {{ error }}
    </div>

    <!-- Order detail -->
    <div v-else-if="order" class="bg-surface rounded-xl border border-line overflow-hidden">
      <!-- Header -->
      <div class="p-4 border-b flex items-center justify-between">
        <div>
          <h1 class="text-xl font-bold">{{ t('order_detail.order', { id: order.id }) }}</h1>
          <div class="text-sm text-ink-3">{{ formatDate(order.created_at, true) }}</div>
        </div>
        <span
          class="px-3 py-1 rounded-full text-sm font-medium"
          :class="statusColors[order.status] || 'text-ink-2 bg-surface-2'"
        >
          {{ statusLabels[order.status] || order.status }}
        </span>
      </div>

      <!-- Items -->
      <div class="p-4">
        <h2 class="font-medium mb-3">{{ t('order_detail.items') }}</h2>
        <div v-for="item in order.items" :key="item.product_id" class="flex items-center gap-4 py-3 border-b last:border-b-0">
          <div class="w-16 h-16 bg-surface-2 rounded-lg overflow-hidden flex-shrink-0">
            <img
              v-if="item.images?.length"
              :src="item.images[0]"
              :alt="item.name"
              loading="lazy"
              decoding="async"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center text-ink-3 text-xs">{{ t('order_detail.no_photo') }}</div>
          </div>
          <div class="flex-1">
            <router-link
              :to="{ name: 'product', params: { id: item.product_id } }"
              class="font-medium hover:text-orange-600"
            >
              {{ item.name }}
            </router-link>
            <div class="text-sm text-ink-3">
              {{ formatPrice(item.price) }} × {{ item.qty }}
            </div>
          </div>
          <div class="font-medium">{{ formatPrice(item.price * item.qty) }}</div>
        </div>
      </div>

      <!-- Shipping info -->
      <div v-if="order.shipping_info" class="p-4 border-t">
        <h2 class="font-medium mb-2">{{ t('order_detail.shipping') }}</h2>
        <div class="text-sm text-ink-2 space-y-1">
          <div>{{ order.shipping_info.name }}</div>
          <div>{{ order.shipping_info.phone }}</div>
          <div>{{ order.shipping_info.city }}, {{ order.shipping_info.address }}</div>
          <div v-if="order.shipping_info.zip">{{ t('order_detail.zip', { zip: order.shipping_info.zip }) }}</div>
        </div>
      </div>

      <!-- Payment info -->
      <div class="p-4 border-t">
        <h2 class="font-medium mb-2">{{ t('order_detail.payment') }}</h2>
        <div class="text-sm text-ink-2">
          <div>{{ t('order_detail.status', { status: statusLabels[order.payment_status] || order.payment_status }) }}</div>
        </div>
      </div>

      <!-- Total -->
      <div class="p-4 border-t bg-surface-2 flex items-center justify-between">
        <span class="font-medium">{{ t('order_detail.total') }}</span>
        <span class="text-xl font-bold">{{ formatPrice(order.total_amount || order.total) }}</span>
      </div>
    </div>
  </div>
</template>
