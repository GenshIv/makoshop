import { createApp } from 'vue';
import { createPinia } from 'pinia';
import App from './App.vue';
import router from './router';
import { useAuthStore } from './stores/auth';
import { useCartStore } from './stores/cart';
import './style.css';

const app = createApp(App);
const pinia = createPinia();

app.use(pinia);
app.use(router);

// Initialize auth and cart after mount
app.mount('#app');

const auth = useAuthStore();
const cart = useCartStore();

// Restore user session
if (auth.token) {
  auth.fetchMe().then(() => {
    // After auth, refresh cart (may merge guest cart)
    cart.fetchCart();
  });
} else {
  cart.fetchCart();
}
