import axios from 'axios';

const api = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
  // Don't follow redirects automatically
  maxRedirects: 0,
  // Only 2xx are considered success; 4xx/5xx go to error handler
  validateStatus: (status) => status >= 200 && status < 300,
});

// Request interceptor: attach JWT if present
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('jwt');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Response interceptor: handle redirects (301/302) and auth errors (401)
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
    // Handle 301/302 redirects
    if (error.response && (error.response.status === 301 || error.response.status === 302)) {
      const location = error.response.headers['location'];
      if (location) {
        window.location.replace(location);
        return Promise.reject(new Error('redirect'));
      }
    }

    // Handle 401 Unauthorized — invalid/expired/missing token
    // Backend decides: any request with bad token -> 401
    if (error.response?.status === 401) {
      console.log('[API] 401 Unauthorized — clearing session, redirecting to /');
      localStorage.removeItem('jwt');
      localStorage.removeItem('user');
      // Avoid infinite reload if already on root
      if (window.location.pathname !== '/') {
        window.location.href = '/';
      } else {
        window.location.reload();
      }
      return Promise.reject(error);
    }

    return Promise.reject(error);
  }
);

export default api;
