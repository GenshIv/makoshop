import { ref, onMounted } from 'vue';
import api from '../api';

const defaultCurrency = ref('PLN');
const loaded = ref(false);

export function useSettings() {
  const loadSettings = async () => {
    if (loaded.value) return;
    try {
      const res = await api.get('/admin/settings');
      defaultCurrency.value = res.data.default_currency || 'PLN';
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
    loadSettings,
  };
}
