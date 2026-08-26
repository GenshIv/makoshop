<script setup>
import { ref, reactive, onMounted, onUnmounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';
import EmptyState from '../../components/EmptyState.vue';
import AdminCategoryTree from '../../components/AdminCategoryTree.vue';

const { t, locale } = useI18n();
const { toast } = useToast();

const categories = ref([]);
const tree = ref([]);
const loading = ref(true);
const showForm = ref(false);
const editingId = ref(null);
const activeTab = ref('list'); // 'list' or 'tree'
const sortField = ref('id'); // 'id', 'name', 'parent'
const sortDirection = ref('asc'); // 'asc' or 'desc'

const form = reactive({
  name_ru: '',
  name_ua: '',
  name_pl: '',
  name_en: '',
  slug: '',
  parent_id: null,
  description: '',
  description_ru: '',
  description_ua: '',
  description_pl: '',
  description_en: '',
  image_light_url: '',
  image_dark_url: '',
  is_active: true,
  sort_order: 0,
  anchor_keywords: '', // comma-separated
});

const uploading = ref({ light: false, dark: false });
const uploadError = ref({ light: '', dark: '' });

// Upload image file
const uploadImage = async (file, theme) => {
  if (!file) return;

  // Validate file type
  const validTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp', 'image/gif'];
  if (!validTypes.includes(file.type)) {
    uploadError.value[theme] = 'Invalid file type. Use JPG, PNG, WEBP or GIF.';
    return;
  }

  // Validate file size (max 10MB)
  if (file.size > 10 * 1024 * 1024) {
    uploadError.value[theme] = 'File too large. Max 10MB.';
    return;
  }

  uploading.value[theme] = true;
  uploadError.value[theme] = '';

  try {
    const formData = new FormData();
    formData.append('file', file);

    const response = await api.post('/admin/upload-image', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });

    if (theme === 'light') {
      form.image_light_url = response.data.url;
    } else {
      form.image_dark_url = response.data.url;
    }
  } catch (e) {
    uploadError.value[theme] = e.response?.data?.message || 'Upload failed';
  } finally {
    uploading.value[theme] = false;
  }
};

// Remove image
const removeImage = async (theme) => {
  const url = theme === 'light' ? form.image_light_url : form.image_dark_url;
  if (!url) return;

  try {
    // Extract filename from URL: /uploads/categories/{filename}
    const filename = url.split('/').pop();
    await api.delete(`/admin/upload-image/${filename}`);
  } catch (e) {
    console.error('Failed to delete image:', e);
  }

  if (theme === 'light') {
    form.image_light_url = '';
  } else {
    form.image_dark_url = '';
  }
};

// Handle file input change
const onFileChange = (event, theme) => {
  const file = event.target.files?.[0];
  if (file) {
    uploadImage(file, theme);
  }
};

const sortedCategories = () => {
  const catMap = new Map(categories.value.map(c => [c.id, c]));
  const sorted = [...categories.value].sort((a, b) => {
    let valA, valB;
    
    if (sortField.value === 'id') {
      valA = a.id;
      valB = b.id;
    } else if (sortField.value === 'name') {
      valA = (a.name_en || a.name_ru || '').toLowerCase();
      valB = (b.name_en || b.name_ru || '').toLowerCase();
    } else if (sortField.value === 'parent') {
      valA = a.parent_id ? (catMap.get(a.parent_id)?.name_en || catMap.get(a.parent_id)?.name_ru || '') : '';
      valB = b.parent_id ? (catMap.get(b.parent_id)?.name_en || catMap.get(b.parent_id)?.name_ru || '') : '';
    }
    
    if (valA < valB) return sortDirection.value === 'asc' ? -1 : 1;
    if (valA > valB) return sortDirection.value === 'asc' ? 1 : -1;
    return 0;
  });
  return sorted;
};

const toggleSort = (field) => {
  if (sortField.value === field) {
    sortDirection.value = sortDirection.value === 'asc' ? 'desc' : 'asc';
  } else {
    sortField.value = field;
    sortDirection.value = 'asc';
  }
};

