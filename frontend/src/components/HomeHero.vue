<script setup>
// Hero block of the storefront home page. Shows the admin-managed
// per-locale hero text (i18n fallback) or, when a branding banner resolves
// for the home_banner slot, the banner itself fills the block edge-to-edge.
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import BrandingSlot from './BrandingSlot.vue';
import { useBranding } from '../composables/useBranding';
import { useSettings } from '../composables/useSettings';

const { t, locale } = useI18n();
const { homeHero } = useSettings();
const { useSlotElement } = useBranding();
const homeBannerEl = useSlotElement('home_banner');

// Manual overrides from global settings (per locale) with i18n fallback.
const heroText = computed(() => {
  const custom = homeHero.value?.[locale.value] || {};
  return {
    headline: custom.headline || t('catalog.hero_headline'),
    sub: custom.sub || t('catalog.hero_sub'),
    tagline: custom.tagline || t('catalog.hero_tagline'),
  };
});
</script>

<template>
  <div
    class="hero-glow relative overflow-hidden rounded-2xl border border-line mb-6"
    :class="homeBannerEl ? '' : 'bg-gradient-to-br from-accent/10 via-surface to-surface-2 py-10 sm:py-14 px-6 text-center'"
  >
    <BrandingSlot v-if="homeBannerEl" slot-name="home_banner" />
    <template v-else>
      <h1 class="text-3xl sm:text-4xl lg:text-5xl font-extrabold tracking-tight text-ink mb-4 leading-tight">
        {{ heroText.headline }}
      </h1>
      <p class="text-lg sm:text-xl text-ink-2 max-w-2xl mx-auto mb-6 leading-relaxed">
        {{ heroText.sub }}
      </p>
      <p class="inline-flex items-center gap-2 text-sm font-semibold text-accent bg-accent/10 px-4 py-2 rounded-full">
        {{ heroText.tagline }}
      </p>
    </template>
  </div>
</template>
