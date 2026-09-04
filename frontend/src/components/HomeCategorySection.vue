<script setup>
// One home page offers section: a themed category header with a "view all"
// link and a paged carousel of random product cards.
//
// Paging (not free scrolling) guarantees cards are never cut off at the
// edge: the scroller contains full-width snap pages, each an exact grid of
// `visibleCount` columns, so every card is either fully visible or on the
// next page.
import { computed, ref, onMounted, onBeforeUnmount } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';
import ProductCard from './ProductCard.vue';

const props = defineProps({
  section: { type: Object, required: true }, // { category: {...}, items: [...], total }
  index: { type: Number, default: 0 }, // section ordinal, drives the theme
  formatPrice: { type: Function, required: true },
  enableImageFade: { type: Boolean, default: false },
});

const { t, locale } = useI18n();
const router = useRouter();

// Rotating visual themes so neighboring sections read as distinct blocks.
// Full class literals keep them discoverable by Tailwind's scanner.
const THEMES = [
  {
    bg: 'from-orange-500/15 via-surface to-surface',
    bar: 'from-orange-500 to-amber-400',
    avatar: 'from-orange-500 to-amber-400',
    hover: 'hover:border-orange-400 hover:bg-orange-50 dark:hover:bg-orange-900/20',
    text: 'text-orange-600 dark:text-orange-400',
  },
  {
    bg: 'from-sky-500/15 via-surface to-surface',
    bar: 'from-sky-500 to-cyan-400',
    avatar: 'from-sky-500 to-cyan-400',
    hover: 'hover:border-sky-400 hover:bg-sky-50 dark:hover:bg-sky-900/20',
    text: 'text-sky-600 dark:text-sky-400',
  },
  {
    bg: 'from-emerald-500/15 via-surface to-surface',
    bar: 'from-emerald-500 to-teal-400',
    avatar: 'from-emerald-500 to-teal-400',
    hover: 'hover:border-emerald-400 hover:bg-emerald-50 dark:hover:bg-emerald-900/20',
    text: 'text-emerald-600 dark:text-emerald-400',
  },
  {
    bg: 'from-violet-500/15 via-surface to-surface',
    bar: 'from-violet-500 to-fuchsia-400',
    avatar: 'from-violet-500 to-fuchsia-400',
    hover: 'hover:border-violet-400 hover:bg-violet-50 dark:hover:bg-violet-900/20',
    text: 'text-violet-600 dark:text-violet-400',
  },
  {
    bg: 'from-rose-500/15 via-surface to-surface',
    bar: 'from-rose-500 to-pink-400',
    avatar: 'from-rose-500 to-pink-400',
    hover: 'hover:border-rose-400 hover:bg-rose-50 dark:hover:bg-rose-900/20',
    text: 'text-rose-600 dark:text-rose-400',
  },
  {
    bg: 'from-indigo-500/15 via-surface to-surface',
    bar: 'from-indigo-500 to-blue-400',
    avatar: 'from-indigo-500 to-blue-400',
    hover: 'hover:border-indigo-400 hover:bg-indigo-50 dark:hover:bg-indigo-900/20',
    text: 'text-indigo-600 dark:text-indigo-400',
  },
];

const theme = computed(() => THEMES[props.index % THEMES.length]);

const category = computed(() => props.section.category || {});

const name = computed(() => {
  const c = category.value;
  const langField = `name_${locale.value}`;
  return c[langField] || c.name_en || c.name_ru || c.name_ua || c.name_pl || '';
});

const letter = computed(() => (name.value.trim()[0] || '?').toUpperCase());

// Placeholder CDN URLs are not real images (same rule as the catalog).
const isValidImage = (url) => {
  if (!url) return false;
  return !url.includes('cdn.makoshop.com');
};

const totalCount = computed(() => Number(props.section.total) || 0);
const formattedCount = computed(() => totalCount.value.toLocaleString());

// ---- Paged carousel ----

const scroller = ref(null);
const scrollerWidth = ref(0);
const pageIndex = ref(0);

// Approximate design width of one card; only used to estimate how many
// columns fit — the grid stretches cards to the exact page width.
const CARD_MIN = 148;
const GAP = 12;

const visibleCount = computed(() => {
  if (!scrollerWidth.value) return 2;
  return Math.max(2, Math.floor((scrollerWidth.value + GAP) / (CARD_MIN + GAP)));
});

// Chunk items into full pages of visibleCount cards.
const pages = computed(() => {
  const size = visibleCount.value;
  const out = [];
  const items = props.section.items || [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
});

const canPrev = computed(() => pageIndex.value > 0);
const canNext = computed(() => pageIndex.value < pages.value.length - 1);

const goToPage = (page) => {
  const el = scroller.value;
  if (!el) return;
  const clamped = Math.min(Math.max(page, 0), pages.value.length - 1);
  el.scrollTo({ left: clamped * el.clientWidth, behavior: 'smooth' });
};

const onScroll = () => {
  const el = scroller.value;
  if (!el || !el.clientWidth) return;
  pageIndex.value = Math.round(el.scrollLeft / el.clientWidth);
};

let resizeObserver = null;

onMounted(() => {
  const el = scroller.value;
  if (!el) return;
  scrollerWidth.value = el.clientWidth;
  resizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      scrollerWidth.value = entry.contentRect.width;
    }
  });
  resizeObserver.observe(el);
});

onBeforeUnmount(() => {
  if (resizeObserver) resizeObserver.disconnect();
});

