import { useI18n } from 'vue-i18n';

const LOCALE_MAP = {
  ru: 'ru-RU',
  en: 'en-US',
  ua: 'uk-UA',
  pl: 'pl-PL',
};

/**
 * Shared formatters so every view formats prices/dates consistently.
 *
 * Currency: taken from i18n key `scupage.currency` (default 'EUR'),
 * matching what the catalog already does.
 */
export function useFormat() {
  const { t, locale } = useI18n();

  const formatPrice = (price, currency) => {
    const value = Number(price);
    if (!Number.isFinite(value)) return '—';
    const cur = currency || t('scupage.currency', 'EUR');
    const loc = LOCALE_MAP[locale.value] || 'en-US';
    try {
      return new Intl.NumberFormat(loc, {
        style: 'currency',
        currency: cur,
      }).format(value);
    } catch {
      return `${value} ${cur}`;
    }
  };

  const formatDate = (dateStr, withTime = false) => {
    if (!dateStr) return '—';
    const loc = LOCALE_MAP[locale.value] || 'en-US';
    try {
      return new Date(dateStr).toLocaleDateString(loc, {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
        ...(withTime ? { hour: '2-digit', minute: '2-digit' } : {}),
      });
    } catch {
      return String(dateStr);
    }
  };

  return { formatPrice, formatDate };
}
