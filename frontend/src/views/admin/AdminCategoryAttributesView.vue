<script setup>
import { ref, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';

const { t } = useI18n();
const { toast } = useToast();

const route = useRoute();
const router = useRouter();

const categoryId = computed(() => parseInt(route.params.id, 10));
const category = ref(null);
const attributes = ref([]);
const loading = ref(true);
const error = ref(null);
const showAddForm = ref(false);
const newCode = ref('');

// Anchor keywords
const editingKeywords = ref(false);
const keywordsInput = ref('');

const saveKeywords = async () => {
  if (!category.value) return;
  const keywords = keywordsInput.value
    .split(',')
    .map(k => k.trim().toLowerCase())
    .filter(k => k.length > 0);

  try {
    await api.patch(`/admin/categories/${categoryId.value}`, {
      anchor_keywords: keywords,
    });
    category.value.anchor_keywords = keywords;
    editingKeywords.value = false;
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.save_error'));
  }
};

const startEditKeywords = () => {
  keywordsInput.value = (category.value?.anchor_keywords || []).join(', ');
  editingKeywords.value = true;
};

const cancelEditKeywords = () => {
  editingKeywords.value = false;
};

// Edit modal
const editingAttr = ref(null);
const editForm = ref({});

const searchFilters = ref({});

const { locale } = useI18n();

// Localized attribute display name
const attrDisplayName = (attr) => {
  if (!attr) return '';
  const langField = `name_${locale.value}`;
  return attr[langField] || attr.name_en || attr.name_ru || attr.name_ua || attr.name_pl || attr.name || humanizeAttrName(attr.code);
};

const humanizeAttrName = (code) => {
  if (!code) return '';
  let s = code.replace(/_/g, ' ').replace(/-/g, ' ');
  return s.replace(/\b\w/g, c => c.toUpperCase());
};

const fetchCategory = async () => {
  try {
    const response = await api.get(`/categories/${categoryId.value}`);
    category.value = response.data;
  } catch (e) {
    console.error('Failed to fetch category:', e);
  }
};

const fetchAttributes = async () => {
  loading.value = true;
  error.value = null;
  try {
    const response = await api.get(`/admin/categories/${categoryId.value}/attributes`);
    const items = response.data.attributes || response.data.items || response.data || [];
    attributes.value = Array.isArray(items) ? items : [];
  } catch (e) {
    error.value = e.response?.data?.message || t('admin.attr_load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const addAttribute = async () => {
  if (!newCode.value.trim()) { toast.error(t('admin.enter_attr_code')); return; }
  try {
    await api.post(`/admin/categories/${categoryId.value}/attributes`, { code: newCode.value.trim() });
    newCode.value = '';
    showAddForm.value = false;
    await fetchAttributes();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.add_error'));
  }
};

const removeAttrCode = ref(null);

const askRemoveAttribute = (code) => {
  removeAttrCode.value = code;
};

const removeAttribute = async (code) => {
  removeAttrCode.value = null;
  try {
    await api.delete(`/admin/categories/${categoryId.value}/attributes?code=${encodeURIComponent(code)}`);
    await fetchAttributes();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.remove_error'));
  }
};

const openEdit = async (attr) => {
  try {
    const response = await api.get(`/admin/attrdefs/${attr.code}`);
    editingAttr.value = attr.code;
    editForm.value = { ...response.data };
  } catch (e) {
    // If no definition yet, create defaults
    editingAttr.value = attr.code;
    editForm.value = {
      code: attr.code,
      name_ru: humanizeAttrName(attr.code),
      name_ua: '',
      name_pl: '',
      name_en: '',
      type: 'string',
      is_active: true,
      is_filterable: true,
      is_sortable: false,
      sort_order: 0,
      unit: '',
      range_params: [],
    };
  }
};

const saveEdit = async () => {
  try {
    const payload = {
      name_ru: editForm.value.name_ru || '',
      name_ua: editForm.value.name_ua || '',
      name_pl: editForm.value.name_pl || '',
      name_en: editForm.value.name_en || '',
      type: editForm.value.type || 'string',
      is_active: !!editForm.value.is_active,
      is_filterable: !!editForm.value.is_filterable,
      is_sortable: !!editForm.value.is_sortable,
      sort_order: parseInt(editForm.value.sort_order) || 0,
      unit: editForm.value.unit || '',
      range_params: editForm.value.range_params || [],
    };
    await api.patch(`/admin/attrdefs/${editingAttr.value}`, payload);
    editingAttr.value = null;
    editForm.value = {};
    await fetchAttributes();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.save_error'));
  }
};

const deleteAttrDefCode = ref(null);

const askDeleteAttrDef = (code) => {
  deleteAttrDefCode.value = code;
};

const deleteAttrDef = async (code) => {
  deleteAttrDefCode.value = null;
  try {
    await api.delete(`/admin/attrdefs/${code}`);
    await fetchAttributes();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.delete_attrdef_error'));
  }
};

const setSearch = (code, value) => {
  searchFilters.value[code] = value;
};

const groupedAttrs = computed(() => {
  const groups = {};
  for (const attr of attributes.value) {
    const type = attr.type || 'text';
    if (!groups[type]) groups[type] = [];
    groups[type].push(attr);
  }
  return groups;
});

const getOptions = (attr) => attr.options || attr.values || [];

const filteredOptions = (attr) => {
  const opts = getOptions(attr);
  if (!Array.isArray(opts)) return [];
  const search = (searchFilters.value[attr.code] || '').toLowerCase();
  if (!search) return opts;
  return opts.filter(opt => String(opt).toLowerCase().includes(search));
};

const MAX_TAGS = 7;
const visibleTags = (attr) => filteredOptions(attr).slice(0, MAX_TAGS);
const hiddenTags = (attr) => filteredOptions(attr).slice(MAX_TAGS);
const hasMoreTags = (attr) => hiddenTags(attr).length > 0;
const hasOptions = (attr) => getOptions(attr).length > 0;

const typeLabels = {
  string: () => t('admin.attr_types.string'),
  int: () => t('admin.attr_types.int'),
  float: () => t('admin.attr_types.float'),
  bool: () => t('admin.attr_types.bool'),
  enum: () => t('admin.attr_types.enum'),
  multi_enum: () => t('admin.attr_types.multi_enum'),
  date: () => t('admin.attr_types.date'),
  range: () => t('admin.attr_types.range'),
};

const attrTypes = [
  { value: 'string', label: () => t('admin.attr_types.string') },
  { value: 'int', label: () => t('admin.attr_types.int') },
  { value: 'float', label: () => t('admin.attr_types.float') },
  { value: 'bool', label: () => t('admin.attr_types.bool') },
  { value: 'enum', label: () => t('admin.attr_types.enum') },
  { value: 'multi_enum', label: () => t('admin.attr_types.multi_enum') },
  { value: 'date', label: () => t('admin.attr_types.date') },
  { value: 'range', label: () => t('admin.attr_types.range') },
];

onMounted(() => {
  fetchCategory();
  fetchAttributes();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <div class="flex items-center gap-4">
        <button @click="router.push({ name: 'admin-categories' })" class="p-2 text-ink-3 hover:text-ink-2 hover:bg-surface-2 rounded-lg transition">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <div>
          <h1 class="text-2xl font-bold text-ink">
            {{ category?.name || `#${categoryId}` }} — {{ t('admin.attributes') }}
          </h1>
          <p class="text-sm text-ink-3 mt-0.5">{{ t('admin.attr_count', { count: attributes.length }) }}</p>
        </div>
      </div>
      <button @click="showAddForm = true" class="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 text-sm">
        {{ t('admin.add_attr') }}
      </button>
    </div>

    <!-- Anchor Keywords -->
    <div class="mb-4 bg-surface rounded-lg shadow-sm p-4 border border-purple-200">
      <div class="flex items-center justify-between mb-2">
        <div>
          <h3 class="font-medium">{{ t('admin.anchor_keywords') || 'Anchor Keywords' }}</h3>
          <p class="text-xs text-ink-3">{{ t('admin.anchor_keywords_hint') || 'Keywords used for auto-catalogization. Comma-separated.' }}</p>
        </div>
        <button
          v-if="!editingKeywords"
          @click="startEditKeywords"
          class="px-3 py-1 text-xs bg-purple-600 text-white rounded-lg hover:bg-purple-700"
        >
          {{ t('common.edit') || 'Edit' }}
        </button>
      </div>
      <div v-if="!editingKeywords">
        <div v-if="category?.anchor_keywords?.length" class="flex flex-wrap gap-1">
          <span
            v-for="kw in category.anchor_keywords"
            :key="kw"
            class="px-2 py-1 bg-purple-100 text-purple-700 text-xs rounded-full"
          >
            {{ kw }}
          </span>
        </div>
        <p v-else class="text-xs text-ink-3">{{ t('admin.anchor_keywords_empty') || 'No keywords set' }}</p>
      </div>
      <div v-else class="flex gap-2">
        <input
          v-model="keywordsInput"
          type="text"
          :placeholder="t('admin.anchor_keywords_placeholder') || 'tv, television, samsung, lg...'"
          class="flex-1 px-3 py-2 border border-line rounded-lg text-sm"
        />
        <button @click="saveKeywords" class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
          {{ t('common.save') || 'Save' }}
        </button>
        <button @click="cancelEditKeywords" class="px-4 py-2 border rounded-lg text-sm hover:bg-surface-2">
          {{ t('common.cancel') || 'Cancel' }}
        </button>
      </div>
    </div>

    <!-- Add form -->
    <div v-if="showAddForm" class="mb-4 bg-surface rounded-lg shadow-sm p-4 border border-purple-200">
      <h3 class="font-medium mb-2">{{ t('admin.add_attr') }}</h3>
      <p class="text-xs text-ink-3 mb-2">{{ t('admin.attr_hint') }}</p>
      <div class="flex gap-2">
        <input v-model="newCode" type="text" :placeholder="t('admin.attr_code_placeholder')" class="flex-1 px-3 py-2 border border-line rounded-lg text-sm" />
        <button @click="addAttribute" class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
          {{ t('admin.create') }}
        </button>
        <button @click="showAddForm = false" class="px-4 py-2 border rounded-lg text-sm hover:bg-surface-2">
          {{ t('admin.cancel') }}
        </button>
      </div>
    </div>

    <!-- Edit modal -->
    <div v-if="editingAttr" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" @click.self="editingAttr = null">
      <div role="dialog" aria-modal="true" class="bg-surface rounded-xl shadow-xl max-w-lg w-full max-h-[90vh] overflow-y-auto">
        <div class="px-5 py-4 border-b flex items-center justify-between">
          <h3 class="font-semibold">{{ t('admin.edit_attrdef', { code: editingAttr }) }}</h3>
          <button @click="editingAttr = null" class="text-ink-3 hover:text-ink-2">✕</button>
        </div>
        <div class="px-5 py-4 space-y-3">
          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_name_ru') || 'Name (RU)' }}</label>
            <input v-model="editForm.name_ru" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_name_ua') || 'Name (UA)' }}</label>
            <input v-model="editForm.name_ua" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_name_pl') || 'Name (PL)' }}</label>
            <input v-model="editForm.name_pl" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_name_en') || 'Name (EN)' }}</label>
            <input v-model="editForm.name_en" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_type') }}</label>
            <select v-model="editForm.type" class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface">
              <option v-for="t in attrTypes" :key="t.value" :value="t.value">{{ t.label }}</option>
            </select>
          </div>

          <!-- Range params for range type -->
          <div v-if="editForm.type === 'range'" class="space-y-2">
            <label class="block text-xs font-medium text-ink-2">{{ t('admin.attr_range_params_label') }}</label>
            <p class="text-[11px] text-ink-3">{{ t('admin.attr_range_params_hint') }}</p>
            <div class="flex flex-wrap gap-2">
              <input
                v-for="(p, i) in editForm.range_params"
                :key="i"
                v-model="editForm.range_params[i]"
                type="text"
                :placeholder="t('admin.attr_range_param_placeholder')"
                class="px-3 py-2 border border-line rounded-lg text-sm w-40"
              />
            </div>
            <button
              v-if="(editForm.range_params || []).length < 3"
              @click="editForm.range_params = (editForm.range_params || []).concat('')"
              class="text-xs text-purple-600 hover:underline"
            >
              {{ t('admin.attr_range_add_param') }}
            </button>
            <button
              v-if="(editForm.range_params || []).length > 0"
              @click="editForm.range_params.pop()"
              class="text-xs text-red-600 hover:underline ml-2"
            >
              {{ t('admin.attr_range_remove_last') }}
            </button>
          </div>

          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_unit') }}</label>
            <input v-model="editForm.unit" type="text" :placeholder="t('admin.attr_unit_placeholder')" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>

          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="editForm.is_active" type="checkbox" />
              {{ t('admin.attr_active') }}
            </label>
          </div>
          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="editForm.is_filterable" type="checkbox" />
              {{ t('admin.attr_filterable') }}
            </label>
            <label class="flex items-center gap-2 text-sm">
              <input v-model="editForm.is_sortable" type="checkbox" />
              {{ t('admin.attr_sortable') }}
            </label>
          </div>
          <div>
            <label class="block text-xs font-medium text-ink-2 mb-1">{{ t('admin.attr_order') }}</label>
            <input v-model.number="editForm.sort_order" type="number" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>
        </div>
        <div class="px-5 py-3 border-t flex gap-2 justify-end">
          <button @click="askDeleteAttrDef(editingAttr)" class="px-3 py-2 text-sm text-red-600 hover:underline">
            {{ t('admin.attr_delete_definition') }}
          </button>
          <button @click="editingAttr = null" class="px-4 py-2 border rounded-lg text-sm hover:bg-surface-2">
            {{ t('admin.cancel') }}
          </button>
          <button @click="saveEdit" class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
            {{ t('admin.save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="mb-4 p-3 bg-red-50 text-red-700 rounded-lg text-sm">{{ error }}</div>

    <!-- Empty -->
    <div v-else-if="attributes.length === 0" class="text-center py-12 text-ink-3">
      <p>{{ t('admin.attr_no_attrs') }}</p>
      <button @click="showAddForm = true" class="mt-3 px-4 py-2 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
        {{ t('admin.attr_add_first') }}
      </button>
    </div>

    <!-- Attributes list -->
    <div v-else class="space-y-4">
      <div v-for="attr in attributes" :key="attr.code" class="bg-surface rounded-lg shadow-sm border border-line p-4">
        <div class="flex items-start justify-between gap-4">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-medium text-ink">
                {{ attrDisplayName(attr) }}
              </span>
              <span class="text-xs text-ink-3 font-mono">{{ attr.code }}</span>
              <span v-if="attr.type" class="text-[11px] px-1.5 py-0.5 rounded bg-purple-100 text-purple-700">
                {{ typeof typeLabels[attr.type] === 'function' ? typeLabels[attr.type]() : (typeLabels[attr.type] || attr.type) }}
              </span>
              <span v-if="attr.unit" class="text-[11px] text-ink-3">({{ attr.unit }})</span>
              <span v-if="attr.range_params?.length" class="text-[11px] text-blue-600">
                {{ attr.range_params.join(' × ') }}
              </span>
              <span v-if="!attr.is_active" class="text-[11px] px-1.5 py-0.5 rounded bg-surface-3 text-ink-3">
                {{ t('admin.attr_inactive') }}
              </span>
              <span v-if="attr.is_filterable" class="text-[11px] px-1.5 py-0.5 rounded bg-green-100 text-green-700">
                {{ t('admin.attr_filter') }}
              </span>
              <span v-if="attr.is_sortable" class="text-[11px] px-1.5 py-0.5 rounded bg-blue-100 text-blue-700">
                {{ t('admin.attr_sort') }}
              </span>
            </div>
            <div v-if="hasOptions(attr)" class="mt-2">
              <div class="flex flex-wrap gap-1.5">
                <span v-for="tag in visibleTags(attr)" :key="tag" class="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-purple-50 text-purple-700 border border-purple-200">
                  {{ tag }}
                </span>
              </div>
              <div v-if="hasMoreTags(attr)" class="border border-line rounded-lg p-2 max-h-48 overflow-y-auto bg-surface-2 mt-1">
                <div class="flex flex-wrap gap-1.5">
                  <span v-for="tag in hiddenTags(attr)" :key="tag" class="inline-flex items-center px-2 py-0.5 rounded-full text-xs bg-purple-50 text-purple-700 border border-purple-200">
                    {{ tag }}
                  </span>
                </div>
              </div>
            </div>
            <div v-else class="text-xs text-ink-3 italic mt-1">{{ t('admin.attr_no_values') }}</div>
          </div>
          <div class="flex flex-col gap-1">
            <button @click="openEdit(attr)" class="text-xs text-blue-600 hover:underline text-right">
              {{ t('admin.edit') }}
            </button>
            <button @click="askRemoveAttribute(attr.code)" class="text-xs text-red-600 hover:underline text-right">
              {{ t('admin.delete') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :open="removeAttrCode !== null"
      :title="t('admin.attributes')"
      :message="removeAttrCode ? t('admin.remove_attr_confirm', { code: removeAttrCode }) : ''"
      variant="danger"
      :confirm-text="t('admin.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="removeAttribute(removeAttrCode)"
      @cancel="removeAttrCode = null"
    />

    <ConfirmDialog
      :open="deleteAttrDefCode !== null"
      :title="t('admin.attributes')"
      :message="deleteAttrDefCode ? t('admin.delete_attrdef_confirm', { code: deleteAttrDefCode }) : ''"
      variant="danger"
      :confirm-text="t('admin.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="deleteAttrDef(deleteAttrDefCode)"
      @cancel="deleteAttrDefCode = null"
    />
  </div>
</template>
