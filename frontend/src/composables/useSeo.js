import { watchEffect } from 'vue';
import { useI18n } from 'vue-i18n';

const SITE_NAME = 'wszyst.pl';

function upsertMeta(attr, key, content) {
  if (typeof document === 'undefined') return;
  let el = document.head.querySelector(
    attr === 'name' ? `meta[name="${key}"]` : `meta[property="${key}"]`
  );
  if (!el) {
    el = document.createElement('meta');
    el.setAttribute(attr, key);
    document.head.appendChild(el);
  }
  el.setAttribute('content', content);
}

function upsertCanonical(href) {
  if (typeof document === 'undefined' || !href) return;
  let el = document.head.querySelector('link[rel="canonical"]');
  if (!el) {
    el = document.createElement('link');
    el.setAttribute('rel', 'canonical');
    document.head.appendChild(el);
  }
  el.setAttribute('href', href);
}

/**
 * Reactive SEO helper.
 *
 * Usage:
 *   const { t } = useI18n();
 *   useSeo({
 *     title: computed(() => product.value?.name ? `${product.value.name} — SITE_NAME` : t('pages.product_title')),
 *     description: computed(() => product.value?.description || t('pages.default_description')),
 *     image: computed(() => product.value?.images?.[0] || null),
 *   });
 *
 * Updates: <title>, meta[description], og:*, twitter:*, canonical.
 */
export function useSeo({ title, description, image } = {}) {
  const { t } = useI18n();

  watchEffect(() => {
    if (typeof document === 'undefined') return;

    const titleVal = title?.value ?? t('pages.default_title');
    const descVal = description?.value ?? t('pages.default_description');
    const imgVal = image?.value || null;

    document.title = titleVal;

    upsertMeta('name', 'description', descVal);

    // Open Graph
    upsertMeta('property', 'og:site_name', SITE_NAME);
    upsertMeta('property', 'og:title', titleVal);
    upsertMeta('property', 'og:description', descVal);
    upsertMeta('property', 'og:type', 'website');
    if (imgVal) upsertMeta('property', 'og:image', imgVal);

    // Twitter Card
    upsertMeta('name', 'twitter:card', imgVal ? 'summary_large_image' : 'summary');
    upsertMeta('name', 'twitter:title', titleVal);
    upsertMeta('name', 'twitter:description', descVal);
    if (imgVal) upsertMeta('name', 'twitter:image', imgVal);

    // Canonical
    upsertCanonical(window.location.href);
  });
}
