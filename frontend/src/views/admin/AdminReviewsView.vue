<script setup>
import { ref, onMounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';

const { t } = useI18n();
const { toast } = useToast();

const reviews = ref([]);
const total = ref(0);
const page = ref(1);
const limit = ref(50);
const statusFilter = ref('');
const eanFilter = ref('');
const loading = ref(false);
const selectedIds = ref([]);
const stats = ref(null);

const statusOptions = [
  { value: '', label: t('admin.reviews.all_statuses', 'All statuses') },
  { value: 'approved', label: t('admin.reviews.approved', 'Approved') },
  { value: 'pending', label: t('admin.reviews.pending', 'Pending') },
  { value: 'rejected', label: t('admin.reviews.rejected', 'Rejected') },
  { value: 'hidden', label: t('admin.reviews.hidden', 'Hidden') },
];

const statusColors = {
  approved: 'bg-green-100 text-green-800',
  pending: 'bg-yellow-100 text-yellow-800',
  rejected: 'bg-red-100 text-red-800',
  hidden: 'bg-gray-100 text-gray-800',
};

const fetchReviews = async () => {
  loading.value = true;
  try {
    const params = { page: page.value, limit: limit.value };
    if (statusFilter.value) params.status = statusFilter.value;
    if (eanFilter.value) params.e = eanFilter.value;
    const res = await api.get('/admin/reviews', { params });
    reviews.value = res.data.items || [];
    total.value = res.data.total || 0;
  } catch (e) {
    console.error('Failed to fetch reviews:', e);
    toast.error(t('admin.reviews.fetch_error', 'Failed to load reviews'));
  } finally {
    loading.value = false;
  }
};

const fetchStats = async () => {
  try {
    const res = await api.get('/admin/reviews/stats');
    stats.value = res.data;
  } catch (e) {
    console.error('Failed to fetch stats:', e);
  }
};

const updateStatus = async (review, newStatus) => {
  try {
    await api.patch(`/admin/reviews/${review.id}`, { status: newStatus });
    toast.success(t('admin.reviews.status_updated', 'Status updated'));
    await Promise.all([fetchReviews(), fetchStats()]);
  } catch (e) {
    console.error('Failed to update status:', e);
    toast.error(t('admin.reviews.update_error', 'Failed to update'));
  }
};

const toggleFeatured = async (review) => {
  try {
    await api.patch(`/admin/reviews/${review.id}`, { is_featured: !review.is_featured });
    toast.success(t('admin.reviews.featured_toggled', 'Featured status toggled'));
    await fetchReviews();
  } catch (e) {
    console.error('Failed to toggle featured:', e);
    toast.error(t('admin.reviews.update_error', 'Failed to update'));
  }
};

const deleteReview = async (review) => {
  if (!confirm(t('admin.reviews.delete_confirm', 'Delete this review?'))) return;
  try {
    await api.delete(`/admin/reviews/${review.id}`);
    toast.success(t('admin.reviews.deleted', 'Review deleted'));
    await Promise.all([fetchReviews(), fetchStats()]);
  } catch (e) {
    console.error('Failed to delete review:', e);
    toast.error(t('admin.reviews.delete_error', 'Failed to delete'));
  }
};

const bulkAction = async (action) => {
  if (selectedIds.value.length === 0) {
    toast.error(t('admin.reviews.select_first', 'Select reviews first'));
    return;
  }
  try {
    await api.post('/admin/reviews/bulk-actions', { action, ids: selectedIds.value });
    toast.success(t('admin.reviews.bulk_done', `Bulk action completed: ${action}`));
    selectedIds.value = [];
    await Promise.all([fetchReviews(), fetchStats()]);
  } catch (e) {
    console.error('Bulk action failed:', e);
    toast.error(t('admin.reviews.bulk_error', 'Bulk action failed'));
  }
};

const recalculateRatings = async () => {
  if (!confirm(t('admin.reviews.recalc_confirm', 'Recalculate ratings for all products?'))) return;
  try {
    await api.post('/admin/reviews/recalculate');
    toast.success(t('admin.reviews.recalc_done', 'Ratings recalculated'));
    await fetchReviews();
  } catch (e) {
    console.error('Recalculate failed:', e);
    toast.error(t('admin.reviews.recalc_error', 'Failed to recalculate'));
  }
};

const toggleSelect = (review) => {
  const idx = selectedIds.value.indexOf(review.id);
  if (idx === -1) {
    selectedIds.value.push(review.id);
  } else {
    selectedIds.value.splice(idx, 1);
  }
};

const selectAll = () => {
  if (selectedIds.value.length === reviews.value.length) {
    selectedIds.value = [];
  } else {
    selectedIds.value = reviews.value.map(r => r.id);
  }
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(price);
};

const formatDate = (ts) => {
  if (!ts) return '';
  return new Date(ts * 1000).toLocaleDateString();
};

const starRating = (rating) => {
  return '★'.repeat(rating) + '☆'.repeat(5 - rating);
};

const totalPages = computed(() => Math.ceil(total.value / limit.value));

onMounted(() => {
  fetchReviews();
  fetchStats();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-ink">{{ t('admin.reviews.title', 'Reviews Management') }}</h1>
    </div>

    <!-- Stats -->
    <div v-if="stats" class="grid grid-cols-2 md:grid-cols-5 gap-4 mb-6">
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-ink">{{ stats.total_reviews }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.reviews.total', 'Total') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-green-600">{{ stats.approved }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.reviews.approved', 'Approved') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-yellow-600">{{ stats.pending }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.reviews.pending', 'Pending') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-red-600">{{ stats.rejected }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.reviews.rejected', 'Rejected') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-accent">{{ stats.avg_rating?.toFixed(1) || '0.0' }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.reviews.avg_rating', 'Avg Rating') }}</div>
      </div>
    </div>

    <!-- Rating breakdown -->
    <div v-if="stats?.rating_breakdown" class="mb-6">
      <h3 class="text-sm font-medium text-ink-2 mb-2">{{ t('admin.reviews.rating_breakdown', 'Rating Breakdown') }}</h3>
      <div class="flex gap-2">
        <div v-for="stars in [5,4,3,2,1]" :key="stars" class="flex items-center gap-1">
          <span class="text-yellow-500 text-sm">★</span>
          <span class="text-xs text-ink-3">{{ stars }}</span>
          <div class="w-24 h-2 bg-surface-2 rounded-full overflow-hidden">
            <div
              class="h-full bg-yellow-500 rounded-full"
              :style="{ width: `${(stats.rating_breakdown[stars] || 0) / (stats.total_reviews || 1) * 100}%` }"
            ></div>
          </div>
          <span class="text-xs text-ink-3 w-8 text-right">{{ stats.rating_breakdown[stars] || 0 }}</span>
        </div>
      </div>
    </div>

    <!-- Filters -->
    <div class="bg-surface rounded-xl border border-line p-4 mb-6">
      <div class="flex flex-wrap gap-4 items-center">
        <div>
          <label class="block text-xs text-ink-3 mb-1">{{ t('admin.reviews.status_filter', 'Status') }}</label>
          <select v-model="statusFilter" @change="page = 1; fetchReviews()" class="px-3 py-2 border border-line rounded-lg text-sm bg-surface-2">
            <option v-for="opt in statusOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
          </select>
        </div>
        <div>
          <label class="block text-xs text-ink-3 mb-1">{{ t('admin.reviews.ean_filter', 'EAN') }}</label>
          <input
            v-model="eanFilter"
            @change="page = 1; fetchReviews()"
            type="text"
            placeholder="123456789"
            class="px-3 py-2 border border-line rounded-lg text-sm bg-surface-2 w-40"
          />
        </div>
        <div class="ml-auto flex gap-2">
          <button @click="recalculateRatings" class="btn btn-secondary btn-sm">
            {{ t('admin.reviews.recalculate', 'Recalculate Ratings') }}
          </button>
          <button
            v-if="selectedIds.length > 0"
            @click="bulkAction('approve')"
            class="btn btn-success btn-sm"
          >
            {{ t('admin.reviews.bulk_approve', 'Approve Selected') }}
          </button>
          <button
            v-if="selectedIds.length > 0"
            @click="bulkAction('reject')"
            class="btn btn-danger btn-sm"
          >
            {{ t('admin.reviews.bulk_reject', 'Reject Selected') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Reviews table -->
    <div class="bg-surface rounded-xl border border-line overflow-hidden">
      <div v-if="loading" class="p-8 text-center text-ink-3">
        {{ t('common.loading', 'Loading...') }}
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full">
          <thead>
            <tr class="bg-surface-2 text-xs text-ink-3 uppercase">
              <th class="px-4 py-3 text-left">
                <input
                  type="checkbox"
                  :checked="selectedIds.length === reviews.length && reviews.length > 0"
                  @change="selectAll()"
                  class="rounded text-orange-600 focus:ring-orange-500"
                />
              </th>
              <th class="px-4 py-3 text-left">{{ t('admin.reviews.product', 'Product') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.reviews.user', 'User') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.reviews.rating', 'Rating') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.reviews.comment', 'Comment') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.reviews.status', 'Status') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.reviews.date', 'Date') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.reviews.actions', 'Actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="review in reviews" :key="review.id" class="border-t border-line hover:bg-surface-2">
              <td class="px-4 py-3">
                <input
                  type="checkbox"
                  :checked="selectedIds.includes(review.id)"
                  @change="toggleSelect(review)"
                  class="rounded text-orange-600 focus:ring-orange-500"
                />
              </td>
              <td class="px-4 py-3">
                <div class="text-sm font-medium text-ink">
                  <router-link :to="`/products/${review.product_id}`" class="hover:text-accent">
                    #{{ review.product_id }}
                  </router-link>
                </div>
                <div v-if="review.ean" class="text-xs text-ink-3">{{ review.ean }}</div>
              </td>
              <td class="px-4 py-3">
                <div class="text-sm text-ink">{{ review.user_name || `#${review.user_id}` }}</div>
              </td>
              <td class="px-4 py-3">
                <span class="text-yellow-500 text-sm">{{ starRating(review.rating) }}</span>
                <span class="text-xs text-ink-3 ml-1">{{ review.rating }}/5</span>
              </td>
              <td class="px-4 py-3 max-w-xs">
                <div v-if="review.comment" class="text-sm text-ink-2 line-clamp-2">{{ review.comment }}</div>
                <div v-else class="text-xs text-ink-3 italic">{{ t('admin.reviews.no_comment', 'No comment') }}</div>
              </td>
              <td class="px-4 py-3">
                <span :class="['px-2 py-1 rounded-full text-xs font-medium', statusColors[review.status] || 'bg-gray-100 text-gray-800']">
                  {{ review.status }}
                </span>
              </td>
              <td class="px-4 py-3 text-xs text-ink-3">{{ formatDate(review.created_at) }}</td>
              <td class="px-4 py-3 text-right">
                <div class="flex justify-end gap-1">
                  <button
                    @click="updateStatus(review, 'approved')"
                    class="text-green-600 hover:text-green-800 text-xs"
                    :title="t('admin.reviews.approve', 'Approve')"
                  >✓</button>
                  <button
                    @click="updateStatus(review, 'rejected')"
                    class="text-red-600 hover:text-red-800 text-xs"
                    :title="t('admin.reviews.reject', 'Reject')"
                  >✗</button>
                  <button
                    @click="toggleFeatured(review)"
                    class="text-yellow-600 hover:text-yellow-800 text-xs"
                    :title="t('admin.reviews.featured', 'Featured')"
                    :class="{ 'font-bold': review.is_featured }"
                  >★</button>
                  <button
                    @click="deleteReview(review)"
                    class="text-red-400 hover:text-red-600 text-xs"
                    :title="t('admin.reviews.delete', 'Delete')"
                  >🗑</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Pagination -->
      <div v-if="totalPages > 1" class="flex justify-center gap-2 p-4 border-t border-line">
        <button
          @click="page--"
          :disabled="page <= 1"
          class="btn btn-secondary btn-sm"
        >
          {{ t('common.back', 'Back') }}
        </button>
        <span class="px-3 py-1.5 text-sm text-ink-3">
          {{ page }} / {{ totalPages }}
        </span>
        <button
          @click="page++"
          :disabled="page >= totalPages"
          class="btn btn-secondary btn-sm"
        >
          {{ t('common.next', 'Next') }}
        </button>
      </div>
    </div>
  </div>
</template>
