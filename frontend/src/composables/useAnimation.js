import { ref, watch, onMounted } from 'vue';

const ANIMATION_KEY = 'makoshop_animation_enabled';

function getSavedAnimationEnabled() {
  try {
    const val = localStorage.getItem(ANIMATION_KEY);
    return val !== null ? val === 'true' : true; // default: enabled
  } catch {
    return true;
  }
}

function applyAnimationClass(enabled) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.classList.toggle('no-animation', !enabled);
}

// Singleton ref shared across all components
const animationEnabled = ref(getSavedAnimationEnabled());

// Apply class on first mount (App.vue)
let initialized = false;

export function useAnimation() {
  const setAnimationEnabled = (value) => {
    animationEnabled.value = value;
    try {
      localStorage.setItem(ANIMATION_KEY, String(value));
    } catch {
      // ignore (private mode)
    }
    applyAnimationClass(value);
  };

  onMounted(() => {
    if (!initialized) {
      applyAnimationClass(animationEnabled.value);
      initialized = true;
    }
  });

  // Watch for changes to sync the DOM class
  watch(animationEnabled, (enabled) => {
    applyAnimationClass(enabled);
  });

  return {
    animationEnabled,
    setAnimationEnabled,
  };
}
