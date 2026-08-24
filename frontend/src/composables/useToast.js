/**
 * Lightweight toast notifications without an extra UI library.
 *
 * Usage:
 *   const toast = useToast();
 *   toast.success('Added to cart');
 *   toast.error('Error');
 *   toast('Info message');
 *   toast('Item removed', 'info', 5000, { actionLabel: 'Undo', onAction: () => {} });
 *
 * Toasts are rendered in a fixed container at the bottom-right and
 * auto-dismiss after `duration` ms (default 2500).
 *
 * Optional `options.actionLabel` / `options.onAction` add an action
 * button inside the toast. Clicking it runs `onAction` and dismisses
 * the toast immediately.
 */

const TOAST_CONTAINER_ID = 'makoshop-toasts';

function ensureContainer() {
  if (typeof document === 'undefined') return null;
  let container = document.getElementById(TOAST_CONTAINER_ID);
  if (!container) {
    container = document.createElement('div');
    container.id = TOAST_CONTAINER_ID;
    container.setAttribute('aria-live', 'polite');
    container.setAttribute('role', 'status');
    container.className =
      'fixed bottom-4 right-4 z-[100] flex flex-col gap-2 items-end pointer-events-none';
    document.body.appendChild(container);
  }
  return container;
}

function showToast(message, type = 'success', duration = 2500, options = {}) {
  const container = ensureContainer();
  if (!container || !message) return;

  const colors = {
    success: 'bg-green-600 text-white',
    error: 'bg-red-600 text-white',
    info: 'bg-slate-800 text-white',
  };

  const { actionLabel, onAction } = options || {};

  const el = document.createElement('div');
  el.className = `pointer-events-auto px-4 py-2.5 rounded-lg shadow-lg text-sm max-w-xs sm:max-w-sm flex items-center gap-3 ${
    colors[type] || colors.info
  }`;
  el.style.opacity = '0';
  el.style.transform = 'translateY(8px)';
  el.style.transition = 'opacity 0.2s ease, transform 0.2s ease';

  const msgSpan = document.createElement('span');
  msgSpan.textContent = message;
  el.appendChild(msgSpan);

  let removed = false;
  const remove = () => {
    if (removed) return;
    removed = true;
    el.style.opacity = '0';
    el.style.transform = 'translateY(8px)';
    setTimeout(() => el.remove(), 250);
  };

  if (actionLabel && typeof onAction === 'function') {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.textContent = actionLabel;
    btn.className =
      'shrink-0 px-2 py-1 rounded text-xs font-semibold bg-white/20 hover:bg-white/30 focus:outline-none focus:ring-2 focus:ring-white/60';
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      onAction();
      remove();
    });
    el.appendChild(btn);
  }

  container.appendChild(el);

  // Trigger enter transition
  requestAnimationFrame(() => {
    el.style.opacity = '1';
    el.style.transform = 'translateY(0)';
  });

  setTimeout(remove, duration);
}

export function useToast() {
  const toast = (message, type = 'success', duration = 2500, options = {}) => {
    showToast(message, type, duration, options);
  };

  // Add methods to toast function
  toast.success = (msg, duration, options) => showToast(msg, 'success', duration, options);
  toast.error = (msg, duration, options) => showToast(msg, 'error', duration, options);
  toast.info = (msg, duration, options) => showToast(msg, 'info', duration, options);

  return {
    toast,
  };
}
