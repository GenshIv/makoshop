import { ref, computed, watch } from 'vue';

const CONSENT_KEY = 'cookie_consent';
const SESSION_KEY = 'cookie_consent_session';
const COOKIE_NAME = 'cookie_consent';

const DEFAULT_CONSENT = {
  essential: true,    // always enabled
  analytics: false,   // Google Analytics, etc.
  marketing: false,   // retargeting, ads
  acceptedAt: null,
};

function getConsentFromSessionStorage() {
  try {
    const stored = sessionStorage.getItem(SESSION_KEY);
    if (stored) {
      return JSON.parse(stored);
    }
  } catch (e) {
    console.warn('Failed to read consent from sessionStorage:', e);
  }
  return null;
}

function getConsentFromCookie() {
  try {
    // Match cookie with proper URL decoding
    const match = document.cookie.match(
      new RegExp('(?:^|;\\s*)' + COOKIE_NAME + '=([^;]*)')
    );
    if (match) {
      let raw = decodeURIComponent(match[1]);
      // Strip invisible Unicode characters that break JSON parsing:
      // \u00A0 (non-breaking space), \u200B (zero-width space),
      // \uFEFF (BOM), \u2000-\u200A (various spaces), \u2028/\u2029
      raw = raw
        .replace(/\u00A0/g, ' ')
        .replace(/\u200B/g, '')
        .replace(/\uFEFF/g, '')
        .replace(/[\u2000-\u200A]/g, '')
        .replace(/\u2028/g, '\n')
        .replace(/\u2029/g, '\n');
      return JSON.parse(raw);
    }
  } catch (e) {
    console.warn('Failed to read consent from cookie:', e);
  }
  return null;
}

// Read consent: cookie is the source of truth (survives ITP/EPSS sessionStorage clearing),
// sessionStorage is a fallback (survives page reloads).
function getConsent() {
  const fromCookie = getConsentFromCookie();
  const fromSession = getConsentFromSessionStorage();

  // Sync sessionStorage from cookie (cookie is the authoritative source)
  if (fromCookie) {
    try {
      sessionStorage.setItem(SESSION_KEY, JSON.stringify(fromCookie));
    } catch (e) {
      console.warn('[CookieConsent] Failed to re-sync sessionStorage:', e);
    }
  }

  // Cookie > sessionStorage > defaults
  return fromSession || fromCookie;
}

function setCookie(name, value, days) {
  const expires = new Date();
  expires.setTime(expires.getTime() + days * 24 * 60 * 60 * 1000);
  // Secure + SameSite=None ensures the cookie is sent on all requests
  // (HTTPS required for SameSite=None per modern browser specs)
  // Only use Secure on HTTPS to avoid cookie not being sent on HTTP
  const secure = location.protocol === 'https:' ? ';Secure' : '';
  document.cookie = `${name}=${encodeURIComponent(value)};expires=${expires.toUTCString()};path=/;SameSite=None${secure}`;
}

function saveConsent(consent) {
  try {
    sessionStorage.setItem(SESSION_KEY, JSON.stringify(consent));
    setCookie(COOKIE_NAME, JSON.stringify(consent), 365);
  } catch (e) {
    console.warn('Failed to save consent:', e);
  }
}

// Singleton instance to share state across all components
let _instance = null;

export function useCookieConsent() {
  if (_instance) return _instance;

  const consent = ref(getConsent() || { ...DEFAULT_CONSENT });
  const showBanner = ref(!consent.value.acceptedAt);

  const hasAnalytics = computed(() => consent.value.analytics);
  const hasMarketing = computed(() => consent.value.marketing);

  const acceptAll = () => {
    consent.value = {
      essential: true,
      analytics: true,
      marketing: true,
      acceptedAt: new Date().toISOString(),
    };
    saveConsent(consent.value);
    showBanner.value = false;
    onConsentChange(consent.value);
  };

  const acceptEssential = () => {
    consent.value = {
      essential: true,
      analytics: false,
      marketing: false,
      acceptedAt: new Date().toISOString(),
    };
    saveConsent(consent.value);
    showBanner.value = false;
    onConsentChange(consent.value);
  };

  const saveCustom = () => {
    consent.value.acceptedAt = new Date().toISOString();
    saveConsent(consent.value);
    showBanner.value = false;
    onConsentChange(consent.value);
  };

  const showSettings = () => {
    showBanner.value = true;
  };

  const closeBanner = () => {
    // Just close the banner without saving — preserves current consent values
    showBanner.value = false;
  };

  const clearConsent = () => {
    consent.value = { ...DEFAULT_CONSENT };
    try {
      sessionStorage.removeItem(SESSION_KEY);
      const secure = location.protocol === 'https:' ? ';Secure' : '';
      document.cookie = `${COOKIE_NAME}=;expires=Thu, 01 Jan 1970 00:00:00 UTC;path=/;SameSite=None${secure}`;
    } catch (e) {
      console.warn('Failed to clear consent:', e);
    }
    onConsentChange(consent.value);
  };

  // Hook for analytics/marketing initialization
  watch([hasAnalytics, hasMarketing], ([analytics, marketing]) => {
    onConsentChange(consent.value);
  });

  return {
    consent,
    showBanner,
    hasAnalytics,
    hasMarketing,
    acceptAll,
    acceptEssential,
    saveCustom,
    showSettings,
    closeBanner,
    clearConsent,
  };
}

// Initialize singleton
_instance = useCookieConsent();

// Callback for analytics/marketing scripts
// Override this function in your app to initialize analytics when consent is given
export let onConsentChange = (consent) => {

  if (consent.analytics && !window.__analyticsInitialized) {
    window.__analyticsInitialized = true;
    initAnalytics();
  }

  if (consent.marketing && !window.__marketingInitialized) {
    window.__marketingInitialized = true;
    initMarketing();
  }
};

// Override these functions in your app to integrate with analytics/marketing services
export function initAnalytics() {
  // Example: Google Analytics
  // window.gtag('consent', 'update', { analytics_storage: 'granted' });
}

export function initMarketing() {
  // Example: Facebook Pixel, etc.
}

// Check if analytics is allowed (for use in components)
export function isAnalyticsAllowed() {
  const consent = getConsent();
  return consent?.analytics === true;
}

// Check if marketing is allowed
export function isMarketingAllowed() {
  const consent = getConsent();
  return consent?.marketing === true;
}
