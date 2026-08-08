import axios from 'axios';

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/',
  headers: {
    'Content-Type': 'application/json',
  },
});

// Request interceptor: attach JWT if present
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('jwt');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Response interceptor: handle 401 (token expired / invalid)
api.interceptors.response.use(
  (response) => response,
  (error) => {
    const skipRedirect = error.config?.headers?.['X-Skip-Auth-Redirect'] === 'true';
    if (error.response?.status === 401 && !skipRedirect) {
      localStorage.removeItem('jwt');
      localStorage.removeItem('user');
      // Only redirect if not already on login page
      if (!window.location.pathname.startsWith('/login')) {
        window.location.href = '/login';
      }
    }
    return Promise.reject(error);
  }
);

export default api;
