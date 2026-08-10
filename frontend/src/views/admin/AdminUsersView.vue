<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

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
    alert(e.response?.data?.message || t('admin.error'));
  }
};

onMounted(fetchUsers);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.users') }}</h1>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <div v-else class="bg-white rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-4 py-3 text-left">ID</th>
            <th class="px-4 py-3 text-left">{{ t('common.email') }}</th>
            <th class="px-4 py-3 text-left">{{ t('admin.role') }}</th>
            <th class="px-4 py-3 text-left">{{ t('admin.status') }}</th>
            <th class="px-4 py-3 text-left">{{ t('admin.created') }}</th>
            <th class="px-4 py-3 text-right">{{ t('admin.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in users" :key="user.id" class="border-t hover:bg-gray-50">
            <td class="px-4 py-3">{{ user.id }}</td>
            <td class="px-4 py-3">{{ user.email }}</td>
            <td class="px-4 py-3">
              <span class="px-2 py-0.5 rounded-full text-xs"
                :class="{
                  'bg-purple-100 text-purple-700': user.role === 'admin',
                  'bg-blue-100 text-blue-700': user.role === 'seller',
                  'bg-gray-100 text-gray-700': user.role === 'buyer',
                }">
                {{ user.role }}
              </span>
            </td>
            <td class="px-4 py-3">
              <select
                v-model="user.status"
                @change="updateUserStatus(user.id, user.status)"
                class="px-2 py-1 border border-gray-300 rounded text-xs"
              >
                <option value="active">Active</option>
                <option value="pending">Pending</option>
                <option value="blocked">Blocked</option>
              </select>
            </td>
            <td class="px-4 py-3 text-gray-500 text-xs">
              {{ user.created_at ? new Date(user.created_at).toLocaleDateString('ru-RU') : '—' }}
            </td>
            <td class="px-4 py-3 text-right">
              <button class="text-indigo-600 hover:underline text-xs">{{ t('admin.details') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
