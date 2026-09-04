<script setup>
import { ref, computed, onMounted } from 'vue';
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

const emptyHomeOffers = () => ({ category_ids: [], per_section: 0 });

const settings = ref({
  default_currency: 'PLN',
  ga_measurement_id: '',
  home_hero: emptyHomeHero(),
  home_offers: emptyHomeOffers(),
});
const loading = ref(true);
const saving = ref(false);

// Root categories for the offers picker (order + selection).
const rootCategories = ref([]);
const catsLoading = ref(false);

const catName = (cat) => cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || cat.slug;

const loadRootCategories = async () => {
  catsLoading.value = true;
  try {
    const res = await api.get('/categories/tree');
    rootCategories.value = Array.isArray(res.data) ? res.data : [];
  } catch (e) {
    console.error('Failed to fetch categories:', e);
    rootCategories.value = [];
  } finally {
    catsLoading.value = false;
  }
};

// Picker rows: selected categories in the configured order first (with
// position numbers), then the rest in tree order.
const categoryRows = computed(() => {
  const byID = new Map(rootCategories.value.map((c) => [c.id, c]));
  const selected = settings.value.home_offers.category_ids
    .map((id) => byID.get(id))
    .filter(Boolean)
    .map((cat) => ({ cat, selected: true }));
  const chosen = new Set(selected.map((r) => r.cat.id));
  const rest = rootCategories.value
    .filter((c) => !chosen.has(c.id))
    .map((cat) => ({ cat, selected: false }));
  return [...selected, ...rest];
});

const toggleCategory = (row) => {
  const ids = settings.value.home_offers.category_ids;
  if (row.selected) {
    settings.value.home_offers.category_ids = ids.filter((id) => id !== row.cat.id);
  } else {
    ids.push(row.cat.id);
  }
};

const moveCategory = (row, dir) => {
  const ids = [...settings.value.home_offers.category_ids];
  const idx = ids.indexOf(row.cat.id);
  const target = idx + dir;
  if (idx < 0 || target < 0 || target >= ids.length) return;
  [ids[idx], ids[target]] = [ids[target], ids[idx]];
  settings.value.home_offers.category_ids = ids;
};

const loadSettings = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/settings');
    settings.value = {
      default_currency: res.data.default_currency || 'PLN',
      ga_measurement_id: res.data.ga_measurement_id || '',
      home_hero: normalizeHomeHero(res.data.home_hero),
      home_offers: {
        category_ids: Array.isArray(res.data.home_offers?.category_ids)
          ? res.data.home_offers.category_ids.map(Number).filter((n) => n > 0)
          : [],
        per_section: Number(res.data.home_offers?.per_section) || 0,
      },
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

onMounted(() => {
  loadSettings();
  loadRootCategories();
});
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

      <!-- Home page offers (category sections) -->
      <div class="bg-surface rounded-xl shadow-sm border border-line p-5">
        <h2 class="text-lg font-semibold text-ink-2 mb-1">
          {{ t('admin.home_offers_title') || 'Home page offers' }}
        </h2>
        <p class="text-xs text-ink-3 mb-4">
          {{ t('admin.home_offers_hint') || 'Category sections with random products on the home page. Empty list — all root categories in their default order.' }}
        </p>
        <div class="space-y-4">
          <!-- Carousel size -->
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">
              {{ t('admin.home_offers_per_section') || 'Products per carousel' }}
            </label>
            <input
              v-model.number="settings.home_offers.per_section"
              type="number"
              min="0"
              max="20"
              class="w-24 px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            />
            <p class="text-xs text-ink-3 mt-1">
              {{ t('admin.home_offers_per_section_hint') || '0 — default (12). From 1 to 20.' }}
            </p>
          </div>

          <!-- Category selection + order -->
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">
              {{ t('admin.home_offers_categories') || 'Categories and order' }}
            </label>
            <div v-if="catsLoading" class="text-xs text-ink-3">Loading categories...</div>
            <div v-else-if="categoryRows.length === 0" class="text-xs text-ink-3">
              {{ t('admin.home_offers_no_categories') || 'No root categories with products yet.' }}
            </div>
            <div v-else class="border border-line rounded-lg divide-y divide-line max-h-80 overflow-y-auto">
              <div
                v-for="row in categoryRows"
                :key="row.cat.id"
                class="flex items-center gap-3 px-3 py-2"
              >
                <input
                  type="checkbox"
                  :checked="row.selected"
                  class="w-4 h-4 accent-purple-600"
                  @change="toggleCategory(row)"
                />
                <span
                  v-if="row.selected"
                  class="w-6 h-6 flex items-center justify-center rounded-full bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 text-xs font-bold flex-shrink-0"
                >
                  {{ settings.home_offers.category_ids.indexOf(row.cat.id) + 1 }}
                </span>
                <span class="flex-1 text-sm text-ink truncate">{{ catName(row.cat) }}</span>
                <div class="flex items-center gap-1 flex-shrink-0">
                  <button
                    type="button"
                    :aria-label="t('admin.home_offers_move_up') || 'Move up'"
                    :disabled="!row.selected || settings.home_offers.category_ids.indexOf(row.cat.id) === 0"
                    class="p-1 rounded border border-line text-ink-2 hover:bg-surface-2 disabled:opacity-30"
                    @click="moveCategory(row, -1)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 15l7-7 7 7" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    :aria-label="t('admin.home_offers_move_down') || 'Move down'"
                    :disabled="!row.selected || settings.home_offers.category_ids.indexOf(row.cat.id) === settings.home_offers.category_ids.length - 1"
                    class="p-1 rounded border border-line text-ink-2 hover:bg-surface-2 disabled:opacity-30"
                    @click="moveCategory(row, 1)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
                    </svg>
                  </button>
                </div>
              </div>
            </div>
            <p class="text-xs text-ink-3 mt-1">
              {{ t('admin.home_offers_categories_hint') || 'Check categories for the home page; arrows set the display order. Unchecked categories are not shown.' }}
            </p>
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
