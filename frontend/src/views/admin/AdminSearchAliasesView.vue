<script setup>
import { ref, reactive, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import EmptyState from '../../components/EmptyState.vue';

const { t } = useI18n();
const { toast } = useToast();

const aliases = ref([]);
const loading = ref(true);
const showForm = ref(false);
const editingId = ref(null);
const submitting = ref(false);

const form = reactive({
  slug: '',
  title: '',
  description: '',
  seo_title: '',
  seo_description: '',
  og_image: '',
  json_ld: '',
  search_query: '',
  category_slug: '',
  price_min: null,
  price_max: null,
  attr_filters: {},
  sort_order: 'relevance',
  is_active: true,
});

const attrFilterCode = ref('');
const attrFilterValues = ref('');

const fetchAliases = async () => {
  loading.value = true;
  try {
    const response = await api.get('/admin/search-aliases');
    aliases.value = response.data.items || [];
  } catch (e) {
    console.error(e);
    toast.error(t('admin.error'));
  } finally {
    loading.value = false;
  }
};

const openCreateForm = () => {
  editingId.value = null;
  resetForm();
  showForm.value = true;
};

const openEditForm = async (id) => {
  try {
    const response = await api.get(`/admin/search-aliases/${id}`);
    const a = response.data;
    editingId.value = id;
    form.slug = a.slug || '';
    form.title = a.title || '';
    form.description = a.description || '';
    form.seo_title = a.seo_title || '';
    form.seo_description = a.seo_description || '';
    form.og_image = a.og_image || '';
    form.json_ld = a.json_ld || '';
    form.search_query = a.search_query || '';
    form.category_slug = a.category_slug || '';
    form.price_min = a.price_min;
    form.price_max = a.price_max;
    form.attr_filters = a.attr_filters || {};
    form.sort_order = a.sort_order || 'relevance';
    form.is_active = a.is_active !== false;
    showForm.value = true;
  } catch (e) {
    toast.error(t('admin.error'));
  }
};

const resetForm = () => {
  form.slug = '';
  form.title = '';
  form.description = '';
  form.seo_title = '';
  form.seo_description = '';
  form.og_image = '';
  form.json_ld = '';
  form.search_query = '';
  form.category_slug = '';
  form.price_min = null;
  form.price_max = null;
  form.attr_filters = {};
  form.sort_order = 'relevance';
  form.is_active = true;
};

const addAttrFilter = () => {
  if (!attrFilterCode.value) return;
  const values = attrFilterValues.value
    .split(',')
    .map(v => v.trim())
    .filter(v => v);
  if (values.length > 0) {
    form.attr_filters[attrFilterCode.value] = values;
  }
  attrFilterCode.value = '';
  attrFilterValues.value = '';
};

const removeAttrFilter = (code) => {
  delete form.attr_filters[code];
};

const validateJsonLd = () => {
  if (!form.json_ld) return true;
  try {
    JSON.parse(form.json_ld);
    return true;
  } catch (e) {
    toast.error('Invalid JSON-LD: ' + e.message);
    return false;
  }
};

const submitForm = async () => {
  if (!validateJsonLd()) return;

  submitting.value = true;
  try {
    const payload = { ...form };
    // Convert price fields to numbers or null
    payload.price_min = payload.price_min ? parseFloat(payload.price_min) : null;
    payload.price_max = payload.price_max ? parseFloat(payload.price_max) : null;

    if (editingId.value) {
      await api.patch(`/admin/search-aliases/${editingId.value}`, payload);
      toast.success(t('admin.updated'));
    } else {
      await api.post('/admin/search-aliases', payload);
      toast.success(t('admin.created'));
    }
    showForm.value = false;
    fetchAliases();
  } catch (e) {
    const msg = e.response?.data?.message || t('admin.error');
    toast.error(msg);
  } finally {
    submitting.value = false;
  }
};

const deleteAlias = async (id) => {
  if (!confirm(t('admin.confirm_delete'))) return;
  try {
    await api.delete(`/admin/search-aliases/${id}`);
    toast.success(t('admin.deleted'));
    fetchAliases();
  } catch (e) {
    toast.error(t('admin.error'));
  }
};

onMounted(fetchAliases);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.search_aliases') }}</h1>
      <button
        @click="openCreateForm"
        class="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition-colors"
      >
        {{ t('admin.create') }}
      </button>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Empty State -->
    <div v-else-if="aliases.length === 0" class="bg-surface rounded-lg shadow-sm">
      <EmptyState icon="search" :title="t('admin.no_items')" />
    </div>

    <!-- List -->
    <div v-else class="bg-surface rounded-lg shadow-sm overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm min-w-[800px]">
          <caption class="sr-only">{{ t('tables.search_aliases') }}</caption>
          <thead class="bg-surface-2">
            <tr>
              <th scope="col" class="px-4 py-3 text-left">ID</th>
              <th scope="col" class="px-4 py-3 text-left">{{ t('admin.slug') }}</th>
              <th scope="col" class="px-4 py-3 text-left">{{ t('common.title') }}</th>
              <th scope="col" class="px-4 py-3 text-left">{{ t('admin.query') }}</th>
              <th scope="col" class="px-4 py-3 text-center">{{ t('admin.active') }}</th>
              <th scope="col" class="px-4 py-3 text-right">{{ t('admin.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in aliases" :key="a.id" class="border-t hover:bg-surface-2">
              <td class="px-4 py-3">{{ a.id }}</td>
              <td class="px-4 py-3 text-xs font-mono text-purple-600">{{ a.slug }}</td>
              <td class="px-4 py-3">{{ a.title }}</td>
              <td class="px-4 py-3 text-xs text-ink-3 max-w-[200px] truncate">
                {{ a.search_query || '—' }}
              </td>
              <td class="px-4 py-3 text-center">
                <span
                  class="px-2 py-0.5 rounded-full text-xs"
                  :class="a.is_active ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'"
                >
                  {{ a.is_active ? t('admin.yes') : t('admin.no') }}
                </span>
              </td>
              <td class="px-4 py-3 text-right space-x-2">
                <button @click="openEditForm(a.id)" class="text-purple-600 hover:underline text-xs">
                  {{ t('admin.edit') }}
                </button>
                <button @click="deleteAlias(a.id)" class="text-red-600 hover:underline text-xs">
                  {{ t('admin.delete') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Create/Edit Form Modal -->
    <Teleport to="body">
      <div v-if="showForm" class="fixed inset-0 z-50 flex items-start justify-center pt-10 px-4 overflow-y-auto">
        <div class="fixed inset-0 bg-black/50" @click="showForm = false"></div>
        <div class="relative bg-surface rounded-xl shadow-xl w-full max-w-2xl p-6 my-8">
          <h2 class="text-xl font-bold mb-4 text-purple-700">
            {{ editingId ? t('admin.edit') : t('admin.create') }} — {{ t('admin.search_alias') }}
          </h2>

          <div class="space-y-4">
            <!-- Slug -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('admin.slug') }}</label>
              <input
                v-model="form.slug"
                type="text"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                placeholder="smartfony-do-500-zl"
              />
            </div>

            <!-- Title -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('common.title') }}</label>
              <input
                v-model="form.title"
                type="text"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              />
            </div>

            <!-- Description -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('common.description') }}</label>
              <textarea
                v-model="form.description"
                rows="2"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              ></textarea>
            </div>

            <!-- SEO Title -->
            <div>
              <label class="block text-sm font-medium mb-1">SEO Title</label>
              <input
                v-model="form.seo_title"
                type="text"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              />
            </div>

            <!-- SEO Description -->
            <div>
              <label class="block text-sm font-medium mb-1">SEO Description</label>
              <textarea
                v-model="form.seo_description"
                rows="2"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              ></textarea>
            </div>

            <!-- OG Image -->
            <div>
              <label class="block text-sm font-medium mb-1">OG Image URL</label>
              <input
                v-model="form.og_image"
                type="text"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              />
            </div>

            <!-- Search Query -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('admin.search_query') }}</label>
              <input
                v-model="form.search_query"
                type="text"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                placeholder="smartfony samsung"
              />
            </div>

            <!-- Category Slug -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('admin.category_slug') }}</label>
              <input
                v-model="form.category_slug"
                type="text"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                placeholder="electronics/phones"
              />
            </div>

            <!-- Price Range -->
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="block text-sm font-medium mb-1">{{ t('admin.price_min') }}</label>
                <input
                  v-model="form.price_min"
                  type="number"
                  step="0.01"
                  class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                />
              </div>
              <div>
                <label class="block text-sm font-medium mb-1">{{ t('admin.price_max') }}</label>
                <input
                  v-model="form.price_max"
                  type="number"
                  step="0.01"
                  class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                />
              </div>
            </div>

            <!-- Sort Order -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('admin.sort_order') }}</label>
              <select
                v-model="form.sort_order"
                class="w-full px-3 py-2 border border-line rounded-lg focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              >
                <option value="relevance">{{ t('admin.relevance') }}</option>
                <option value="price_asc">{{ t('admin.price_asc') }}</option>
                <option value="price_desc">{{ t('admin.price_desc') }}</option>
              </select>
            </div>

            <!-- Attribute Filters -->
            <div>
              <label class="block text-sm font-medium mb-1">{{ t('admin.attr_filters') }}</label>
              <div v-for="(values, code) in form.attr_filters" :key="code" class="flex items-center gap-2 mb-2">
                <span class="text-xs bg-purple-100 text-purple-700 px-2 py-1 rounded">{{ code }}</span>
                <span class="text-xs text-ink-3">{{ values.join(', ') }}</span>
                <button @click="removeAttrFilter(code)" class="text-red-500 text-xs">×</button>
              </div>
              <div class="flex gap-2">
                <input
                  v-model="attrFilterCode"
                  type="text"
                  placeholder="attr code (e.g. brand)"
                  class="flex-1 px-3 py-2 border border-line rounded-lg text-sm focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                />
                <input
                  v-model="attrFilterValues"
                  type="text"
                  placeholder="values (comma-separated)"
                  class="flex-1 px-3 py-2 border border-line rounded-lg text-sm focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                />
                <button @click="addAttrFilter" class="px-3 py-2 bg-purple-600 text-white rounded-lg text-sm">
                  +
                </button>
              </div>
            </div>

            <!-- JSON-LD -->
            <div>
              <label class="block text-sm font-medium mb-1">JSON-LD (Structured Data)</label>
              <textarea
                v-model="form.json_ld"
                rows="4"
                class="w-full px-3 py-2 border border-line rounded-lg font-mono text-xs focus:ring-2 focus:ring-purple-500 focus:border-transparent"
                placeholder='{"@context": "https://schema.org", "@type": "WebPage", ...}'
              ></textarea>
            </div>

            <!-- Is Active -->
            <div class="flex items-center gap-2">
              <input
                v-model="form.is_active"
                type="checkbox"
                id="is_active"
                class="h-4 w-4 text-purple-600 border-line rounded focus:ring-purple-500"
              />
              <label for="is_active" class="text-sm font-medium">{{ t('admin.is_active') }}</label>
            </div>
          </div>

          <!-- Actions -->
          <div class="flex justify-end gap-3 mt-6">
            <button
              @click="showForm = false"
              class="px-4 py-2 border border-line rounded-lg hover:bg-surface-2 transition-colors"
            >
              {{ t('common.cancel') }}
            </button>
            <button
              @click="submitForm"
              :disabled="submitting"
              class="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition-colors disabled:opacity-50"
            >
              {{ submitting ? t('common.saving') : t('common.save') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
