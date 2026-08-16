import { ref, watch, onMounted, onBeforeUnmount } from 'vue';

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

function getSavedTheme() {
  try {
    return localStorage.getItem(THEME_KEY) || THEMES.AUTO;
  } catch {
    return THEMES.AUTO;
  }
}

export function useTheme() {
  // Theme is applied to <html> before paint (see index.html), so initial
  // value must match what's already on the DOM to avoid a flash.
  const initial = getSavedTheme();
  const theme = ref(initial);
  const activeTheme = ref(
    initial === THEMES.AUTO ? getSystemTheme() : initial
  );

  const setTheme = (value) => {
    theme.value = value;
    try {
      localStorage.setItem(THEME_KEY, value);
    } catch {
      // ignore (private mode)
    }
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

  let mediaQuery = null;
  onMounted(() => {
    mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    try {
      mediaQuery.addEventListener('change', handleSystemThemeChange);
    } catch (e) {
      // Older browsers
      mediaQuery.addListener(handleSystemThemeChange);
    }
    // Sync in case the user changed system theme while the app was open
    updateActiveTheme();
  });

  onBeforeUnmount(() => {
    if (mediaQuery) {
      try {
        mediaQuery.removeEventListener('change', handleSystemThemeChange);
      } catch (e) {
        mediaQuery.removeListener(handleSystemThemeChange);
      }
    }
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
