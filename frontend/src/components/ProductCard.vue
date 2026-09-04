<script setup>
import { computed, ref, onBeforeUnmount } from 'vue';
import { useI18n } from 'vue-i18n';
import PriceSparkline from './PriceSparkline.vue';
import { useAuthStore } from '../stores/auth';
import api from '../api';

const { t } = useI18n();
const auth = useAuthStore();

const props = defineProps({
  product: { type: Object, required: true },
  formatPrice: { type: Function, required: true },
  view: { type: String, default: 'grid', validator: (v) => ['grid', 'list'].includes(v) },
  // Optional: array of recent prices for sparkline (e.g. [old, ..., current])
  priceHistory: { type: Array, default: () => [] },
  // Enable the "image fade / wake on hover" effect for this card
  enableImageFade: { type: Boolean, default: false },
});

const emit = defineEmits(['click']);

const isAdmin = computed(() => auth.role === 'admin');

// Image fade / wake-on-hover logic (purely visual)
const isImageActive = ref(false);
let hoverTimer = null;
let fadeTimer = null;

const HOVER_WAKE_MS = 300;   // must keep mouse on card for 300ms to wake image
const FADE_AFTER_MS = 30000; // image fades 30 seconds after last wake

const wakeImage = () => {
  isImageActive.value = true;
  clearTimeout(fadeTimer);
  fadeTimer = setTimeout(() => {
    isImageActive.value = false;
  }, FADE_AFTER_MS);
};

const onImageMouseEnter = () => {
  if (!props.enableImageFade) return;
  clearTimeout(hoverTimer);
  hoverTimer = setTimeout(wakeImage, HOVER_WAKE_MS);
};

const onImageMouseLeave = () => {
  if (!props.enableImageFade) return;
  clearTimeout(hoverTimer);
  // Keep isImageActive and fadeTimer as is:
  // once image is "awake", it stays bright for FADE_AFTER_MS
  // regardless of mouse leaving before that time.
};

onBeforeUnmount(() => {
  clearTimeout(hoverTimer);
  clearTimeout(fadeTimer);
});

const title = computed(() => props.product.title || props.product.name || '');
const price = computed(() => props.product.price ?? props.product.min_price ?? 0);

// Previous (old) price: shown struck-through when higher than current price.
const previousPrice = computed(() => {
  const pp = Number(props.product.previous_price);
  const cur = Number(price.value);
  if (Number.isFinite(pp) && Number.isFinite(cur) && pp > cur) {
    return pp;
  }
  return null;
});

// Orange price: true when there is a discount (previous_price > current price).
const hasDiscount = computed(() => previousPrice.value !== null);

// Derive a mini price trend from available data:
// - explicit priceHistory if provided
// - otherwise synthesize from min/max price when both exist
const sparklineValues = computed(() => {
  if (props.priceHistory && props.priceHistory.length >= 2) {
    return props.priceHistory;
  }
  const min = Number(props.product.min_price);
  const max = Number(props.product.max_price);
  const cur = Number(price.value);
  if (Number.isFinite(min) && Number.isFinite(max) && max > min && Number.isFinite(cur)) {
    return [max, (max + cur) / 2, cur, min];
  }
  return [];
});

const hasSparkline = computed(() => sparklineValues.value.length >= 2);

const attrsString = computed(() => {
  const attrs = props.product.attributes;
  if (!attrs || !attrs.length) return '';
  return attrs
    .slice(0, 4)
    .filter((a) => a && a.value != null && String(a.value).trim() !== '')
    .map((a) => a.value)
    .join(' · ');
});

// Catalogizer modal state
const catalogModal = ref(null);
const addTokenInputs = ref({});
const categories = ref([]);
const selectedCategorySlug = ref('');
const categorySearch = ref('');
const training = ref(false);

const fetchCategories = async () => {
  try {
    const res = await api.get('/admin/categories');
    const data = res.data;
    categories.value = Array.isArray(data) ? data : (data?.items || []);
  } catch (e) {
    console.error('Failed to fetch categories:', e);
  }
};

