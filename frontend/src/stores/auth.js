import { defineStore } from 'pinia';
import api from '../api';

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: JSON.parse(localStorage.getItem('user') || 'null'),
    token: localStorage.getItem('jwt') || null,
    loading: false,
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
        // Backend may return {token, user} or {token, user_id, email, role}
        this.token = data.token;
        this.user = data.user || {
          id: data.user_id,
          email: data.email,
          role: data.role,
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
        this.user = result.user || {
          id: result.user_id,
          email: result.email,
          role: result.role,
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
      localStorage.removeItem('jwt');
      localStorage.removeItem('user');
    },
  },
});
