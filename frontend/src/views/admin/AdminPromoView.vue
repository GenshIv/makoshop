<script setup>
import { ref, reactive, onMounted } from 'vue';
import api from '../../api';

const campaigns = ref([]);
const plans = ref([]);
const loading = ref(true);
const showPlanForm = ref(false);

const planForm = reactive({ name: '', price_per_click: 0, price_per_impression: 0 });

const fetchCampaigns = async () => {
  loading.value = true;
  try {
    const response = await api.get('/admin/promo/campaigns');
    campaigns.value = response.data.items || response.data || [];
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const fetchPlans = async () => {
  try {
    const response = await api.get('/admin/promo-plans');
    plans.value = response.data.items || response.data || [];
  } catch (e) {
    console.error(e);
  }
};

const createPlan = async () => {
  if (!planForm.name) return;
  try {
    await api.post('/admin/promo-plans', planForm);
    showPlanForm.value = false;
    Object.assign(planForm, { name: '', price_per_click: 0, price_per_impression: 0 });
    await fetchPlans();
  } catch (e) {
    alert(e.response?.data?.message || 'Ошибка');
  }
};

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

onMounted(() => {
  fetchCampaigns();
  fetchPlans();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">Промо</h1>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else class="space-y-6">
      <!-- Plans -->
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="font-bold">Планы продвижения</h2>
          <button @click="showPlanForm = true" class="px-3 py-1.5 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
            + План
          </button>
        </div>

        <!-- Plan form -->
        <div v-if="showPlanForm" class="mb-3 p-3 bg-gray-50 rounded-lg">
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <input v-model="planForm.name" type="text" placeholder="Название" class="px-3 py-2 border border-gray-300 rounded-lg text-sm" />
            <input v-model.number="planForm.price_per_click" type="number" placeholder="Цена за клик" class="px-3 py-2 border border-gray-300 rounded-lg text-sm" />
            <input v-model.number="planForm.price_per_impression" type="number" placeholder="Цена за показ" class="px-3 py-2 border border-gray-300 rounded-lg text-sm" />
          </div>
          <div class="flex gap-2 mt-2">
            <button @click="createPlan" class="px-3 py-1.5 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
              Создать
            </button>
            <button @click="showPlanForm = false" class="px-3 py-1.5 border rounded-lg text-sm hover:bg-gray-50">
              Отмена
            </button>
          </div>
        </div>

        <div v-if="plans.length === 0" class="text-sm text-gray-500">Планов пока нет</div>
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <div v-for="plan in plans" :key="plan.id" class="border rounded-lg p-3 text-sm">
            <div class="font-medium">{{ plan.name }}</div>
            <div class="text-gray-500 mt-1">
              CPC: {{ formatPrice(plan.price_per_click || 0) }} / CPM: {{ formatPrice(plan.price_per_impression || 0) }}
            </div>
          </div>
        </div>
      </div>

      <!-- Campaigns -->
      <div class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-3">Кампании</h2>
        <div v-if="campaigns.length === 0" class="text-sm text-gray-500">Кампаний пока нет</div>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-4 py-3 text-left">ID</th>
                <th class="px-4 py-3 text-left">Компания</th>
                <th class="px-4 py-3 text-left">План</th>
                <th class="px-4 py-3 text-left">Бюджет</th>
                <th class="px-4 py-3 text-left">Использовано</th>
                <th class="px-4 py-3 text-left">Статус</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in campaigns" :key="c.id" class="border-t hover:bg-gray-50">
                <td class="px-4 py-3">{{ c.id }}</td>
                <td class="px-4 py-3">{{ c.company_id }}</td>
                <td class="px-4 py-3">{{ c.plan_id }}</td>
                <td class="px-4 py-3">{{ formatPrice(c.budget) }}</td>
                <td class="px-4 py-3 text-gray-500">{{ formatPrice(c.budget_used || 0) }}</td>
                <td class="px-4 py-3">
                  <span :class="c.status === 'active' ? 'text-green-600' : 'text-gray-500'">
                    {{ c.status }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>
