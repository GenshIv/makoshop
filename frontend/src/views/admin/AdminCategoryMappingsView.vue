<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';

const { t } = useI18n();
const { toast } = useToast();

// Mappings list
const mappings = ref([]);
const loading = ref(true);
const deletingId = ref(null);

// Scan results
const scanning = ref(false);
const scanResults = ref([]);

// Editor form
const editing = ref(false);
const saving = ref(false);
const form = ref({
  id: null,
  source_code: '',
  target_category_id: '',
  company_id: null,
});

// Categories for dropdown
const categories = ref([]);

const loadMappings = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/category-mappings');
    mappings.value = res.data || [];
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.load_error'));
  } finally {
    loading.value = false;
  }
};

const loadCategories = async () => {
  try {
    const res = await api.get('/admin/categories');
    // Flatten tree to list for dropdown
    const flatten = (nodes) => {
      let result = [];
      for (const node of nodes) {
        result.push({ id: node.id, name: node.name_ru || node.name });
        if (node.children && node.children.length > 0) {
          result = result.concat(flatten(node.children));
        }
      }
      return result;
    };
    categories.value = flatten(res.data || []);
  } catch (e) {
    console.error('Failed to load categories:', e);
  }
};

const openNew = () => {
  form.value = { id: null, source_code: '', target_category_id: '', company_id: null };
  editing.value = true;
};

const openEdit = (mapping) => {
  form.value = {
    id: mapping.id,
    source_code: mapping.source_code,
    target_category_id: mapping.target_category_id,
    company_id: mapping.company_id,
  };
  editing.value = true;
};

const closeEditor = () => {
  editing.value = false;
};

const saveMapping = async () => {
  if (!form.value.source_code || !form.value.target_category_id) {
    toast.error(t('admin.category_mappings.fill_required'));
    return;
  }
  saving.value = true;
  try {
    const payload = {
      source_code: form.value.source_code,
      target_category_id: parseInt(form.value.target_category_id),
      company_id: form.value.company_id ? parseInt(form.value.company_id) : null,
    };
    if (form.value.id) {
      await api.patch(`/admin/category-mappings/${form.value.id}`, payload);
      toast.success(t('admin.category_mappings.updated'));
    } else {
      await api.post('/admin/category-mappings', payload);
      toast.success(t('admin.category_mappings.created'));
    }
    editing.value = false;
    loadMappings();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.save_error'));
  } finally {
    saving.value = false;
  }
};

const deleteMapping = async (id) => {
  deletingId.value = id;
};

const confirmDelete = async () => {
  if (!deletingId.value) return;
  try {
    await api.delete(`/admin/category-mappings/${deletingId.value}`);
    toast.success(t('admin.category_mappings.deleted'));
    loadMappings();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.delete_error'));
  } finally {
    deletingId.value = null;
  }
};