const getCategoryBySlug = (slug) => {
  return categories.value.find(c => c.slug === slug.trim());
};

const trainCatalogizer = async () => {
  training.value = true;
  try {
    await api.post('/admin/catalogizer/train');
    openCatalogModal();
  } catch (e) {
    console.error('Train error:', e);
  } finally {
    training.value = false;
  }
};

const addTokenToSelectedCategory = async () => {
  const cat = getCategoryBySlug(selectedCategorySlug.value);
  if (!cat) {
    console.error('Category not found:', selectedCategorySlug.value);
    return;
  }
  const token = (categorySearch.value || '').trim().toLowerCase();
  if (!token) return;
  try {
    // Fetch current keywords and append new one
    const res = await api.get(`/admin/categories/${cat.id}`);
    const current = res.data.anchor_keywords || [];
    if (current.includes(token)) return;
    const updated = [...current, token];
    await api.patch(`/admin/categories/${cat.id}`, { anchor_keywords: updated });
    categorySearch.value = '';
    openCatalogModal();
  } catch (e) {
    console.error('Failed to add token:', e);
  }
};

const openCatalogModal = async () => {
  const name = props.product.title || props.product.name || '';
  if (!name) return;
  catalogModal.value = { loading: true, results: null };
  await fetchCategories();
  try {
    const res = await api.post('/admin/catalogizer/test', { name });
    catalogModal.value.results = res.data;
  } catch (e) {
    console.error('Catalogizer test error:', e);
  } finally {
    catalogModal.value.loading = false;
  }
};

const closeCatalogModal = () => {
  catalogModal.value = null;
};

const addTokenToCategory = async (catId, token) => {
  const t = (token || '').trim().toLowerCase();
  if (!t) return;
  try {
    const res = await api.get(`/admin/categories/${catId}`);
    const current = res.data.anchor_keywords || [];
    if (current.includes(t)) return;
    const updated = [...current, t];
    await api.patch(`/admin/categories/${catId}`, { anchor_keywords: updated });
    openCatalogModal();
  } catch (e) {
    console.error('Failed to add token:', e);
  }
};

const removeTokenFromCategory = async (catId, token) => {
  try {
    const res = await api.get(`/admin/categories/${catId}`);
    const current = res.data.anchor_keywords || [];
    const updated = current.filter(kw => kw !== token);
    await api.patch(`/admin/categories/${catId}`, { anchor_keywords: updated });
    openCatalogModal();
  } catch (e) {
    console.error('Failed to remove token:', e);
  }
};

const onAddTokenInput = (catId, event) => {
  addTokenInputs.value[catId] = event.target.value;
};

const handleAddTokenEnter = (catId) => {
  addTokenToCategory(catId, addTokenInputs.value[catId] || '');
  addTokenInputs.value[catId] = '';
};
</script>

