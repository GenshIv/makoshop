<script setup>
import { ref, onMounted, watch } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';

const { t, locale } = useI18n();
const { toast } = useToast();
const router = useRouter();
const route = useRoute();

const eanpages = ref([]);
const total = ref(0);
const page = ref(1);
const limit = ref(50);
const searchQuery = ref('');
const loading = ref(false);
const editing = ref(null); // { id, data }
const deleteConfirm = ref(null);

// Catalogizer modal state
const catalogModal = ref(null); // { eanpage, results, loading }
const categories = ref([]);
const addTokenInputs = ref({});

const catDisplayName = (cat) => {
  if (!cat) return String(cat?.id || '');
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || String(cat.id);
};

const fetchCategories = async () => {
  try {
    const res = await api.get('/admin/categories');
    const data = res.data;
    categories.value = Array.isArray(data) ? data : (data?.items || []);
  } catch (e) {
    console.error('Failed to fetch categories:', e);
  }
};

const getCategoryById = (id) => categories.value.find(c => c.id === id);

const openCatalogModal = async (sp) => {
  catalogModal.value = { eanpage: sp, results: null, loading: true };
  try {
    const res = await api.post('/admin/catalogizer/test', { name: sp.title || sp.ean });
    catalogModal.value.results = res.data;
  } catch (e) {
    console.error('Catalogizer test error:', e);
    toast.error(t('admin.catalogizer_test_failed') || 'Test failed');
  } finally {
    catalogModal.value.loading = false;
  }
};

const closeCatalogModal = () => {
  catalogModal.value = null;
};

const updateAnchorKeywords = async (catId, newKeywords) => {
  try {
    await api.patch(`/admin/categories/${catId}`, { anchor_keywords: newKeywords });
    await fetchCategories();
    return true;
  } catch (e) {
    console.error('Failed to update anchor_keywords:', e);
    toast.error(t('admin.catalogizer_update_failed') || 'Update failed');
    return false;
  }
};

const addTokenToCategory = async (catId, token) => {
  const t = (token || '').trim().toLowerCase();
  if (!t) return;
  const cat = categories.value.find(c => c.id === catId);
  if (!cat) return;
  const current = cat.anchor_keywords || [];
  if (current.includes(t)) return;
  const updated = [...current, t].slice(0, 50);
  await updateAnchorKeywords(catId, updated);
};

const removeTokenFromCategory = async (catId, token) => {
  const cat = categories.value.find(c => c.id === catId);
  if (!cat) return;
  const current = cat.anchor_keywords || [];
  const updated = current.filter(kw => kw !== token);
  await updateAnchorKeywords(catId, updated);
};

const onAddTokenInput = (catId, event) => {
  addTokenInputs.value[catId] = event.target.value;
};

const handleAddTokenEnter = (catId) => {
  addTokenToCategory(catId, addTokenInputs.value[catId] || '');
  addTokenInputs.value[catId] = '';
};

const fetchEANPages = async () => {
  loading.value = true;
  try {
    const params = { page: page.value, limit: limit.value };
    if (searchQuery.value) params.q = searchQuery.value;
    const res = await api.get('/admin/eanpages', { params });
    eanpages.value = res.data.items || [];
    total.value = res.data.total || 0;
  } catch (e) {
    console.error('Failed to fetch EAN pages:', e);
  } finally {
    loading.value = false;
  }
};

const recalculateMinPrices = () => {
  console.log('Recalculate button clicked! Function exists:', typeof recalculateMinPrices);
  
  if (!confirm('Recalculate min prices for all EAN pages? This may take a while.')) {
    console.log('User cancelled');
    return;
  }
  
  console.log('Starting recalculation...');
  const recalcLoading = ref(true);
  
  (async () => {
    try {
      console.log('Making API call to:', '/admin/eanpages/recalculate-min-prices');
      const response = await api.post('/admin/eanpages/recalculate-min-prices');
      console.log('API response:', response);
      toast.success('Min prices recalculated successfully!');
      await fetchEANPages();
    } catch (e) {
      console.error('Failed to recalculate min prices:', e);
      toast.error('Failed to recalculate min prices');
    } finally {
      recalcLoading.value = false;
    }
  })();
};

