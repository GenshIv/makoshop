<script setup>
import { ref, reactive, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useFormat } from '../../composables/useFormat';
import { useToast } from '../../composables/useToast';

const { t } = useI18n();
const { formatPrice } = useFormat();
const { toast } = useToast();

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
    toast.error(e.response?.data?.message || t('admin.error'));
  }
};

onMounted(() => {
  fetchCampaigns();
  fetchPlans();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.promo_title') }}</h1>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else class="space-y-6">
      <!-- Plans -->
      <div class="bg-surface rounded-lg shadow-sm p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="font-bold">{{ t('admin.promo_plans') }}</h2>
          <button @click="showPlanForm = true" class="px-3 py-1.5 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
            {{ t('admin.add_plan') }}
          </button>
        </div>

        <!-- Plan form -->
        <div v-if="showPlanForm" class="mb-3 p-3 bg-surface-2 rounded-lg">
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <input v-model="planForm.name" type="text" :placeholder="t('admin.plan_name_placeholder')" class="px-3 py-2 border border-line rounded-lg text-sm" />
            <input v-model.number="planForm.price_per_click" type="number" :placeholder="t('admin.price_per_click_placeholder')" class="px-3 py-2 border border-line rounded-lg text-sm" />
            <input v-model.number="planForm.price_per_impression" type="number" :placeholder="t('admin.price_per_impression_placeholder')" class="px-3 py-2 border border-line rounded-lg text-sm" />
          </div>
          <div class="flex gap-2 mt-2">
            <button @click="createPlan" class="px-3 py-1.5 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
              {{ t('admin.create') }}
            </button>
            <button @click="showPlanForm = false" class="px-3 py-1.5 border rounded-lg text-sm hover:bg-surface-2">
              {{ t('admin.cancel') }}
            </button>
          </div>
        </div>

        <div v-if="plans.length === 0" class="text-sm text-ink-3">{{ t('admin.no_plans') }}</div>
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <div v-for="plan in plans" :key="plan.id" class="border rounded-lg p-3 text-sm">
            <div class="font-medium">{{ plan.name }}</div>
            <div class="text-ink-3 mt-1">
              CPC: {{ formatPrice(plan.price_per_click || 0) }} / CPM: {{ formatPrice(plan.price_per_impression || 0) }}
            </div>
          </div>
        </div>
      </div>

      <!-- Campaigns -->
      <div class="bg-surface rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-3">{{ t('admin.campaigns') }}</h2>
        <div v-if="campaigns.length === 0" class="text-sm text-ink-3">{{ t('admin.no_campaigns') }}</div>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead class="bg-surface-2">
              <tr>
                <th scope="col" class="px-4 py-3 text-left">ID</th>
                <th scope="col" class="px-4 py-3 text-left">{{ t('admin.company') }}</th>
                <th scope="col" class="px-4 py-3 text-left">{{ t('admin.plan') }}</th>
                <th scope="col" class="px-4 py-3 text-left">{{ t('admin.budget') }}</th>
                <th scope="col" class="px-4 py-3 text-left">{{ t('admin.used') }}</th>
                <th scope="col" class="px-4 py-3 text-left">{{ t('admin.status') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in campaigns" :key="c.id" class="border-t hover:bg-surface-2">
                <td class="px-4 py-3">{{ c.id }}</td>
                <td class="px-4 py-3">{{ c.company_id }}</td>
                <td class="px-4 py-3">{{ c.plan_id }}</td>
                <td class="px-4 py-3">{{ formatPrice(c.budget) }}</td>
                <td class="px-4 py-3 text-ink-3">{{ formatPrice(c.budget_used || 0) }}</td>
                <td class="px-4 py-3">
                  <span :class="c.status === 'active' ? 'text-green-600' : 'text-ink-3'">
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
