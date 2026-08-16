<script setup>
import { onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useCartStore } from '../stores/cart';
import { useFormat } from '../composables/useFormat';
import { useToast } from '../composables/useToast';

const router = useRouter();
const cart = useCartStore();
const { t } = useI18n();
const { formatPrice } = useFormat();
const { toast } = useToast();

const handleRemoveItem = async (item) => {
  const productId = item.product_id;
  const qty = item.qty || 1;
  const ok = await cart.removeItem(productId);
  if (ok) {
    toast.info(t('cart.item_removed'), 6000, {
      actionLabel: t('common.undo'),
      onAction: () => cart.restoreItem(productId, qty),
    });
  }
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
    <h1 class="text-2xl font-bold mb-6">{{ t('cart.title') }}</h1>

    <!-- Loading -->
    <div v-if="cart.loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Empty -->
    <div v-else-if="cart.items.length === 0" class="text-center py-12 bg-surface rounded-lg shadow-sm">
      <svg xmlns="http://www.w3.org/2000/svg" class="h-16 w-16 mx-auto text-ink-3 mb-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z" />
      </svg>
      <p class="text-ink-3 mb-4">{{ t('cart.empty') }}</p>
      <router-link to="/" class="inline-block px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm hover:bg-indigo-700">
        {{ t('cart.go_to_catalog') }}
      </router-link>
    </div>

    <!-- Cart items -->
    <div v-else>
      <div class="bg-surface rounded-lg shadow-sm overflow-hidden">
        <div v-for="item in cart.items" :key="item.product_id" class="flex flex-wrap items-center gap-3 sm:gap-4 p-4 border-b last:border-b-0">
          <!-- Image -->
          <div class="w-20 h-20 bg-surface-2 rounded-lg overflow-hidden flex-shrink-0">
            <img
              v-if="item.images?.length"
              :src="item.images[0]"
              :alt="item.product_name || item.name"
              loading="lazy"
              decoding="async"
              class="w-full h-full object-cover"
            />
            <div v-else class="w-full h-full flex items-center justify-center text-ink-3 text-xs">
              {{ t('common.no_photo') }}
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
            <div class="text-sm text-ink-3">{{ formatPrice(item.price) }}</div>
          </div>

          <!-- Qty controls -->
          <div class="flex items-center gap-1">
            <button
              @click="cart.updateItem(item.product_id, item.qty - 1)"
              class="w-8 h-8 flex items-center justify-center border rounded-lg hover:bg-surface-2 text-sm"
            >
              −
            </button>
            <span class="w-8 text-center text-sm">{{ item.qty }}</span>
            <button
              @click="cart.updateItem(item.product_id, item.qty + 1)"
              class="w-8 h-8 flex items-center justify-center border rounded-lg hover:bg-surface-2 text-sm"
            >
              +
            </button>
          </div>

          <!-- Subtotal -->
          <div class="w-24 text-right font-medium text-sm ml-auto sm:ml-0">
            {{ formatPrice(item.price * item.qty) }}
          </div>

          <!-- Remove -->
          <button
            @click="handleRemoveItem(item)"
            class="text-ink-3 hover:text-red-600 p-1 flex-shrink-0"
            :aria-label="t('cart.remove_item')"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      </div>

      <!-- Summary -->
      <div class="mt-6 bg-surface rounded-lg shadow-sm p-4 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div>
          <div class="text-sm text-ink-3">{{ t('cart.items_count', { count: cart.totalCount }) }}</div>
          <div class="text-xl font-bold text-indigo-600">{{ formatPrice(cart.totalPrice) }}</div>
        </div>
        <button
          @click="goToCheckout"
          class="px-6 py-3 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 transition whitespace-nowrap"
        >
          {{ t('cart.checkout') }}
        </button>
      </div>
    </div>
  </div>
</template>
