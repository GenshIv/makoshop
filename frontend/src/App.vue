<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from './stores/auth';
import { useCartStore } from './stores/cart';
import CategoryTree from './components/CategoryTree.vue';
import CookieConsentBanner from './components/CookieConsentBanner.vue';
import ShardUsageBar from './components/ShardUsageBar.vue';
import { useCookieConsent } from './composables/useCookieConsent';
import { useTheme } from './composables/useTheme';
import { useSeo } from './composables/useSeo';
import { setLocale, SUPPORTED_LOCALES } from './i18n';

const { showSettings } = useCookieConsent();
const { theme, setTheme, THEMES } = useTheme();
const { t, locale } = useI18n();

const langLabels = {
  ru: 'RU',
  en: 'EN',
  ua: 'UA',
  pl: 'PL',
};

const showLangMenu = ref(false);

const switchLang = (lang) => {
  setLocale(lang);
  showLangMenu.value = false;
};

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();
const cart = useCartStore();

const mobileMenuOpen = ref(false);
const categoriesSidebarOpen = ref(false);

const isAuthenticated = computed(() => auth.isAuthenticated);
const userRole = computed(() => {
  const role = auth.user?.role;
  if (!role) return '';
  if (role === 'admin') return 'Admin';
  if (role === 'seller') return 'Seller';
  return '';
});

// SEO page titles (computed to react to locale changes).
// Product and SCU pages override title/description from their own data
// via useSeo() in the respective views, so we skip them here.
const pageTitles = computed(() => ({
  catalog: t('pages.catalog_title'),
  cart: t('pages.cart_title'),
  checkout: t('pages.checkout_title'),
  login: t('pages.login_title'),
  register: t('pages.register_title'),
  profile: t('pages.profile_title'),
  orders: t('pages.orders_title'),
  'order-detail': t('pages.order_detail_title'),
  reviews: t('pages.reviews_title'),
  'seller-dashboard': t('pages.seller_dashboard_title'),
  'seller-products': t('pages.seller_products_title'),
  'seller-product-new': t('pages.seller_product_new_title'),
  'seller-product-edit': t('pages.seller_product_edit_title'),
  'seller-orders': t('pages.seller_orders_title'),
  'seller-promo': t('pages.seller_promo_title'),
  'admin-dashboard': t('pages.admin_dashboard_title'),
  'admin-users': t('pages.admin_users_title'),
  'admin-companies': t('pages.admin_companies_title'),
  'admin-categories': t('pages.admin_categories_title'),
  'admin-analytics': t('pages.admin_analytics_title'),
  'admin-promo': t('pages.admin_promo_title'),
}));

useSeo({
  title: computed(() => pageTitles.value[route.name] || t('pages.default_title')),
});

const handleLogout = () => {
  auth.logout();
  router.push({ name: 'catalog' });
};

const goToCart = () => {
  router.push({ name: 'cart' });
};

// Close mobile menu on route change
watch(() => route.fullPath, () => {
  mobileMenuOpen.value = false;
});

// Lock body scroll while a mobile overlay is open
const anyOverlayOpen = computed(
  () => mobileMenuOpen.value || categoriesSidebarOpen.value
);
watch(
  anyOverlayOpen,
  (open) => {
    if (typeof document === 'undefined') return;
    document.body.style.overflow = open ? 'hidden' : '';
  },
  { immediate: true }
);
onBeforeUnmount(() => {
  if (typeof document !== 'undefined') document.body.style.overflow = '';
});

// Close language dropdown on outside click
const langMenuRef = ref(null);
const onDocClick = (e) => {
  if (showLangMenu.value && langMenuRef.value && !langMenuRef.value.contains(e.target)) {
    showLangMenu.value = false;
  }
};
watch(showLangMenu, (v) => {
  if (typeof document === 'undefined') return;
  if (v) document.addEventListener('click', onDocClick);
  else document.removeEventListener('click', onDocClick);
});
onBeforeUnmount(() => {
  if (typeof document !== 'undefined') document.removeEventListener('click', onDocClick);
});
</script>