const fetchCategories = async () => {
  loading.value = true;
  try {
    const response = await api.get('/categories');
    categories.value = Array.isArray(response.data.items) ? response.data.items : [];
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const fetchTree = async () => {
  try {
    const response = await api.get('/categories/tree');
    tree.value = Array.isArray(response.data) ? response.data : [];
  } catch (e) {
    console.error(e);
  }
};

// Flatten tree for dropdown (exclude self and descendants when editing)
const parentOptions = ref([]);

// Get localized category name based on current locale
const catDisplayName = (cat) => {
  if (!cat) return String(cat?.id || '');
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || String(cat.id);
};

const updateParentOptions = () => {
  const excludeId = editingId.value;
  const flatten = (nodes, result = []) => {
    for (const node of nodes) {
      if (node.id !== excludeId) {
        result.push({ id: node.id, name: catDisplayName(node), level: node._level || 0 });
        if (node.children?.length) {
          flatten(node.children, result);
        }
      }
    }
    return result;
  };
  const flat = flatten(tree.value);
  // Set _level during flatten
  const setLevel = (nodes, level = 0) => {
    for (const node of nodes) {
      node._level = level;
      if (node.children?.length) setLevel(node.children, level + 1);
    }
  };
  setLevel(tree.value);
  parentOptions.value = [{ id: null, name: t('admin.root_category'), level: -1 }, ...flatten(tree.value)];
};

const resetForm = () => {
  Object.assign(form, {
    name_ru: '',
    name_ua: '',
    name_pl: '',
    name_en: '',
    slug: '',
    parent_id: null,
    description: '',
    description_ru: '',
    description_ua: '',
    description_pl: '',
    description_en: '',
    image_light_url: '',
    image_dark_url: '',
    is_active: true,
    sort_order: 0,
    anchor_keywords: '',
  });
  editingId.value = null;
  showForm.value = false;
};

const openNewCategoryForm = () => {
  resetForm();
  showForm.value = true;
};

const editCategory = (cat) => {
  editingId.value = cat.id;
  form.name_ru = cat.name_ru || '';
  form.name_ua = cat.name_ua || '';
  form.name_pl = cat.name_pl || '';
  form.name_en = cat.name_en || '';
  form.slug = cat.slug || '';
  form.parent_id = cat.parent_id || null;
  form.description = cat.description || '';
  form.description_ru = cat.description_ru || '';
  form.description_ua = cat.description_ua || '';
  form.description_pl = cat.description_pl || '';
  form.description_en = cat.description_en || '';
  form.image_light_url = cat.image_light_url || '';
  form.image_dark_url = cat.image_dark_url || '';
  form.is_active = cat.is_active;
  form.sort_order = cat.sort_order || 0;
  form.anchor_keywords = (cat.anchor_keywords || []).join(', ');
  showForm.value = true;
  updateParentOptions();
};

const saveCategory = async () => {
  if (!form.name_en && !form.name_ru) {
    toast.error(t('admin.category_name_required'));
    return;
  }
  try {
    // Parse anchor_keywords: split by comma, trim, filter empty
    const keywords = form.anchor_keywords
      .split(',')
      .map(k => k.trim())
      .filter(k => k.length > 0);

    const payload = {
      name_ru: form.name_ru,
      name_ua: form.name_ua,
      name_pl: form.name_pl,
      name_en: form.name_en,
      slug: form.slug || null,
      parent_id: form.parent_id,
      description: form.description,
      description_ru: form.description_ru,
      description_ua: form.description_ua,
      description_pl: form.description_pl,
      description_en: form.description_en,
      image_light_url: form.image_light_url,
      image_dark_url: form.image_dark_url,
      is_active: form.is_active,
      sort_order: form.sort_order,
      anchor_keywords: keywords,
    };

    if (editingId.value) {
      await api.patch(`/admin/categories/${editingId.value}`, payload);
    } else {
      await api.post('/admin/categories', payload);
    }
    resetForm();
    await fetchCategories();
    await fetchTree();
  } catch (e) {
    toast.error(e.response?.data?.message || e.response?.data?.error?.message || t('admin.save_error'));
  }
};

const deleteCat = ref(null);

const askDelete = (cat) => {
  deleteCat.value = cat;
};

const deleteCategory = async (cat) => {
  deleteCat.value = null;
  try {
    await api.delete(`/admin/categories/${cat.id}`);
    await fetchCategories();
    await fetchTree();
  } catch (e) {
    toast.error(e.response?.data?.message || e.response?.data?.error?.message || t('admin.delete_error'));
  }
};

const toggleActive = async (cat) => {
  try {
    await api.patch(`/admin/categories/${cat.id}`, { is_active: !cat.is_active });
    await fetchCategories();
    await fetchTree();
  } catch (e) {
    toast.error(t('admin.error'));
  }
};

const goToAttributes = (cat) => {
  window.open(`/admin/categories/${cat.id}/attributes`, '_blank');
};

const rebuildOpen = ref(false);

const askRebuild = () => {
  rebuildOpen.value = true;
};

const rebuildIndexes = async () => {
  rebuildOpen.value = false;
  try {
    const response = await api.post('/admin/rebuild-category-indexes?force=1');
    toast.success(t('admin.rebuild_indexes_done', { result: JSON.stringify(response.data) }));
    await fetchCategories();
    await fetchTree();
  } catch (e) {
    toast.error(e.response?.data?.message || e.response?.data?.error?.message || t('admin.rebuild_indexes_failed'));
  }
};

const handleKeydown = (e) => {
  if (e.key === 'Escape' && showForm.value) {
    resetForm();
  }
};

onMounted(() => {
  fetchCategories();
  fetchTree();
  document.addEventListener('keydown', handleKeydown);
});

onUnmounted(() => {
  document.removeEventListener('keydown', handleKeydown);
});

watch(showForm, (val) => {
  if (val) updateParentOptions();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.categories_title') }}</h1>
      <div class="flex gap-2">
        <button @click="askRebuild" class="px-4 py-2 bg-amber-600 text-white rounded-lg hover:bg-amber-700">
          {{ t('admin.rebuild_indexes') || 'Rebuild Indexes' }}
        </button>
        <button @click="openNewCategoryForm" class="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700">
          {{ t('admin.add_category') }}
        </button>
      </div>
    </div>

    <!-- Tab switcher -->
    <div class="flex gap-2 mb-4">
      <button
        @click="activeTab = 'list'"
        :class="[
          'px-4 py-2 rounded-lg text-sm font-medium transition',
          activeTab === 'list'
            ? 'bg-purple-600 text-white'
            : 'bg-surface-2 text-ink-2 hover:bg-surface-3'
        ]"
      >
        {{ t('admin.tab_list') || 'List' }}
      </button>
      <button
        @click="activeTab = 'tree'"
        :class="[
          'px-4 py-2 rounded-lg text-sm font-medium transition',
          activeTab === 'tree'
            ? 'bg-purple-600 text-white'
            : 'bg-surface-2 text-ink-2 hover:bg-surface-3'
        ]"
      >
        {{ t('admin.tab_tree') || 'Tree (Drag & Drop)' }}
      </button>
    </div>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Form Modal -->
    <Teleport to="body">
      <div v-if="showForm" class="fixed inset-0 z-50 overflow-y-auto">
        <!-- Backdrop -->
        <div class="fixed inset-0 bg-black bg-opacity-50" @click="resetForm"></div>
        
        <!-- Modal -->
        <div class="relative min-h-full flex items-center justify-center p-4">
          <div class="relative bg-surface rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] overflow-y-auto">
            <!-- Header -->
            <div class="sticky top-0 bg-surface border-b border-line px-6 py-4 flex items-center justify-between">
              <h3 class="font-medium text-lg">
                {{ editingId ? t('admin.edit_category') : t('admin.new_category') }}
              </h3>
              <button @click="resetForm" class="text-ink-3 hover:text-ink-1">
                <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
            
            <!-- Content -->
            <div class="px-6 py-4">
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <input v-model="form.name_ru" type="text" :placeholder="t('admin.category_name_placeholder') + ' (RU)'" class="px-3 py-2 border border-line rounded-lg text-sm" />
                <input v-model="form.name_ua" type="text" placeholder="Name UA" class="px-3 py-2 border border-line rounded-lg text-sm" />
                <input v-model="form.name_pl" type="text" placeholder="Name PL" class="px-3 py-2 border border-line rounded-lg text-sm" />
                <input v-model="form.name_en" type="text" placeholder="Name EN (used for slug)" class="px-3 py-2 border border-line rounded-lg text-sm" />
                <input v-model="form.slug" type="text" placeholder="Slug (auto from EN if empty)" class="sm:col-span-2 px-3 py-2 border border-line rounded-lg text-sm" />
                <select v-model="form.parent_id" class="px-3 py-2 border border-line rounded-lg text-sm bg-surface">
                  <option :value="null">{{ t('admin.root_category') }}</option>
                  <option v-for="opt in parentOptions.filter(o => o.id !== null)" :key="opt.id" :value="opt.id">
                    {{ '  '.repeat(opt.level) }}{{ opt.name }}
                  </option>
                </select>
                <!-- Descriptions per language -->
                <div class="sm:col-span-2">
                  <label class="block text-sm font-medium text-ink-2 mb-1">Descriptions</label>
                  <textarea v-model="form.description_ru" placeholder="Description RU" rows="2" class="w-full px-3 py-2 border border-line rounded-lg text-sm mb-1"></textarea>
                  <textarea v-model="form.description_ua" placeholder="Description UA" rows="2" class="w-full px-3 py-2 border border-line rounded-lg text-sm mb-1"></textarea>
                  <textarea v-model="form.description_pl" placeholder="Description PL" rows="2" class="w-full px-3 py-2 border border-line rounded-lg text-sm mb-1"></textarea>
                  <textarea v-model="form.description_en" placeholder="Description EN" rows="2" class="w-full px-3 py-2 border border-line rounded-lg text-sm"></textarea>
                </div>
                <!-- Images (dark/light theme) -->
                <div class="sm:col-span-2">
                  <label class="block text-sm font-medium text-ink-2 mb-2">Category Images</label>
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <!-- Light theme image -->
                    <div class="border border-line rounded-lg p-3">
                      <div class="flex items-center justify-between mb-2">
                        <span class="text-sm font-medium text-ink-2">Light Theme</span>
                        <span class="text-xs text-ink-3">JPG, PNG, WEBP (max 10MB)</span>
                      </div>
                      <div v-if="form.image_light_url" class="mb-2">
                        <img :src="form.image_light_url" alt="Light theme" class="w-full h-32 object-cover rounded-lg border border-line" />
                      </div>
                      <div v-else class="mb-2 h-32 border-2 border-dashed border-line rounded-lg flex items-center justify-center bg-surface-2">
                        <span class="text-xs text-ink-3">No image</span>
                      </div>
                      <div class="flex gap-2">
                        <label class="flex-1 cursor-pointer">
                          <span class="inline-block px-3 py-1.5 bg-orange-600 text-white text-xs rounded-lg hover:bg-orange-700">
                            {{ uploading.light ? 'Uploading...' : 'Upload' }}
                          </span>
                          <input type="file" accept="image/*" class="hidden" @change="onFileChange($event, 'light')" :disabled="uploading.light" />
                        </label>
                        <button v-if="form.image_light_url" @click="removeImage('light')" class="px-3 py-1.5 bg-red-600 text-white text-xs rounded-lg hover:bg-red-700">
                          Remove
                        </button>
                      </div>
                      <p v-if="uploadError.light" class="text-xs text-red-500 mt-1">{{ uploadError.light }}</p>
                    </div>
                    <!-- Dark theme image -->
                    <div class="border border-line rounded-lg p-3">
                      <div class="flex items-center justify-between mb-2">
                        <span class="text-sm font-medium text-ink-2">Dark Theme</span>
                        <span class="text-xs text-ink-3">JPG, PNG, WEBP (max 10MB)</span>
                      </div>
                      <div v-if="form.image_dark_url" class="mb-2">
                        <img :src="form.image_dark_url" alt="Dark theme" class="w-full h-32 object-cover rounded-lg border border-line" />
                      </div>
                      <div v-else class="mb-2 h-32 border-2 border-dashed border-line rounded-lg flex items-center justify-center bg-surface-2">
                        <span class="text-xs text-ink-3">No image</span>
                      </div>
                      <div class="flex gap-2">
                        <label class="flex-1 cursor-pointer">
                          <span class="inline-block px-3 py-1.5 bg-orange-600 text-white text-xs rounded-lg hover:bg-orange-700">
                            {{ uploading.dark ? 'Uploading...' : 'Upload' }}
                          </span>
                          <input type="file" accept="image/*" class="hidden" @change="onFileChange($event, 'dark')" :disabled="uploading.dark" />
                        </label>
                        <button v-if="form.image_dark_url" @click="removeImage('dark')" class="px-3 py-1.5 bg-red-600 text-white text-xs rounded-lg hover:bg-red-700">
                          Remove
                        </button>
                      </div>
                      <p v-if="uploadError.dark" class="text-xs text-red-500 mt-1">{{ uploadError.dark }}</p>
                    </div>
                  </div>
                </div>
                <div class="flex items-center gap-4">
                  <label class="flex items-center gap-2 text-sm">
                    <input v-model="form.is_active" type="checkbox" />
                    {{ t('admin.is_active') }}
                  </label>
                  <div class="flex items-center gap-2">
                    <span class="text-sm">{{ t('admin.order') }}</span>
                    <input v-model.number="form.sort_order" type="number" class="w-20 px-2 py-1 border border-line rounded text-sm" />
                  </div>
                </div>
                <div class="sm:col-span-2">
                  <label class="block text-sm font-medium text-ink-2 mb-1">
                    {{ t('admin.anchor_keywords') || 'Anchor keywords (comma-separated)' }}
                  </label>
                  <input v-model="form.anchor_keywords" type="text"
                    :placeholder="t('admin.anchor_keywords_placeholder')"
                    class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
                  <p class="text-xs text-ink-3 mt-1">
                    {{ t('admin.anchor_keywords_hint') || 'Root words for auto-catalogization. One per product type.' }}
                  </p>
                </div>
              </div>
            </div>
            
            <!-- Footer -->
            <div class="sticky bottom-0 bg-surface border-t border-line px-6 py-4 flex gap-2 justify-end">
              <button @click="resetForm" class="px-4 py-2 border rounded-lg text-sm hover:bg-surface-2">
                {{ t('admin.cancel') }}
              </button>
              <button @click="saveCategory" class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
                {{ editingId ? t('admin.save') : t('admin.create') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Tree view (drag & drop) -->
    <div v-if="activeTab === 'tree'" class="bg-surface rounded-lg shadow-sm p-4">
      <p class="text-sm text-ink-3 mb-4">
        {{ t('admin.tree_hint') || 'Drag categories to reorder or change parent. Drop in the middle to make a child, top/bottom to reorder siblings.' }}
      </p>
      <AdminCategoryTree
        :categories="categories"
        @reordered="fetchCategories"
      />
    </div>

    <!-- List view -->
    <div v-else-if="activeTab === 'list' && categories.length === 0" class="bg-surface rounded-lg shadow-sm">
      <EmptyState icon="tag" :title="t('admin.no_categories')" />
    </div>

    <div v-else-if="activeTab === 'list'" class="bg-surface rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <caption class="sr-only">{{ t('tables.admin_categories') }}</caption>
        <thead class="bg-surface-2">
          <tr>
            <th scope="col" class="px-4 py-3 text-left cursor-pointer hover:bg-surface-3" @click="toggleSort('id')">
              ID
              <span v-if="sortField === 'id'" class="ml-1">
                {{ sortDirection === 'asc' ? '↑' : '↓' }}
              </span>
            </th>
            <th scope="col" class="px-4 py-3 text-left cursor-pointer hover:bg-surface-3" @click="toggleSort('name')">
              {{ t('admin.name') }}
              <span v-if="sortField === 'name'" class="ml-1">
                {{ sortDirection === 'asc' ? '↑' : '↓' }}
              </span>
            </th>
            <th scope="col" class="px-4 py-3 text-left cursor-pointer hover:bg-surface-3" @click="toggleSort('parent')">
              {{ t('admin.parent') }}
              <span v-if="sortField === 'parent'" class="ml-1">
                {{ sortDirection === 'asc' ? '↑' : '↓' }}
              </span>
            </th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.status') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="cat in sortedCategories()" :key="cat.id" class="border-t hover:bg-surface-2">
            <td class="px-4 py-3">{{ cat.id }}</td>
            <td class="px-4 py-3 font-medium">{{ catDisplayName(cat) }}</td>
            <td class="px-4 py-3 text-ink-3">
              {{ cat.parent_id ? `#${cat.parent_id}` : '—' }}
            </td>
            <td class="px-4 py-3">
              <button
                @click="toggleActive(cat)"
                :class="cat.is_active ? 'text-green-600 hover:text-green-700' : 'text-ink-3 hover:text-ink-2'"
                class="text-xs underline cursor-pointer"
              >
                {{ cat.is_active ? t('admin.active') : t('admin.inactive') }}
              </button>
            </td>
            <td class="px-4 py-3">
              <div class="flex gap-2">
                <button @click="editCategory(cat)" class="text-xs text-orange-600 hover:underline">
                  {{ t('admin.edit') }}
                </button>
                <button @click="goToAttributes(cat)" class="text-xs text-purple-600 hover:underline">
                  {{ t('admin.attributes') }}
                </button>
                <button @click="askDelete(cat)" class="text-xs text-red-600 hover:underline">
                  {{ t('admin.delete') }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <ConfirmDialog
      :open="deleteCat !== null"
      :title="t('admin.categories_title')"
      :message="deleteCat ? t('admin.delete_confirm', { name: catDisplayName(deleteCat) }) : ''"
      variant="danger"
      :confirm-text="t('admin.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="deleteCategory(deleteCat)"
      @cancel="deleteCat = null"
    />

    <ConfirmDialog
      :open="rebuildOpen"
      :title="t('admin.categories_title')"
      :message="t('admin.rebuild_indexes_confirm')"
      :confirm-text="t('admin.save')"
      :cancel-text="t('admin.cancel')"
      @confirm="rebuildIndexes"
      @cancel="rebuildOpen = false"
    />
  </div>
</template>
