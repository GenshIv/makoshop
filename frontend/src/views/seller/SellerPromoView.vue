<script setup>
import { ref, reactive, onMounted } from 'vue';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';

const auth = useAuthStore();
const campaigns = ref([]);
const plans = ref([]);
const loading = ref(true);
const error = ref(null);
const showForm = ref(false);

const form = reactive({
  plan_id: '',
  target_filters: '{}',
  target_position: 1,
  budget: 1000,
  start_at: '',
  end_at: '',
});

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const fetchCampaigns = async () => {
  loading.value = true;
  try {
    const response = await api.get('/promo-campaigns');
    const items = response.data.items || response.data || [];
    const companyId = auth.user?.profile?.company_id;
    campaigns.value = items.filter(c => c.company_id === companyId);
  } catch (e) {
    error.value = 'Ошибка загрузки кампаний';
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const fetchPlans = async () => {
  try {
    const response = await api.get('/promo/plans');
    plans.value = response.data.items || response.data || [];
  } catch (e) {
    console.error(e);
  }
};

const createCampaign = async () => {
  try {
    const payload = {
      company_id: auth.user?.profile?.company_id,
      plan_id: form.plan_id,
      target_filters: JSON.parse(form.target_filters || '{}'),
      target_position: form.target_position,
      budget: form.budget,
      start_at: form.start_at || undefined,
      end_at: form.end_at || undefined,
    };
    await api.post('/promo-campaigns', payload);
    showForm.value = false;
    await fetchCampaigns();
  } catch (e) {
    error.value = e.response?.data?.message || 'Ошибка создания кампании';
  }
};

onMounted(() => {
  fetchCampaigns();
  fetchPlans();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">{{ t('seller.promotion') }}</h1>
      <button @click="showForm = true" class="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">
        {{ t('seller.new_campaign') }}
      </button>
    </div>

    <!-- Error -->
    <div v-if="error" class="mb-4 p-3 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Form -->
    <div v-if="showForm" class="mb-6 bg-white rounded-lg shadow-sm p-4">
      <h3 class="font-medium mb-3">Новая кампания</h3>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label class="block text-sm text-gray-700 mb-1">План</label>
          <select v-model="form.plan_id" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm">
            <option value="">Выберите план</option>
            <option v-for="plan in plans" :key="plan.id" :value="plan.id">{{ plan.name }}</option>
          </select>
        </div>
        <div>
          <label class="block text-sm text-gray-700 mb-1">Бюджет</label>
          <input v-model.number="form.budget" type="number" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" />
        </div>
        <div>
          <label class="block text-sm text-gray-700 mb-1">Позиция</label>
          <input v-model.number="form.target_position" type="number" min="1" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" />
        </div>
        <div>
          <label class="block text-sm text-gray-700 mb-1">Target filters (JSON)</label>
          <input v-model="form.target_filters" type="text" placeholder='{"category_id":1}' class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" />
        </div>
      </div>
      <div class="flex gap-2 mt-3">
        <button @click="createCampaign" class="px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm hover:bg-indigo-700">
          Создать
        </button>
        <button @click="showForm = false" class="px-4 py-2 border rounded-lg text-sm hover:bg-gray-50">
          Отмена
        </button>
      </div>
    </div>

    <!-- Empty -->
    <div v-else-if="campaigns.length === 0" class="text-center py-12 text-gray-500">
      У вас пока нет рекламных кампаний
    </div>

    <!-- Campaigns table -->
    <div v-else class="bg-white rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-4 py-3 text-left">ID</th>
            <th class="px-4 py-3 text-left">План</th>
            <th class="px-4 py-3 text-left">Бюджет</th>
            <th class="px-4 py-3 text-left">Использовано</th>
            <th class="px-4 py-3 text-left">Статус</th>
            <th class="px-4 py-3 text-left">Период</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in campaigns" :key="c.id" class="border-t hover:bg-gray-50">
            <td class="px-4 py-3">{{ c.id }}</td>
            <td class="px-4 py-3">{{ c.plan_id }}</td>
            <td class="px-4 py-3">{{ formatPrice(c.budget) }}</td>
            <td class="px-4 py-3 text-gray-500">{{ formatPrice(c.budget_used || 0) }}</td>
            <td class="px-4 py-3">
              <span :class="c.status === 'active' ? 'text-green-600' : 'text-gray-500'">
                {{ c.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-xs text-gray-500">
              {{ c.start_at ? new Date(c.start_at).toLocaleDateString('ru-RU') : '—' }} —
              {{ c.end_at ? new Date(c.end_at).toLocaleDateString('ru-RU') : '∞' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
