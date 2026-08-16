<script setup>
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from '../stores/auth';

const router = useRouter();
const auth = useAuthStore();
const { t } = useI18n();

const form = reactive({
  email: '',
  password: '',
  name: '',
  role: 'buyer',
});

const error = ref(null);

const register = async () => {
  if (!form.email || !form.password || !form.name) {
    error.value = t('auth.fill_required_fields');
    return;
  }
  if (form.password.length < 6) {
    error.value = t('auth.password_min_length');
    return;
  }
  error.value = null;
  try {
    await auth.register(form);
    router.push('/');
  } catch (e) {
    error.value = e.response?.data?.message || t('auth.register_error');
  }
};
</script>

<template>
  <div class="min-h-[60vh] flex items-center justify-center">
    <div class="w-full max-w-md bg-surface rounded-lg shadow-sm p-6">
      <h1 class="text-2xl font-bold mb-6 text-center">{{ t('auth.register') }}</h1>

      <div v-if="error" class="mb-4 p-3 bg-red-100 text-red-700 rounded-lg text-sm">
        {{ error }}
      </div>

      <form @submit.prevent="register" class="space-y-4">
        <div>
          <label class="block text-sm text-ink-2 mb-1">{{ t('common.name') }}</label>
          <input v-model="form.name" type="text" class="w-full px-3 py-2 border border-line rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-ink-2 mb-1">Email</label>
          <input v-model="form.email" type="email" class="w-full px-3 py-2 border border-line rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-ink-2 mb-1">{{ t('common.password') }}</label>
          <input v-model="form.password" type="password" class="w-full px-3 py-2 border border-line rounded-lg" required />
        </div>
        <div>
          <label class="block text-sm text-ink-2 mb-1">{{ t('common.role') }}</label>
          <select v-model="form.role" class="w-full px-3 py-2 border border-line rounded-lg">
            <option value="buyer">{{ t('auth.role_buyer') }}</option>
            <option value="seller">{{ t('auth.role_seller') }}</option>
          </select>
        </div>
        <button
          type="submit"
          :disabled="auth.loading"
          class="w-full btn btn-primary"
        >
          {{ auth.loading ? t('auth.registering') : t('auth.register_link') }}
        </button>
      </form>

      <p class="mt-4 text-center text-sm text-ink-2">
        {{ t('auth.have_account') }}
        <router-link to="/login" class="text-indigo-600 hover:underline">{{ t('auth.login_link') }}</router-link>
      </p>
    </div>
  </div>
</template>
