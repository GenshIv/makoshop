<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import { useFormat } from '../../composables/useFormat';
import EmptyState from '../../components/EmptyState.vue';

const { t } = useI18n();
const { toast } = useToast();
const { formatDate } = useFormat();

const users = ref([]);
const loading = ref(true);
const error = ref(null);

const fetchUsers = async () => {
  loading.value = true;
  error.value = null;
  try {
    const response = await api.get('/admin/users');
    users.value = response.data.items || response.data || [];
  } catch (e) {
    error.value = t('admin.users_load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const updateUserStatus = async (id, status) => {
  try {
    await api.patch(`/admin/users/${id}`, { status });
    const user = users.value.find(u => u.id === id);
    if (user) user.status = status;
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.error'));
  }
};

onMounted(fetchUsers);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.users') }}</h1>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <div v-else-if="users.length === 0" class="bg-surface rounded-lg shadow-sm">
      <EmptyState icon="users" :title="t('admin.no_items')" />
    </div>

    <div v-else class="bg-surface rounded-lg shadow-sm overflow-hidden">
      <div class="overflow-x-auto">
      <table class="w-full text-sm min-w-[640px]">
        <caption class="sr-only">{{ t('tables.admin_users') }}</caption>
        <thead class="bg-surface-2">
          <tr>
            <th scope="col" class="px-4 py-3 text-left">ID</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('common.email') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.role') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.status') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.created') }}</th>
            <th scope="col" class="px-4 py-3 text-right">{{ t('admin.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in users" :key="user.id" class="border-t hover:bg-surface-2">
            <td class="px-4 py-3">{{ user.id }}</td>
            <td class="px-4 py-3">{{ user.email }}</td>
            <td class="px-4 py-3">
              <span class="px-2 py-0.5 rounded-full text-xs"
                :class="{
                  'bg-purple-100 text-purple-700': user.role === 'admin',
                  'bg-orange-100 text-orange-700': user.role === 'seller',
                  'bg-surface-2 text-ink-2': user.role === 'buyer',
                }">
                {{ user.role }}
              </span>
            </td>
            <td class="px-4 py-3">
              <select
                v-model="user.status"
                @change="updateUserStatus(user.id, user.status)"
                class="px-2 py-1 border border-line rounded text-xs"
              >
                <option value="active">{{ t('admin.user_status_active') }}</option>
                <option value="pending">{{ t('admin.user_status_pending') }}</option>
                <option value="blocked">{{ t('admin.user_status_blocked') }}</option>
              </select>
            </td>
            <td class="px-4 py-3 text-ink-3 text-xs">
              {{ user.created_at ? formatDate(user.created_at) : '—' }}
            </td>
            <td class="px-4 py-3 text-right">
              <button class="text-orange-600 hover:underline text-xs">{{ t('admin.details') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
      </div>
    </div>
  </div>
</template>
