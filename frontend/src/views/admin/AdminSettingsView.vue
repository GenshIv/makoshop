<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';

const { t } = useI18n();
const { toast } = useToast();

const settings = ref({
  default_currency: 'PLN',
});
const loading = ref(true);
const saving = ref(false);

const currencies = ['PLN', 'EUR', 'USD', 'RUB', 'UAH', 'GBP', 'CHF'];

const loadSettings = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/settings');
    settings.value = res.data;
  } catch (e) {
    toast.error(e.response?.data?.message || 'Failed to load settings');
  } finally {
    loading.value = false;
  }
};

const saveSettings = async () => {
  saving.value = true;
  try {
    await api.patch('/admin/settings', settings.value);
    toast.success(t('admin.settings_saved') || 'Settings saved');
  } catch (e) {
    toast.error(e.response?.data?.message || 'Failed to save settings');
  } finally {
    saving.value = false;
  }
};

onMounted(loadSettings);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="mb-6">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.settings') || 'Settings' }}</h1>
    </div>

    <div v-if="loading" class="text-sm text-ink-3">Loading...</div>
    <div v-else class="space-y-6">
      <!-- Global Settings -->
      <div class="bg-surface rounded-xl shadow-sm border border-line p-5">
        <h2 class="text-lg font-semibold text-ink-2 mb-4">{{ t('admin.global_settings') || 'Global Settings' }}</h2>
        <div class="space-y-4">
          <!-- Default Currency -->
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.default_currency') || 'Default Currency' }}</label>
            <select
              v-model="settings.default_currency"
              class="w-full max-w-xs px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            >
              <option v-for="cur in currencies" :key="cur" :value="cur">
                {{ cur }}
              </option>
            </select>
            <p class="text-xs text-ink-3 mt-1">{{ t('admin.default_currency_hint') || 'Currency used for all prices by default' }}</p>
          </div>
        </div>
        <div class="mt-4">
          <button
            @click="saveSettings"
            :disabled="saving"
            class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm font-medium hover:bg-purple-700 transition disabled:opacity-50"
          >
            {{ saving ? (t('admin.saving') || 'Saving...') : (t('admin.save') || 'Save') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
