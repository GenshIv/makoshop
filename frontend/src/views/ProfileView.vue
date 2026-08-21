<script setup>
import { reactive, ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from '../stores/auth';

const auth = useAuthStore();
const { t } = useI18n();
const editing = ref(false);
const saving = ref(false);
const error = ref(null);
const success = ref(null);

const form = reactive({
  name: '',
  email: '',
  phone: '',
});

const loadProfile = async () => {
  await auth.fetchMe();
  if (auth.user) {
    form.name = auth.user.name || '';
    form.email = auth.user.email || '';
    form.phone = auth.user.phone || '';
  }
};

const saveProfile = async () => {
  saving.value = true;
  error.value = null;
  success.value = null;
  try {
    await auth.updateProfile({
      name: form.name,
      email: form.email,
      phone: form.phone,
    });
    success.value = t('profile.updated');
    editing.value = false;
  } catch (e) {
    error.value = e.response?.data?.message || t('profile.update_error');
  } finally {
    saving.value = false;
  }
};

onMounted(loadProfile);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-ink">{{ t('profile.title') }}</h1>

    <div v-if="!auth.user" class="text-ink-3">
      {{ t('profile.load_error') }}
    </div>

    <div v-else class="bg-surface rounded-xl border border-line p-6">
      <!-- Messages -->
      <div v-if="error" class="mb-4 p-3 bg-red-50 text-red-700 rounded-lg text-sm theme-dark:bg-red-900/30 theme-dark:text-red-300">{{ error }}</div>
      <div v-if="success" class="mb-4 p-3 bg-green-50 text-green-700 rounded-lg text-sm theme-dark:bg-green-900/30 theme-dark:text-green-300">{{ success }}</div>

      <!-- Profile info -->
      <div class="space-y-4">
        <div>
          <label class="block text-sm text-ink-3">{{ t('auth.email') }}</label>
          <div class="font-medium">{{ auth.user.email }}</div>
        </div>
        <div>
          <label class="block text-sm text-ink-3">{{ t('common.role') }}</label>
          <div class="font-medium capitalize">{{ auth.user.role }}</div>
        </div>

        <!-- Editable fields -->
        <div>
          <label class="block text-sm text-ink-3">{{ t('common.name') }}</label>
          <template v-if="editing">
            <input v-model="form.name" type="text" class="w-full px-3 py-2 border border-line rounded-lg mt-1 bg-surface-2/50 focus:outline-none focus:ring-2 focus:ring-accent transition" />
          </template>
          <div v-else class="font-medium">{{ auth.user.name || '—' }}</div>
        </div>

        <div>
          <label class="block text-sm text-ink-3">{{ t('common.phone') }}</label>
          <template v-if="editing">
            <input v-model="form.phone" type="tel" class="w-full px-3 py-2 border border-line rounded-lg mt-1 bg-surface-2/50 focus:outline-none focus:ring-2 focus:ring-accent transition" />
          </template>
          <div v-else class="font-medium">{{ auth.user.phone || '—' }}</div>
        </div>

        <!-- Actions -->
        <div class="flex gap-3 pt-4">
          <template v-if="!editing">
            <button @click="editing = true" class="btn btn-secondary">
              {{ t('profile.edit') }}
            </button>
            <router-link to="/orders" class="btn btn-primary">
              {{ t('profile.my_orders') }}
            </router-link>
            <router-link to="/reviews" class="btn btn-secondary">
              {{ t('profile.my_reviews') }}
            </router-link>
          </template>
          <template v-else>
            <button
              @click="saveProfile"
              :disabled="saving"
              class="btn btn-primary"
            >
              {{ saving ? t('profile.saving') : t('profile.save') }}
            </button>
            <button @click="editing = false; loadProfile()" class="btn btn-secondary">
              {{ t('profile.cancel') }}
            </button>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
