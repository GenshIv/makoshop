<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useAuthStore } from '../stores/auth';
import { useFormat } from '../composables/useFormat';

const router = useRouter();
const auth = useAuthStore();
const { t } = useI18n();
const { formatDate } = useFormat();
const reviews = ref([]);
const loading = ref(true);
const error = ref(null);

const fetchReviews = async () => {
  loading.value = true;
  try {
    const response = await api.get('/reviews', {
      params: { user_id: auth.user?.id },
    });
    reviews.value = response.data.reviews || response.data || [];
  } catch (e) {
    error.value = t('reviews.load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(fetchReviews);
</script>

<template>
  <div class="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6">{{ t('reviews.title') }}</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-indigo-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <!-- Empty -->
    <div v-else-if="reviews.length === 0" class="text-center py-12 text-ink-3">
      {{ t('reviews.no_reviews') }}
    </div>

    <!-- Reviews list -->
    <div v-else class="space-y-4">
      <div v-for="review in reviews" :key="review.id" class="bg-surface rounded-lg shadow-sm p-4">
        <div class="flex items-center justify-between mb-2">
          <router-link
            :to="{ name: 'product', params: { id: review.product_id } }"
            class="font-medium hover:text-indigo-600"
          >
            {{ review.product_name || t('reviews.product', { id: review.product_id }) }}
          </router-link>
          <span class="text-yellow-500">{{ '★'.repeat(review.rating) }}</span>
        </div>
        <p class="text-sm text-ink-2">{{ review.comment }}</p>
        <div class="mt-2 text-xs text-ink-3">{{ formatDate(review.created_at) }}</div>
      </div>
    </div>
  </div>
</template>
