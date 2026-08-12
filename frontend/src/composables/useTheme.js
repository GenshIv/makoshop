import { ref, watch, onMounted } from 'vue';

const THEME_KEY = 'makoshop_theme';
const THEMES = {
  LIGHT: 'light',
  DARK: 'dark',
  AUTO: 'auto',
};

function getSystemTheme() {
  if (typeof window === 'undefined') return THEMES.LIGHT;
  return window.matchMedia('(prefers-color-scheme: dark)').matches
    ? THEMES.DARK
    : THEMES.LIGHT;
}

function applyTheme(activeTheme) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.classList.remove('theme-light', 'theme-dark');
  root.classList.add(`theme-${activeTheme}`);
}

export function useTheme() {
  const theme = ref(THEMES.LIGHT);
  const activeTheme = ref(THEMES.LIGHT);

  const setTheme = (value) => {
    theme.value = value;
    localStorage.setItem(THEME_KEY, value);
    updateActiveTheme();
  };

  const updateActiveTheme = () => {
    if (theme.value === THEMES.AUTO) {
      activeTheme.value = getSystemTheme();
    } else {
      activeTheme.value = theme.value;
    }
    applyTheme(activeTheme.value);
  };

  // Watch system theme changes when in auto mode
  const handleSystemThemeChange = (e) => {
    if (theme.value === THEMES.AUTO) {
      activeTheme.value = e.matches ? THEMES.DARK : THEMES.LIGHT;
      applyTheme(activeTheme.value);
    }
  };

  onMounted(() => {
    // Load saved theme or default to auto
    const saved = localStorage.getItem(THEME_KEY);
    theme.value = saved || THEMES.AUTO;

    // Listen to system theme changes
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    try {
      mediaQuery.addEventListener('change', handleSystemThemeChange);
    } catch (e) {
      // Older browsers
      mediaQuery.addListener(handleSystemThemeChange);
    }

    updateActiveTheme();
  });

  watch(theme, () => {
    updateActiveTheme();
  });

  return {
    theme,
    activeTheme,
    setTheme,
    THEMES,
  };
}
