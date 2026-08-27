import { useI18n } from 'vue-i18n';
import { useSettings } from './useSettings';

const LOCALE_MAP = {
  ru: 'ru-RU',
  en: 'en-US',
  ua: 'uk-UA',
  pl: 'pl-PL',
};

/**
 * Shared formatters so every view formats prices/dates consistently.
 *
 * Currency: taken from global settings (default 'PLN'),
 * or from the product's currency if provided.
 */
export function useFormat() {
  const { locale } = useI18n();
  const { defaultCurrency } = useSettings();

  const formatPrice = (price, currency) => {
    const value = Number(price);
    if (!Number.isFinite(value)) return '—';
    const cur = currency || defaultCurrency.value || 'PLN';
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