const startEdit = (sp) => {
  editing.value = {
    id: sp.id,
    data: {
      title: sp.title || '',
      description: sp.description || '',
      keywords: sp.keywords || '',
      slug: sp.slug || '',
      is_active: sp.is_active !== false,
      category_id: sp.category_id || 0,
      content: sp.content || '',
      images: sp.images || [],
    },
  };
};

const cancelEdit = () => {
  editing.value = null;
};

const saveEdit = async () => {
  if (!editing.value) return;
  try {
    await api.patch(`/admin/eanpages/${editing.value.id}`, editing.value.data);
    editing.value = null;
    await fetchEANPages();
  } catch (e) {
    console.error('Failed to update EAN page:', e);
    toast.error(t('admin.eanpage_save_error') || 'Failed to save');
  }
};

const confirmDelete = (sp) => {
  deleteConfirm.value = sp;
};

const cancelDelete = () => {
  deleteConfirm.value = null;
};

const doDelete = async () => {
  if (!deleteConfirm.value) return;
  try {
    await api.delete(`/admin/eanpages/${deleteConfirm.value.id}`);
    deleteConfirm.value = null;
    await fetchEANPages();
  } catch (e) {
    console.error('Failed to delete EAN page:', e);
    toast.error(t('admin.eanpage_delete_error') || 'Failed to delete');
  }
};

const goToPage = (p) => {
  if (p >= 1 && p <= Math.ceil(total.value / limit.value)) {
    page.value = p;
  }
};

// Debounced search
watch(searchQuery, () => {
  clearTimeout(window._scuSearchTimeout);
  window._scuSearchTimeout = setTimeout(() => {
    page.value = 1;
    fetchEANPages();
  }, 400);
});

