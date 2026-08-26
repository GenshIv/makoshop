<script setup>
import { ref, computed, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useFormat } from '../composables/useFormat';
import { useSeo } from '../composables/useSeo';
import Breadcrumbs from '../components/Breadcrumbs.vue';
import ProductCard from '../components/ProductCard.vue';

const { t, locale } = useI18n();
const { formatPrice } = useFormat();
const route = useRoute();

const company = ref(null);
const stats = ref({});
const products = ref([]);
const loading = ref(true);
const error = ref(null);

// Multilang description: pick by current locale, fallback chain.
const description = computed(() => {
  const c = company.value;
  if (!c) return '';
  const map = { ru: c.desc_ru, ua: c.desc_ua, pl: c.desc_pl, en: c.desc_en };
  return map[locale.value] || c.desc_en || c.desc_ru || '';
});

useSeo({
  title: computed(() => (company.value?.name ? `${company.value.name} — wszyst.pl` : 'Company')),
  description: computed(() => description.value),
  image: computed(() => company.value?.hero_image || company.value?.logo_url || null),
});

const fetchCompany = async () => {
  loading.value = true;
  error.value = null;
  try {
    const res = await api.get(`/company/${route.params.slug}`);
    company.value = res.data.company;
    stats.value = res.data.stats || {};
    products.value = res.data.products || [];
  } catch (e) {
    error.value = t('company.not_found');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const onProductClick = (p) => {
  // Navigate to the product page
  if (p.id) {
    window.location.href = `/products/${p.id}`;
  }
};

onMounted(fetchCompany);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 py-6">
    <!-- Breadcrumbs -->
    <Breadcrumbs :categories="[]" :product-name="company?.name || ''" />

    <!-- Loading -->
    <div v-if="loading" class="flex items-center justify-center py-20 text-ink-3">
      <span class="animate-pulse">{{ t('common.loading') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="flex items-center justify-center py-20 text-ink-3">
      {{ error }}
    </div>

    <template v-else-if="company">
      <!-- Hero -->
      <div class="relative rounded-2xl overflow-hidden mb-6 border border-line bg-surface">
        <img
          v-if="company.hero_image"
          :src="company.hero_image"
          :alt="company.name"
          class="w-full h-48 sm:h-64 object-cover"
        />
        <div
          v-else
          class="w-full h-48 sm:h-64 bg-gradient-to-br from-orange-500/20 to-amber-500/10 flex items-center justify-center"
        >
          <img
            v-if="company.logo_url"
            :src="company.logo_url"
            :alt="company.name"
            class="h-20 object-contain"
          />
        </div>
        <!-- Overlay -->
        <div class="absolute inset-0 bg-gradient-to-t from-black/60 via-black/20 to-transparent flex flex-col justify-end p-5 sm:p-8">
          <div class="flex items-center gap-4">
            <img
              v-if="company.logo_url"
              :src="company.logo_url"
              :alt="company.name"
              class="h-14 w-14 rounded-xl bg-white/90 p-1 object-contain shadow-lg"
            />
            <div>
              <h1 class="text-2xl sm:text-3xl font-bold text-white drop-shadow-md">{{ company.name }}</h1>
              <div v-if="stats.product_count" class="text-sm text-white/80 mt-0.5">
                {{ t('company.products_count', { count: stats.product_count }) }}
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Description -->
      <div v-if="description" class="mb-6 text-ink-2 leading-relaxed whitespace-pre-line">
        {{ description }}
      </div>

      <!-- Products -->
      <div v-if="products.length">
        <h2 class="text-lg font-semibold text-ink mb-4">{{ t('company.latest_products') }}</h2>
        <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6 gap-3 sm:gap-4">
          <ProductCard
            v-for="p in products"
            :key="p.id"
            :product="p"
            :format-price="formatPrice"
            view="grid"
            @click="onProductClick(p)"
          />
        </div>
      </div>
    </template>
  </div>
</template>
