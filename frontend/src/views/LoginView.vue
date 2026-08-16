<script setup>
import { reactive, ref } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from '../stores/auth';

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();
const { t } = useI18n();

const form = reactive({ email: '', password: '' });
const error = ref(null);

const login = async () => {
  if (!form.email || !form.password) {
    error.value = t('auth.fill_all_fields');
    return;
  }
  error.value = null;
  try {
    await auth.login(form.email, form.password);
    const redirect = route.query.redirect || '/';
    router.push(redirect);
  } catch (e) {
    error.value = e.response?.data?.message || t('auth.login_error');
  }
};
</script>

<template>
  <div class="min-h-[60vh] flex items-center justify-center">
    <div class="w-full max-w-md bg-surface rounded-lg shadow-sm p-6">
      <h1 class="text-2xl font-bold mb-6 text-center">{{ t('auth.login') }}</h1>

      <div v-if="error" class="mb-4 p-3 bg-red-100 text-red-700 rounded-lg text-sm">
        {{ error }}
      </div>

      <form @submit.prevent="login" class="space-y-4">
        <div>
          <label class="block text-sm text-ink-2 mb-1">Email</label>
          <input v-model="form.email" type="email" class="w-full px-3 py-2 border border-line rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-ink-2 mb-1">{{ t('common.password') }}</label>
          <input v-model="form.password" type="password" class="w-full px-3 py-2 border border-line rounded-lg" required />
        </div>
        <button
          type="submit"
          :disabled="auth.loading"
          class="w-full px-4 py-2.5 bg-indigo-600 text-white rounded-lg font-medium hover:bg-indigo-700 disabled:opacity-40"
        >
          {{ auth.loading ? t('auth.logging_in') : t('auth.login') }}
        </button>
      </form>

      <p class="mt-4 text-center text-sm text-ink-2">
        {{ t('auth.no_account') }}
        <router-link to="/register" class="text-indigo-600 hover:underline">{{ t('auth.register_link') }}</router-link>
      </p>
    </div>
  </div>
</template>
