import { defineStore } from 'pinia';
import api from '../api';

// How long the cached branding data is considered fresh. After this the
// next fetch (e.g. on route change) re-validates against the server version.
const BRANDING_TTL_MS = 60_000;

// Branding: page decoration system (banners for different occasions).
// The store holds the ACTIVE data (enabled sets + category overrides)
// fetched from GET /branding/active. Slot resolution (regex matching,
// priorities, category overrides) lives in composables/useBranding.js.
export const useBrandingStore = defineStore('branding', {
  state: () => ({
    version: 0,
    sets: [], // enabled brand sets (already filtered server-side)
    categoryOverrides: [], // per-category slot overrides
    loaded: false,
    loading: false,
    lastFetchAt: 0,
    // Chain of category IDs (root -> current) set by CatalogView while
    // browsing a category. Used for per-section image overrides.
    categoryChain: [],
  }),

  actions: {
    isStale() {
      return !this.loaded || Date.now() - this.lastFetchAt > BRANDING_TTL_MS;
    },

    async fetchActive(force = false) {
      if (this.loading) return;
      if (!force && this.loaded && !this.isStale()) return;
      this.loading = true;
      try {
        const res = await api.get('/branding/active');
        const data = res.data || {};
        // Server-side version unchanged — data is identical, keep the cache.
        if (this.loaded && data.version === this.version) {
          this.lastFetchAt = Date.now();
          return;
        }
        this.version = data.version || 0;
        this.sets = data.sets || [];
        this.categoryOverrides = data.category_overrides || [];
        this.loaded = true;
        this.lastFetchAt = Date.now();
      } catch (e) {
        // Branding is decorative — never break the page because of it.
        console.error('Failed to load branding:', e);
      } finally {
        this.loading = false;
      }
    },

    // Called by the admin panel after saving changes: the server version has
    // changed, so force a re-fetch immediately.
    async invalidate() {
      this.version = -1;
      await this.fetchActive(true);
    },

    // CatalogView sets the current category chain (root -> current) so the
    // resolution can apply per-section overrides. Pass [] to clear.
    setCategoryChain(ids) {
      this.categoryChain = Array.isArray(ids) ? ids : [];
    },
  },
});
