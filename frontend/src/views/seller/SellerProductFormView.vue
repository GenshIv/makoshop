<script setup>
import { reactive, ref, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();

const form = reactive({
  sku: '',
  name: '',
  description: '',
  category_id: 0,
  price: 0,
  currency: 'RUB',
  stock_qty: 0,
  status: 'active',
});

const loading = ref(false);
const submitting = ref(false);
const error = ref(null);

const isEdit = () => !!route.params.id;

const fetchProduct = async () => {
  loading.value = true;
  try {
    const response = await api.get(`/products/${route.params.id}`);
    const p = response.data;
    Object.assign(form, {
      sku: p.sku || '',
      name: p.name || '',
      description: p.description || '',
      category_id: p.category_id || 0,
      price: p.price || 0,
      currency: p.currency || 'RUB',
      stock_qty: p.stock_qty || 0,
      status: p.status || 'active',
    });
  } catch (e) {
    error.value = 'Товар не найден';
  } finally {
    loading.value = false;
  }
};

const submit = async () => {
  if (!form.name || !form.sku || !form.price) {
    error.value = 'Заполните обязательные поля';
    return;
  }
  submitting.value = true;
  error.value = null;
  try {
    const payload = { ...form, company_id: auth.user?.profile?.company_id || 0 };
    if (isEdit()) {
      await api.patch(`/products/${route.params.id}`, payload);
    } else {
      await api.post('/products', payload);
    }
    router.push({ name: 'seller-products' });
  } catch (e) {
    error.value = e.response?.data?.message || 'Ошибка сохранения';
  } finally {
    submitting.value = false;
  }
};

onMounted(() => {
  if (isEdit()) fetchProduct();
});
</script>

<template>
  <div class="max-w-3xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">{{ isEdit() ? 'Редактировать товар' : 'Новый товар' }}</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else>
      <!-- Error -->
      <div v-if="error" class="mb-4 p-3 bg-red-100 text-red-700 rounded-lg">
        {{ error }}
      </div>

      <form @submit.prevent="submit" class="bg-white rounded-lg shadow-sm p-6 space-y-4">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label class="block text-sm text-gray-700 mb-1">SKU *</label>
            <input v-model="form.sku" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg" required />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Цена *</label>
            <input v-model.number="form.price" type="number" min="0" class="w-full px-3 py-2 border border-gray-300 rounded-lg" required />
          </div>
        </div>

        <div>
          <label class="block text-sm text-gray-700 mb-1">Название *</label>
          <input v-model="form.name" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg" required />
        </div>

        <div>
          <label class="block text-sm text-gray-700 mb-1">Описание</label>
          <textarea v-model="form.description" rows="4" class="w-full px-3 py-2 border border-gray-300 rounded-lg"></textarea>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
          <div>
            <label class="block text-sm text-gray-700 mb-1">Категория (ID)</label>
            <input v-model.number="form.category_id" type="number" class="w-full px-3 py-2 border border-gray-300 rounded-lg" />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Остаток</label>
            <input v-model.number="form.stock_qty" type="number" min="0" class="w-full px-3 py-2 border border-gray-300 rounded-lg" />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">{{ t('seller.status') }}</label>
            <select v-model="form.status" class="w-full px-3 py-2 border border-gray-300 rounded-lg">
              <option value="active">{{ t('seller.status_active') }}</option>
              <option value="draft">{{ t('seller.status_draft') }}</option>
              <option value="hidden">{{ t('seller.status_hidden') }}</option>
            </select>
          </div>
        </div>

        <div class="flex gap-3 pt-4">
          <button type="submit" :disabled="submitting" class="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 disabled:opacity-40">
            {{ submitting ? t('seller.saving') : t('seller.save') }}
          </button>
          <router-link to="/seller/products" class="px-4 py-2 border rounded-lg hover:bg-gray-50">
            {{ t('seller.cancel') }}
          </router-link>
        </div>
      </form>
    </div>
  </div>
</template>
