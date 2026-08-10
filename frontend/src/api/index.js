import axios from 'axios';

const api = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
  // Don't follow redirects automatically
  maxRedirects: 0,
  // Allow 301/302 to be handled manually
  validateStatus: (status) => status >= 200 && status < 500,
});

// Request interceptor: attach JWT if present
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('jwt');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Response interceptor: handle redirects (301/302) and 401
api.interceptors.response.use(
  (response) => {
    // Handle 301/302 redirects — full page navigation to canonical URL
    if (response.status === 301 || response.status === 302) {
      console.log('[API] Redirect:', response.status, response.headers);
      const location = response.headers['location'] || response.config.headers?.['x-redirect-location'];
      if (location) {
        console.log('[API] Navigating to:', location);
        window.location.replace(location);
        return Promise.reject(new Error('redirect'));
      }
    }
    return response;
  },
  (error) => {
    // Debug: log redirect-related errors
    if (error.response && (error.response.status === 301 || error.response.status === 302)) {
      console.log('[API] Redirect error:', error.response.status, error.response.headers);
      const location = error.response.headers['location'];
      if (location) {
        console.log('[API] Navigating to:', location);
        window.location.replace(location);
        return Promise.reject(new Error('redirect'));
      }
    }
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
