<script setup>
import { computed, ref, onBeforeUnmount } from 'vue';
import { useI18n } from 'vue-i18n';
import PriceSparkline from './PriceSparkline.vue';

const { t } = useI18n();

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
</script>

<template>
  <!-- GRID card -->
  <div
    v-if="view === 'grid'"
    role="button"
    tabindex="0"
    :aria-label="title"
    class="group bg-surface rounded-xl border border-line overflow-hidden cursor-pointer relative
           transition-all duration-200 ease-out
           hover:shadow-lg hover:-translate-y-0.5 hover:border-indigo-300
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
      class="absolute top-2 right-2 z-10 bg-accent text-white text-[11px] font-semibold px-1.5 py-0.5 rounded-full"
    >
      {{ product.product_count }}
    </span>

    <!-- Image -->
    <div
      class="aspect-[4/3] bg-surface-2 flex items-center justify-center overflow-hidden"
      @mouseenter="onImageMouseEnter"
      @mouseleave="onImageMouseLeave"
    >
      <img
        v-if="product.images?.length"
        :src="product.images[0]"
        :alt="title"
        loading="lazy"
        decoding="async"
        class="w-full h-full object-cover transition-all duration-500 ease-out group-hover:scale-[1.03]"
        :class="{
          'image-fade': enableImageFade,
          'image-fade-active': enableImageFade && isImageActive
        }"
      />
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
          <span class="font-bold text-sm sm:text-base text-accent">
            {{ formatPrice(price) }}
          </span>
          <span
            v-if="product.sellers_count && product.sellers_count > 1"
            class="text-[11px] text-ink-3"
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
    role="button"
    tabindex="0"
    :aria-label="title"
    class="group bg-surface rounded-xl border border-line overflow-hidden cursor-pointer
           transition-all duration-200 ease-out
           hover:shadow-lg hover:border-indigo-300
           focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
    @click="$emit('click')"
    @keydown.enter="$emit('click')"
  >
    <div class="flex flex-col sm:flex-row gap-3 sm:gap-4 p-3">
      <!-- Image -->
      <div
        class="relative w-full sm:w-32 h-32 sm:h-32 flex-shrink-0 bg-surface-2 rounded-lg overflow-hidden"
        @mouseenter="onImageMouseEnter"
        @mouseleave="onImageMouseLeave"
      >
        <img
          v-if="product.images?.length"
          :src="product.images[0]"
          :alt="title"
          loading="lazy"
          decoding="async"
          class="w-full h-full object-cover transition-all duration-500 ease-out"
          :class="{
            'image-fade': enableImageFade,
            'image-fade-active': enableImageFade && isImageActive
          }"
        />
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

      <!-- Info: 3 columns - attributes | description | price -->
      <div class="flex-1 min-w-0 flex flex-col">
        <h3 class="font-semibold text-sm text-ink line-clamp-2">{{ title }}</h3>
        <div v-if="product.brand" class="mt-0.5 text-xs font-medium text-indigo-600 theme-dark:text-indigo-400">{{ product.brand }}</div>

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
            <div class="font-bold text-base text-accent">{{ formatPrice(price) }}</div>
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
</template>
