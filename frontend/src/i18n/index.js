import { createI18n } from 'vue-i18n';
import ru from './ru.json';
import en from './en.json';
import ua from './ua.json';
import pl from './pl.json';

const SUPPORTED_LOCALES = ['pl', 'ua', 'en', 'ru'];
const DEFAULT_LOCALE_ORDER = ['pl', 'ua', 'en', 'ru'];

// Detect language from URL param (?lang=en) or browser or default order
function getInitialLocale() {
  const params = new URLSearchParams(window.location.search);
  const fromUrl = params.get('lang');
  if (fromUrl && SUPPORTED_LOCALES.includes(fromUrl)) {
    return fromUrl;
  }
  const browser = navigator.language?.toLowerCase()?.slice(0, 2);
  if (browser && SUPPORTED_LOCALES.includes(browser)) {
    return browser;
  }
  // Fallback: first supported locale from default order
  return DEFAULT_LOCALE_ORDER[0];
}

export const i18n = createI18n({
  legacy: false,
  locale: getInitialLocale(),
  fallbackLocale: 'en',
  messages: {
    ru,
    en,
    ua,
    pl,
  },
});

export function setLocale(locale) {
  if (!SUPPORTED_LOCALES.includes(locale)) return;
  i18n.global.locale.value = locale;
  // Update URL without reload
  const params = new URLSearchParams(window.location.search);
  params.set('lang', locale);
  window.history.replaceState({}, '', `${window.location.pathname}?${params.toString()}`);
}

export { SUPPORTED_LOCALES };
