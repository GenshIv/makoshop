import { defineStore } from 'pinia';
import api from '../api';

export const useCartStore = defineStore('cart', {
  state: () => ({
    cartId: null,
    items: [],
    loading: false,
  }),

  getters: {
    totalCount: (state) => (state.items || []).reduce((sum, item) => sum + item.qty, 0),
    totalPrice: (state) => (state.items || []).reduce((sum, item) => sum + item.price * item.qty, 0),
  },

  actions: {
    async fetchCart() {
      this.loading = true;
      try {
        const token = localStorage.getItem('jwt');

        if (token) {
          // Try auth cart first (skip auth redirect so we can fallback to guest cart)
          const response = await api.get('/cart/me', { headers: { 'X-Skip-Auth-Redirect': 'true' } });
          this.cartId = response.data.id;
          this.items = response.data.items || [];
          return;
        } else {
          // No token — try guest cart directly
          const guestCartId = localStorage.getItem('guest_cart_id');
          if (guestCartId) {
            try {
              const response = await api.get(`/cart/${guestCartId}`);
              this.cartId = response.data.id;
              this.items = response.data.items || [];
              return;
            } catch (guestErr) {
              // Guest cart expired or deleted
              localStorage.removeItem('guest_cart_id');
            }
          }
        }

        // No cart
        this.items = [];
        this.cartId = null;
      } finally {
        this.loading = false;
      }
    },

    async ensureCart() {
      if (this.cartId) return this.cartId;
      try {
        const response = await api.post('/cart', {});
        this.cartId = response.data.id;
        // If not authenticated, store as guest cart
        const token = localStorage.getItem('jwt');
        if (!token) {
          localStorage.setItem('guest_cart_id', this.cartId);
        }
        return this.cartId;
      } catch (e) {
        console.error('Create cart error:', e);
        throw e;
      }
    },

    async addItem(productId, qty = 1) {
      try {
        const cartId = await this.ensureCart();
        await api.post(`/cart/${cartId}/items`, { product_id: productId, qty });
        await this.fetchCart();
      } catch (e) {
        console.error('Add to cart error:', e);
        throw e;
      }
    },

    async updateItem(productId, qty) {
      if (qty <= 0) {
        await this.removeItem(productId);
        return;
      }
      try {
        await api.patch(`/cart/${this.cartId}/items/${productId}`, { qty });
        await this.fetchCart();
      } catch (e) {
        console.error('Update cart item error:', e);
      }
    },

    async removeItem(productId) {
      try {
        // Backend does not support DELETE; use PATCH with qty=0
        await api.patch(`/cart/${this.cartId}/items/${productId}`, { qty: 0 });
        await this.fetchCart();
      } catch (e) {
        console.error('Remove cart item error:', e);
      }
    },

    clear() {
      this.items = [];
    },
  },
});
