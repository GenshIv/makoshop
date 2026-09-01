import { ref, onMounted } from 'vue';
import api from '../api';

const defaultCurrency = ref('PLN');
// Manual hero text overrides for the main page:
// { ru: {headline, sub, tagline}, ua: {...}, en: {...}, pl: {...} }
// Empty/missing fields mean "use the default i18n text".
const homeHero = ref({});
const loaded = ref(false);

export function useSettings() {
  const loadSettings = async () => {
    if (loaded.value) return;
    try {
      const res = await api.get('/admin/settings');
      defaultCurrency.value = res.data.default_currency || 'PLN';
      homeHero.value = res.data.home_hero || {};
    } catch (e) {
      console.error('Failed to load settings:', e);
    } finally {
      loaded.value = true;
    }
  };

  onMounted(() => {
    loadSettings();
  });

  return {
    defaultCurrency,
    homeHero,
    loadSettings,
  };
}
