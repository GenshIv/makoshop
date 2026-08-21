<script setup>
import { ref, onMounted, computed } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useFormat } from '../composables/useFormat';
import EmptyState from '../components/EmptyState.vue';
import SkeletonList from '../components/SkeletonList.vue';

const router = useRouter();
const orders = ref([]);
const loading = ref(true);
const error = ref(null);
const { t } = useI18n();
const { formatPrice, formatDate } = useFormat();

const statusLabels = computed(() => ({
  new: t('orders.statuses.new'),
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
  processing: 'text-indigo-600 bg-indigo-50',
  shipped: 'text-purple-600 bg-purple-50',
  delivered: 'text-green-600 bg-green-50',
  cancelled: 'text-red-600 bg-red-50',
  refunded: 'text-ink-2 bg-surface-2',
};

const fetchOrders = async () => {
  loading.value = true;
  try {
    const response = await api.get('/orders');
    orders.value = response.data.items || response.data.orders || response.data || [];
  } catch (e) {
    error.value = t('orders.load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchOrders);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-ink">{{ t('orders.title') }}</h1>

    <!-- Loading -->
    <div v-if="loading">
      <p class="text-sm text-ink-3 mb-4">{{ t('orders.loading') }}</p>
      <SkeletonList :count="4" />
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-50 text-red-700 rounded-lg theme-dark:bg-red-900/30 theme-dark:text-red-300">
      {{ error }}
    </div>

    <!-- Empty -->
    <div v-else-if="orders.length === 0" class="bg-surface rounded-lg border border-line">
      <EmptyState icon="doc" :title="t('orders.no_orders')">
        <router-link to="/" class="btn btn-primary">{{ t('orders.go_to_catalog') }}</router-link>
      </EmptyState>
    </div>

    <!-- Orders list -->
    <div v-else class="space-y-4">
      <div
        v-for="order in orders"
        :key="order.id"
        class="bg-surface rounded-lg border border-line p-4 hover:shadow-md hover:border-indigo-300 transition-all cursor-pointer"
        @click="router.push({ name: 'order-detail', params: { id: order.id } })"
      >
        <div class="flex items-center justify-between">
          <div>
            <div class="font-medium">{{ t('orders.order', { id: order.id }) }}</div>
            <div class="text-sm text-ink-3">{{ formatDate(order.created_at, true) }}</div>
          </div>
          <div class="flex items-center gap-4">
            <span class="text-sm font-medium">{{ formatPrice(order.total_amount || order.total) }}</span>
            <span
              class="px-2.5 py-1 rounded-full text-xs font-medium"
              :class="statusColors[order.status] || 'text-ink-2 bg-surface-2'"
            >
              {{ statusLabels[order.status] || order.status }}
            </span>
          </div>
        </div>
        <div class="mt-2 text-sm text-ink-3">
          {{ t('orders.items', { count: order.items?.length || 0 }) }}
        </div>
      </div>
    </div>
  </div>
</template>
