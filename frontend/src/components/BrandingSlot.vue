<script setup>
// BrandingSlot renders the resolved branding element for one slot
// (see composables/useBranding.js and docs/BRANDING_SYSTEM_PLAN.md).
//
// Sizing per slot kind:
//   - fullwidth (header/footer): full-bleed strip, capped height, object-cover
//   - banner (home/category): content-width block, natural aspect ratio
//   - side (left/right columns): small fixed-width image
import { computed } from 'vue';
import { useBranding } from '../composables/useBranding';

const props = defineProps({
  slotName: { type: String, required: true },
});

const { useSlotElement } = useBranding();
const element = useSlotElement(props.slotName);

const isFullwidth = computed(() =>
  props.slotName === 'header_fullwidth' || props.slotName === 'footer_fullwidth'
);
const isSide = computed(() => props.slotName.startsWith('side_'));

const imgClass = computed(() => {
  if (isFullwidth.value) {
    // Full-bleed strip: fill the width, cap the height, crop the rest.
    return 'w-full h-28 sm:h-36 lg:h-44 object-cover';
  }
  if (isSide.value) {
    // Small decorative image in the side gutter.
    return 'w-full max-w-[150px] h-auto rounded-lg object-cover';
  }
  // Banner slots (home/category): natural aspect ratio, content width.
  return 'w-full h-auto object-contain';
});

const linkTarget = computed(() => {
  const link = element.value?.link_url;
  if (!link) return undefined;
  // External links open in a new tab; internal paths stay in the SPA.
  return /^https?:\/\//.test(link) ? '_blank' : undefined;
});

const loadPriority = computed(() =>
  props.slotName === 'header_fullwidth' ? 'eager' : 'lazy'
);

// Light image is hidden in dark mode when a dark variant exists
// (same pattern as category images in CatalogView).
const lightImgClass = computed(() =>
  imgClass.value + (element.value?.image_dark_url ? ' dark:hidden' : '')
);
const darkImgClass = computed(() => imgClass.value + ' hidden dark:block');
</script>

<template>
  <a
    v-if="element && element.link_url"
    :href="element.link_url"
    :target="linkTarget"
    :rel="linkTarget ? 'noopener' : undefined"
    class="block"
  >
    <img
      v-if="element.image_url"
      :src="element.image_url"
      :alt="element.alt_text || ''"
      :class="lightImgClass"
      :loading="loadPriority"
      decoding="async"
    />
    <img
      v-if="element.image_dark_url"
      :src="element.image_dark_url"
      :alt="element.alt_text || ''"
      :class="darkImgClass"
      loading="lazy"
      decoding="async"
    />
  </a>
  <template v-else-if="element">
    <img
      v-if="element.image_url"
      :src="element.image_url"
      :alt="element.alt_text || ''"
      :class="lightImgClass"
      :loading="loadPriority"
      decoding="async"
    />
    <img
      v-if="element.image_dark_url"
      :src="element.image_dark_url"
      :alt="element.alt_text || ''"
      :class="darkImgClass"
      loading="lazy"
      decoding="async"
    />
  </template>
</template>