<template>
  <div class="min-h-screen flex flex-col bg-surface-2/40">
    <!-- Header -->
    <header class="bg-surface shadow-sm border-b border-line sticky top-0 z-30">
      <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div class="flex items-center justify-between h-16 gap-4">
          <!-- Left: Logo + mobile buttons -->
          <div class="flex items-center gap-3">
            <!-- Mobile menu button -->
            <button
              @click="mobileMenuOpen = !mobileMenuOpen"
              class="lg:hidden p-2 rounded-lg text-ink-2 hover:bg-surface-2"
            :aria-label="t('common.menu')"
          >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>

            <!-- Categories button (mobile) -->
            <button
              @click="categoriesSidebarOpen = true"
              class="lg:hidden px-3 py-1.5 bg-surface-2 text-ink-2 rounded-lg text-sm hover:bg-surface-3 flex items-center gap-1"
              :aria-label="t('common.categories')"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h10M4 18h6" />
              </svg>
              {{ t('common.categories') }}
            </button>

            <!-- Logo -->
            <router-link to="/" class="flex items-center gap-2">
              <img
                src="/koshik.png"
                alt="MakoShop"
                class="h-8 w-auto"
              />
              <span class="text-xl font-bold text-indigo-600 hover:text-indigo-700 whitespace-nowrap">
                MakoShop
              </span>
            </router-link>
          </div>

          <!-- Search bar (hidden on very small screens) -->
          <form
            @submit.prevent="$router.push({ name: 'catalog', query: { q: $refs.search?.value } })"
            class="flex-1 max-w-xl hidden sm:block"
          >
            <input
              ref="search"
              type="text"
              :placeholder="t('common.search_placeholder')"
              class="w-full px-4 py-2 border border-line rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm"
            />
          </form>

          <!-- Right: Nav links -->
          <nav class="flex items-center gap-2 sm:gap-3">
            <!-- Cart -->
            <button @click="goToCart" class="relative p-2 text-ink-2 hover:text-indigo-600 hover:bg-surface-2 rounded-lg" :aria-label="t('common.cart')">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z" />
              </svg>
              <span v-if="cart.totalCount > 0" class="absolute -top-1 -right-1 bg-indigo-600 text-white text-[11px] rounded-full w-4 h-4 flex items-center justify-center">
                {{ cart.totalCount }}
              </span>
            </button>

            <!-- Desktop auth links -->
            <template v-if="!isAuthenticated" class="hidden sm:flex items-center gap-2">
              <router-link to="/login" class="text-sm text-ink-2 hover:text-indigo-600 px-2 py-1">{{ t('common.login') }}</router-link>
              <router-link to="/register" class="text-sm px-3 py-1.5 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">{{ t('common.register') }}</router-link>
            </template>

            <template v-else class="hidden sm:flex items-center gap-2">
              <div class="flex items-center gap-1">
                <router-link to="/profile" class="text-sm text-ink-2 hover:text-indigo-600">
                  {{ auth.user?.name || auth.user?.email }}
                </router-link>
                <span v-if="userRole" class="text-[11px] px-1.5 py-0.5 rounded-full bg-surface-2 text-ink-3">
                  {{ userRole }}
                </span>
              </div>
              <router-link v-if="isAuthenticated" to="/seller" class="text-xs text-indigo-600 hover:underline px-1">
                {{ t('common.seller_cabinet') }}
              </router-link>
              <router-link v-if="isAuthenticated" to="/admin" class="text-xs text-purple-600 hover:underline px-1">
                {{ t('common.admin_panel') }}
              </router-link>
              <button @click="handleLogout" class="text-xs text-ink-3 hover:text-red-600 px-1">{{ t('common.logout') }}</button>
            </template>

            <!-- Mobile auth dropdown trigger -->
            <div v-if="isAuthenticated" class="sm:hidden relative">
              <button class="p-2 text-ink-2 hover:bg-surface-2 rounded-lg" :aria-label="t('common.profile')">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                </svg>
              </button>
            </div>

            <!-- Language switcher -->
            <div ref="langMenuRef" class="relative">
              <button
                @click="showLangMenu = !showLangMenu"
                class="hidden sm:flex items-center gap-1 px-1.5 py-1 text-xs rounded-md bg-surface-2 text-ink-2 hover:bg-surface-3"
                :title="t('common.menu')"
                :aria-label="t('common.menu')"
              >
                <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 5h12M9 3v2m1.048 9.5A18.022 18.022 0 016.412 9m6.588 9a18.023 18.023 0 01-6.588-9m5.012-5l3 3m0 0l-3 3m3-3H3" />
                </svg>
                <span>{{ langLabels[locale] || locale }}</span>
              </button>
              <div
                v-if="showLangMenu"
                class="absolute right-0 mt-1 z-50 bg-surface border border-line rounded-md shadow-lg overflow-hidden text-xs"
              >
                <button
                  v-for="lang in SUPPORTED_LOCALES"
                  :key="lang"
                  @click="switchLang(lang)"
                  class="block w-full px-3 py-1.5 text-left hover:bg-surface-2"
                  :class="locale === lang ? 'bg-surface-2 font-medium' : 'text-ink-2'"
                >
                  {{ langLabels[lang] }}
                </button>
              </div>
            </div>

            <!-- Theme switcher -->
            <div class="relative">
              <button
                @click="theme = theme === THEMES.LIGHT ? THEMES.DARK : theme === THEMES.DARK ? THEMES.AUTO : THEMES.LIGHT"
                class="hidden sm:flex items-center gap-1 px-1.5 py-1 text-xs rounded-md bg-surface-2 text-ink-2 hover:bg-surface-3"
                :title="t('common.theme')"
                :aria-label="t('common.theme')"
              >
                <svg v-if="theme === THEMES.LIGHT" xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
                </svg>
                <svg v-else-if="theme === THEMES.DARK" xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
                </svg>
                <svg v-else xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
                </svg>
                <span class="hidden md:inline">
                  {{ theme === THEMES.LIGHT ? 'Light' : theme === THEMES.DARK ? 'Dark' : 'Auto' }}
                </span>
              </button>
            </div>

            <!-- Shard usage bar -->
            <ShardUsageBar v-if="isAuthenticated" />
          </nav>
        </div>
      </div>
    </header>

    <!-- Mobile menu overlay -->
    <div
      v-if="mobileMenuOpen"
      class="lg:hidden fixed inset-0 z-40 bg-black/30"
      @click="mobileMenuOpen = false"
    >
      <div
        role="dialog"
        aria-modal="true"
        :aria-label="t('common.menu')"
        class="bg-surface w-72 h-full shadow-lg p-4 overflow-y-auto"
        @click.stop
      >
        <div class="flex items-center justify-between mb-4">
          <span class="font-bold">{{ t('common.menu') }}</span>
          <button @click="mobileMenuOpen = false" class="p-1" :aria-label="t('common.close')">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Mobile search (header search is hidden below sm) -->
        <form
          @submit.prevent="$router.push({ name: 'catalog', query: { q: $refs.mobileSearch?.value } }); mobileMenuOpen = false;"
          class="mb-4"
        >
          <input
            ref="mobileSearch"
            type="text"
            :placeholder="t('common.search_placeholder')"
            class="w-full px-3 py-2 border border-line rounded-lg text-sm"
          />
        </form>

        <nav class="space-y-1">
          <router-link to="/" class="block px-3 py-2 rounded-lg text-sm hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.catalog') }}</router-link>
          <router-link to="/cart" class="block px-3 py-2 rounded-lg text-sm hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.cart') }}</router-link>

          <template v-if="!isAuthenticated">
            <router-link to="/login" class="block px-3 py-2 rounded-lg text-sm hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.login') }}</router-link>
            <router-link to="/register" class="block px-3 py-2 rounded-lg text-sm hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.register') }}</router-link>
          </template>
          <template v-else>
            <router-link to="/profile" class="block px-3 py-2 rounded-lg text-sm hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.profile') }}</router-link>
            <router-link to="/orders" class="block px-3 py-2 rounded-lg text-sm hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.my_orders') }}</router-link>
            <router-link v-if="isAuthenticated" to="/seller" class="block px-3 py-2 rounded-lg text-sm text-indigo-600 hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.seller_cabinet') }}</router-link>
            <router-link v-if="isAuthenticated" to="/admin" class="block px-3 py-2 rounded-lg text-sm text-purple-600 hover:bg-surface-2" @click="mobileMenuOpen = false">{{ t('common.admin_panel') }}</router-link>
            <button @click="handleLogout; mobileMenuOpen = false" class="w-full text-left px-3 py-2 rounded-lg text-sm text-red-600 hover:bg-surface-2">{{ t('common.logout') }}</button>
          </template>
        </nav>
      </div>
    </div>

    <!-- Categories sidebar: mobile overlay only (desktop sidebar hidden) -->

    <!-- Mobile categories overlay -->
    <div
      v-if="categoriesSidebarOpen"
      class="lg:hidden fixed inset-0 z-40 bg-black/30"
      @click="categoriesSidebarOpen = false"
    >
      <div
        role="dialog"
        aria-modal="true"
        :aria-label="t('common.categories')"
        class="bg-surface w-72 h-full shadow-lg flex flex-col"
        @click.stop
      >
        <div class="flex items-center justify-between px-4 py-3 border-b border-line">
          <span class="font-semibold text-sm">{{ t('common.categories') }}</span>
          <button @click="categoriesSidebarOpen = false" class="p-1 rounded hover:bg-surface-2">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div class="flex-1 overflow-y-auto p-3">
          <CategoryTree />
        </div>
      </div>
    </div>

    <!-- Main content -->
    <main class="flex-1">
      <router-view />
    </main>

    <!-- Footer -->
    <footer class="bg-surface border-t border-line py-4">
      <div class="max-w-7xl mx-auto px-4">
        <div class="flex flex-col sm:flex-row items-center justify-between gap-2 text-sm text-ink-3">
          <span>© {{ new Date().getFullYear() }} {{ t('common.app_name') }} — {{ t('common.app_tagline') }}</span>
          <div class="flex items-center gap-4">
            <a href="/privacy-policy" class="hover:text-indigo-600">{{ t('common.privacy_policy') }}</a>
            <button @click="showSettings" class="hover:text-indigo-600">{{ t('common.cookie_settings') }}</button>
          </div>
        </div>
      </div>
    </footer>

    <!-- Cookie Consent Banner -->
    <CookieConsentBanner />
  </div>
</template>
