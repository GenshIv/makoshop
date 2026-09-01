// Client-side branding resolution.
// Data comes from GET /branding/active (enabled sets + category overrides +
// version). Resolution priority (per docs/BRANDING_SYSTEM_PLAN.md):
//   1. Category override (deepest category in the chain first)
//   2. Exact regex match — highest-priority enabled set wins
//   3. Default element (no page patterns) — highest priority wins
//   4. Slot not rendered
import { computed, onMounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useBrandingStore } from '../stores/branding';
import { elementAppliesToPath, resolveSlot } from './brandingResolve';

export { elementAppliesToPath, resolveSlot };

export function useBranding() {
  const store = useBrandingStore();
  const route = useRoute();

  // Fetch on mount (TTL-guarded) and re-check on navigation when stale.
  onMounted(() => {
    store.fetchActive();
  });
  watch(
    () => route.path,
    () => {
      if (store.isStale()) store.fetchActive();
    }
  );

  function useSlotElement(slot) {
    return computed(() =>
      resolveSlot(
        store.sets,
        store.categoryOverrides,
        store.categoryChain,
        slot,
        route.path
      )
    );
  }

  return { store, useSlotElement, refresh: () => store.invalidate() };
}
