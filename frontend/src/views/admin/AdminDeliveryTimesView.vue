<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';

const { t } = useI18n();
const { toast } = useToast();

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
    const res = await api.get('/admin/delivery-times');
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
    toast.error(t('admin.name_required'));
    return;
  }
  try {
    if (editingId.value) {
      await api.patch(`/admin/delivery-times/${editingId.value}`, form.value);
    } else {
      await api.post('/admin/delivery-times', form.value);
    }
    resetForm();
    await fetchItems();
  } catch (e) {
    toast.error(e.response?.data?.message || 'Save error');
  }
};

const removeItem = ref(null);

const askRemove = (item) => {
  removeItem.value = item;
};

const remove = async (item) => {
  removeItem.value = null;
  try {
    await api.delete(`/admin/delivery-times/${item.id}`);
    await fetchItems();
  } catch (e) {
    toast.error(e.response?.data?.message || 'Delete error');
  }
};

onMounted(fetchItems);
</script>

<template>
  <div class="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.delivery_times_title') }}</h1>
      <button @click="openCreate" class="px-3 py-1.5 text-sm rounded-md bg-purple-600 text-white hover:bg-purple-700">
        {{ t('admin.add') }}
      </button>
    </div>

    <div v-if="error" class="mb-3 text-sm text-red-600">{{ error }}</div>

    <!-- Form -->
    <div v-if="showForm" class="mb-4 p-3 bg-surface-2 rounded-lg border border-line">
      <div class="text-sm font-medium mb-2">
        {{ editingId ? t('admin.edit') : t('admin.create') }}
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 mb-2">
        <div>
          <label class="sr-only" for="dt-name">{{ t('common.name') }}</label>
          <input id="dt-name" v-model="form.name" :placeholder="t('common.name')" class="px-2 py-1.5 text-sm border rounded w-full" />
        </div>
        <div>
          <label class="sr-only" for="dt-slug">{{ t('common.slug') }}</label>
          <input id="dt-slug" v-model="form.slug" :placeholder="t('common.slug')" class="px-2 py-1.5 text-sm border rounded w-full" />
        </div>
        <div class="flex items-center gap-2">
          <input id="dt-active" v-model="form.is_active" type="checkbox" class="h-4 w-4" />
          <label for="dt-active" class="text-sm">{{ t('admin.active') }}</label>
        </div>
        <div>
          <label class="sr-only" for="dt-sort">{{ t('admin.sort_order') }}</label>
          <input id="dt-sort" v-model.number="form.sort_order" type="number" :placeholder="t('admin.sort_order')" class="px-2 py-1.5 text-sm border rounded w-full" />
        </div>
      </div>
      <div class="flex gap-2">
        <button @click="save" class="px-3 py-1.5 text-sm rounded-md bg-purple-600 text-white hover:bg-purple-700">
          {{ t('admin.save') }}
        </button>
        <button @click="resetForm" class="px-3 py-1.5 text-sm rounded-md border border-line bg-surface hover:bg-surface-2">
          {{ t('admin.cancel') }}
        </button>
      </div>
    </div>

    <!-- List -->
    <div v-if="loading" class="text-sm text-ink-3">{{ t('common.loading') }}</div>
    <table v-else class="min-w-full text-sm">
      <thead class="border-b">
        <tr>
          <th scope="col" class="text-left py-2 px-2">{{ t('common.name') }}</th>
          <th scope="col" class="text-left py-2 px-2">{{ t('common.slug') }}</th>
          <th scope="col" class="text-left py-2 px-2">{{ t('admin.active') }}</th>
          <th scope="col" class="text-left py-2 px-2">{{ t('admin.order') }}</th>
          <th scope="col" class="text-left py-2 px-2">{{ t('admin.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.id" class="border-b">
          <td class="py-2 px-2">{{ item.name }}</td>
          <td class="py-2 px-2 text-ink-3">{{ item.slug }}</td>
          <td class="py-2 px-2">{{ item.is_active ? t('common.yes') : t('common.no') }}</td>
          <td class="py-2 px-2">{{ item.sort_order }}</td>
          <td class="py-2 px-2 flex gap-2">
            <button @click="openEdit(item)" class="text-xs text-purple-700 hover:underline">{{ t('admin.edit') }}</button>
            <button @click="askRemove(item)" class="text-xs text-red-600 hover:underline">{{ t('admin.delete') }}</button>
          </td>
        </tr>
        <tr v-if="items.length === 0">
          <td colspan="5" class="py-4 text-sm text-ink-3">{{ t('admin.no_items') }}</td>
        </tr>
      </tbody>
    </table>

    <ConfirmDialog
      :open="removeItem !== null"
      :title="t('admin.delivery_times_title')"
      :message="removeItem ? t('admin.delete_item_confirm', { name: removeItem.name }) : ''"
      variant="danger"
      :confirm-text="t('admin.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="remove(removeItem)"
      @cancel="removeItem = null"
    />
  </div>
</template>
