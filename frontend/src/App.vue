<script setup>
import { computed, ref, watch } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useAuthStore } from './stores/auth';
import { useCartStore } from './stores/cart';
import CategoryTree from './components/CategoryTree.vue';

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();
const cart = useCartStore();

const mobileMenuOpen = ref(false);
const categoriesSidebarOpen = ref(false);

const isAuthenticated = computed(() => auth.isAuthenticated);
const userRole = computed(() => auth.user?.role || null);

// SEO page titles
const pageTitles = {
  catalog: 'Каталог товаров — MakoShop',
  product: 'Товар — MakoShop',
  cart: 'Корзина — MakoShop',
  checkout: 'Оформление заказа — MakoShop',
  login: 'Вход — MakoShop',
  register: 'Регистрация — MakoShop',
  profile: 'Личный кабинет — MakoShop',
  orders: 'Мои заказы — MakoShop',
  'order-detail': 'Заказ — MakoShop',
  reviews: 'Мои отзывы — MakoShop',
  'seller-dashboard': 'Кабинет продавца — MakoShop',
  'seller-products': 'Товары — Кабинет продавца',
  'seller-product-new': 'Добавить товар — Кабинет продавца',
  'seller-product-edit': 'Редактировать товар — Кабинет продавца',
  'seller-orders': 'Заказы — Кабинет продавца',
  'seller-promo': 'Продвижение — Кабинет продавца',
  'admin-dashboard': 'Админ-панель — MakoShop',
  'admin-users': 'Пользователи — Админ-панель',
  'admin-companies': 'Компании — Админ-панель',
  'admin-categories': 'Категории — Админ-панель',
  'admin-analytics': 'Аналитика — Админ-панель',
  'admin-promo': 'Промо — Админ-панель',
};

watch(
  () => route.name,
  (name) => {
    document.title = pageTitles[name] || 'MakoShop — Маркетплейс';
  },
  { immediate: true }
);

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
</script>