onMounted(() => {
  fetchEANPages();
  fetchCategories();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.eanpage_title') || 'EAN Pages' }}</h1>
      <div class="flex items-center gap-3">
        <button
          @click="recalculateMinPrices"
          class="px-4 py-2 bg-orange-600 text-white rounded-lg hover:bg-orange-700 transition-colors text-sm"
        >
          Recalculate Min Prices
        </button>
        <router-link
          to="/admin"
          class="text-sm text-ink-3 hover:text-purple-600"
        >
          {{ t('admin.back_to_dashboard') || 'Back to Dashboard' }}
        </router-link>
      </div>
    </div>

    <!-- Search -->
    <div class="mb-4">
      <input
        v-model="searchQuery"
        type="text"
        class="w-full max-w-md px-4 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
        :placeholder="t('admin.eanpage_search_placeholder') || 'Search by EAN, title...'"
      />
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- List -->
    <div v-else>
      <div class="bg-surface rounded-lg shadow-sm overflow-hidden">
        <table class="w-full text-sm">
          <thead class="bg-surface-2">
            <tr>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_id') || 'ID' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_scu') || 'EAN' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_title') || 'Title' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_keywords') || 'Keywords' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_slug') || 'Slug' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_products') || 'Products' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_active') || 'Active' }}</th>
              <th scope="col" class="px-4 py-2 text-left">{{ t('admin.eanpage_actions') || 'Actions' }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="sp in eanpages"
              :key="sp.id"
              class="border-t hover:bg-surface-2"
            >
              <td class="px-4 py-2">{{ sp.id }}</td>
              <td class="px-4 py-2 max-w-xs truncate" :title="sp.ean">{{ sp.ean }}</td>
              <td class="px-4 py-2 max-w-xs truncate" :title="sp.title">{{ sp.title }}</td>
              <td class="px-4 py-2 max-w-xs truncate text-xs text-ink-2" :title="sp.keywords">{{ sp.keywords || '-' }}</td>
              <td class="px-4 py-2 max-w-xs truncate" :title="sp.slug">{{ sp.slug }}</td>
              <td class="px-4 py-2">{{ sp.product_count || sp.product_ids?.length || 0 }}</td>
              <td class="px-4 py-2">
                <span
                  :class="sp.is_active !== false ? 'text-green-600' : 'text-red-600'"
                >
                  {{ sp.is_active !== false ? t('common.yes') || 'Yes' : t('common.no') || 'No' }}
                </span>
              </td>
              <td class="px-4 py-2 space-x-2">
                <button
                  @click="openCatalogModal(sp)"
                  class="text-purple-600 hover:text-purple-800 text-xs"
                >
                  {{ t('admin.catalogize') || 'Catalogize' }}
                </button>
                <button
                  @click="startEdit(sp)"
                  class="text-orange-600 hover:text-orange-800 text-xs"
                >
                  {{ t('common.edit') || 'Edit' }}
                </button>
                <button
                  @click="confirmDelete(sp)"
                  class="text-red-600 hover:text-red-800 text-xs"
                >
                  {{ t('common.delete') || 'Delete' }}
                </button>
              </td>
            </tr>
            <tr v-if="eanpages.length === 0">
              <td colspan="8" class="px-4 py-8 text-center text-ink-3">
                {{ t('admin.eanpage_no_results') || 'No EAN pages found' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Pagination -->
      <div class="mt-4 flex justify-between items-center text-sm text-ink-2">
        <div>
          {{ t('admin.eanpage_showing') || 'Showing' }} {{ total }}
        </div>
        <div class="space-x-1">
          <button
            @click="goToPage(page - 1)"
            :disabled="page <= 1"
            class="px-3 py-1 border rounded disabled:opacity-50 hover:bg-surface-2"
          >
            &laquo;
          </button>
          <span class="px-3 py-1">{{ page }} / {{ Math.ceil(total / limit) || 1 }}</span>
          <button
            @click="goToPage(page + 1)"
            :disabled="page >= Math.ceil(total / limit)"
            class="px-3 py-1 border rounded disabled:opacity-50 hover:bg-surface-2"
          >
            &raquo;
          </button>
        </div>
      </div>
    </div>

    <!-- Edit Modal -->
    <div
      v-if="editing"
      class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
      @click.self="cancelEdit"
    >
      <div role="dialog" aria-modal="true" class="bg-surface rounded-lg shadow-xl p-6 w-full max-w-2xl max-h-[90vh] overflow-y-auto">
        <h2 class="text-xl font-bold mb-4 text-purple-700">
          {{ t('admin.eanpage_edit_title') || 'Edit EAN Page' }} #{{ editing.id }}
        </h2>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-ink-2 mb-1">
              {{ t('admin.eanpage_title') || 'Title' }}
            </label>
            <input
              v-model="editing.data.title"
              type="text"
              class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-ink-2 mb-1">
              {{ t('admin.eanpage_description') || 'Description' }}
            </label>
            <textarea
              v-model="editing.data.description"
              rows="3"
              class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            ></textarea>
          </div>

          <div>
            <label class="block text-sm font-medium text-ink-2 mb-1">
              {{ t('admin.eanpage_keywords') || 'Keywords' }}
            </label>
            <input
              v-model="editing.data.keywords"
              type="text"
              class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-ink-2 mb-1">
              {{ t('admin.eanpage_slug') || 'Slug' }}
            </label>
            <input
              v-model="editing.data.slug"
              type="text"
              class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-ink-2 mb-1">
              {{ t('admin.eanpage_category_id') || 'Category ID' }}
            </label>
            <input
              v-model.number="editing.data.category_id"
              type="number"
              class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="flex items-center space-x-2">
              <input
                v-model="editing.data.is_active"
                type="checkbox"
                class="rounded border-line text-purple-600 focus:ring-purple-500"
              />
              <span class="text-sm font-medium text-ink-2">
                {{ t('admin.eanpage_active') || 'Active' }}
              </span>
            </label>
          </div>

          <div>
            <label class="block text-sm font-medium text-ink-2 mb-1">
              {{ t('admin.eanpage_content') || 'Content (HTML)' }}
            </label>
            <textarea
              v-model="editing.data.content"
              rows="6"
              class="w-full px-3 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 font-mono text-xs"
            ></textarea>
          </div>
        </div>

        <div class="mt-6 flex justify-end space-x-2">
          <button
            @click="cancelEdit"
            class="px-4 py-2 text-sm text-ink-2 hover:text-ink"
          >
            {{ t('common.cancel') || 'Cancel' }}
          </button>
          <button
            @click="saveEdit"
            class="px-4 py-2 text-sm bg-purple-600 text-white rounded-lg hover:bg-purple-700"
          >
            {{ t('common.save') || 'Save' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Delete Confirmation Modal -->
    <div
      v-if="deleteConfirm"
      class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
      @click.self="cancelDelete"
    >
      <div role="dialog" aria-modal="true" class="bg-surface rounded-lg shadow-xl p-6 w-full max-w-md">
        <h2 class="text-xl font-bold mb-4 text-red-600">
          {{ t('admin.eanpage_delete_confirm_title') || 'Delete EAN Page?' }}
        </h2>
        <p class="text-sm text-ink-2 mb-2">
          {{ t('admin.eanpage_delete_confirm_msg') || 'Are you sure you want to delete this EAN page?' }}
        </p>
        <p class="text-xs text-ink-3 mb-4">
          EAN: {{ deleteConfirm.ean }}<br>
          Title: {{ deleteConfirm.title }}
        </p>
        <div class="flex justify-end space-x-2">
          <button
            @click="cancelDelete"
            class="px-4 py-2 text-sm text-ink-2 hover:text-ink"
          >
            {{ t('common.cancel') || 'Cancel' }}
          </button>
          <button
            @click="doDelete"
            class="px-4 py-2 text-sm bg-red-600 text-white rounded-lg hover:bg-red-700"
          >
            {{ t('common.delete') || 'Delete' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Catalogizer Modal -->
    <div
      v-if="catalogModal"
      class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
      @click.self="closeCatalogModal"
    >
      <div role="dialog" aria-modal="true" class="bg-surface rounded-lg shadow-xl p-6 w-full max-w-3xl max-h-[90vh] overflow-y-auto">
        <h2 class="text-xl font-bold mb-4 text-purple-700">
          {{ t('admin.catalogizer_test_title') || 'Catalogize Product' }}
        </h2>

        <!-- Product info -->
        <div class="mb-4 bg-surface-2 rounded-lg p-3">
          <div class="text-sm font-medium text-ink-2">
            {{ catalogModal.eanpage.title || catalogModal.eanpage.ean }}
          </div>
          <div class="text-xs text-ink-3">EAN: {{ catalogModal.eanpage.ean }}</div>
        </div>

        <!-- Loading -->
        <div v-if="catalogModal.loading" class="flex justify-center py-8">
          <div class="animate-spin h-6 w-6 border-4 border-purple-600 border-t-transparent rounded-full"></div>
        </div>

        <!-- Results -->
        <div v-else-if="catalogModal.results">
          <!-- Tokens -->
          <div class="mb-3 text-sm text-ink-2">
            <span class="font-medium">{{ t('admin.catalogizer_tokens') || 'Tokens' }}:</span>
            {{ catalogModal.results.tokens?.join(', ') || '—' }}
          </div>

          <!-- No matches -->
          <div v-if="!catalogModal.results.matches || catalogModal.results.matches.length === 0" class="text-sm text-ink-3">
            {{ t('admin.catalogizer_no_matches') || 'No matches found. Add anchor keywords to relevant categories.' }}
          </div>

          <!-- Matches -->
          <div v-else class="space-y-4">
            <div
              v-for="m in catalogModal.results.matches"
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
                  <span class="text-xs text-ink-3">{{ t('admin.catalogizer_matched') || 'matched' }}: {{ (m.MatchedTokens||[]).join(', ') }}</span>
                </div>
              </div>

              <!-- Current anchor keywords -->
              <div class="mb-2">
                <div class="text-xs text-ink-3 mb-1">{{ t('admin.catalogizer_anchor_keywords') || 'Anchor keywords' }}:</div>
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
                  :placeholder="t('admin.catalogizer_add_token') || 'Add token'"
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

        <div class="mt-6 flex justify-end">
          <button
            @click="closeCatalogModal"
            class="px-4 py-2 text-sm bg-purple-600 text-white rounded-lg hover:bg-purple-700"
          >
            {{ t('common.close') || 'Close' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
