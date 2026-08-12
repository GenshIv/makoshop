import { createRouter, createWebHistory } from 'vue-router';
import CatalogView from '../views/CatalogView.vue';
import ProductView from '../views/ProductView.vue';
import SCUPageView from '../views/SCUPageView.vue';
import CartView from '../views/CartView.vue';
import CheckoutView from '../views/CheckoutView.vue';
import LoginView from '../views/LoginView.vue';
import RegisterView from '../views/RegisterView.vue';
import ProfileView from '../views/ProfileView.vue';
import OrdersView from '../views/OrdersView.vue';
import OrderDetailView from '../views/OrderDetailView.vue';
import ReviewsView from '../views/ReviewsView.vue';
import PrivacyPolicyView from '../views/PrivacyPolicyView.vue';

// Seller views
import SellerDashboardView from '../views/seller/SellerDashboardView.vue';
import SellerProductsView from '../views/seller/SellerProductsView.vue';
import SellerProductFormView from '../views/seller/SellerProductFormView.vue';
import SellerOrdersView from '../views/seller/SellerOrdersView.vue';
import SellerPromoView from '../views/seller/SellerPromoView.vue';

// Admin views
import AdminDashboardView from '../views/admin/AdminDashboardView.vue';
import AdminUsersView from '../views/admin/AdminUsersView.vue';
import AdminCompaniesView from '../views/admin/AdminCompaniesView.vue';
import AdminCategoriesView from '../views/admin/AdminCategoriesView.vue';
import AdminCategoryAttributesView from '../views/admin/AdminCategoryAttributesView.vue';
import AdminAnalyticsView from '../views/admin/AdminAnalyticsView.vue';
import AdminPromoView from '../views/admin/AdminPromoView.vue';
import AdminSCUPageView from '../views/admin/AdminSCUPageView.vue';
import AdminCatalogizerView from '../views/admin/AdminCatalogizerView.vue';
import AdminStatsView from '../views/admin/AdminStatsView.vue';
import AdminPaymentMethodsView from '../views/admin/AdminPaymentMethodsView.vue';
import AdminDeliveryTimesView from '../views/admin/AdminDeliveryTimesView.vue';
import AdminInstallmentPlansView from '../views/admin/AdminInstallmentPlansView.vue';

const routes = [
  // Public
  { path: '/', name: 'catalog', component: CatalogView },
  { path: '/shop', name: 'shop-catalog', component: CatalogView },
  { path: '/shop/:pathMatch(.*)*', name: 'shop-catalog-path', component: CatalogView },
  { path: '/scupage/:pathMatch(.*)*', name: 'scupage', component: SCUPageView },
  { path: '/products/:id', name: 'product', component: ProductView },
  { path: '/cart', name: 'cart', component: CartView },
  { path: '/checkout', name: 'checkout', component: CheckoutView },
  { path: '/login', name: 'login', component: LoginView },
  { path: '/register', name: 'register', component: RegisterView },
  { path: '/privacy-policy', name: 'privacy-policy', component: PrivacyPolicyView },

  // Buyer
  { path: '/profile', name: 'profile', component: ProfileView, meta: { requiresAuth: true } },
  { path: '/orders', name: 'orders', component: OrdersView, meta: { requiresAuth: true } },
  { path: '/orders/:id', name: 'order-detail', component: OrderDetailView, meta: { requiresAuth: true } },
  { path: '/reviews', name: 'reviews', component: ReviewsView, meta: { requiresAuth: true } },

  // Seller (role: seller or admin)
  { path: '/seller', name: 'seller-dashboard', component: SellerDashboardView, meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/products', name: 'seller-products', component: SellerProductsView, meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/products/new', name: 'seller-product-new', component: SellerProductFormView, meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/products/:id/edit', name: 'seller-product-edit', component: SellerProductFormView, meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/orders', name: 'seller-orders', component: SellerOrdersView, meta: { requiresAuth: true, requiresRole: 'seller' } },
  { path: '/seller/promo', name: 'seller-promo', component: SellerPromoView, meta: { requiresAuth: true, requiresRole: 'seller' } },

  // Admin (role: admin)
  { path: '/admin', name: 'admin-dashboard', component: AdminDashboardView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/users', name: 'admin-users', component: AdminUsersView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/companies', name: 'admin-companies', component: AdminCompaniesView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/categories', name: 'admin-categories', component: AdminCategoriesView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/categories/:id/attributes', name: 'admin-category-attributes', component: AdminCategoryAttributesView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/scupages', name: 'admin-scupages', component: AdminSCUPageView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/catalogizer', name: 'admin-catalogizer', component: AdminCatalogizerView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/stats', name: 'admin-stats', component: AdminStatsView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/analytics', name: 'admin-analytics', component: AdminAnalyticsView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/promo', name: 'admin-promo', component: AdminPromoView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/payment-methods', name: 'admin-payment-methods', component: AdminPaymentMethodsView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/delivery-times', name: 'admin-delivery-times', component: AdminDeliveryTimesView, meta: { requiresAuth: true, requiresRole: 'admin' } },
  { path: '/admin/installment-plans', name: 'admin-installment-plans', component: AdminInstallmentPlansView, meta: { requiresAuth: true, requiresRole: 'admin' } },
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
  const token = localStorage.getItem('jwt');

  if (to.meta.requiresAuth && !token) {
    return { name: 'login', query: { redirect: to.fullPath } };
  }
});

export default router;
