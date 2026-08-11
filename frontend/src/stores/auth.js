import { defineStore } from 'pinia';
import api from '../api';

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: JSON.parse(localStorage.getItem('user') || 'null'),
    token: localStorage.getItem('jwt') || null,
    loading: false,
    firstLogin: false, // true if user must change password
  }),

  getters: {
    isAuthenticated: (state) => !!state.token,
    role: (state) => state.user?.role || null,
  },

  actions: {
    async login(email, password) {
      this.loading = true;
      try {
        const response = await api.post('/auth/login', { email, password });
        const data = response.data;
        this.token = data.token;
        // Don't store role in localStorage — fetch from backend when needed
        this.user = {
          id: data.user_id,
          email: data.email,
        };
        localStorage.setItem('jwt', this.token);
        localStorage.setItem('user', JSON.stringify(this.user));
        return data;
      } finally {
        this.loading = false;
      }
    },

    async register(data) {
      this.loading = true;
      try {
        const response = await api.post('/auth/register', data);
        const result = response.data;
        this.token = result.token;
        // Don't store role in localStorage
        this.user = {
          id: result.user_id,
          email: result.email,
        };
        localStorage.setItem('jwt', this.token);
        localStorage.setItem('user', JSON.stringify(this.user));
        return result;
      } finally {
        this.loading = false;
      }
    },

    async fetchMe() {
      if (!this.token) return;
      try {
        const response = await api.get('/auth/me');
        this.user = response.data;
        localStorage.setItem('user', JSON.stringify(this.user));
      } catch (e) {
        this.logout();
      }
    },

    async updateProfile(data) {
      const response = await api.patch('/users/me', data);
      this.user = response.data;
      localStorage.setItem('user', JSON.stringify(this.user));
    },

    logout() {
      this.token = null;
      this.user = null;
      this.firstLogin = false;
      localStorage.removeItem('jwt');
      localStorage.removeItem('user');
    },

    // Change password (for first-time superadmin setup)
    async changePassword(newPassword) {
      try {
        await api.post('/auth/change-password', { password: newPassword });
        this.firstLogin = false;
      } catch (e) {
        throw e;
      }
    },
  },
});
