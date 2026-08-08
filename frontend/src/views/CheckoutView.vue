<script setup>
import { reactive, ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import api from '../api';
import { useCartStore } from '../stores/cart';
import { useAuthStore } from '../stores/auth';

const router = useRouter();
const cart = useCartStore();
const auth = useAuthStore();

const shipping = reactive({
  name: '',
  phone: '',
  email: '',
  address: '',
  city: '',
  zip: '',
  comment: '',
});

const submitting = ref(false);
const error = ref(null);
const success = ref(false);
const orderId = ref(null);

const formatPrice = (price) => {
  return new Intl.NumberFormat('ru-RU', { style: 'currency', currency: 'RUB' }).format(price);
};

const submitOrder = async () => {
  if (!shipping.name || !shipping.phone || !shipping.address || !shipping.city) {
    alert('Заполните все обязательные поля');
    return;
  }

  submitting.value = true;
  error.value = null;

  try {
    const orderResponse = await api.post('/orders', {
      cart_id: cart.cartId,
      shipping_info: {
        name: shipping.name,
        phone: shipping.phone,
        email: shipping.email,
        address: shipping.address,
        city: shipping.city,
        zip: shipping.zip,
      },
      comment: shipping.comment || undefined,
    });

    orderId.value = orderResponse.data.id;

    // Clear cart
    cart.clear();

    success.value = true;
  } catch (e) {
    error.value = e.response?.data?.message || 'Ошибка оформления заказа';
    console.error(e);
  } finally {
    submitting.value = false;
  }
};

onMounted(async () => {
  await cart.fetchCart();
  if (cart.items.length === 0) {
    router.push({ name: 'cart' });
    return;
  }
  if (auth.user) {
    shipping.name = auth.user.name || '';
    shipping.email = auth.user.email || '';
    shipping.phone = auth.user.phone || '';
  }
});
</script>

<template>
  <div class="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">Оформление заказа</h1>

    <!-- Success -->
    <div v-if="success" class="bg-green-50 border border-green-200 rounded-lg p-6 text-center">
      <div class="text-green-700 font-medium text-lg mb-2">Заказ успешно создан!</div>
      <div class="text-green-600 text-sm mb-4">Номер заказа: #{{ orderId }}</div>
      <div class="flex justify-center gap-3">
        <router-link :to="{ name: 'order-detail', params: { id: orderId } }" class="px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm hover:bg-indigo-700">
          Посмотреть заказ
        </router-link>
        <router-link to="/" class="px-4 py-2 border border-gray-300 rounded-lg text-sm hover:bg-gray-50">
          В каталог
        </router-link>
      </div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="mb-4 p-3 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Form -->
    <div v-else class="grid grid-cols-1 md:grid-cols-2 gap-6">
      <!-- Shipping form -->
      <div class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-4">Данные получателя</h2>
        <div class="space-y-3">
          <div>
            <label class="block text-sm text-gray-700 mb-1">Имя *</label>
            <input v-model="shipping.name" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" required />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Телефон *</label>
            <input v-model="shipping.phone" type="tel" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" required />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Email</label>
            <input v-model="shipping.email" type="email" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Город *</label>
            <input v-model="shipping.city" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" required />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Адрес *</label>
            <input v-model="shipping.address" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" required />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Индекс</label>
            <input v-model="shipping.zip" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm" />
          </div>
          <div>
            <label class="block text-sm text-gray-700 mb-1">Комментарий к заказу</label>
            <textarea v-model="shipping.comment" rows="2" class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"></textarea>
          </div>
        </div>
      </div>

      <!-- Order summary -->
      <div class="bg-white rounded-lg shadow-sm p-4">
        <h2 class="font-bold mb-4">Ваш заказ</h2>
        <div v-if="cart.loading" class="text-sm text-gray-500">Загрузка...</div>
        <div v-else>
          <div v-for="item in cart.items" :key="item.product_id" class="flex justify-between text-sm py-2 border-b last:border-b-0">
            <div>
              <span class="font-medium">{{ item.product_name || item.name }}</span>
              <span class="text-gray-500 ml-1">× {{ item.qty }}</span>
            </div>
            <span>{{ formatPrice(item.price * item.qty) }}</span>
          </div>
          <div class="flex justify-between font-bold mt-4 pt-2 text-lg">
            <span>Итого:</span>
            <span class="text-indigo-600">{{ formatPrice(cart.totalPrice) }}</span>
          </div>
        </div>

        <button
          @click="submitOrder"
          :disabled="submitting || cart.loading"
          class="mt-6 w-full px-4 py-3 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 disabled:opacity-40 transition"
        >
          {{ submitting ? 'Создание заказа...' : 'Подтвердить заказ' }}
        </button>
      </div>
    </div>
  </div>
</template>
