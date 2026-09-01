import { createRouter, createWebHistory } from 'vue-router';

// All views are lazy-loaded so Vite can split them into separate chunks.
const routes = [
  // Public
  { path: '/', name: 'catalog', component: () => import('../views/CatalogView.vue') },
  { path: '/shop', name: 'shop-catalog', component: () => import('../views/CatalogView.vue') },
  { path: '/shop/:pathMatch(.*)*', name: 'shop-catalog-path', component: () => import('../views/CatalogView.vue') },
  { path: '/company/:slug', name: 'company', component: () => import('../views/CompanyView.vue') },
  { path: '/products/:id', name: 'product', component: () => import('../views/ProductView.vue') },
  { path: '/cart', name: 'cart', component: () => import('../views/CartView.vue') },
  { path: '/checkout', name: 'checkout', component: () => import('../views/CheckoutView.vue') },
  { path: '/login', name: 'login', component: () => import('../views/LoginView.vue') },
  { path: '/register', name: 'register', component: () => import('../views/RegisterView.vue') },
  { path: '/privacy-policy', name: 'privacy-policy', component: () => import('../views/PrivacyPolicyView.vue') },

  // Buyer
  { path: '/profile', name: 'profile', component: () => import('../views/ProfileView.vue'), meta: { requiresAuth: true } },
  { path: '/orders', name: 'orders', component: () => import('../views/OrdersView.vue'), meta: { requiresAuth: true } },
  { path: '/orders/:id', name: 'order-detail', component: () => import('../views/OrderDetailView.vue'), meta: { requiresAuth: true } },
  { path: '/reviews', name: 'reviews', component: () => import('../views/ReviewsView.vue'), meta: { requiresAuth: true } },

  // Seller (role: seller or admin)
  { path: '/seller', name: 'seller-dashboard', component: () => import('../views/seller/SellerDashboardView.vue'), meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/products', name: 'seller-products', component: () => import('../views/seller/SellerProductsView.vue'), meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/products/new', name: 'seller-product-new', component: () => import('../views/seller/SellerProductFormView.vue'), meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/products/:id/edit', name: 'seller-product-edit', component: () => import('../views/seller/SellerProductFormView.vue'), meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/orders', name: 'seller-orders', component: () => import('../views/seller/SellerOrdersView.vue'), meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/promo', name: 'seller-promo', component: () => import('../views/seller/SellerPromoView.vue'), meta: { requiresAuth: true, requiresRole: 'seller' } },

  // Admin (role: admin)
  { path: '/admin', name: 'admin-dashboard', component: () => import('../views/admin/AdminDashboardView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/settings', name: 'admin-settings', component: () => import('../views/admin/AdminSettingsView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/users', name: 'admin-users', component: () => import('../views/admin/AdminUsersView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/companies', name: 'admin-companies', component: () => import('../views/admin/AdminCompaniesView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/categories', name: 'admin-categories', component: () => import('../views/admin/AdminCategoriesView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/categories/:id/attributes', name: 'admin-category-attributes', component: () => import('../views/admin/AdminCategoryAttributesView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/eanpages', name: 'admin-eanpages', component: () => import('../views/admin/AdminEANPageView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/catalogizer', name: 'admin-catalogizer', component: () => import('../views/admin/AdminCatalogizerView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/stats', name: 'admin-stats', component: () => import('../views/admin/AdminStatsView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/metrics', redirect: '/admin/stats' },
  { path: '/admin/analytics', name: 'admin-analytics', component: () => import('../views/admin/AdminAnalyticsView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/promo', name: 'admin-promo', component: () => import('../views/admin/AdminPromoView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/delivery-times', name: 'admin-delivery-times', component: () => import('../views/admin/AdminDeliveryTimesView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/delivery-methods', name: 'admin-delivery-methods', component: () => import('../views/admin/AdminDeliveryMethodsView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/installment-plans', name: 'admin-installment-plans', component: () => import('../views/admin/AdminInstallmentPlansView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/reviews', name: 'admin-reviews', component: () => import('../views/admin/AdminReviewsView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/comments', name: 'admin-comments', component: () => import('../views/admin/AdminCommentsView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/branding', name: 'admin-branding', component: () => import('../views/admin/AdminBrandingView.vue'), meta: { requiresAuth: true, requiresRole: 'admin' } },
];

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(to, from, savedPosition) {
    if (savedPosition) return savedPosition;
    return { top: 0 };
  },
});

router.beforeEach((to, from) => {
  const token = sessionStorage.getItem('jwt');

  if (to.meta.requiresAuth && !token) {
    return { name: 'login', query: { redirect: to.fullPath } };
  }
});

export default router;