// Same navigation contract as the catalog: EAN page by seo_url, then slug,
// then the numeric product page as a last resort.
const goToProduct = (product) => {
  if (product.seo_url) {
    router.push({ path: product.seo_url });
    return;
  }
  if (product.slug) {
    router.push({ path: '/shop/' + product.slug });
    return;
  }
  router.push({ name: 'product', params: { id: product.id } });
};

const goToCategory = () => {
  if (category.value.url) {
    router.push({ path: category.value.url });
  }
};
</script>

<template>
  <section
    class="rounded-2xl border border-line bg-gradient-to-br p-3.5 sm:p-5"
    :class="theme.bg"
  >
    <!-- Header -->
    <div class="flex items-center gap-3 sm:gap-4 mb-4">
      <!-- Avatar: category image or themed letter chip -->
      <button
        class="flex-shrink-0 w-11 h-11 sm:w-14 sm:h-14 rounded-xl overflow-hidden border-2 border-white/70 dark:border-slate-800 shadow-md cursor-pointer transition-transform hover:scale-105"
        :class="!isValidImage(category.image_light_url) && !isValidImage(category.image_dark_url) ? `bg-gradient-to-br ${theme.avatar}` : 'bg-surface'"
        :aria-label="name"
        @click="goToCategory"
      >
        <img
          v-if="isValidImage(category.image_light_url) || isValidImage(category.image_dark_url)"
          :src="isValidImage(category.image_light_url) ? category.image_light_url : category.image_dark_url"
          :alt="name"
          loading="lazy"
          decoding="async"
          class="w-full h-full object-cover dark:hidden"
        />
        <img
          v-if="isValidImage(category.image_dark_url)"
          :src="category.image_dark_url"
          :alt="name"
          loading="lazy"
          decoding="async"
          class="w-full h-full object-cover hidden dark:block"
        />
        <span
          v-if="!isValidImage(category.image_light_url) && !isValidImage(category.image_dark_url)"
          class="w-full h-full flex items-center justify-center text-white text-xl sm:text-2xl font-black"
        >
          {{ letter }}
        </span>
      </button>

      <!-- Screaming category title -->
      <div class="min-w-0 flex-1 cursor-pointer" @click="goToCategory">
        <h2
          class="text-xl sm:text-2xl lg:text-3xl font-black uppercase tracking-tight text-ink leading-none truncate"
        >
          {{ name }}
        </h2>
        <div class="mt-1.5 flex items-center gap-2.5">
          <span class="h-1.5 w-10 sm:w-14 rounded-full bg-gradient-to-r" :class="theme.bar" />
          <span
            v-if="totalCount"
            class="inline-flex items-center gap-1 text-[11px] sm:text-xs font-bold px-2 py-0.5 rounded-full bg-surface border border-line text-ink-2"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M10 2a6 6 0 00-6 6c0 1.9.9 3.6 2.2 4.7V16a1 1 0 001 1h1l1 1 1-1h1.6l1 1 1-1H15a1 1 0 001-1v-3.3A6 6 0 0010 2zm-2.5 6a1.5 1.5 0 113 0 1.5 1.5 0 01-3 0zm5 0a1.5 1.5 0 113 0 1.5 1.5 0 01-3 0z" />
            </svg>
            {{ formattedCount }}
          </span>
        </div>
      </div>

      <!-- Arrows + view all -->
      <div class="flex items-center gap-1.5 sm:gap-2 flex-shrink-0">
        <button
          :aria-label="t('home.scroll_left')"
          class="p-1.5 sm:p-2 rounded-full border border-line bg-surface text-ink-2 transition-opacity"
          :class="canPrev ? 'hover:text-ink hover:shadow-sm ' + theme.hover : 'opacity-30 pointer-events-none'"
          @click="goToPage(pageIndex - 1)"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 sm:h-5 sm:w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
        <button
          :aria-label="t('home.scroll_right')"
          class="p-1.5 sm:p-2 rounded-full border border-line bg-surface text-ink-2 transition-opacity"
          :class="canNext ? 'hover:text-ink hover:shadow-sm ' + theme.hover : 'opacity-30 pointer-events-none'"
          @click="goToPage(pageIndex + 1)"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 sm:h-5 sm:w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
          </svg>
        </button>
        <button
          class="hidden sm:inline-flex items-center gap-1 px-3 py-1.5 rounded-full border border-line bg-surface text-sm font-semibold transition-colors"
          :class="theme.hover + ' ' + theme.text"
          @click="goToCategory"
        >
          {{ t('home.view_all') }}
          <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Paged carousel: full-width snap pages, each an exact grid -->
    <div
      ref="scroller"
      class="carousel-scroll flex overflow-x-auto snap-x snap-mandatory scroll-smooth"
      @scroll.passive="onScroll"
    >
      <div
        v-for="(page, pi) in pages"
        :key="pi"
        class="w-full flex-shrink-0 snap-start"
        :style="{ display: 'grid', gridTemplateColumns: `repeat(${visibleCount}, minmax(0, 1fr))`, gap: `${GAP}px` }"
      >
        <ProductCard
          v-for="product in page"
          :key="product.id"
          :product="product"
          :format-price="formatPrice"
          view="grid"
          :enable-image-fade="enableImageFade"
          @click="goToProduct(product)"
        />
      </div>
    </div>
  </section>
</template>

<style scoped>
/* Carousel: no visible scrollbar — arrows and swipe provide the navigation. */
.carousel-scroll {
  scrollbar-width: none;
}
.carousel-scroll::-webkit-scrollbar {
  display: none;
}
</style>
