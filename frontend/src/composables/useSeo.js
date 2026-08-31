import { watchEffect, onUnmounted } from 'vue';
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
 * Inject or update a JSON-LD script tag.
 * Uses the body placeholder if available, otherwise creates a new one.
 *
 * @param {string} type — e.g. "Product", "WebPage", "BreadcrumbList"
 * @param {object} payload — the structured-data payload (will be wrapped in @context)
 * @returns {() => void} cleanup function that removes the script tag
 */
function injectJsonLd(type, payload) {
  if (typeof document === 'undefined') return () => {};

  const id = `dsh-jsonld-${type}`;
  let el = document.getElementById(id);

  // Build the full JSON-LD document
  const ldData = {
    '@context': 'https://schema.org',
    ...payload,
  };

  const json = JSON.stringify(ldData);

  if (!el) {
    // Try to reuse the body placeholder
    el = document.getElementById('dsh-jsonld-body');
    if (el) {
      el.id = id;
      el.textContent = json;
    } else {
      el = document.createElement('script');
      el.id = id;
      el.type = 'application/ld+json';
      document.body.appendChild(el);
      el.textContent = json;
    }
  } else {
    el.textContent = json;
  }

  // Return cleanup
  return () => {
    if (el && el.parentNode) {
      el.parentNode.removeChild(el);
    }
  };
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
 * With structured data:
 *   useSeo({
 *     title: computed(() => ...),
 *     description: computed(() => ...),
 *     jsonLd: computed(() => product.value ? {
 *       '@type': 'Product',
 *       name: product.value.name,
 *       image: product.value.images?.[0],
 *       description: product.value.description,
 *       offers: { '@type': 'Offer', price: product.value.price, ... },
 *       aggregateRating: product.value.avg_rating ? {
 *         '@type': 'AggregateRating',
 *         ratingValue: product.value.avg_rating,
 *         bestRating: 5,
 *         ratingCount: product.value.review_count,
 *       } : undefined,
 *     } : undefined),
 *   });
 *
 * Updates: <title>, meta[description], og:*, twitter:*, canonical, JSON-LD.
 */
export function useSeo({ title, description, image, jsonLd } = {}) {
  const { t } = useI18n();
  let ldInjected = false;
  let ldCleanup = null;

  watchEffect((onCleanup) => {
    if (typeof document === 'undefined') return;

    const titleVal = title?.value ?? t('pages.default_title');
    const descVal = description?.value ?? t('pages.default_description');
    const imgVal = image?.value || null;
    const ldVal = jsonLd?.value;

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

    // JSON-LD structured data — inject immediately with placeholder, then update
    if (ldVal && typeof ldVal === 'object') {
      const type = ldVal['@type'] || 'Thing';
      if (ldInjected && ldCleanup) ldCleanup();
      ldCleanup = injectJsonLd(type, ldVal);
      ldInjected = true;
    } else if (ldInjected && !ldCleanup) {
      // First render: inject placeholder immediately
      const type = 'Thing';
      const placeholder = { '@context': 'https://schema.org', '@type': type };
      ldCleanup = injectJsonLd(type, placeholder);
      ldInjected = true;
    } else if (ldVal === undefined && ldInjected && ldCleanup) {
      // No data — keep placeholder
    } else if (ldVal === undefined && ldInjected && ldCleanup) {
      // No data — remove the script tag
      ldCleanup();
      ldCleanup = null;
      ldInjected = false;
    }

    onCleanup(() => {
      if (ldCleanup) {
        ldCleanup();
        ldCleanup = null;
        ldInjected = false;
      }
    });
  });
}
