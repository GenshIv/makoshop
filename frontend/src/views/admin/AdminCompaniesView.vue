<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

const companies = ref([]);
const loading = ref(true);
const error = ref(null);

// Settings modal
const showSettingsModal = ref(false);
const settingsLoading = ref(false);
const selectedCompany = ref(null);
const companySettings = ref(null);

const paymentMethods = ref([]);
const deliveryTimes = ref([]);
const installmentPlans = ref([]);

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

const openSettings = async (company) => {
  selectedCompany.value = company;
  settingsLoading.value = true;
  try {
    // Fetch full lists
    const [pmRes, dtRes, ipRes] = await Promise.all([
      api.get('/admin/payment-methods'),
      api.get('/admin/delivery-times'),
      api.get('/admin/installment-plans'),
    ]);
    paymentMethods.value = pmRes.data || [];
    deliveryTimes.value = dtRes.data || [];
    installmentPlans.value = ipRes.data || [];

    // Fetch company settings
    const res = await api.get(`/admin/companies/${company.id}/settings`);
    companySettings.value = {
      payment_method_ids: (res.data.company?.payment_method_ids || []).map(Number),
      delivery_time_ids: (res.data.company?.delivery_time_ids || []).map(Number),
      installment_plan_ids: (res.data.company?.installment_plan_ids || []).map(Number),
    };
  } catch (e) {
    console.error('load settings:', e);
    companySettings.value = {
      payment_method_ids: [],
      delivery_time_ids: [],
      installment_plan_ids: [],
    };
  } finally {
    settingsLoading.value = false;
    showSettingsModal.value = true;
  }
};

const saveSettings = async () => {
  if (!selectedCompany.value || !companySettings.value) return;
  try {
    await api.patch(`/admin/companies/${selectedCompany.value.id}`, {
      payment_method_ids: companySettings.value.payment_method_ids,
      delivery_time_ids: companySettings.value.delivery_time_ids,
      installment_plan_ids: companySettings.value.installment_plan_ids,
    });
    showSettingsModal.value = false;
    alert(t('admin.settings_saved') || 'Settings saved');
  } catch (e) {
    alert(e.response?.data?.message || 'Save error');
  }
};

const toggleSelection = (listKey, id) => {
  const list = companySettings.value[listKey];
  const idx = list.indexOf(id);
  if (idx >= 0) {
    list.splice(idx, 1);
  } else {
    list.push(id);
  }
};

const isSelected = (listKey, id) => {
  return companySettings.value[listKey].includes(id);
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
              <button @click="openSettings(c)" class="text-purple-700 hover:underline text-xs">
                {{ t('admin.settings') || 'Settings' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Settings modal -->
    <div v-if="showSettingsModal" class="fixed inset-0 z-40 flex items-center justify-center bg-black/30">
      <div class="bg-white rounded-xl shadow-lg w-full max-w-3xl max-h-[90vh] overflow-y-auto p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold text-purple-700">
            {{ t('admin.company_settings_title') || 'Company Settings' }}: {{ selectedCompany?.name }}
          </h2>
          <button @click="showSettingsModal = false" class="text-gray-500 hover:text-gray-700 text-xl">&times;</button>
        </div>

        <div v-if="settingsLoading" class="text-sm text-gray-500">Loading...</div>
        <div v-else class="space-y-4">
          <!-- Payment methods -->
          <div>
            <div class="text-sm font-medium text-gray-700 mb-1">{{ t('admin.payment_methods') || 'Payment Methods' }}</div>
            <div class="flex flex-wrap gap-2">
              <label v-for="pm in paymentMethods" :key="pm.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isSelected('payment_method_ids', pm.id)" @change="toggleSelection('payment_method_ids', pm.id)" />
                {{ pm.name }}
              </label>
              <span v-if="paymentMethods.length === 0" class="text-xs text-gray-400">No payment methods defined.</span>
            </div>
          </div>

          <!-- Delivery times -->
          <div>
            <div class="text-sm font-medium text-gray-700 mb-1">{{ t('admin.delivery_times') || 'Delivery Times' }}</div>
            <div class="flex flex-wrap gap-2">
              <label v-for="dt in deliveryTimes" :key="dt.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isSelected('delivery_time_ids', dt.id)" @change="toggleSelection('delivery_time_ids', dt.id)" />
                {{ dt.name }}
              </label>
              <span v-if="deliveryTimes.length === 0" class="text-xs text-gray-400">No delivery times defined.</span>
            </div>
          </div>

          <!-- Installment plans -->
          <div>
            <div class="text-sm font-medium text-gray-700 mb-1">{{ t('admin.installment_plans') || 'Installment Plans' }}</div>
            <div class="flex flex-wrap gap-2">
              <label v-for="ip in installmentPlans" :key="ip.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isSelected('installment_plan_ids', ip.id)" @change="toggleSelection('installment_plan_ids', ip.id)" />
                {{ ip.name }}
              </label>
              <span v-if="installmentPlans.length === 0" class="text-xs text-gray-400">No installment plans defined.</span>
            </div>
          </div>
        </div>

        <div class="mt-4 flex justify-end gap-2">
          <button @click="showSettingsModal = false" class="px-3 py-1.5 text-xs rounded-md border border-gray-300 bg-white hover:bg-gray-50">
            {{ t('admin.cancel') }}
          </button>
          <button @click="saveSettings" class="px-3 py-1.5 text-xs rounded-md bg-purple-600 text-white hover:bg-purple-700">
            {{ t('admin.save') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
