<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

const items = ref([]);
const loading = ref(false);
const error = ref('');

const form = ref({
  name: '',
  slug: '',
  is_active: true,
  sort_order: 999,
});
const editingId = ref(null);
const showForm = ref(false);

const fetchItems = async () => {
  loading.value = true;
  error.value = '';
  try {
    const res = await api.get('/admin/installment-plans');
    items.value = res.data || [];
  } catch (e) {
    error.value = e.response?.data?.message || e.message;
  } finally {
    loading.value = false;
  }
};

const resetForm = () => {
  form.value = { name: '', slug: '', is_active: true, sort_order: 999 };
  editingId.value = null;
  showForm.value = false;
};

const openCreate = () => {
  resetForm();
  showForm.value = true;
};

const openEdit = (item) => {
  editingId.value = item.id;
  form.value = {
    name: item.name || '',
    slug: item.slug || '',
    is_active: item.is_active !== false,
    sort_order: item.sort_order || 999,
  };
  showForm.value = true;
};

const save = async () => {
  if (!form.value.name) {
    alert(t('admin.name_required'));
    return;
  }
  try {
    if (editingId.value) {
      await api.patch(`/admin/installment-plans/${editingId.value}`, form.value);
    } else {
      await api.post('/admin/installment-plans', form.value);
    }
    resetForm();
    await fetchItems();
  } catch (e) {
    alert(e.response?.data?.message || 'Save error');
  }
};

const remove = async (item) => {
  if (!confirm(`Delete "${item.name}"?`)) return;
  try {
    await api.delete(`/admin/installment-plans/${item.id}`);
    await fetchItems();
  } catch (e) {
    alert(e.response?.data?.message || 'Delete error');
  }
};

onMounted(fetchItems);
</script>

<template>
  <div class="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.installment_plans_title') }}</h1>
      <button @click="openCreate" class="px-3 py-1.5 text-sm rounded-md bg-purple-600 text-white hover:bg-purple-700">
        {{ t('admin.add') }}
      </button>
    </div>

    <div v-if="error" class="mb-3 text-sm text-red-600">{{ error }}</div>

    <!-- Form -->
    <div v-if="showForm" class="mb-4 p-3 bg-gray-50 rounded-lg border border-gray-200">
      <div class="text-sm font-medium mb-2">
        {{ editingId ? t('admin.edit') : t('admin.create') }}
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 mb-2">
        <input v-model="form.name" placeholder="Name" class="px-2 py-1.5 text-sm border rounded" />
        <input v-model="form.slug" placeholder="Slug" class="px-2 py-1.5 text-sm border rounded" />
        <div class="flex items-center gap-2">
          <input v-model="form.is_active" type="checkbox" class="h-4 w-4" />
          <label class="text-sm">{{ t('admin.active') }}</label>
        </div>
        <input v-model.number="form.sort_order" type="number" placeholder="Sort order" class="px-2 py-1.5 text-sm border rounded" />
      </div>
      <div class="flex gap-2">
        <button @click="save" class="px-3 py-1.5 text-sm rounded-md bg-purple-600 text-white hover:bg-purple-700">
          {{ t('admin.save') }}
        </button>
        <button @click="resetForm" class="px-3 py-1.5 text-sm rounded-md border border-gray-300 bg-white hover:bg-gray-50">
          {{ t('admin.cancel') }}
        </button>
      </div>
    </div>

    <!-- List -->
    <div v-if="loading" class="text-sm text-gray-500">Loading...</div>
    <table v-else class="min-w-full text-sm">
      <thead class="border-b">
        <tr>
          <th class="text-left py-2 px-2">Name</th>
          <th class="text-left py-2 px-2">Slug</th>
          <th class="text-left py-2 px-2">Active</th>
          <th class="text-left py-2 px-2">Order</th>
          <th class="text-left py-2 px-2">Actions</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.id" class="border-b">
          <td class="py-2 px-2">{{ item.name }}</td>
          <td class="py-2 px-2 text-gray-500">{{ item.slug }}</td>
          <td class="py-2 px-2">{{ item.is_active ? 'Yes' : 'No' }}</td>
          <td class="py-2 px-2">{{ item.sort_order }}</td>
          <td class="py-2 px-2 flex gap-2">
            <button @click="openEdit(item)" class="text-xs text-purple-700 hover:underline">Edit</button>
            <button @click="remove(item)" class="text-xs text-red-600 hover:underline">Delete</button>
          </td>
        </tr>
        <tr v-if="items.length === 0">
          <td colspan="5" class="py-4 text-sm text-gray-500">No items yet.</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