<template>
  <!-- GRID card -->
  <div
    v-if="view === 'grid'"
    tabindex="0"
    class="group bg-surface rounded-xl border border-line overflow-hidden cursor-pointer relative
           transition-all duration-200 ease-out
           hover:shadow-lg hover:-translate-y-0.5 hover:border-orange-300 dark:hover:bg-surface-elevated
           focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-1
           active:scale-[0.99]
           flex flex-col h-full"
    @click="$emit('click')"
    @keydown.enter="$emit('click')"
  >
    <!-- Badges -->
    <span
      v-if="product.promoted"
      class="absolute top-2 left-2 z-10 bg-yellow-400 text-yellow-900 text-[11px] font-semibold px-1.5 py-0.5 rounded-full"
    >
      {{ t('catalog.ad') }}
    </span>
    <span
      v-if="product.product_count && product.product_count > 1"
      class="absolute top-2 right-8 z-10 bg-accent text-white text-[11px] font-semibold px-1.5 py-0.5 rounded-full"
    >
      {{ product.product_count }}
    </span>

    <!-- Admin catalogize button -->
    <button
      v-if="isAdmin"
      class="absolute top-2 right-2 z-20 bg-purple-600 text-white w-7 h-7 rounded-full flex items-center justify-center text-xs hover:bg-purple-700 transition-colors shadow-sm"
      @click.stop="openCatalogModal"
      title="Catalogize"
    >
      🏷
    </button>

    <!-- Image -->
    <div
      class="aspect-[4/3] bg-white flex items-center justify-center overflow-hidden"
      @mouseenter="onImageMouseEnter"
      @mouseleave="onImageMouseLeave"
    >
      <div
        v-if="product.images?.length"
        class="w-full h-full bg-white flex items-center justify-center transition-all duration-500 ease-out group-hover:scale-[1.03]"
        :class="{
          'image-fade': enableImageFade,
          'image-fade-active': enableImageFade && isImageActive
        }"
      >
        <img
          :src="product.images[0]"
          :alt="title"
          loading="lazy"
          decoding="async"
          class="w-full h-full object-contain"
        />
      </div>
      <span v-else class="text-ink-3 text-xs">{{ t('catalog.no_photo') }}</span>

      <!-- Price sparkline on hover -->
      <Transition
        enter-active-class="transition duration-200 ease-out"
        enter-from-class="opacity-0 translate-y-1"
        enter-to-class="opacity-100 translate-y-0"
        leave-active-class="transition duration-150 ease-in"
        leave-from-class="opacity-100"
        leave-to-class="opacity-0"
      >
        <div
          v-if="hasSparkline"
          class="absolute bottom-2 left-2 right-2 h-8 flex items-center justify-center rounded-md bg-white/85 theme-dark:bg-slate-900/85 backdrop-blur-sm text-accent opacity-0 group-hover:opacity-100 transition-opacity"
        >
          <PriceSparkline :values="sparklineValues" :width="96" :height="24" />
        </div>
      </Transition>
    </div>

    <!-- Info -->
    <div class="p-2.5 sm:p-3 space-y-1 flex flex-col flex-1 min-h-[80px]">
      <h3 class="font-semibold text-[13px] sm:text-sm leading-tight line-clamp-2 text-ink">{{ title }}</h3>

      <div v-if="product.brand" class="text-[11px] sm:text-xs text-ink-3">{{ product.brand }}</div>

      <div v-if="attrsString" class="text-[11px] text-ink-3 truncate">{{ attrsString }}</div>

      <!-- Price + sellers + rating (pushed to bottom for equal card heights) -->
      <div class="flex items-end justify-between pt-1 gap-2 mt-auto">
        <div class="flex flex-col">
          <div class="flex items-baseline gap-1.5 flex-wrap">
            <span
              v-if="previousPrice"
              class="text-[11px] sm:text-xs text-ink-3 line-through"
            >
              {{ formatPrice(previousPrice, product.currency) }}
            </span>
            <span
              class="font-bold text-sm sm:text-base"
              :class="hasDiscount ? 'text-orange-600 theme-dark:text-orange-400' : 'text-accent'"
            >
              {{ formatPrice(price, product.currency) }}
            </span>
          </div>
          <span
            v-if="product.sellers_count && product.sellers_count > 1"
            class="text-[11px] text-ink-3 dark:text-amber-400/90"
          >
            {{ t('catalog.from_price_sellers', { count: product.sellers_count }) }}
          </span>
        </div>
        <span v-if="product.avg_rating" class="text-xs text-yellow-500 flex-shrink-0">
          ★ {{ Number(product.avg_rating).toFixed(1) }}
        </span>
      </div>
    </div>
  </div>

  <!-- LIST card -->
  <div
    v-else
    tabindex="0"
    class="group bg-surface rounded-xl border border-line overflow-hidden cursor-pointer
           transition-all duration-200 ease-out
           hover:shadow-lg hover:border-orange-300 dark:hover:bg-surface-elevated
           focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
    @click="$emit('click')"
    @keydown.enter="$emit('click')"
  >
    <div class="flex flex-col sm:flex-row gap-3 sm:gap-4 p-3">
      <!-- Image -->
      <div
        class="relative w-full sm:w-32 h-32 sm:h-32 flex-shrink-0 bg-white rounded-lg overflow-hidden"
        @mouseenter="onImageMouseEnter"
        @mouseleave="onImageMouseLeave"
      >
        <div
          v-if="product.images?.length"
          class="w-full h-full bg-white flex items-center justify-center transition-all duration-500 ease-out"
          :class="{
            'image-fade': enableImageFade,
            'image-fade-active': enableImageFade && isImageActive
          }"
        >
          <img
            :src="product.images[0]"
            :alt="title"
            loading="lazy"
            decoding="async"
            class="w-full h-full object-cover"
          />
        </div>
        <span v-else class="w-full h-full flex items-center justify-center text-ink-3 text-xs">
          {{ t('catalog.no_photo') }}
        </span>
        <span
          v-if="product.promoted"
          class="absolute top-1.5 left-1.5 bg-yellow-400 text-yellow-900 text-[10px] font-semibold px-1.5 py-0.5 rounded-full"
        >
          {{ t('catalog.ad') }}
        </span>
      </div>

      <!-- Admin catalogize button -->
      <button
        v-if="isAdmin"
        class="absolute top-2 right-2 z-20 bg-purple-600 text-white w-7 h-7 rounded-full flex items-center justify-center text-xs hover:bg-purple-700 transition-colors shadow-sm"
        @click.stop="openCatalogModal"
        title="Catalogize"
      >
        🏷
      </button>

      <!-- Info: 3 columns - attributes | description | price -->
      <div class="flex-1 min-w-0 flex flex-col">
        <h3 class="font-semibold text-sm text-ink line-clamp-2">{{ title }}</h3>
        <div v-if="product.brand" class="mt-0.5 text-xs font-medium text-orange-600 theme-dark:text-orange-400">{{ product.brand }}</div>

        <!-- Content row: attributes | description | price -->
        <div class="mt-2 flex items-start gap-3 flex-1 min-h-0">
          <!-- Left: attributes (narrow) -->
          <div v-if="attrsString" class="flex-shrink-0 w-24 text-xs text-sky-700 theme-dark:text-sky-400 line-clamp-3">
            {{ attrsString }}
          </div>
          <!-- Middle: description (wide, multi-line) -->
          <div v-if="product.description" class="flex-1 min-w-0 text-xs text-ink-3 leading-relaxed line-clamp-3 break-words">
            {{ product.description }}
          </div>
          <!-- Right: price + tags -->
          <div class="flex-shrink-0 text-right">
            <div class="flex items-baseline gap-1.5 justify-end flex-wrap">
              <span
                v-if="previousPrice"
                class="text-xs text-ink-3 line-through"
              >
                {{ formatPrice(previousPrice, product.currency) }}
              </span>
              <span
                class="font-bold text-base"
                :class="hasDiscount ? 'text-orange-600 theme-dark:text-orange-400' : 'text-accent'"
              >
                {{ formatPrice(price, product.currency) }}
              </span>
            </div>
            <div v-if="product.product_count && product.product_count > 1" class="mt-1 inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium bg-surface-2 text-ink-2 border border-line">
              {{ product.product_count }}
            </div>
            <div v-if="product.avg_rating" class="mt-1 text-xs text-yellow-500">
              ★ {{ Number(product.avg_rating).toFixed(1) }}
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- Catalogizer Modal -->
  <div
    v-if="catalogModal"
    class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
    @click.self="closeCatalogModal"
  >
    <div role="dialog" aria-modal="true" class="bg-surface rounded-lg shadow-xl p-6 w-full max-w-3xl max-h-[90vh] overflow-y-auto">
      <h2 class="text-xl font-bold mb-4 text-purple-700">
        {{ t('admin.catalogizer_test_title') || 'Catalogize Product' }}
      </h2>

      <div class="mb-4 bg-surface-2 rounded-lg p-3">
        <div class="text-sm font-medium text-ink-2">
          {{ props.product.title || props.product.name }}
        </div>
      </div>

      <!-- Train button -->
      <div class="mb-4">
        <button
          @click="trainCatalogizer"
          :disabled="training"
          class="px-3 py-1.5 text-xs bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
        >
          {{ training ? 'Training...' : 'Train Catalogizer' }}
        </button>
      </div>

      <!-- Add to custom category -->
      <div class="mb-4 border border-line rounded-lg p-3">
        <div class="text-xs font-medium text-ink-2 mb-2">Add keyword to any category</div>
        <div class="flex gap-2 items-start">
          <input
            type="text"
            v-model="selectedCategorySlug"
            placeholder="Category slug (e.g. smartphones)"
            class="flex-1 px-2 py-1.5 text-xs border border-line rounded"
            @keydown.enter="addTokenToSelectedCategory"
          />
          <input
            type="text"
            v-model="categorySearch"
            placeholder="Keyword"
            class="w-32 px-2 py-1.5 text-xs border border-line rounded"
            @keydown.enter="addTokenToSelectedCategory"
          />
          <button
            @click="addTokenToSelectedCategory"
            class="px-2 py-1.5 text-xs bg-green-600 text-white rounded hover:bg-green-700"
          >+</button>
        </div>
      </div>

      <div v-if="catalogModal.loading" class="flex justify-center py-8">
        <div class="animate-spin h-6 w-6 border-4 border-purple-600 border-t-transparent rounded-full"></div>
      </div>

      <div v-else-if="catalogModal.results">
        <div class="mb-3 text-sm text-ink-2">
          <span class="font-medium">{{ t('admin.catalogizer_tokens') || 'Tokens' }}:</span>
          {{ catalogModal.results.tokens?.join(', ') || '—' }}
        </div>

        <div v-if="!catalogModal.results.matches || catalogModal.results.matches.length === 0" class="text-sm text-ink-3">
          {{ t('admin.catalogizer_no_matches') || 'No matches found.' }}
        </div>

        <div v-else class="space-y-4">
          <div
            v-for="m in catalogModal.results.matches"
            :key="m.NewCategoryID"
            class="border rounded-lg p-3"
          >
            <div class="flex justify-between items-center mb-2">
              <div>
                <span class="font-medium">{{ m.NewCategorySlug }}</span>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-sm font-semibold text-purple-700">Score: {{ m.Score }}</span>
                <span class="text-xs text-ink-3">{{ t('admin.catalogizer_matched') || 'matched' }}: {{ (m.MatchedTokens||[]).join(', ') }}</span>
              </div>
            </div>

            <div class="mb-2">
              <div class="text-xs text-ink-3 mb-1">{{ t('admin.catalogizer_anchor_keywords') || 'Anchor keywords' }}:</div>
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="kw in (m.anchor_keywords||[])"
                  :key="kw"
                  class="inline-flex items-center gap-1 px-2 py-0.5 bg-purple-100 text-purple-800 rounded text-xs"
                >
                  {{ kw }}
                  <button
                    @click="removeTokenFromCategory(m.NewCategoryID, kw)"
                    class="text-purple-500 hover:text-red-600 font-bold leading-none"
                  >×</button>
                </span>
                <span v-if="!(m.anchor_keywords||[]).length" class="text-xs text-ink-3">(none)</span>
              </div>
            </div>

            <div class="flex gap-2">
              <input
                type="text"
                :placeholder="t('admin.catalogizer_add_token') || 'Add token'"
                class="flex-1 px-2 py-1 text-xs border border-line rounded"
                :value="addTokenInputs[m.NewCategoryID] || ''"
                @input="onAddTokenInput(m.NewCategoryID, $event)"
                @keydown.enter="handleAddTokenEnter(m.NewCategoryID)"
              />
              <button
                @click="handleAddTokenEnter(m.NewCategoryID)"
                class="px-2 py-1 text-xs bg-green-600 text-white rounded hover:bg-green-700"
              >+</button>
            </div>
          </div>
        </div>
      </div>

      <div class="mt-6 flex justify-end">
        <button
          @click="closeCatalogModal"
          class="px-4 py-2 text-sm bg-purple-600 text-white rounded-lg hover:bg-purple-700"
        >
          {{ t('common.close') || 'Close' }}
        </button>
      </div>
    </div>
  </div>
</template>
