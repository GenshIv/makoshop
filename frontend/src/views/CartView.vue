<script setup>
import { onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useCartStore } from '../stores/cart';

const router = useRouter();
const cart = useCartStore();

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const goToCheckout = () => {
  if (cart.items.length === 0) {
    router.push({ name: 'catalog' });
    return;
  }
  router.push({ name: 'checkout' });
};

onMounted(() => {
  cart.fetchCart();
});
</script>

<template>
  <div class="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">Корзина</h1>

    <!-- Loading -->
    <div v-if="cart.loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Empty -->
    <div v-else-if="cart.items.length === 0" class="text-center py-12 bg-white rounded-lg shadow-sm">
      <svg xmlns="http://www.w3.org/2000/svg" class="h-16 w-16 mx-auto text-gray-300 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z" />
      </svg>
      <p class="text-gray-500 mb-4">Корзина пуста</p>
      <router-link to="/" class="inline-block px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm hover:bg-indigo-700">
        Перейти в каталог
      </router-link>
    </div>

    <!-- Cart items -->
    <div v-else>
      <div class="bg-white rounded-lg shadow-sm overflow-hidden">
        <div v-for="item in cart.items" :key="item.product_id" class="flex items-center gap-4 p-4 border-b last:border-b-0">
          <!-- Image -->
          <div class="w-20 h-20 bg-gray-100 rounded-lg overflow-hidden flex-shrink-0">
            <img
              v-if="item.images?.length"
              :src="item.images[0]"
              :alt="item.product_name || item.name"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center text-gray-400 text-xs">
              Нет фото
            </div>
          </div>

          <!-- Info -->
          <div class="flex-1 min-w-0">
            <router-link
              :to="{ name: 'product', params: { id: item.product_id } }"
              class="font-medium hover:text-indigo-600 truncate block"
            >
              {{ item.product_name || item.name }}
            </router-link>
            <div class="text-sm text-gray-500">{{ formatPrice(item.price) }}</div>
          </div>

          <!-- Qty controls -->
          <div class="flex items-center gap-1">
            <button
              @click="cart.updateItem(item.product_id, item.qty - 1)"
              class="w-8 h-8 flex items-center justify-center border rounded-lg hover:bg-gray-50 text-sm"
            >
              −
            </button>
            <span class="w-8 text-center text-sm">{{ item.qty }}</span>
            <button
              @click="cart.updateItem(item.product_id, item.qty + 1)"
              class="w-8 h-8 flex items-center justify-center border rounded-lg hover:bg-gray-50 text-sm"
            >
              +
            </button>
          </div>

          <!-- Subtotal -->
          <div class="w-24 text-right font-medium text-sm">
            {{ formatPrice(item.price * item.qty) }}
          </div>

          <!-- Remove -->
          <button
            @click="cart.removeItem(item.product_id)"
            class="text-gray-400 hover:text-red-600 p-1"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      </div>

      <!-- Summary -->
      <div class="mt-6 bg-white rounded-lg shadow-sm p-4 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div>
          <div class="text-sm text-gray-500">{{ cart.totalCount }} товаров</div>
          <div class="text-xl font-bold text-indigo-600">{{ formatPrice(cart.totalPrice) }}</div>
        </div>
        <button
          @click="goToCheckout"
          class="px-6 py-3 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 transition whitespace-nowrap"
        >
          Оформить заказ
        </button>
      </div>
    </div>
  </div>
</template>
