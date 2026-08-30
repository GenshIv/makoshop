import { watch } from 'vue';
import router from './router';
import api from './api';
import { useCookieConsent, isAnalyticsAllowed } from './composables/useCookieConsent';

// ============================================================================
// Google Analytics (GA4) integration.
//
// The Measurement ID is NOT hardcoded here — it's fetched at runtime from the
// public settings endpoint so it can be configured in the admin UI without a
// rebuild. Tracking only happens when BOTH conditions are true:
//   1. A Measurement ID has been configured by an admin.
//   2. The visitor has granted "analytics" cookie consent.
//
// Consent Mode is used so that data collection is gated on the visitor's choice
// and can be revoked (not just granted). This is a single-page app, so page
// views are sent manually on every route change. Only public storefront pages
// are tracked; back-office and account areas (/admin, /seller, auth, ...) are
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

// Inject and configure the GA4 loader. Reads the current consentGranted state
// (set by the caller) to establish the initial consent default.
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

// Send a page_view for the given path, honoring all gating conditions.
function sendPageView(path) {
  if (!gtagReady || !gaMeasurementId || !consentGranted) return;
  if (!isPublicPage(path)) {
    lastTrackedPath = null; // reset so re-entering a public page is tracked again
    return;
  }
  if (path === lastTrackedPath) return; // avoid duplicate hits on the same route
  lastTrackedPath = path;
  window.gtag('event', 'page_view', { page_path: path });
}

// React to consent changes (e.g. visitor accepts or revokes via the banner).
const { hasAnalytics } = useCookieConsent();
watch(hasAnalytics, (allowed) => {
  if (!gaMeasurementId) return; // config not loaded yet; handled in init below
  if (!gtagReady) {
    consentGranted = allowed;
    if (allowed) loadGATag(); // establishes the initial consent default
  } else {
    updateConsent(allowed);
  }
  if (allowed) sendPageView(window.location.pathname);
});

// React to SPA route changes so each navigation is recorded as a page view.
router.afterEach((to) => {
  sendPageView(to.fullPath);
});

// Initial setup: fetch the configured Measurement ID, then start if permitted.
(async () => {
  try {
    const res = await api.get(GA_SETTINGS_ENDPOINT);
    gaMeasurementId = (res.data?.ga_measurement_id || '').trim();
  } catch (e) {
    // Non-fatal: GA simply stays disabled.
    gaMeasurementId = '';
  }

  if (gaMeasurementId && isAnalyticsAllowed()) {
    consentGranted = true;
    loadGATag();
    sendPageView(window.location.pathname);
  }
})();

// Public helpers for future event tracking (kept for API stability).
export function trackEvent(name, params = {}) {
  if (!window.gtag || !gtagReady || !consentGranted) return;
  window.gtag('event', name, params);
}

export { trackEvent as gtag };
