<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';

const { t } = useI18n();
const { toast } = useToast();

const currencies = ['PLN', 'EUR', 'USD', 'RUB', 'UAH', 'GBP', 'CHF'];

// Home page hero text: per-locale manual overrides.
// Empty field = use the default i18n text for that locale.
const heroLangs = ['ru', 'ua', 'en', 'pl'];

const emptyHomeHero = () =>
  Object.fromEntries(heroLangs.map((l) => [l, { headline: '', sub: '', tagline: '' }]));

const normalizeHomeHero = (raw) => {
  const result = emptyHomeHero();
  for (const lang of heroLangs) {
    const block = raw?.[lang] || {};
    result[lang] = {
      headline: block.headline || '',
      sub: block.sub || '',
      tagline: block.tagline || '',
    };
  }
  return result;
};

const settings = ref({
  default_currency: 'PLN',
  ga_measurement_id: '',
  home_hero: emptyHomeHero(),
});
const loading = ref(true);
const saving = ref(false);

const loadSettings = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/settings');
    settings.value = {
      default_currency: res.data.default_currency || 'PLN',
      ga_measurement_id: res.data.ga_measurement_id || '',
      home_hero: normalizeHomeHero(res.data.home_hero),
    };
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

          <!-- Google Analytics Measurement ID -->
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.ga_measurement_id') || 'Google Analytics Measurement ID' }}</label>
            <input
              v-model.trim="settings.ga_measurement_id"
              type="text"
              placeholder="G-XXXXXXXXXX"
              class="w-full max-w-xs px-3 py-2 border border-line rounded-lg text-sm bg-surface font-mono"
            />
            <p class="text-xs text-ink-3 mt-1">{{ t('admin.ga_measurement_id_hint') || 'Leave empty to disable tracking. Format: G-XXXXXXXXXX' }}</p>
          </div>
        </div>
      </div>

      <!-- Home page hero text (manual management) -->
      <div class="bg-surface rounded-xl shadow-sm border border-line p-5">
        <h2 class="text-lg font-semibold text-ink-2 mb-1">
          {{ t('admin.home_hero_title') || 'Home page text' }}
        </h2>
        <p class="text-xs text-ink-3 mb-4">
          {{ t('admin.home_hero_hint') || 'Hero block text on the main page. Empty field — default text. An enabled branding banner replaces this text.' }}
        </p>
        <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <div
            v-for="lang in heroLangs"
            :key="lang"
            class="border border-line rounded-lg p-4 bg-surface-2/50"
          >
            <div class="text-xs font-bold uppercase tracking-wide text-ink-3 mb-3">{{ lang }}</div>
            <div class="space-y-3">
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">
                  {{ t('admin.home_hero_headline') || 'Headline' }}
                </label>
                <input
                  v-model.trim="settings.home_hero[lang].headline"
                  type="text"
                  maxlength="300"
                  class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
                />
              </div>
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">
                  {{ t('admin.home_hero_sub') || 'Subheadline' }}
                </label>
                <input
                  v-model.trim="settings.home_hero[lang].sub"
                  type="text"
                  maxlength="300"
                  class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
                />
              </div>
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">
                  {{ t('admin.home_hero_tagline') || 'Tagline' }}
                </label>
                <input
                  v-model.trim="settings.home_hero[lang].tagline"
                  type="text"
                  maxlength="300"
                  class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <div>
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
</template>
