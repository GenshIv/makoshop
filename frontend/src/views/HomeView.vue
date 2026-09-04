<script setup>
// Storefront home page: hero, root category tiles and random product
// sections per root category (data from GET /home/offers).
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';
import api from '../api';
import HomeHero from '../components/HomeHero.vue';
import HomeCategorySection from '../components/HomeCategorySection.vue';
import SkeletonCard from '../components/SkeletonCard.vue';
import { useFormat } from '../composables/useFormat';
import { useAnimation } from '../composables/useAnimation';

const { t, locale } = useI18n();
const router = useRouter();
const { formatPrice } = useFormat();
const { animationEnabled } = useAnimation();

const rootCategories = ref([]);
const rootCatsLoading = ref(false);

const sections = ref([]);
const sectionsLoading = ref(true);

// Placeholder CDN URLs are not real images (same rule as the catalog).
const isValidImage = (url) => {
  if (!url) return false;
  return !url.includes('cdn.makoshop.com');
};

const catName = (cat) => {
  if (!cat) return '';
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || '';
};

const fetchRootCategories = async () => {
  rootCatsLoading.value = true;
  try {
    const response = await api.get('/categories/tree');
    rootCategories.value = Array.isArray(response.data) ? response.data : [];
  } catch (e) {
    console.error('Failed to fetch root categories:', e);
    rootCategories.value = [];
  } finally {
    rootCatsLoading.value = false;
  }
};

const fetchOffers = async () => {
  sectionsLoading.value = true;
  try {
    const response = await api.get('/home/offers');
    sections.value = response.data?.sections || [];
  } catch (e) {
    console.error('Failed to fetch home offers:', e);
    sections.value = [];
  } finally {
    sectionsLoading.value = false;
  }
};

const goToCategory = (cat) => {
  router.push({ path: '/shop/' + cat.slug });
};

onMounted(() => {
  fetchRootCategories();
  fetchOffers();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <HomeHero />

    <!-- Root categories tiles row -->
    <div class="mb-8">
      <div v-if="rootCatsLoading" class="flex gap-2">
        <div v-for="i in 12" :key="i" class="flex-1 aspect-square rounded-lg bg-surface-3 animate-pulse" />
      </div>
      <div v-else class="flex gap-2">
        <div
          v-for="cat in rootCategories"
          :key="cat.id"
          class="flex-1 cursor-pointer group rounded-xl overflow-hidden border border-line hover:border-orange-300 hover:shadow-md transition-all duration-200 flex flex-col"
          @click="goToCategory(cat)"
        >
          <div class="relative w-full pt-[100%] bg-surface-2 overflow-hidden">
            <img
              v-if="isValidImage(cat.image_light_url)"
              :src="cat.image_light_url"
              :alt="catName(cat)"
              loading="lazy"
              decoding="async"
              class="absolute inset-1 w-full h-full object-cover dark:hidden"
            />
            <img
              v-if="isValidImage(cat.image_dark_url) || isValidImage(cat.image_light_url)"
              :src="isValidImage(cat.image_dark_url) ? cat.image_dark_url : cat.image_light_url"
              :alt="catName(cat)"
              loading="lazy"
              decoding="async"
              class="absolute inset-1 w-full h-full object-cover hidden dark:block"
            />
            <div
              v-if="!isValidImage(cat.image_light_url) && !isValidImage(cat.image_dark_url)"
              class="absolute inset-1 flex items-center justify-center text-ink-3"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-8 w-8" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
              </svg>
            </div>
          </div>
          <div class="flex-1 flex items-center px-2 py-1.5 bg-surface">
            <div class="text-xs font-medium text-left line-clamp-2 text-ink">
              {{ catName(cat) }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Random offers sections -->
    <div v-if="sectionsLoading" class="space-y-6 sm:space-y-8" aria-hidden="true">
      <div v-for="s in 3" :key="s" class="rounded-2xl border border-line p-3.5 sm:p-5">
        <div class="flex items-center gap-3 mb-4">
          <div class="w-11 h-11 sm:w-14 sm:h-14 rounded-xl bg-surface-3 animate-pulse" />
          <div class="flex-1">
            <div class="h-6 w-56 max-w-full bg-surface-3 rounded animate-pulse" />
            <div class="mt-2 h-1.5 w-24 bg-surface-3 rounded-full animate-pulse" />
          </div>
        </div>
        <div class="flex gap-3 overflow-hidden">
          <SkeletonCard v-for="i in 6" :key="i" class="w-40 sm:w-48 flex-shrink-0" />
        </div>
      </div>
    </div>

    <div v-else-if="sections.length" class="space-y-6 sm:space-y-8">
      <HomeCategorySection
        v-for="(section, i) in sections"
        :key="section.category.id"
        :section="section"
        :index="i"
        :format-price="formatPrice"
        :enable-image-fade="animationEnabled"
      />
    </div>
  </div>
</template>
