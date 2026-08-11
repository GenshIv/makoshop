<script setup>
import { ref, onMounted, watch } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();
const router = useRouter();
const route = useRoute();

const scupages = ref([]);
const total = ref(0);
const page = ref(1);
const limit = ref(50);
const searchQuery = ref('');
const loading = ref(false);
const editing = ref(null); // { id, data }
const deleteConfirm = ref(null);

const fetchSCUPages = async () => {
  loading.value = true;
  try {
    const params = { page: page.value, limit: limit.value };
    if (searchQuery.value) params.q = searchQuery.value;
    const res = await api.get('/admin/scupages', { params });
    scupages.value = res.data.items || [];
    total.value = res.data.total || 0;
  } catch (e) {
    console.error('Failed to fetch SCU pages:', e);
  } finally {
    loading.value = false;
  }
};

const startEdit = (sp) => {
  editing.value = {
    id: sp.id,
    data: {
      title: sp.title || '',
      description: sp.description || '',
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
    await api.patch(`/admin/scupages/${editing.value.id}`, editing.value.data);
    editing.value = null;
    await fetchSCUPages();
  } catch (e) {
    console.error('Failed to update SCU page:', e);
    alert(t('admin.scupage_save_error') || 'Failed to save');
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
    await api.delete(`/admin/scupages/${deleteConfirm.value.id}`);
    deleteConfirm.value = null;
    await fetchSCUPages();
  } catch (e) {
    console.error('Failed to delete SCU page:', e);
    alert(t('admin.scupage_delete_error') || 'Failed to delete');
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
    fetchSCUPages();
  }, 400);
});

onMounted(fetchSCUPages);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.scupage_title') || 'SCU Pages' }}</h1>
      <router-link
        to="/admin"
        class="text-sm text-gray-500 hover:text-purple-600"
      >
        {{ t('admin.back_to_dashboard') || 'Back to Dashboard' }}
      </router-link>
    </div>

    <!-- Search -->
    <div class="mb-4">
      <input
        v-model="searchQuery"
        type="text"
        class="w-full max-w-md px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
        :placeholder="t('admin.scupage_search_placeholder') || 'Search by SCU, title...'"
      />
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- List -->
    <div v-else>
      <div class="bg-white rounded-lg shadow-sm overflow-hidden">
        <table class="w-full text-sm">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_id') || 'ID' }}</th>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_scu') || 'SCU' }}</th>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_title') || 'Title' }}</th>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_slug') || 'Slug' }}</th>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_products') || 'Products' }}</th>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_active') || 'Active' }}</th>
              <th class="px-4 py-2 text-left">{{ t('admin.scupage_actions') || 'Actions' }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="sp in scupages"
              :key="sp.id"
              class="border-t hover:bg-gray-50"
            >
              <td class="px-4 py-2">{{ sp.id }}</td>
              <td class="px-4 py-2 max-w-xs truncate" :title="sp.scu">{{ sp.scu }}</td>
              <td class="px-4 py-2 max-w-xs truncate" :title="sp.title">{{ sp.title }}</td>
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
                  @click="startEdit(sp)"
                  class="text-blue-600 hover:text-blue-800 text-xs"
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
            <tr v-if="scupages.length === 0">
              <td colspan="7" class="px-4 py-8 text-center text-gray-500">
                {{ t('admin.scupage_no_results') || 'No SCU pages found' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Pagination -->
      <div class="mt-4 flex justify-between items-center text-sm text-gray-600">
        <div>
          {{ t('admin.scupage_showing') || 'Showing' }} {{ total }}
        </div>
        <div class="space-x-1">
          <button
            @click="goToPage(page - 1)"
            :disabled="page <= 1"
            class="px-3 py-1 border rounded disabled:opacity-50 hover:bg-gray-50"
          >
            &laquo;
          </button>
          <span class="px-3 py-1">{{ page }} / {{ Math.ceil(total / limit) || 1 }}</span>
          <button
            @click="goToPage(page + 1)"
            :disabled="page >= Math.ceil(total / limit)"
            class="px-3 py-1 border rounded disabled:opacity-50 hover:bg-gray-50"
          >
            &raquo;
          </button>
        </div>
      </div>
    </div>

    <!-- Edit Modal -->
    <div
      v-if="editing"
      class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      @click.self="cancelEdit"
    >
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-2xl max-h-[90vh] overflow-y-auto">
        <h2 class="text-xl font-bold mb-4 text-purple-700">
          {{ t('admin.scupage_edit_title') || 'Edit SCU Page' }} #{{ editing.id }}
        </h2>

        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              {{ t('admin.scupage_title') || 'Title' }}
            </label>
            <input
              v-model="editing.data.title"
              type="text"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              {{ t('admin.scupage_description') || 'Description' }}
            </label>
            <textarea
              v-model="editing.data.description"
              rows="3"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            ></textarea>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              {{ t('admin.scupage_slug') || 'Slug' }}
            </label>
            <input
              v-model="editing.data.slug"
              type="text"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              {{ t('admin.scupage_category_id') || 'Category ID' }}
            </label>
            <input
              v-model.number="editing.data.category_id"
              type="number"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label class="flex items-center space-x-2">
              <input
                v-model="editing.data.is_active"
                type="checkbox"
                class="rounded border-gray-300 text-purple-600 focus:ring-purple-500"
              />
              <span class="text-sm font-medium text-gray-700">
                {{ t('admin.scupage_active') || 'Active' }}
              </span>
            </label>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">
              {{ t('admin.scupage_content') || 'Content (HTML)' }}
            </label>
            <textarea
              v-model="editing.data.content"
              rows="6"
              class="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 font-mono text-xs"
            ></textarea>
          </div>
        </div>

        <div class="mt-6 flex justify-end space-x-2">
          <button
            @click="cancelEdit"
            class="px-4 py-2 text-sm text-gray-600 hover:text-gray-800"
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
      class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      @click.self="cancelDelete"
    >
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-md">
        <h2 class="text-xl font-bold mb-4 text-red-600">
          {{ t('admin.scupage_delete_confirm_title') || 'Delete SCU Page?' }}
        </h2>
        <p class="text-sm text-gray-600 mb-2">
          {{ t('admin.scupage_delete_confirm_msg') || 'Are you sure you want to delete this SCU page?' }}
        </p>
        <p class="text-xs text-gray-500 mb-4">
          SCU: {{ deleteConfirm.scu }}<br>
          Title: {{ deleteConfirm.title }}
        </p>
        <div class="flex justify-end space-x-2">
          <button
            @click="cancelDelete"
            class="px-4 py-2 text-sm text-gray-600 hover:text-gray-800"
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
  </div>
</template>
