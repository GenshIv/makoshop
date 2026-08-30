import { ref, computed, watch } from 'vue';

const CONSENT_KEY = 'cookie_consent';
const COOKIE_NAME = 'cookie_consent';

const DEFAULT_CONSENT = {
  essential: true,    // always enabled
  analytics: false,   // Google Analytics, etc.
  marketing: false,   // retargeting, ads
  acceptedAt: null,
};

function getConsentFromStorage() {
  try {
    const stored = localStorage.getItem(CONSENT_KEY);
    if (stored) {
      return JSON.parse(stored);
    }
  } catch (e) {
    console.warn('Failed to read consent from localStorage:', e);
  }
  return null;
}

function getConsentFromCookie() {
  try {
    const cookies = document.cookie.split(';');
    for (const cookie of cookies) {
      const idx = cookie.indexOf(`${COOKIE_NAME}=`);
      if (idx !== -1 && cookie.slice(0, idx).trim() === '') {
        return JSON.parse(decodeURIComponent(cookie.slice(idx + COOKIE_NAME.length + 1)));
      }
    }
  } catch (e) {
    console.warn('Failed to read consent from cookie:', e);
  }
  return null;
}

// Read consent from localStorage first, falling back to the cookie so the
// choice survives even if one storage mechanism is cleared or blocked.
function getConsent() {
  return getConsentFromStorage() || getConsentFromCookie();
}

function setCookie(name, value, days) {
  const expires = new Date();
  expires.setTime(expires.getTime() + days * 24 * 60 * 60 * 1000);
  document.cookie = `${name}=${encodeURIComponent(value)};expires=${expires.toUTCString()};path=/;SameSite=Lax`;
}

function saveConsent(consent) {
  try {
    localStorage.setItem(CONSENT_KEY, JSON.stringify(consent));
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
  // Re-persist an existing choice to both localStorage and the cookie so the
  // two stay in sync and the consent isn't lost if one gets cleared.
  if (consent.value.acceptedAt) {
    saveConsent(consent.value);
  }
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

  const clearConsent = () => {
    consent.value = { ...DEFAULT_CONSENT };
    try {
      localStorage.removeItem(CONSENT_KEY);
      document.cookie = `${COOKIE_NAME}=;expires=Thu, 01 Jan 1970 00:00:00 UTC;path=/;`;
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
    clearConsent,
  };
}

// Initialize singleton
_instance = useCookieConsent();

// Callback for analytics/marketing scripts
// Override this function in your app to initialize analytics when consent is given
export let onConsentChange = (consent) => {
  console.log('[CookieConsent] Consent changed:', consent);

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
  console.log('[CookieConsent] Analytics initialized');
  // Example: Google Analytics
  // window.gtag('consent', 'update', { analytics_storage: 'granted' });
}

export function initMarketing() {
  console.log('[CookieConsent] Marketing initialized');
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
