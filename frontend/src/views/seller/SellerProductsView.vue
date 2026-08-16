<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useAuthStore } from '../../stores/auth';
import { useFormat } from '../../composables/useFormat';
import { useToast } from '../../composables/useToast';
import EmptyState from '../../components/EmptyState.vue';
import ConfirmDialog from '../../components/ConfirmDialog.vue';

const router = useRouter();
const auth = useAuthStore();
const { t } = useI18n();
const { formatPrice } = useFormat();
const { toast } = useToast();
const products = ref([]);
const deleteProductId = ref(null);
const loading = ref(true);
const error = ref(null);

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
    error.value = t('seller.load_products_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const askDeleteProduct = (id) => {
  deleteProductId.value = id;
};

const deleteProduct = async (id) => {
  deleteProductId.value = null;
  try {
    await api.delete(`/products/${id}`);
    products.value = products.value.filter(p => p.id !== id);
    toast.success(t('seller.product_deleted') || t('seller.delete_error'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('seller.delete_error'));
  }
};

onMounted(fetchProducts);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">{{ t('seller.title') }}</h1>
      <router-link to="/seller/products/new" class="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">
        {{ t('seller.add_product') }}
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
    <div v-else-if="products.length === 0" class="bg-surface rounded-lg shadow-sm">
      <EmptyState icon="box" :title="t('seller.no_products')">
        <router-link to="/seller/products/new" class="btn btn-primary">{{ t('seller.add_first_product') }}</router-link>
      </EmptyState>
    </div>

    <!-- Products table -->
    <div v-else class="bg-surface rounded-lg shadow-sm overflow-hidden">
      <div class="overflow-x-auto">
      <table class="w-full text-sm min-w-[600px]">
        <caption class="sr-only">{{ t('tables.seller_products') }}</caption>
        <thead class="bg-surface-2">
          <tr>
            <th scope="col" class="px-4 py-3 text-left">ID</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.name') }}</th>
            <th scope="col" class="px-4 py-3 text-left">SKU</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.price') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('seller.status') }}</th>
            <th scope="col" class="px-4 py-3 text-right">{{ t('seller.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="product in products" :key="product.id" class="border-t hover:bg-surface-2">
            <td class="px-4 py-3">{{ product.id }}</td>
            <td class="px-4 py-3">{{ product.name }}</td>
            <td class="px-4 py-3 text-ink-3">{{ product.sku }}</td>
            <td class="px-4 py-3">{{ formatPrice(product.price) }}</td>
            <td class="px-4 py-3">
              <span :class="product.status === 'active' ? 'text-green-600' : 'text-ink-3'">
                {{ product.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right">
              <router-link :to="{ name: 'seller-product-edit', params: { id: product.id } }" class="text-indigo-600 hover:underline mr-3">
                {{ t('seller.edit') }}
              </router-link>
              <button @click="askDeleteProduct(product.id)" class="text-red-600 hover:underline">
                {{ t('seller.delete') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      </div>
    </div>

    <ConfirmDialog
      :open="deleteProductId !== null"
      :title="t('seller.title')"
      :message="t('seller.delete_confirm')"
      variant="danger"
      :confirm-text="t('seller.delete')"
      :cancel-text="t('common.cancel')"
      @confirm="deleteProduct(deleteProductId)"
      @cancel="deleteProductId = null"
    />
  </div>
</template>
