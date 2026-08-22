<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';

const { t, locale } = useI18n();
const { toast } = useToast();

const catDisplayName = (cat) => {
  if (!cat) return String(cat?.id || '');
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || String(cat.id);
};
const router = useRouter();

const loading = ref(false);
const running = ref(false);
const results = ref(null);
const log = ref([]);

const form = ref({
  apply: false,
  limit: 1000,
  category_id: '',
  company_id: '',
});

const categories = ref([]);
const companies = ref([]);

const fetchCategories = async () => {
  try {
    const res = await api.get('/admin/categories');
    const data = res.data;
    categories.value = Array.isArray(data) ? data : (data?.items || []);
  } catch (e) {
    console.error('Failed to fetch categories:', e);
  }
};

const fetchCompanies = async () => {
  try {
    const res = await api.get('/admin/companies');
    companies.value = res.data || [];
  } catch (e) {
    console.error('Failed to fetch companies:', e);
  }
};

// Category import/export
const exportCategories = async () => {
  try {
    const res = await api.get('/admin/categories?export=1');
    const data = JSON.stringify(res.data, null, 2);
    const blob = new Blob([data], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'categories-export.json';
    a.click();
    URL.revokeObjectURL(url);
    addLog('Categories exported.');
  } catch (e) {
    console.error('Export error:', e);
    toast.error(t('admin.catalogizer_export_failed'));
  }
};

const importCategories = async () => {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.json';
  input.onchange = async (e) => {
    const file = e.target.files[0];
    if (!file) return;

    try {
      const text = await file.text();
      const data = JSON.parse(text);
      const res = await api.post('/admin/categories?import=1', data);
      addLog(`Import complete: created=${res.data.created}, updated=${res.data.updated}`);
      toast.success(t('admin.catalogizer_import_success'));
    } catch (e) {
      console.error('Import error:', e);
      toast.error(t('admin.catalogizer_import_failed', { error: e.response?.data?.message || e.message }));
    }
  };
  input.click();
};

// SCU Page catalogize all (TurboTopNByIntersection)
const catalogizeAllOpen = ref(false);

const askCatalogizeAll = () => {
  catalogizeAllOpen.value = true;
};

const runCatalogizeAll = async () => {
  catalogizeAllOpen.value = false;
  running.value = true;

  try {
    const res = await api.post('/admin/scupages/catalogize-all', {
      apply: true,
    });
    addLog(`Catalogize all complete: processed=${res.data.processed}, catalogized=${res.data.catalogized}`);
    toast.success(t('admin.catalogizer_catalogize_all_done', { count: res.data.catalogized }));
  } catch (e) {
    console.error('Catalogize all error:', e);
    toast.error(t('admin.catalogizer_catalogize_all_failed'));
  } finally {
    running.value = false;
  }
};

const runCatalogize = async () => {
  if (running.value) return;
  running.value = true;
  results.value = null;
  log.value = [];

  addLog('Starting catalogization...');

  const body = {
    apply: form.value.apply,
    limit: form.value.limit,
  };
  if (form.value.category_id) {
    body.category_id = parseInt(form.value.category_id);
  }
  if (form.value.company_id) {
    body.company_id = parseInt(form.value.company_id);
  }

  try {
    addLog(`Parameters: apply=${body.apply}, limit=${body.limit}`);
    const res = await api.post('/admin/catalogize', body);
    results.value = res.data;
    addLog(`Done. Processed: ${res.data.processed}, Matched: ${res.data.matched}`);
  } catch (e) {
    addLog(`Error: ${e.response?.data?.message || e.message}`);
    console.error('Catalogization error:', e);
  } finally {
    running.value = false;
  }
};

const addLog = (msg) => {
  log.value.push(new Date().toLocaleTimeString() + ' ' + msg);
};

// --- Test & Tune ---

const testName = ref('');
const testLoading = ref(false);
const testResults = ref(null);
const testError = ref('');

const runTest = async () => {
  if (!testName.value.trim()) return;
  testLoading.value = true;
  testResults.value = null;
  testError.value = '';
  try {
    const res = await api.post('/admin/catalogizer/test', { name: testName.value.trim() });
    testResults.value = res.data;
  } catch (e) {
    testError.value = e.response?.data?.message || e.message;
  } finally {
    testLoading.value = false;
  }
};

// Update anchor_keywords for a category
const updateAnchorKeywords = async (catId, newKeywords) => {
  try {
    await api.patch(`/admin/categories/${catId}`, { anchor_keywords: newKeywords });
    // Refresh categories list
    await fetchCategories();
    return true;
  } catch (e) {
    console.error('Failed to update anchor_keywords:', e);
    toast.error(t('admin.catalogizer_update_failed', { error: e.response?.data?.message || e.message }));
    return false;
  }
};

const addTokenToCategory = async (catId, token) => {
  const t = (token || '').trim().toLowerCase();
  if (!t) return;
  const cat = categories.value.find(c => c.id === catId);
  if (!cat) return;
  const current = cat.anchor_keywords || [];
  if (current.includes(t)) {
    toast.info(t('admin.catalogizer_token_exists'));
    return;
  }
  const updated = [...current, t].slice(0, 50);
  if (await updateAnchorKeywords(catId, updated)) {
    addLog(`Added token "${t}" to category "${catDisplayName(cat)}"`);
  }
};

const removeTokenFromCategory = async (catId, token) => {
  const cat = categories.value.find(c => c.id === catId);
  if (!cat) return;
  const current = cat.anchor_keywords || [];
  const updated = current.filter(kw => kw !== token);
  if (await updateAnchorKeywords(catId, updated)) {
    addLog(`Removed token "${token}" from category "${catDisplayName(cat)}"`);
  }
};

// Get category object by id
const getCategoryById = (id) => categories.value.find(c => c.id === id);

// Local state for "add token" inputs per category
const addTokenInputs = ref({});

const onAddTokenInput = (catId, event) => {
  addTokenInputs.value[catId] = event.target.value;
};

const handleAddTokenEnter = (catId) => {
  addTokenToCategory(catId, addTokenInputs.value[catId] || '');
  addTokenInputs.value[catId] = '';
};

// --- Coverage ---

const coverageLoading = ref(false);
const coverageData = ref(null);

const fetchCoverage = async () => {
  coverageLoading.value = true;
  coverageData.value = null;
  try {
    const res = await api.get('/admin/catalogizer/coverage');
    coverageData.value = res.data;
  } catch (e) {
    console.error('Coverage error:', e);
    addLog('Failed to fetch coverage: ' + (e.response?.data?.message || e.message));
  } finally {
    coverageLoading.value = false;
  }
};

// --- Train ---



onMounted(() => {
  fetchCategories();
  fetchCompanies();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-purple-700">
        {{ t('admin.catalogizer_title') || 'Auto-Catalogizer' }}
      </h1>
      <router-link
        to="/admin"
        class="text-sm text-ink-3 hover:text-purple-600"
      >
        {{ t('admin.back_to_dashboard') || 'Back to Dashboard' }}
      </router-link>
    </div>

    <!-- Description -->
    <div class="mb-6 bg-purple-50 border border-purple-200 rounded-lg p-4">
      <h2 class="font-medium text-purple-800 mb-2">
        {{ t('admin.catalogizer_desc_title') || 'How it works' }}
      </h2>
      <p class="text-sm text-purple-700">
        {{ t('admin.catalogizer_desc') || 'The catalogizer analyzes product names, descriptions, and attributes, then matches them against category anchor keywords to determine the best category. First, set anchor keywords for your categories, then run the catalogizer.' }}
      </p>
    </div>

    <!-- Category Management -->
    <div class="mb-6 bg-surface rounded-lg shadow-sm p-6">
      <h2 class="font-semibold mb-3">{{ t('admin.catalogizer_category_management') || 'Category Management' }}</h2>
      <p class="text-sm text-ink-3 mb-3">
        {{ t('admin.catalogizer_category_mgmt_desc') || 'Export/import category tree with anchor keywords and attributes.' }}
      </p>
      <div class="flex flex-wrap gap-2">
        <button
          @click="exportCategories"
          class="px-4 py-2 text-sm bg-blue-600 text-white rounded-lg hover:bg-blue-700"
        >
          {{ t('admin.catalogizer_export_categories') || 'Export Categories' }}
        </button>
        <button
          @click="importCategories"
          class="px-4 py-2 text-sm bg-green-600 text-white rounded-lg hover:bg-green-700"
        >
          {{ t('admin.catalogizer_import_categories') || 'Import Categories' }}
        </button>
        <button
          @click="askCatalogizeAll"
          :disabled="running"
          class="px-4 py-2 text-sm bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:opacity-50"
        >
          {{ t('admin.catalogizer_catalogize_all_scupages') || 'Catalogize All SCU Pages' }}
        </button>
      </div>
    </div>

    <!-- Test & Tune -->
    <div class="mb-6 bg-surface rounded-lg shadow-sm p-6">
      <h2 class="font-semibold mb-3">Test & Tune</h2>
      <p class="text-sm text-ink-3 mb-3">
        Enter a product name to see how it would be catalogized. You can add/remove tokens directly from the results to tune categories.
      </p>

      <div class="flex gap-2 mb-4">
        <input
          v-model="testName"
          @keydown.enter="runTest"
          type="text"
          placeholder="e.g. ASUS ROG Strix G15"
          class="flex-1 px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
        />
        <button
          @click="runTest"
          :disabled="testLoading || !testName.trim()"
          class="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:opacity-50"
        >
          {{ testLoading ? 'Testing...' : 'Test' }}
        </button>
      </div>

      <div v-if="testError" class="mb-3 text-sm text-red-600">{{ testError }}</div>

      <div v-if="testResults">
        <div class="mb-2 text-sm text-ink-2">
          Tokens: {{ testResults.tokens?.join(', ') || '—' }}
        </div>

        <div v-if="!testResults.matches || testResults.matches.length === 0" class="text-sm text-ink-3">
          No matches found. Try adding anchor keywords to relevant categories.
        </div>

        <div v-else class="space-y-4">
          <div
            v-for="m in testResults.matches"
            :key="m.NewCategoryID"
            class="border rounded-lg p-3"
          >
            <div class="flex justify-between items-center mb-2">
              <div>
                <span class="font-medium">{{ catDisplayName(getCategoryById(m.NewCategoryID)) || 'Cat #' + m.NewCategoryID }}</span>
                <span class="ml-2 text-xs text-ink-3">slug: {{ m.NewCategorySlug }}</span>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-sm font-semibold text-purple-700">Score: {{ m.Score }}</span>
                <span class="text-xs text-ink-3">matched: {{ (m.MatchedTokens||[]).join(', ') }}</span>
              </div>
            </div>

            <!-- Current anchor keywords -->
            <div class="mb-2">
              <div class="text-xs text-ink-3 mb-1">Anchor keywords:</div>
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="kw in (getCategoryById(m.NewCategoryID)?.anchor_keywords||[])"
                  :key="kw"
                  class="inline-flex items-center gap-1 px-2 py-0.5 bg-purple-100 text-purple-800 rounded text-xs"
                >
                  {{ kw }}
                  <button
                    @click="removeTokenFromCategory(m.NewCategoryID, kw)"
                    class="text-purple-500 hover:text-red-600 font-bold leading-none"
                  >×</button>
                </span>
                <span v-if="!(getCategoryById(m.NewCategoryID)?.anchor_keywords||[]).length" class="text-xs text-ink-3">(none)</span>
              </div>
            </div>

            <!-- Add token -->
            <div class="flex gap-2">
              <input
                type="text"
                placeholder="Add token (e.g. asus)"
                class="flex-1 px-2 py-1 text-xs border border-line rounded"
                :value="addTokenInputs[m.NewCategoryID] || ''"
                @input="onAddTokenInput(m.NewCategoryID, $event)"
                @keydown.enter="handleAddTokenEnter(m.NewCategoryID)"
              />
              <button
                @click="handleAddTokenEnter(m.NewCategoryID)"
                class="px-2 py-1 text-xs bg-green-600 text-white rounded hover:bg-green-700"
              >+</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Coverage -->
    <div class="mb-6 bg-surface rounded-lg shadow-sm p-6">
      <div class="flex justify-between items-center mb-3">
        <h2 class="font-semibold">{{ t('admin.coverage') }}</h2>
        <button
          @click="fetchCoverage"
          :disabled="coverageLoading"
          class="px-3 py-1 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {{ coverageLoading ? t('common.loading') : t('admin.refresh') }}
        </button>
      </div>

      <div v-if="!coverageData" class="text-sm text-ink-3">
        {{ t('admin.coverage_hint') }}
      </div>

      <div v-else>
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-4">
          <div class="bg-surface-2 rounded p-3">
            <div class="text-xs text-ink-3">{{ t('admin.total_categories') }}</div>
            <div class="text-xl font-bold">{{ coverageData.total_categories || 0 }}</div>
          </div>
          <div class="bg-green-50 rounded p-3">
            <div class="text-xs text-green-600">{{ t('admin.with_keywords') }}</div>
            <div class="text-xl font-bold text-green-700">{{ coverageData.with_keywords || 0 }}</div>
          </div>
          <div class="bg-yellow-50 rounded p-3">
            <div class="text-xs text-yellow-600">{{ t('admin.empty_no_keywords') }}</div>
            <div class="text-xl font-bold text-yellow-700">{{ coverageData.empty || 0 }}</div>
          </div>
          <div class="bg-blue-50 rounded p-3">
            <div class="text-xs text-blue-600">{{ t('admin.active_categories') }}</div>
            <div class="text-xl font-bold text-blue-700">{{ coverageData.active || 0 }}</div>
          </div>
        </div>

        <!-- Categories with few tokens -->
        <div v-if="coverageData.few_tokens && coverageData.few_tokens.length" class="mb-3">
          <div class="text-sm font-medium mb-1">{{ t('admin.few_tokens') }}:</div>
          <div class="flex flex-wrap gap-1">
            <span
              v-for="c in coverageData.few_tokens"
              :key="c.id"
              class="inline-block px-2 py-0.5 bg-yellow-100 text-yellow-800 rounded text-xs"
            >
              {{ c.name }} ({{ c.token_count }})
            </span>
          </div>
        </div>

        <!-- Categories with many tokens -->
        <div v-if="coverageData.many_tokens && coverageData.many_tokens.length">
          <div class="text-sm font-medium mb-1">{{ t('admin.many_tokens') }}:</div>
          <div class="flex flex-wrap gap-1">
            <span
              v-for="c in coverageData.many_tokens"
              :key="c.id"
              class="inline-block px-2 py-0.5 bg-green-100 text-green-800 rounded text-xs"
            >
              {{ c.name }} ({{ c.token_count }})
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- Form -->
    <div class="bg-surface rounded-lg shadow-sm p-6 mb-6">
      <h2 class="font-semibold mb-4">{{ t('admin.catalogizer_settings') || 'Settings' }}</h2>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
        <div>
          <label class="block text-sm font-medium text-ink-2 mb-1">
            {{ t('admin.catalogizer_limit') || 'Max products to process' }}
          </label>
          <input
            v-model.number="form.limit"
            type="number"
            min="1"
            max="1000000"
            class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
          />
        </div>

        <div>
          <label class="block text-sm font-medium text-ink-2 mb-1">
            {{ t('admin.catalogizer_category') || 'Category (optional)' }}
          </label>
          <select
            v-model="form.category_id"
            class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
          >
            <option value="">{{ t('admin.catalogizer_all_categories') || 'All categories' }}</option>
            <option
              v-for="cat in categories"
              :key="cat.id"
              :value="cat.id"
            >
              {{ catDisplayName(cat) }} ({{ cat.id }})
            </option>
          </select>
        </div>

        <div>
          <label class="block text-sm font-medium text-ink-2 mb-1">
            {{ t('admin.catalogizer_company') || 'Company (optional)' }}
          </label>
          <select
            v-model="form.company_id"
            class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
          >
            <option value="">{{ t('admin.catalogizer_all_companies') || 'All companies' }}</option>
            <option
              v-for="comp in companies"
              :key="comp.id"
              :value="comp.id"
            >
              {{ comp.name }} ({{ comp.id }})
            </option>
          </select>
        </div>

        <div>
          <label class="flex items-center space-x-2 mt-6">
            <input
              v-model="form.apply"
              type="checkbox"
              class="rounded border-line text-purple-600 focus:ring-purple-500"
            />
            <span class="text-sm font-medium text-ink-2">
              {{ t('admin.catalogizer_apply') || 'Apply changes' }}
            </span>
          </label>
          <p class="text-xs text-ink-3 mt-1">
            {{ t('admin.catalogizer_apply_hint') || 'If unchecked, only shows recommendations without changing categories.' }}
          </p>
        </div>
      </div>

      <button
        @click="runCatalogize"
        :disabled="running"
        class="px-6 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:opacity-50 disabled:cursor-not-allowed"
      >
        <span v-if="running">{{ t('admin.catalogizer_running') || 'Running...' }}</span>
        <span v-else>{{ t('admin.catalogizer_run') || 'Run Catalogizer' }}</span>
      </button>
    </div>

    <!-- Log -->
    <div v-if="log.length" class="bg-gray-900 rounded-lg p-4 mb-6 font-mono text-xs text-green-400 overflow-y-auto max-h-48">
      <div v-for="(line, i) in log" :key="i">{{ line }}</div>
    </div>

    <!-- Results -->
    <div v-if="results" class="bg-surface rounded-lg shadow-sm p-6">
      <h2 class="font-semibold mb-4">{{ t('admin.catalogizer_results') || 'Results' }}</h2>

      <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-4">
        <div class="bg-surface-2 rounded-lg p-4">
          <div class="text-sm text-ink-3">{{ t('admin.catalogizer_processed') || 'Processed' }}</div>
          <div class="text-2xl font-bold">{{ results.processed }}</div>
        </div>
        <div class="bg-green-50 rounded-lg p-4">
          <div class="text-sm text-green-600">{{ t('admin.catalogizer_matched') || 'Matched' }}</div>
          <div class="text-2xl font-bold text-green-700">{{ results.matched }}</div>
        </div>
        <div class="bg-blue-50 rounded-lg p-4">
          <div class="text-sm text-blue-600">{{ t('admin.catalogizer_applied') || 'Applied' }}</div>
          <div class="text-2xl font-bold text-blue-700">{{ results.apply ? results.matched : 0 }}</div>
        </div>
      </div>

      <!-- Sample results -->
      <div v-if="results.results && results.results.length" class="mt-4">
        <h3 class="font-medium text-sm mb-2">
          {{ t('admin.catalogizer_sample') || 'Sample matches' }} ({{ results.results.length }} shown)
        </h3>
        <div class="overflow-x-auto">
          <table class="w-full text-xs">
            <thead class="bg-surface-2">
              <tr>
                <th scope="col" class="px-3 py-2 text-left">Product ID</th>
                <th scope="col" class="px-3 py-2 text-left">Old Category</th>
                <th scope="col" class="px-3 py-2 text-left">New Category</th>
                <th scope="col" class="px-3 py-2 text-left">Score</th>
                <th scope="col" class="px-3 py-2 text-left">Keywords</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="r in results.results"
                :key="r.product_id"
                class="border-t"
              >
                <td class="px-3 py-2">{{ r.product_id }}</td>
                <td class="px-3 py-2">{{ r.old_category_id }}</td>
                <td class="px-3 py-2">
                  <span class="text-green-600">{{ r.new_category_slug }}</span>
                </td>
                <td class="px-3 py-2">{{ r.score }}</td>
                <td class="px-3 py-2">
                  <span
                    v-for="kw in r.matched_keywords || []"
                    :key="kw"
                    class="inline-block px-1 py-0.5 bg-purple-100 text-purple-700 rounded text-[11px] mr-1"
                  >
                    {{ kw }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :open="catalogizeAllOpen"
      :title="t('admin.catalogizer_catalogize_all_scupages')"
      :message="t('admin.catalogizer_catalogize_all_confirm')"
      :confirm-text="t('admin.save')"
      :cancel-text="t('admin.cancel')"
      @confirm="runCatalogizeAll"
      @cancel="catalogizeAllOpen = false"
    />
  </div>
</template>
