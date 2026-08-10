<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

const companies = ref([]);
const loading = ref(true);
const error = ref(null);

const fetchCompanies = async () => {
  loading.value = true;
  error.value = null;
  try {
    const response = await api.get('/admin/companies');
    companies.value = response.data.items || response.data || [];
  } catch (e) {
    error.value = t('admin.companies_load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const verifyCompany = async (id) => {
  try {
    await api.patch(`/admin/companies/${id}`, { status: 'verified' });
    const c = companies.value.find(x => x.id === id);
    if (c) c.status = 'verified';
  } catch (e) {
    alert(e.response?.data?.message || t('admin.error'));
  }
};

const blockCompany = async (id) => {
  if (!confirm(t('admin.block_confirm'))) return;
  try {
    await api.patch(`/admin/companies/${id}`, { status: 'blocked' });
    const c = companies.value.find(x => x.id === id);
    if (c) c.status = 'blocked';
  } catch (e) {
    alert(e.response?.data?.message || t('admin.error'));
  }
};

onMounted(fetchCompanies);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.companies') }}</h1>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <div v-else-if="companies.length === 0" class="text-center py-12 text-gray-500">
      {{ t('admin.no_companies') }}
    </div>

    <div v-else class="bg-white rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-4 py-3 text-left">ID</th>
            <th class="px-4 py-3 text-left">{{ t('admin.name') }}</th>
            <th class="px-4 py-3 text-left">Owner</th>
            <th class="px-4 py-3 text-left">{{ t('admin.status') }}</th>
            <th class="px-4 py-3 text-right">{{ t('admin.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in companies" :key="c.id" class="border-t hover:bg-gray-50">
            <td class="px-4 py-3">{{ c.id }}</td>
            <td class="px-4 py-3">{{ c.name || '—' }}</td>
            <td class="px-4 py-3 text-gray-500">{{ c.owner_user_id }}</td>
            <td class="px-4 py-3">
              <span :class="{
                'text-green-600': c.status === 'verified',
                'text-yellow-600': c.status === 'pending',
                'text-red-600': c.status === 'blocked',
              }">
                {{ c.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right space-x-2">
              <button v-if="c.status === 'pending'" @click="verifyCompany(c.id)" class="text-green-600 hover:underline text-xs">
                {{ t('admin.verify') }}
              </button>
              <button @click="blockCompany(c.id)" class="text-red-600 hover:underline text-xs">
                {{ t('admin.block') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
