// Analytics integration module
// Override functions from useCookieConsent to initialize analytics when consent is given

import { onConsentChange, initAnalytics, initMarketing } from './composables/useCookieConsent';

// Google Analytics configuration
const GA_MEASUREMENT_ID = 'G-XXXXXXXXXX'; // TODO: Replace with your GA4 measurement ID

let gaInitialized = false;
let gtagScriptLoaded = false;

// Load Google Analytics script
function loadGATag() {
  if (gtagScriptLoaded) return;
  gtagScriptLoaded = true;

  const script = document.createElement('script');
  script.async = true;
  script.src = `https://www.googletagmanager.com/gtag/js?id=${GA_MEASUREMENT_ID}`;
  document.head.appendChild(script);

  window.dataLayer = window.dataLayer || [];
  window.gtag = function gtag() {
    window.dataLayer.push(arguments);
  };
  window.gtag('js', new Date());
  window.gtag('config', GA_MEASUREMENT_ID, {
    send_page_view: true,
  });
}

// Facebook Pixel configuration (optional)
const FB_PIXEL_ID = 'YOUR_PIXEL_ID'; // TODO: Replace with your Pixel ID

function loadFBPixel() {
  if (window.fbq) return;

  window.fbq = function fbq() {
    window.fbq.callMethod
      ? window.fbq.callMethod.apply(window.fbq, arguments)
      : window.fbq.queue.push(arguments);
  };
  window.fbq.loaded = true;
  window.fbq.callMethod = 'queue';
  window.fbq.queue = [];

  const script = document.createElement('script');
  script.async = true;
  script.src = `https://connect.facebook.net/en_US/fbevents.js`;
  document.head.appendChild(script);

  window.fbq('init', FB_PIXEL_ID);
  window.fbq('track', 'PageView');
}

// Override initAnalytics
window.initAnalytics = function () {
  if (gaInitialized) return;
  gaInitialized = true;
  console.log('[Analytics] Initializing Google Analytics...');
  loadGATag();
};

// Override initMarketing
window.initMarketing = function () {
  console.log('[Marketing] Initializing marketing scripts...');
  // loadFBPixel(); // Uncomment when ready
};

// Export a helper to track events (respects consent)
export function trackEvent(name, params = {}) {
  if (!window.gtag) {
    console.warn('[Analytics] GA not loaded (no consent or not initialized)');
    return;
  }
  window.gtag('event', name, params);
}

export function trackFBEvent(eventName, params = {}) {
  if (!window.fbq) {
    console.warn('[Marketing] FB Pixel not loaded (no consent or not initialized)');
    return;
  }
  window.fbq('track', eventName, params);
}

// Export for use in components
export { trackEvent as gtag };
