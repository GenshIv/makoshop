<script setup>
import { ref, onMounted, computed } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';
import { useFormat } from '../../composables/useFormat';
import EmptyState from '../../components/EmptyState.vue';

const router = useRouter();
const auth = useAuthStore();
const { t } = useI18n();
const { formatPrice, formatDate } = useFormat();
const orders = ref([]);
const loading = ref(true);
const error = ref(null);

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
  new: 'text-blue-600 bg-blue-50',
  pending: 'text-yellow-600 bg-yellow-50',
  paid: 'text-green-600 bg-green-50',
  processing: 'text-orange-600 bg-orange-50',
  shipped: 'text-purple-600 bg-purple-50',
  delivered: 'text-green-700 bg-green-100',
  cancelled: 'text-red-600 bg-red-50',
  refunded: 'text-ink-2 bg-surface-2',
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
    error.value = t('seller.orders_load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchOrders);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">{{ t('seller.orders') }}</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-orange-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Empty -->
    <div v-else-if="orders.length === 0" class="bg-surface rounded-lg shadow-sm">
      <EmptyState icon="doc" :title="t('seller.no_orders')" />
    </div>

    <!-- Orders table -->
    <div v-else class="bg-surface rounded-lg shadow-sm overflow-hidden">
      <div class="overflow-x-auto">
      <table class="w-full text-sm min-w-[640px]">
        <caption class="sr-only">{{ t('tables.seller_orders') }}</caption>
        <thead class="bg-surface-2">
          <tr>
            <th scope="col" class="px-4 py-3 text-left">ID</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.date') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.buyer') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.items_count') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.amount') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.status') }}</th>
            <th scope="col" class="px-4 py-3 text-right">{{ t('seller.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="order in orders" :key="order.id" class="border-t hover:bg-surface-2">
            <td class="px-4 py-3 font-medium">#{{ order.id }}</td>
            <td class="px-4 py-3 text-ink-3">{{ formatDate(order.created_at, true) }}</td>
            <td class="px-4 py-3">{{ order.shipping_info?.name || '—' }}</td>
            <td class="px-4 py-3">{{ order.items?.length || 0 }}</td>
            <td class="px-4 py-3 font-medium">{{ formatPrice(order.total_amount || order.total) }}</td>
            <td class="px-4 py-3">
              <span class="px-2.5 py-1 rounded-full text-xs font-medium" :class="statusColors[order.status] || 'text-ink-2 bg-surface-2'">
                {{ statusLabels[order.status] || order.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right">
              <router-link :to="{ name: 'order-detail', params: { id: order.id } }" class="text-orange-600 hover:underline">
                {{ t('seller.details') }}
              </router-link>
            </td>
          </tr>
        </tbody>
      </table>
      </div>
    </div>
  </div>
</template>
