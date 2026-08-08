<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';

const router = useRouter();
const auth = useAuthStore();
const products = ref([]);
const loading = ref(true);
const error = ref(null);

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const fetchProducts = async () => {
  loading.value = true;
  error.value = null;
  try {
    const response = await api.get('/products', {
      params: { limit: 100 },
    });
    const items = response.data.items || [];
    // Filter by company_id from user profile
    const companyId = auth.user?.profile?.company_id;
    if (companyId) {
      products.value = items.filter(p => p.company_id === companyId);
    } else {
      products.value = items;
    }
  } catch (e) {
    error.value = 'Ошибка загрузки товаров';
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const deleteProduct = async (id) => {
  if (!confirm('Удалить товар?')) return;
  try {
    await api.delete(`/products/${id}`);
    products.value = products.value.filter(p => p.id !== id);
  } catch (e) {
    alert(e.response?.data?.message || 'Ошибка удаления');
  }
};

onMounted(fetchProducts);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Товары</h1>
      <router-link to="/seller/products/new" class="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">
        + Добавить товар
      </router-link>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Empty -->
    <div v-else-if="products.length === 0" class="text-center py-12 text-gray-500">
      У вас пока нет товаров
      <router-link to="/seller/products/new" class="block mt-2 text-indigo-600 hover:underline">
        Добавить первый товар
      </router-link>
    </div>

    <!-- Products table -->
    <div v-else class="bg-white rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-4 py-3 text-left">ID</th>
            <th class="px-4 py-3 text-left">Название</th>
            <th class="px-4 py-3 text-left">SKU</th>
            <th class="px-4 py-3 text-left">Цена</th>
            <th class="px-4 py-3 text-left">Статус</th>
            <th class="px-4 py-3 text-right">Действия</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="product in products" :key="product.id" class="border-t hover:bg-gray-50">
            <td class="px-4 py-3">{{ product.id }}</td>
            <td class="px-4 py-3">{{ product.name }}</td>
            <td class="px-4 py-3 text-gray-500">{{ product.sku }}</td>
            <td class="px-4 py-3">{{ formatPrice(product.price) }}</td>
            <td class="px-4 py-3">
              <span :class="product.status === 'active' ? 'text-green-600' : 'text-gray-500'">
                {{ product.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right">
              <router-link :to="{ name: 'seller-product-edit', params: { id: product.id } }" class="text-indigo-600 hover:underline mr-3">
                Изменить
              </router-link>
              <button @click="deleteProduct(product.id)" class="text-red-600 hover:underline">
                Удалить
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
