<script setup>
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useAuthStore } from '../stores/auth';

const router = useRouter();
const auth = useAuthStore();

const form = reactive({
  email: '',
  password: '',
  name: '',
  role: 'buyer',
});

const error = ref(null);

const register = async () => {
  if (!form.email || !form.password || !form.name) {
    error.value = 'Заполните все обязательные поля';
    return;
  }
  if (form.password.length < 6) {
    error.value = 'Пароль должен содержать минимум 6 символов';
    return;
  }
  error.value = null;
  try {
    await auth.register(form);
    router.push('/');
  } catch (e) {
    error.value = e.response?.data?.message || 'Ошибка регистрации';
  }
};
</script>

<template>
  <div class="min-h-[60vh] flex items-center justify-center">
    <div class="w-full max-w-md bg-white rounded-lg shadow-sm p-6">
      <h1 class="text-2xl font-bold mb-6 text-center">Регистрация</h1>

      <div v-if="error" class="mb-4 p-3 bg-red-100 text-red-700 rounded-lg text-sm">
        {{ error }}
      </div>

      <form @submit.prevent="register" class="space-y-4">
        <div>
          <label class="block text-sm text-gray-700 mb-1">Имя</label>
          <input v-model="form.name" type="text" class="w-full px-3 py-2 border border-gray-300 rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-gray-700 mb-1">Email</label>
          <input v-model="form.email" type="email" class="w-full px-3 py-2 border border-gray-300 rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-gray-700 mb-1">Пароль</label>
          <input v-model="form.password" type="password" class="w-full px-3 py-2 border border-gray-300 rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-gray-700 mb-1">Роль</label>
          <select v-model="form.role" class="w-full px-3 py-2 border border-gray-300 rounded-lg">
            <option value="buyer">Покупатель</option>
            <option value="seller">Продавец</option>
          </select>
        </div>
        <button
          type="submit"
          :disabled="auth.loading"
          class="w-full px-4 py-2.5 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 disabled:opacity-40"
        >
          {{ auth.loading ? 'Регистрация...' : 'Зарегистрироваться' }}
        </button>
      </form>

      <p class="mt-4 text-center text-sm text-gray-600">
        Уже есть аккаунт?
        <router-link to="/login" class="text-indigo-600 hover:underline">Войти</router-link>
      </p>
    </div>
  </div>
</template>
