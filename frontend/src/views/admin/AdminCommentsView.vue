<script setup>
import { ref, onMounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';

const { t } = useI18n();
const { toast } = useToast();

const comments = ref([]);
const total = ref(0);
const page = ref(1);
const limit = ref(50);
const statusFilter = ref('');
const targetTypeFilter = ref('');
const loading = ref(false);
const selectedIds = ref([]);
const stats = ref(null);

const statusOptions = [
  { value: '', label: t('admin.comments.all_statuses', 'All statuses') },
  { value: 'approved', label: t('admin.comments.approved', 'Approved') },
  { value: 'pending', label: t('admin.comments.pending', 'Pending') },
  { value: 'rejected', label: t('admin.comments.rejected', 'Rejected') },
  { value: 'hidden', label: t('admin.comments.hidden', 'Hidden') },
];

const targetTypeOptions = [
  { value: '', label: t('admin.comments.all_types', 'All types') },
  { value: 'product', label: t('admin.comments.product', 'Product') },
  { value: 'category', label: t('admin.comments.category', 'Category') },
  { value: 'eanpage', label: t('admin.comments.eanpage', 'EAN Page') },
];

const statusColors = {
  approved: 'bg-green-100 text-green-800',
  pending: 'bg-yellow-100 text-yellow-800',
  rejected: 'bg-red-100 text-red-800',
  hidden: 'bg-gray-100 text-gray-800',
};

const fetchComments = async () => {
  loading.value = true;
  try {
    const params = { page: page.value, limit: limit.value };
    if (statusFilter.value) params.status = statusFilter.value;
    if (targetTypeFilter.value) params.target_type = targetTypeFilter.value;
    const res = await api.get('/admin/comments', { params });
    comments.value = res.data.items || [];
    total.value = res.data.total || 0;
  } catch (e) {
    console.error('Failed to fetch comments:', e);
    toast.error(t('admin.comments.fetch_error', 'Failed to load comments'));
  } finally {
    loading.value = false;
  }
};

const fetchStats = async () => {
  try {
    const res = await api.get('/admin/comments/stats');
    stats.value = res.data;
  } catch (e) {
    console.error('Failed to fetch stats:', e);
  }
};

const updateStatus = async (comment, newStatus) => {
  try {
    await api.patch(`/admin/comments/${comment.id}`, { status: newStatus });
    toast.success(t('admin.comments.status_updated', 'Status updated'));
    await Promise.all([fetchComments(), fetchStats()]);
  } catch (e) {
    console.error('Failed to update status:', e);
    toast.error(t('admin.comments.update_error', 'Failed to update'));
  }
};

const toggleFeatured = async (comment) => {
  try {
    await api.patch(`/admin/comments/${comment.id}`, { is_featured: !comment.is_featured });
    toast.success(t('admin.comments.featured_toggled', 'Featured status toggled'));
    await fetchComments();
  } catch (e) {
    console.error('Failed to toggle featured:', e);
    toast.error(t('admin.comments.update_error', 'Failed to update'));
  }
};

const deleteComment = async (comment) => {
  if (!confirm(t('admin.comments.delete_confirm', 'Delete this comment?'))) return;
  try {
    await api.delete(`/admin/comments/${comment.id}`);
    toast.success(t('admin.comments.deleted', 'Comment deleted'));
    await Promise.all([fetchComments(), fetchStats()]);
  } catch (e) {
    console.error('Failed to delete comment:', e);
    toast.error(t('admin.comments.delete_error', 'Failed to delete'));
  }
};

const bulkAction = async (action) => {
  if (selectedIds.value.length === 0) {
    toast.error(t('admin.comments.select_first', 'Select comments first'));
    return;
  }
  try {
    await api.post('/admin/comments/bulk-actions', { action, ids: selectedIds.value });
    toast.success(t('admin.comments.bulk_done', `Bulk action completed: ${action}`));
    selectedIds.value = [];
    await Promise.all([fetchComments(), fetchStats()]);
  } catch (e) {
    console.error('Bulk action failed:', e);
    toast.error(t('admin.comments.bulk_error', 'Bulk action failed'));
  }
};

const toggleSelect = (comment) => {
  const idx = selectedIds.value.indexOf(comment.id);
  if (idx === -1) {
    selectedIds.value.push(comment.id);
  } else {
    selectedIds.value.splice(idx, 1);
  }
};

const selectAll = () => {
  if (selectedIds.value.length === comments.value.length && comments.value.length > 0) {
    selectedIds.value = [];
  } else {
    selectedIds.value = comments.value.map(c => c.id);
  }
};

const totalPages = computed(() => Math.ceil(total.value / limit.value));

const formatDate = (ts) => {
  if (!ts) return '';
  return new Date(ts * 1000).toLocaleDateString();
};

onMounted(() => {
  fetchComments();
  fetchStats();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-ink">{{ t('admin.comments.title', 'Comments Management') }}</h1>
    </div>

    <!-- Stats -->
    <div v-if="stats" class="grid grid-cols-2 md:grid-cols-5 gap-4 mb-6">
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-ink">{{ stats.total_comments }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.comments.total', 'Total') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-green-600">{{ stats.approved }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.comments.approved', 'Approved') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-yellow-600">{{ stats.pending }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.comments.pending', 'Pending') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-red-600">{{ stats.rejected }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.comments.rejected', 'Rejected') }}</div>
      </div>
      <div class="bg-surface rounded-xl border border-line p-4 text-center">
        <div class="text-2xl font-bold text-accent">{{ stats.by_target_type?.product || 0 }}</div>
        <div class="text-sm text-ink-3">{{ t('admin.comments.on_products', 'On Products') }}</div>
      </div>
    </div>

    <!-- Filters -->
    <div class="bg-surface rounded-xl border border-line p-4 mb-6">
      <div class="flex flex-wrap gap-4 items-center">
        <div>
          <label class="block text-xs text-ink-3 mb-1">{{ t('admin.comments.status_filter', 'Status') }}</label>
          <select v-model="statusFilter" @change="page = 1; fetchComments()" class="px-3 py-2 border border-line rounded-lg text-sm bg-surface-2">
            <option v-for="opt in statusOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
          </select>
        </div>
        <div>
          <label class="block text-xs text-ink-3 mb-1">{{ t('admin.comments.type_filter', 'Target Type') }}</label>
          <select v-model="targetTypeFilter" @change="page = 1; fetchComments()" class="px-3 py-2 border border-line rounded-lg text-sm bg-surface-2">
            <option v-for="opt in targetTypeOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
          </select>
        </div>
        <div class="ml-auto flex gap-2">
          <button
            v-if="selectedIds.length > 0"
            @click="bulkAction('approve')"
            class="btn btn-success btn-sm"
          >
            {{ t('admin.comments.bulk_approve', 'Approve Selected') }}
          </button>
          <button
            v-if="selectedIds.length > 0"
            @click="bulkAction('reject')"
            class="btn btn-danger btn-sm"
          >
            {{ t('admin.comments.bulk_reject', 'Reject Selected') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Comments table -->
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
                  :checked="selectedIds.length === comments.length && comments.length > 0"
                  @change="selectAll()"
                  class="rounded text-orange-600 focus:ring-orange-500"
                />
              </th>
              <th class="px-4 py-3 text-left">{{ t('admin.comments.target', 'Target') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.comments.user', 'User') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.comments.content', 'Content') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.comments.likes', 'Likes') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.comments.status', 'Status') }}</th>
              <th class="px-4 py-3 text-left">{{ t('admin.comments.date', 'Date') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.comments.actions', 'Actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="comment in comments" :key="comment.id" class="border-t border-line hover:bg-surface-2">
              <td class="px-4 py-3">
                <input
                  type="checkbox"
                  :checked="selectedIds.includes(comment.id)"
                  @change="toggleSelect(comment)"
                  class="rounded text-orange-600 focus:ring-orange-500"
                />
              </td>
              <td class="px-4 py-3">
                <div class="text-sm font-medium text-ink capitalize">{{ comment.target_type }}</div>
                <router-link
                  v-if="comment.target_type === 'product'"
                  :to="`/products/${comment.target_id}`"
                  class="text-xs text-accent hover:underline"
                >
                  #{{ comment.target_id }}
                </router-link>
                <span v-else class="text-xs text-ink-3">#{{ comment.target_id }}</span>
              </td>
              <td class="px-4 py-3">
                <div class="text-sm text-ink">{{ comment.user_name || `#${comment.user_id}` }}</div>
              </td>
              <td class="px-4 py-3 max-w-xs">
                <div class="text-sm text-ink-2 line-clamp-2">{{ comment.content }}</div>
              </td>
              <td class="px-4 py-3">
                <div class="flex items-center gap-2 text-sm">
                  <span class="text-green-600">👍 {{ comment.like_count || 0 }}</span>
                  <span class="text-red-600">👎 {{ comment.dislike_count || 0 }}</span>
                </div>
              </td>
              <td class="px-4 py-3">
                <span :class="['px-2 py-1 rounded-full text-xs font-medium', statusColors[comment.status] || 'bg-gray-100 text-gray-800']">
                  {{ comment.status }}
                </span>
              </td>
              <td class="px-4 py-3 text-xs text-ink-3">{{ formatDate(comment.created_at) }}</td>
              <td class="px-4 py-3 text-right">
                <div class="flex justify-end gap-1">
                  <button
                    @click="updateStatus(comment, 'approved')"
                    class="text-green-600 hover:text-green-800 text-xs"
                    :title="t('admin.comments.approve', 'Approve')"
                  >✓</button>
                  <button
                    @click="updateStatus(comment, 'rejected')"
                    class="text-red-600 hover:text-red-800 text-xs"
                    :title="t('admin.comments.reject', 'Reject')"
                  >✗</button>
                  <button
                    @click="toggleFeatured(comment)"
                    class="text-yellow-600 hover:text-yellow-800 text-xs"
                    :title="t('admin.comments.featured', 'Featured')"
                    :class="{ 'font-bold': comment.is_featured }"
                  >★</button>
                  <button
                    @click="deleteComment(comment)"
                    class="text-red-400 hover:text-red-600 text-xs"
                    :title="t('admin.comments.delete', 'Delete')"
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
