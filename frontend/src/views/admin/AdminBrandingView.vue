<script setup>
// Admin branding: page decoration sets + per-category overrides.
// Design: docs/BRANDING_SYSTEM_PLAN.md
import { ref, computed, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';
import { useBrandingStore } from '../../stores/branding';

const { t } = useI18n();
const { toast } = useToast();
const brandingStore = useBrandingStore();

// ---------- Slots ----------
// maxDim is passed to the upload endpoint as the resize target.
const SLOTS = [
  { id: 'header_fullwidth', maxDim: 1920 },
  { id: 'home_banner', maxDim: 1600 },
  { id: 'category_banner', maxDim: 1600 },
  { id: 'footer_fullwidth', maxDim: 1920 },
  { id: 'side_left_top', maxDim: 400 },
  { id: 'side_left_bottom', maxDim: 400 },
  { id: 'side_right_top', maxDim: 400 },
  { id: 'side_right_bottom', maxDim: 400 },
];
const slotLabel = (id) => t(`admin.branding.slot_${id}`);
const slotHint = (id) => t(`admin.branding.slot_hint_${id}`);

// ---------- Tabs ----------
const activeTab = ref('sets'); // 'sets' | 'overrides'

// ---------- Sets list ----------
const sets = ref([]);
const loading = ref(true);
const togglingId = ref(null);
const deletingId = ref(null);

const loadSets = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/branding/sets');
    sets.value = res.data || [];
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.load_error'));
  } finally {
    loading.value = false;
  }
};

