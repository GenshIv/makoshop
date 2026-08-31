import { watch } from 'vue';
import router from './router';
import api from './api';
import { useCookieConsent, isAnalyticsAllowed } from './composables/useCookieConsent';

// ============================================================================
// Google Analytics (GA4) integration.
//
// The Measurement ID is NOT hardcoded here — it's fetched at runtime from the
// public settings endpoint so it can be configured in the admin UI without a
// rebuild.
//
// The tag ALWAYS loads whenever a Measurement ID is configured. What Google
// actually receives is controlled by Consent Mode, driven by our unified
// cookie_consent store (the same one behind the banner):
//   - analytics consent granted -> full data incl. user identifiers
//   - analytics consent denied  -> limited, non-identifiable "general" data
// This keeps the analytics setting tied to our own persistent consent rather
// than to any separate Google-side state.
//
// This is a single-page app, so page views are sent manually on every route
// change (Consent Mode gates transmission). Only public storefront pages are
// tracked; back-office and account areas (/admin, /seller, auth, ...) are
// excluded so staff activity doesn't pollute visitor analytics.
// ============================================================================

const GA_SETTINGS_ENDPOINT = '/admin/settings';

// Path prefixes that must never be sent to Google (staff + authenticated areas).
const EXCLUDED_PATH_PREFIXES = [
  '/admin',
  '/seller',
  '/login',
  '/register',
  '/profile',
  '/orders',
  '/reviews',
  '/checkout',
];

let gaMeasurementId = ''; // fetched from server; empty string disables GA
let gtagReady = false; // true once the gtag script is loaded & configured
let consentGranted = false; // current analytics consent state
let lastTrackedPath = null; // dedupe guard for SPA page views

function isPublicPage(path) {
  const clean = (path || window.location.pathname || '/').split('?')[0].split('#')[0];
  return !EXCLUDED_PATH_PREFIXES.some(
    (prefix) => clean === prefix || clean.startsWith(prefix + '/')
  );
}

// Update GA's consent state (used after the tag has already loaded).
// The source of truth for analytics consent is our unified cookie_consent store;
// this only propagates that choice to Google's Consent Mode.
function updateConsent(granted) {
  consentGranted = granted;
  if (!gtagReady) return;
  const s = granted ? 'granted' : 'denied';
  window.gtag('consent', 'update', {
    analytics_storage: s,
    ad_storage: 'denied',
    ad_user_data: 'denied',
    ad_personalization: 'denied',
  });
}

// Inject and configure the GA4 loader. The tag ALWAYS loads (when a Measurement
// ID is configured) so that Consent Mode can manage data transmission. It reads
// the current consentGranted state to establish the initial consent default.
function loadGATag() {
  if (gtagReady || !gaMeasurementId) return;
  gtagReady = true;

  const script = document.createElement('script');
  script.async = true;
  script.src = `https://www.googletagmanager.com/gtag/js?id=${encodeURIComponent(gaMeasurementId)}`;
  document.head.appendChild(script);

  window.dataLayer = window.dataLayer || [];
  window.gtag = function gtag() {
    window.dataLayer.push(arguments);
  };
  window.gtag('js', new Date());

  const s = consentGranted ? 'granted' : 'denied';
  window.gtag('consent', 'default', {
    analytics_storage: s,
    ad_storage: 'denied',
    ad_user_data: 'denied',
    ad_personalization: 'denied',
  });

  // Page views are managed manually for correct SPA behavior.
  window.gtag('config', gaMeasurementId, { send_page_view: false });
}

// Send a page_view for the given path. The event is ALWAYS fired (when the tag
// is loaded) — Google's Consent Mode decides what actually leaves the browser:
//   - analytics consent granted  -> full data incl. client ID / user identifiers
//   - analytics consent denied   -> limited, non-identifiable "general" data only
// So we do not gate on consentGranted here; gating happens at the tag level.
function sendPageView(path) {
  if (!gtagReady || !gaMeasurementId) return;
  if (!isPublicPage(path)) {
    lastTrackedPath = null; // reset so re-entering a public page is tracked again
    return;
  }
  if (path === lastTrackedPath) return; // avoid duplicate hits on the same route
  lastTrackedPath = path;
  window.gtag('event', 'page_view', { page_path: path });
}

// React to consent changes (e.g. visitor accepts or revokes via the banner).
// The tag is always loaded once configured; consent changes only flip the
// Consent Mode state so Google starts/stops receiving identifiable data.
const { hasAnalytics } = useCookieConsent();
watch(hasAnalytics, (allowed) => {
  if (!gaMeasurementId) return; // config not loaded yet; handled in init below
  if (!gtagReady) {
    consentGranted = allowed;
    loadGATag(); // establishes the initial consent default
  } else {
    updateConsent(allowed);
  }
  sendPageView(window.location.pathname);
});

// React to SPA route changes so each navigation is recorded as a page view.
router.afterEach((to) => {
  sendPageView(to.fullPath);
});

// Initial setup: fetch the configured Measurement ID, then ALWAYS start the tag.
// The analytics consent (from our unified cookie_consent store) only controls
// whether Google receives identifiable data via Consent Mode — never whether
// the tag runs at all.
(async () => {
  try {
    const res = await api.get(GA_SETTINGS_ENDPOINT);
    gaMeasurementId = (res.data?.ga_measurement_id || '').trim();
  } catch (e) {
    // Non-fatal: GA simply stays disabled.
    gaMeasurementId = '';
  }

  if (gaMeasurementId) {
    consentGranted = isAnalyticsAllowed();
    loadGATag();
    sendPageView(window.location.pathname);
  }
})();

// Public helpers for future event tracking (kept for API stability).
// Fired regardless of consent — Consent Mode controls what Google receives.
export function trackEvent(name, params = {}) {
  if (!window.gtag || !gtagReady) return;
  window.gtag('event', name, params);
}

export { trackEvent as gtag };