<template>
  <div class="min-h-screen flex flex-col bg-gray-50">
    <!-- Header -->
    <header class="bg-white shadow-sm border-b sticky top-0 z-30">
      <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div class="flex items-center justify-between h-16 gap-4">
          <!-- Left: Logo + mobile buttons -->
          <div class="flex items-center gap-3">
            <!-- Mobile menu button -->
            <button
              @click="mobileMenuOpen = !mobileMenuOpen"
              class="lg:hidden p-2 rounded-lg text-gray-600 hover:bg-gray-100"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>

            <!-- Categories button (mobile) -->
            <button
              @click="categoriesSidebarOpen = true"
              class="lg:hidden px-3 py-1.5 bg-gray-100 text-gray-700 rounded-lg text-sm hover:bg-gray-200 flex items-center gap-1"
            >
              <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h10M4 18h6" />
              </svg>
              Категории
            </button>

            <!-- Logo -->
            <router-link to="/" class="text-xl font-bold text-indigo-600 hover:text-indigo-700 whitespace-nowrap">
              MakoShop
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
              placeholder="Поиск товаров..."
              class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 text-sm"
            />
          </form>

          <!-- Right: Nav links -->
          <nav class="flex items-center gap-2 sm:gap-3">
            <!-- Cart -->
            <button @click="goToCart" class="relative p-2 text-gray-700 hover:text-indigo-600 hover:bg-gray-100 rounded-lg">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 3h2l.4 2M7 13h10l4-8H5.4M7 13L5.4 5M7 13l-2.293 2.293c-.63.63-.184 1.707.707 1.707H17m0 0a2 2 0 100 4 2 2 0 000-4zm-8 2a2 2 0 11-4 0 2 2 0 014 0z" />
              </svg>
              <span v-if="cart.totalCount > 0" class="absolute -top-1 -right-1 bg-indigo-600 text-white text-[10px] rounded-full w-4 h-4 flex items-center justify-center">
                {{ cart.totalCount }}
              </span>
            </button>

            <!-- Desktop auth links -->
            <template v-if="!isAuthenticated" class="hidden sm:flex items-center gap-2">
              <router-link to="/login" class="text-sm text-gray-700 hover:text-indigo-600 px-2 py-1">Войти</router-link>
              <router-link to="/register" class="text-sm px-3 py-1.5 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700">Регистрация</router-link>
            </template>

            <template v-else class="hidden sm:flex items-center gap-2">
              <div class="flex items-center gap-1">
                <router-link to="/profile" class="text-sm text-gray-700 hover:text-indigo-600">
                  {{ auth.user?.name || auth.user?.email }}
                </router-link>
                <span v-if="userRole" class="text-[10px] px-1.5 py-0.5 rounded-full bg-gray-100 text-gray-500">
                  {{ userRole }}
                </span>
              </div>
              <router-link v-if="userRole === 'seller' || userRole === 'admin'" to="/seller" class="text-xs text-indigo-600 hover:underline px-1">
                Продавец
              </router-link>
              <router-link v-if="userRole === 'admin'" to="/admin" class="text-xs text-purple-600 hover:underline px-1">
                Админ
              </router-link>
              <button @click="handleLogout" class="text-xs text-gray-500 hover:text-red-600 px-1">Выйти</button>
            </template>

            <!-- Mobile auth dropdown trigger -->
            <div v-if="isAuthenticated" class="sm:hidden relative">
              <button class="p-2 text-gray-700 hover:bg-gray-100 rounded-lg">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                </svg>
              </button>
            </div>
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
      <div class="bg-white w-72 h-full shadow-lg p-4 overflow-y-auto" @click.stop>
        <div class="flex items-center justify-between mb-4">
          <span class="font-bold">Меню</span>
          <button @click="mobileMenuOpen = false" class="p-1">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Mobile search -->
        <form @submit.prevent="$router.push({ name: 'catalog', query: { q: $refs.mobileSearch?.value } }); mobileMenuOpen = false;" class="mb-4">
          <input
            ref="mobileSearch"
            type="text"
            placeholder="Поиск товаров..."
            class="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm"
          />
        </form>

        <nav class="space-y-1">
          <router-link to="/" class="block px-3 py-2 rounded-lg text-sm hover:bg-gray-100" @click="mobileMenuOpen = false">Каталог</router-link>
          <router-link to="/cart" class="block px-3 py-2 rounded-lg text-sm hover:bg-gray-100" @click="mobileMenuOpen = false">Корзина</router-link>

          <template v-if="!isAuthenticated">
            <router-link to="/login" class="block px-3 py-2 rounded-lg text-sm hover:bg-gray-100" @click="mobileMenuOpen = false">Войти</router-link>
            <router-link to="/register" class="block px-3 py-2 rounded-lg text-sm hover:bg-gray-100" @click="mobileMenuOpen = false">Регистрация</router-link>
          </template>
          <template v-else>
            <router-link to="/profile" class="block px-3 py-2 rounded-lg text-sm hover:bg-gray-100" @click="mobileMenuOpen = false">Профиль</router-link>
            <router-link to="/orders" class="block px-3 py-2 rounded-lg text-sm hover:bg-gray-100" @click="mobileMenuOpen = false">Мои заказы</router-link>
            <router-link v-if="userRole === 'seller' || userRole === 'admin'" to="/seller" class="block px-3 py-2 rounded-lg text-sm text-indigo-600 hover:bg-gray-100" @click="mobileMenuOpen = false">Кабинет продавца</router-link>
            <router-link v-if="userRole === 'admin'" to="/admin" class="block px-3 py-2 rounded-lg text-sm text-purple-600 hover:bg-gray-100" @click="mobileMenuOpen = false">Админ-панель</router-link>
            <button @click="handleLogout; mobileMenuOpen = false" class="w-full text-left px-3 py-2 rounded-lg text-sm text-red-600 hover:bg-gray-100">Выйти</button>
          </template>
        </nav>
      </div>
    </div>

    <!-- Categories sidebar (desktop: left column, mobile: overlay) -->
    <!-- Desktop sidebar -->
    <aside class="hidden lg:block fixed left-0 top-16 w-64 h-[calc(100vh-4rem)] bg-white border-r border-gray-200 overflow-y-auto z-20">
      <div class="p-3">
        <h3 class="text-[11px] font-semibold text-gray-400 uppercase tracking-wider mb-2 px-2">Категории</h3>
        <CategoryTree />
      </div>
    </aside>

    <!-- Mobile categories overlay -->
    <div
      v-if="categoriesSidebarOpen"
      class="lg:hidden fixed inset-0 z-40 bg-black/30"
      @click="categoriesSidebarOpen = false"
    >
      <div class="bg-white w-72 h-full shadow-lg flex flex-col" @click.stop>
        <div class="flex items-center justify-between px-4 py-3 border-b">
          <span class="font-semibold text-sm">Категории</span>
          <button @click="categoriesSidebarOpen = false" class="p-1 rounded hover:bg-gray-100">
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
    <main class="flex-1 lg:ml-64">
      <router-view />
    </main>

    <!-- Footer -->
    <footer class="bg-white border-t py-4 lg:ml-64">
      <div class="max-w-7xl mx-auto px-4 text-center text-sm text-gray-500">
        © {{ new Date().getFullYear() }} MakoShop — B2B/B2C маркетплейс
      </div>
    </footer>
  </div>
</template>