const toggleSet = async (set) => {
  togglingId.value = set.id;
  try {
    await api.patch(`/admin/branding/sets/${set.id}`, {
      name: set.name,
      description: set.description,
      priority: set.priority,
      enabled: !set.enabled,
      elements: set.elements,
    });
    set.enabled = !set.enabled;
    brandingStore.invalidate();
    toast.success(t('admin.branding.saved'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.save_error'));
  } finally {
    togglingId.value = null;
  }
};

// ---------- Set editor ----------
const emptyElement = () => ({
  image_url: '',
  image_dark_url: '',
  link_url: '',
  alt_text: '',
  page_patterns: [],
});

const emptyForm = () => ({
  id: null,
  name: '',
  description: '',
  priority: 0,
  enabled: false,
  elements: Object.fromEntries(SLOTS.map((s) => [s.id, emptyElement()])),
});

const editing = ref(false);
const form = ref(emptyForm());
const saving = ref(false);
const uploading = ref({}); // `${slot}:${theme}` -> bool

const openNew = () => {
  form.value = emptyForm();
  editing.value = true;
};

const openEdit = (set) => {
  const f = emptyForm();
  f.id = set.id;
  f.name = set.name;
  f.description = set.description || '';
  f.priority = set.priority || 0;
  f.enabled = !!set.enabled;
  for (const el of set.elements || []) {
    if (f.elements[el.slot]) {
      f.elements[el.slot] = {
        image_url: el.image_url || '',
        image_dark_url: el.image_dark_url || '',
        link_url: el.link_url || '',
        alt_text: el.alt_text || '',
        page_patterns: [...(el.page_patterns || [])],
      };
    }
  }
  form.value = f;
  editing.value = true;
};

const closeEditor = () => {
  editing.value = false;
};

// --- Pattern validation (JS regex, same engine as the runtime matching) ---
const validatePattern = (p) => {
  if (!p || !p.trim()) return t('admin.branding.pattern_empty');
  if (p.length > 200) return t('admin.branding.pattern_too_long');
  try {
    // eslint-disable-next-line no-new
    new RegExp(p);
    return null;
  } catch (e) {
    return e.message;
  }
};

const patternErrors = (slot) =>
  form.value.elements[slot].page_patterns.map(validatePattern);

const hasPatternErrors = computed(() =>
  SLOTS.some((s) => patternErrors(s.id).some((err) => err !== null))
);

const addPattern = (slot) => {
  form.value.elements[slot].page_patterns.push('');
};

const removePattern = (slot, idx) => {
  form.value.elements[slot].page_patterns.splice(idx, 1);
};

// Map a theme ('light' | 'dark') to its element field name. The light image is
// stored in `image_url` (NOT `image_light_url`), the dark one in `image_dark_url`.
const imageField = (theme) => (theme === 'light' ? 'image_url' : 'image_dark_url');

// --- Image upload (subdir=branding, per-slot max_dim) ---
const uploadSlotImage = async (slot, theme, file) => {
  if (!file) return;
  const validTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp', 'image/gif'];
  if (!validTypes.includes(file.type)) {
    toast.error(t('admin.branding.invalid_file'));
    return;
  }
  if (file.size > 10 * 1024 * 1024) {
    toast.error(t('admin.branding.file_too_large'));
    return;
  }
  const key = `${slot}:${theme}`;
  uploading.value[key] = true;
  try {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('subdir', 'branding');
    formData.append('max_dim', String(SLOTS.find((s) => s.id === slot).maxDim));
    const res = await api.post('/admin/upload-image', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    form.value.elements[slot][imageField(theme)] = res.data.url;
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.upload_error'));
  } finally {
    uploading.value[key] = false;
  }
};

const removeSlotImage = (slot, theme) => {
  form.value.elements[slot][imageField(theme)] = '';
};

// --- Save ---
const saveSet = async () => {
  if (!form.value.name.trim()) {
    toast.error(t('admin.branding.name_required'));
    return;
  }
  if (hasPatternErrors.value) {
    toast.error(t('admin.branding.pattern_invalid'));
    return;
  }
  // Elements: only slots with an image, patterns trimmed & non-empty.
  const elements = [];
  for (const s of SLOTS) {
    const el = form.value.elements[s.id];
    if (!el.image_url) continue;
    elements.push({
      slot: s.id,
      image_url: el.image_url,
      image_dark_url: el.image_dark_url || undefined,
      link_url: el.link_url?.trim() || undefined,
      alt_text: el.alt_text?.trim() || undefined,
      page_patterns: el.page_patterns
        .map((p) => p.trim())
        .filter(Boolean),
    });
  }
  const payload = {
    name: form.value.name.trim(),
    description: form.value.description.trim(),
    priority: Number(form.value.priority) || 0,
    enabled: form.value.enabled,
    elements,
  };
  saving.value = true;
  try {
    if (form.value.id) {
      await api.patch(`/admin/branding/sets/${form.value.id}`, payload);
    } else {
      await api.post('/admin/branding/sets', payload);
    }
    toast.success(t('admin.branding.saved'));
    brandingStore.invalidate();
    editing.value = false;
    await loadSets();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.save_error'));
  } finally {
    saving.value = false;
  }
};

const confirmDeleteSet = async () => {
  const id = deletingId.value;
  deletingId.value = null;
  try {
    await api.delete(`/admin/branding/sets/${id}`);
    toast.success(t('admin.branding.deleted'));
    brandingStore.invalidate();
    await loadSets();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.save_error'));
  }
};

// ---------- Category overrides ----------
const overrides = ref([]);
const categories = ref([]);
const catSearch = ref('');
const overrideForm = ref({
  category_id: null,
  slot: 'header_fullwidth',
  image_url: '',
  image_dark_url: '',
  link_url: '',
});
const overrideUploading = ref({ light: false, dark: false });
const savingOverride = ref(false);
const deletingOverride = ref(null);

const filteredCategories = computed(() => {
  const q = catSearch.value.trim().toLowerCase();
  if (!q) return categories.value.slice(0, 100);
  return categories.value
    .filter((c) => {
      const name = c.name_ru || c.name_en || c.slug || '';
      return name.toLowerCase().includes(q);
    })
    .slice(0, 100);
});

const loadOverrides = async () => {
  try {
    const res = await api.get('/admin/branding/category-overrides');
    overrides.value = res.data || [];
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.load_error'));
  }
};

const loadCategories = async () => {
  try {
    const res = await api.get('/categories');
    categories.value = Array.isArray(res.data?.items) ? res.data.items : [];
  } catch (e) {
    console.error('Failed to load categories:', e);
  }
};

const categoryName = (id) => {
  const c = categories.value.find((x) => x.id === id);
  return c ? c.name_ru || c.name_en || `#${id}` : `#${id}`;
};

const uploadOverrideImage = async (theme, file) => {
  if (!file) return;
  const validTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp', 'image/gif'];
  if (!validTypes.includes(file.type)) {
    toast.error(t('admin.branding.invalid_file'));
    return;
  }
  overrideUploading.value[theme] = true;
  try {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('subdir', 'branding');
    formData.append('max_dim', '1600');
    const res = await api.post('/admin/upload-image', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    overrideForm.value[imageField(theme)] = res.data.url;
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.upload_error'));
  } finally {
    overrideUploading.value[theme] = false;
  }
};

const saveOverride = async () => {
  if (!overrideForm.value.category_id) {
    toast.error(t('admin.branding.category_required'));
    return;
  }
  if (!overrideForm.value.image_url) {
    toast.error(t('admin.branding.image_required'));
    return;
  }
  savingOverride.value = true;
  try {
    await api.post('/admin/branding/category-overrides', {
      category_id: overrideForm.value.category_id,
      slot: overrideForm.value.slot,
      image_url: overrideForm.value.image_url,
      image_dark_url: overrideForm.value.image_dark_url || undefined,
      link_url: overrideForm.value.link_url?.trim() || undefined,
    });
    toast.success(t('admin.branding.saved'));
    brandingStore.invalidate();
    overrideForm.value = {
      category_id: null,
      slot: 'header_fullwidth',
      image_url: '',
      image_dark_url: '',
      link_url: '',
    };
    await loadOverrides();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.save_error'));
  } finally {
    savingOverride.value = false;
  }
};

const confirmDeleteOverride = async () => {
  const o = deletingOverride.value;
  deletingOverride.value = null;
  try {
    await api.delete(
      `/admin/branding/category-overrides?category_id=${o.category_id}&slot=${o.slot}`
    );
    toast.success(t('admin.branding.deleted'));
    brandingStore.invalidate();
    await loadOverrides();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.branding.save_error'));
  }
};

onMounted(() => {
  loadSets();
  loadOverrides();
  loadCategories();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="mb-6 flex items-center justify-between flex-wrap gap-3">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.branding.title') }}</h1>
      <div class="flex items-center gap-2">
        <button
          @click="activeTab = 'sets'"
          class="px-3 py-1.5 text-sm rounded-lg border transition"
          :class="activeTab === 'sets' ? 'bg-purple-600 text-white border-purple-600' : 'bg-surface text-ink-2 border-line hover:bg-surface-2'"
        >
          {{ t('admin.branding.tab_sets') }}
        </button>
        <button
          @click="activeTab = 'overrides'"
          class="px-3 py-1.5 text-sm rounded-lg border transition"
          :class="activeTab === 'overrides' ? 'bg-purple-600 text-white border-purple-600' : 'bg-surface text-ink-2 border-line hover:bg-surface-2'"
        >
          {{ t('admin.branding.tab_overrides') }}
        </button>
      </div>
    </div>

    <!-- ==================== SETS TAB ==================== -->
    <div v-if="activeTab === 'sets'">
      <!-- List -->
      <div v-if="!editing">
        <div class="mb-4 flex justify-end">
          <button
            @click="openNew"
            class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm font-medium hover:bg-purple-700 transition"
          >
            + {{ t('admin.branding.new_set') }}
          </button>
        </div>

        <div v-if="loading" class="text-sm text-ink-3 py-8 text-center">
          {{ t('admin.branding.loading') }}
        </div>

        <div v-else-if="sets.length === 0" class="text-sm text-ink-3 py-8 text-center">
          {{ t('admin.branding.no_sets') }}
        </div>

        <div v-else class="space-y-3">
          <div
            v-for="set in sets"
            :key="set.id"
            class="bg-surface rounded-xl shadow-sm border border-line p-4 flex items-center gap-4 flex-wrap"
          >
            <!-- Enable toggle -->
            <label class="flex items-center gap-2 cursor-pointer shrink-0" :title="t('admin.branding.toggle_hint')">
              <input
                type="checkbox"
                :checked="set.enabled"
                :disabled="togglingId === set.id"
                @change="toggleSet(set)"
                class="form-checkbox h-4 w-4 accent-purple-600"
              />
              <span class="text-xs" :class="set.enabled ? 'text-green-600 font-medium' : 'text-ink-3'">
                {{ set.enabled ? t('admin.branding.enabled') : t('admin.branding.disabled') }}
              </span>
            </label>

            <!-- Name + description -->
            <div class="flex-1 min-w-[200px]">
              <div class="font-medium text-ink">{{ set.name }}</div>
              <div v-if="set.description" class="text-xs text-ink-3 mt-0.5">{{ set.description }}</div>
              <div class="text-[11px] text-ink-3 mt-1">
                {{ t('admin.branding.priority') }}: {{ set.priority }} ·
                {{ t('admin.branding.slots_filled') }}: {{ (set.elements || []).length }}/{{ SLOTS.length }}
              </div>
            </div>

            <!-- Actions -->
            <div class="flex items-center gap-2 shrink-0">
              <button
                @click="openEdit(set)"
                class="px-3 py-1.5 text-xs rounded-md border border-line hover:bg-surface-2 transition"
              >
                {{ t('admin.branding.edit') }}
              </button>
              <button
                @click="deletingId = set.id"
                class="px-3 py-1.5 text-xs rounded-md border border-red-300 text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 transition"
              >
                {{ t('admin.branding.delete') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Editor -->
      <div v-else class="bg-surface rounded-xl shadow-sm border border-line p-5">
        <div class="mb-4 flex items-center justify-between">
          <h2 class="text-lg font-semibold text-ink-2">
            {{ form.id ? t('admin.branding.edit_set') : t('admin.branding.new_set') }}
          </h2>
          <button @click="closeEditor" class="text-sm text-ink-3 hover:text-ink-2">
            {{ t('admin.cancel') }}
          </button>
        </div>

        <!-- Meta -->
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-6">
          <div class="sm:col-span-2">
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.branding.name') }} *</label>
            <input
              v-model.trim="form.name"
              type="text"
              :placeholder="t('admin.branding.name_placeholder')"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            />
          </div>
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.branding.priority') }}</label>
            <input v-model.number="form.priority" type="number" class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface" />
            <p class="text-xs text-ink-3 mt-1">{{ t('admin.branding.priority_hint') }}</p>
          </div>
          <div class="sm:col-span-3">
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.branding.description') }}</label>
            <input
              v-model.trim="form.description"
              type="text"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            />
          </div>
        </div>

        <!-- Slots -->
        <div class="space-y-4">
          <div
            v-for="slot in SLOTS"
            :key="slot.id"
            class="border border-line rounded-lg p-4"
            :class="form.elements[slot.id].image_url ? 'border-purple-300 bg-purple-50/40 dark:bg-purple-900/10' : ''"
          >
            <div class="flex items-center justify-between mb-3">
              <div>
                <span class="text-sm font-semibold text-ink">{{ slotLabel(slot.id) }}</span>
                <span class="text-xs text-ink-3 ml-2">{{ slotHint(slot.id) }}</span>
              </div>
              <button
                v-if="form.elements[slot.id].image_url"
                @click="
                  form.elements[slot.id] = { image_url: '', image_dark_url: '', link_url: '', alt_text: '', page_patterns: [] }
                "
                class="text-xs text-ink-3 hover:text-red-600"
              >
                {{ t('admin.branding.clear_slot') }}
              </button>
            </div>

            <!-- Images -->
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-3">
              <!-- Light -->
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">{{ t('admin.branding.image_light') }} *</label>
                <div class="flex items-center gap-2">
                  <input
                    type="file"
                    accept="image/jpeg,image/png,image/webp,image/gif"
                    class="text-xs flex-1 min-w-0"
                    @change="uploadSlotImage(slot.id, 'light', $event.target.files?.[0]); $event.target.value = ''"
                  />
                  <span v-if="uploading[`${slot.id}:light`]" class="text-xs text-ink-3">...</span>
                  <button
                    v-else-if="form.elements[slot.id].image_url"
                    @click="removeSlotImage(slot.id, 'light')"
                    class="text-xs text-ink-3 hover:text-red-600 shrink-0"
                  >
                    ✕
                  </button>
                </div>
                <img
                  v-if="form.elements[slot.id].image_url"
                  :src="form.elements[slot.id].image_url"
                  class="mt-2 max-h-24 rounded border border-line"
                  alt=""
                />
              </div>
              <!-- Dark -->
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">{{ t('admin.branding.image_dark') }}</label>
                <div class="flex items-center gap-2">
                  <input
                    type="file"
                    accept="image/jpeg,image/png,image/webp,image/gif"
                    class="text-xs flex-1 min-w-0"
                    @change="uploadSlotImage(slot.id, 'dark', $event.target.files?.[0]); $event.target.value = ''"
                  />
                  <span v-if="uploading[`${slot.id}:dark`]" class="text-xs text-ink-3">...</span>
                  <button
                    v-else-if="form.elements[slot.id].image_dark_url"
                    @click="removeSlotImage(slot.id, 'dark')"
                    class="text-xs text-ink-3 hover:text-red-600 shrink-0"
                  >
                    ✕
                  </button>
                </div>
                <img
                  v-if="form.elements[slot.id].image_dark_url"
                  :src="form.elements[slot.id].image_dark_url"
                  class="mt-2 max-h-24 rounded border border-line"
                  alt=""
                />
              </div>
            </div>

            <!-- Link + alt -->
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-3">
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">{{ t('admin.branding.link_url') }}</label>
                <input
                  v-model.trim="form.elements[slot.id].link_url"
                  type="text"
                  placeholder="/shop/akciya"
                  class="w-full px-2 py-1.5 border border-line rounded-lg text-xs bg-surface"
                />
              </div>
              <div>
                <label class="text-xs font-medium text-ink-2 block mb-1">Alt</label>
                <input
                  v-model.trim="form.elements[slot.id].alt_text"
                  type="text"
                  class="w-full px-2 py-1.5 border border-line rounded-lg text-xs bg-surface"
                />
              </div>
            </div>

            <!-- Page patterns -->
            <div>
              <div class="flex items-center justify-between mb-1">
                <label class="text-xs font-medium text-ink-2">
                  {{ t('admin.branding.page_patterns') }}
                  <span class="text-ink-3 font-normal">({{ t('admin.branding.page_patterns_hint') }})</span>
                </label>
                <button
                  @click="addPattern(slot.id)"
                  class="text-xs text-purple-600 hover:underline"
                >
                  + {{ t('admin.branding.add_pattern') }}
                </button>
              </div>
              <div v-if="form.elements[slot.id].page_patterns.length === 0" class="text-xs text-ink-3">
                {{ t('admin.branding.patterns_empty') }}
              </div>
              <div v-else class="space-y-1.5">
                <div v-for="(p, idx) in form.elements[slot.id].page_patterns" :key="idx" class="flex items-start gap-2">
                  <input
                    v-model.trim="form.elements[slot.id].page_patterns[idx]"
                    type="text"
                    placeholder="^/shop/telefony"
                    class="flex-1 px-2 py-1.5 border border-line rounded-lg text-xs font-mono bg-surface"
                    :class="patternErrors(slot.id)[idx] ? 'border-red-400' : ''"
                  />
                  <button
                    @click="removePattern(slot.id, idx)"
                    class="text-xs text-ink-3 hover:text-red-600 mt-1.5"
                  >
                    ✕
                  </button>
                  <div v-if="patternErrors(slot.id)[idx]" class="w-full text-[11px] text-red-600 -mt-1">
                    {{ patternErrors(slot.id)[idx] }}
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- Save bar -->
        <div class="mt-6 flex items-center gap-3">
          <label class="flex items-center gap-2 cursor-pointer text-sm">
            <input v-model="form.enabled" type="checkbox" class="form-checkbox h-4 w-4 accent-purple-600" />
            {{ t('admin.branding.enabled') }}
          </label>
          <div class="flex-1"></div>
          <button
            @click="saveSet"
            :disabled="saving"
            class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm font-medium hover:bg-purple-700 transition disabled:opacity-50"
          >
            {{ saving ? t('admin.saving') : t('admin.save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- ==================== OVERRIDES TAB ==================== -->
    <div v-else>
      <!-- New override form -->
      <div class="bg-surface rounded-xl shadow-sm border border-line p-5 mb-6">
        <h2 class="text-lg font-semibold text-ink-2 mb-4">{{ t('admin.branding.new_override') }}</h2>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.branding.category') }} *</label>
            <input
              v-model="catSearch"
              type="text"
              :placeholder="t('admin.branding.category_search')"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface mb-2"
            />
            <select
              v-model="overrideForm.category_id"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            >
              <option :value="null">—</option>
              <option v-for="c in filteredCategories" :key="c.id" :value="c.id">
                {{ c.name_ru || c.name_en || c.slug }}
              </option>
            </select>
          </div>
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.branding.slot') }} *</label>
            <select
              v-model="overrideForm.slot"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            >
              <option v-for="s in SLOTS" :key="s.id" :value="s.id">
                {{ slotLabel(s.id) }}
              </option>
            </select>
          </div>
          <div>
            <label class="text-xs font-medium text-ink-2 block mb-1">{{ t('admin.branding.image_light') }} *</label>
            <div class="flex items-center gap-2">
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp,image/gif"
                class="text-xs flex-1 min-w-0"
                @change="uploadOverrideImage('light', $event.target.files?.[0]); $event.target.value = ''"
              />
              <span v-if="overrideUploading.light" class="text-xs text-ink-3">...</span>
              <button
                v-else-if="overrideForm.image_url"
                @click="overrideForm.image_url = ''"
                class="text-xs text-ink-3 hover:text-red-600 shrink-0"
              >
                ✕
              </button>
            </div>
            <img
              v-if="overrideForm.image_url"
              :src="overrideForm.image_url"
              class="mt-2 max-h-24 rounded border border-line"
              alt=""
            />
          </div>
          <div>
            <label class="text-xs font-medium text-ink-2 block mb-1">{{ t('admin.branding.image_dark') }}</label>
            <div class="flex items-center gap-2">
              <input
                type="file"
                accept="image/jpeg,image/png,image/webp,image/gif"
                class="text-xs flex-1 min-w-0"
                @change="uploadOverrideImage('dark', $event.target.files?.[0]); $event.target.value = ''"
              />
              <span v-if="overrideUploading.dark" class="text-xs text-ink-3">...</span>
              <button
                v-else-if="overrideForm.image_dark_url"
                @click="overrideForm.image_dark_url = ''"
                class="text-xs text-ink-3 hover:text-red-600 shrink-0"
              >
                ✕
              </button>
            </div>
            <img
              v-if="overrideForm.image_dark_url"
              :src="overrideForm.image_dark_url"
              class="mt-2 max-h-24 rounded border border-line"
              alt=""
            />
          </div>
          <div class="sm:col-span-2">
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.branding.link_url') }}</label>
            <input
              v-model.trim="overrideForm.link_url"
              type="text"
              placeholder="/shop/akciya"
              class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface"
            />
          </div>
        </div>
        <div class="mt-4">
          <button
            @click="saveOverride"
            :disabled="savingOverride"
            class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm font-medium hover:bg-purple-700 transition disabled:opacity-50"
          >
            {{ savingOverride ? t('admin.saving') : t('admin.branding.save_override') }}
          </button>
        </div>
      </div>

      <!-- Overrides list -->
      <div class="bg-surface rounded-xl shadow-sm border border-line p-5">
        <h2 class="text-lg font-semibold text-ink-2 mb-4">{{ t('admin.branding.overrides_list') }}</h2>
        <div v-if="overrides.length === 0" class="text-sm text-ink-3 py-4 text-center">
          {{ t('admin.branding.no_overrides') }}
        </div>
        <table v-else class="w-full text-sm">
          <thead>
            <tr class="text-left text-xs text-ink-3 border-b border-line">
              <th class="py-2 pr-4">{{ t('admin.branding.category') }}</th>
              <th class="py-2 pr-4">{{ t('admin.branding.slot') }}</th>
              <th class="py-2 pr-4">{{ t('admin.branding.image') }}</th>
              <th class="py-2"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="o in overrides" :key="`${o.category_id}:${o.slot}`" class="border-b border-line/50">
              <td class="py-2 pr-4">{{ categoryName(o.category_id) }}</td>
              <td class="py-2 pr-4 text-xs">{{ slotLabel(o.slot) }}</td>
              <td class="py-2 pr-4">
                <img :src="o.image_url" class="h-10 rounded border border-line" alt="" />
              </td>
              <td class="py-2 text-right">
                <button
                  @click="deletingOverride = o"
                  class="px-2 py-1 text-xs rounded-md border border-red-300 text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 transition"
                >
                  {{ t('admin.branding.delete') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Delete set confirm -->
    <ConfirmDialog
      :open="deletingId !== null"
      :title="t('admin.branding.delete')"
      :message="t('admin.branding.delete_set_confirm')"
      :confirm-text="t('admin.branding.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="confirmDeleteSet"
      @cancel="deletingId = null"
    />

    <!-- Delete override confirm -->
    <ConfirmDialog
      :open="deletingOverride !== null"
      :title="t('admin.branding.delete')"
      :message="t('admin.branding.delete_override_confirm')"
      :confirm-text="t('admin.branding.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="confirmDeleteOverride"
      @cancel="deletingOverride = null"
    />
  </div>
</template>