const scanPriceFiles = async () => {
  scanning.value = true;
  scanResults.value = [];
  try {
    const res = await api.post('/admin/category-mappings/scan');
    scanResults.value = res.data || [];
    toast.success(t('admin.category_mappings.scan_complete'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.scan_error'));
  } finally {
    scanning.value = false;
  }
};

const exportMappings = async () => {
  try {
    const res = await api.get('/admin/category-mappings/export', { responseType: 'blob' });
    const url = window.URL.createObjectURL(res.data);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'category-mappings.json';
    a.click();
    window.URL.revokeObjectURL(url);
    toast.success(t('admin.category_mappings.export_success'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.export_error'));
  }
};

const exportScanResults = () => {
  if (scanResults.value.length === 0) return;
  const data = JSON.stringify(scanResults.value, null, 2);
  const blob = new Blob([data], { type: 'application/json' });
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'category-mappings-scan.json';
  a.click();
  window.URL.revokeObjectURL(url);
  toast.success(t('admin.category_mappings.export_scan_success'));
};

const importMappings = async (event) => {
  const file = event.target.files[0];
  if (!file) return;
  try {
    const text = await file.text();
    const data = JSON.parse(text);
    const res = await api.post('/admin/category-mappings/import', data);
    toast.success(t('admin.category_mappings.import_success', { count: res.data.imported }));
    loadMappings();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.import_error'));
  }
  event.target.value = '';
};

const useSuggestion = (suggestion) => {
  form.value = {
    id: null,
    source_code: suggestion.source_code,
    target_category_id: suggestion.top_category_id,
    company_id: null,
  };
  editing.value = true;
};

const applyAllSuggestions = async () => {
  if (scanResults.value.length === 0) return;
  try {
    const res = await api.post('/admin/category-mappings/apply-scan', scanResults.value);
    toast.success(t('admin.category_mappings.apply_all_success', { count: res.data.applied }));
    loadMappings();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.category_mappings.apply_all_error'));
  }
};

const getCategoryName = (id) => {
  const cat = categories.value.find(c => c.id === id);
  return cat ? cat.name : `#${id}`;
};

onMounted(async () => {
  await Promise.all([loadMappings(), loadCategories()]);
});
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">{{ t('admin.category_mappings.title') }}</h1>

    <!-- Scan Section -->
    <section class="mb-8 bg-gray-50 dark:bg-gray-800 rounded-lg p-4">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold">{{ t('admin.category_mappings.scan_title') }}</h2>
        <button
          @click="scanPriceFiles"
          :disabled="scanning"
          class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {{ scanning ? t('admin.category_mappings.scanning') : t('admin.category_mappings.scan_button') }}
        </button>
      </div>

      <p class="text-sm text-gray-600 dark:text-gray-400 mb-4">
        {{ t('admin.category_mappings.scan_description') }}
      </p>

      <!-- Scan Results -->
      <div v-if="scanResults.length > 0">
        <div class="flex items-center justify-between mb-4">
          <p class="text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.category_mappings.found_suggestions', { count: scanResults.length }) }}
          </p>
          <div class="flex space-x-2">
            <button
              @click="exportScanResults"
              class="px-3 py-1.5 border rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-sm"
            >
              📤 {{ t('admin.category_mappings.export_scan') }}
            </button>
            <button
              @click="applyAllSuggestions"
              class="px-3 py-1.5 bg-green-600 text-white rounded hover:bg-green-700 text-sm"
            >
              {{ t('admin.category_mappings.apply_all') }}
            </button>
          </div>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-200 dark:border-gray-700">
                <th class="text-left py-2 px-3">{{ t('admin.category_mappings.source_code') }}</th>
                <th class="text-left py-2 px-3">{{ t('admin.category_mappings.products') }}</th>
                <th class="text-left py-2 px-3">{{ t('admin.category_mappings.suggested_category') }}</th>
                <th class="text-left py-2 px-3">{{ t('admin.category_mappings.confidence') }}</th>
                <th class="text-left py-2 px-3">{{ t('admin.category_mappings.source') }}</th>
                <th class="text-left py-2 px-3">{{ t('admin.category_mappings.action') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(result, idx) in scanResults" :key="idx" class="border-b border-gray-100 dark:border-gray-800">
                <td class="py-2 px-3 font-mono text-xs">{{ result.source_code }}</td>
                <td class="py-2 px-3">{{ result.total_products }}</td>
                <td class="py-2 px-3">{{ getCategoryName(result.top_category_id) }}</td>
                <td class="py-2 px-3">{{ Math.round(result.confidence) }}%</td>
                <td class="py-2 px-3 text-xs text-gray-500">
                  {{ result.companies.length }} {{ t('admin.category_mappings.company') }}
                </td>
                <td class="py-2 px-3">
                  <button
                    @click="useSuggestion(result)"
                    class="text-blue-600 hover:text-blue-800 text-xs"
                  >
                    {{ t('admin.category_mappings.use_mapping') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </section>

    <!-- Mappings List -->
    <section class="mb-8">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold">{{ t('admin.category_mappings.mappings_title') }}</h2>
        <div class="flex space-x-2">
          <button
            @click="exportMappings"
            class="px-3 py-2 border rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-sm"
          >
            📤 {{ t('admin.category_mappings.export') }}
          </button>
          <label class="px-3 py-2 border rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-sm cursor-pointer">
            📥 {{ t('admin.category_mappings.import') }}
            <input type="file" accept=".json" @change="importMappings" class="hidden" />
          </label>
          <button
            @click="openNew"
            class="px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700"
          >
            {{ t('admin.category_mappings.add_mapping') }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="text-center py-8 text-gray-500">
        {{ t('common.loading') }}
      </div>

      <div v-else-if="mappings.length === 0" class="text-center py-8 text-gray-500">
        {{ t('admin.category_mappings.no_mappings') }}
      </div>

      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
              <th class="text-left py-3 px-4">{{ t('admin.category_mappings.source_code') }}</th>
              <th class="text-left py-3 px-4">{{ t('admin.category_mappings.target_category') }}</th>
              <th class="text-left py-3 px-4">{{ t('admin.category_mappings.scope') }}</th>
              <th class="text-left py-3 px-4">{{ t('admin.category_mappings.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="mapping in mappings" :key="mapping.id" class="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800">
              <td class="py-3 px-4 font-mono text-xs">{{ mapping.source_code }}</td>
              <td class="py-3 px-4">{{ getCategoryName(mapping.target_category_id) }}</td>
              <td class="py-3 px-4 text-xs text-gray-500">
                {{ mapping.company_id ? `Company #${mapping.company_id}` : t('admin.category_mappings.global') }}
              </td>
              <td class="py-3 px-4">
                <button
                  @click="openEdit(mapping)"
                  class="text-blue-600 hover:text-blue-800 mr-3 text-xs"
                >
                  {{ t('common.edit') }}
                </button>
                <button
                  @click="deleteMapping(mapping.id)"
                  class="text-red-600 hover:text-red-800 text-xs"
                >
                  {{ t('common.delete') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Editor Modal -->
    <div v-if="editing" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div class="bg-white dark:bg-gray-900 rounded-lg p-6 w-full max-w-md mx-4">
        <h3 class="text-lg font-semibold mb-4">
          {{ form.id ? t('admin.category_mappings.edit_mapping') : t('admin.category_mappings.new_mapping') }}
        </h3>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium mb-1">{{ t('admin.category_mappings.source_code') }}</label>
            <input
              v-model="form.source_code"
              type="text"
              class="w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
              :placeholder="t('admin.category_mappings.source_code_placeholder')"
            />
          </div>

          <div>
            <label class="block text-sm font-medium mb-1">{{ t('admin.category_mappings.target_category') }}</label>
            <select
              v-model="form.target_category_id"
              class="w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
            >
              <option value="">{{ t('admin.category_mappings.select_category') }}</option>
              <option v-for="cat in categories" :key="cat.id" :value="cat.id">
                {{ cat.name }}
              </option>
            </select>
          </div>

          <div>
            <label class="block text-sm font-medium mb-1">{{ t('admin.category_mappings.scope') }}</label>
            <select
              v-model="form.company_id"
              class="w-full px-3 py-2 border rounded dark:bg-gray-800 dark:border-gray-700"
            >
              <option :value="null">{{ t('admin.category_mappings.global') }}</option>
              <!-- Company options would be loaded here -->
            </select>
          </div>
        </div>

        <div class="flex justify-end space-x-3 mt-6">
          <button
            @click="closeEditor"
            class="px-4 py-2 border rounded hover:bg-gray-100 dark:hover:bg-gray-800"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            @click="saveMapping"
            :disabled="saving"
            class="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
          >
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Delete Confirmation -->
    <ConfirmDialog
      :open="deletingId !== null"
      :title="t('admin.category_mappings.confirm_delete')"
      :message="t('admin.category_mappings.delete_message')"
      @confirm="confirmDelete"
      @cancel="deletingId = null"
    />
  </div>
</template>