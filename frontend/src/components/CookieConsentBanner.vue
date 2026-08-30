<template>
  <Teleport to="body">
    <Transition name="cookie-banner">
      <div v-if="showBanner" class="cookie-banner-overlay">
        <div class="cookie-banner">
          <div class="cookie-banner-header">
            <h3>🍪 {{ t('cookie.banner_title') }}</h3>
            <button class="cookie-banner-close" @click="acceptEssential" :aria-label="t('cookie.close')">
              ✕
            </button>
          </div>

          <div class="cookie-banner-body">
            <p>{{ t('cookie.banner_text') }}</p>

            <div class="cookie-banner-options">
              <div class="cookie-option">
                <div class="cookie-option-header">
                  <span class="cookie-option-title">{{ t('cookie.essential') }}</span>
                  <span class="cookie-option-badge essential">Essential</span>
                </div>
                <p class="cookie-option-desc">
                  {{ t('cookie.essential_desc') }}
                </p>
              </div>

              <div class="cookie-option">
                <label class="cookie-toggle">
                  <input
                    type="checkbox"
                    v-model="consent.analytics"
                    :aria-label="t('cookie.analytics')"
                    @change="onOptionChange"
                  />
                  <span class="sr-only">{{ t('cookie.analytics') }}</span>
                  <span class="cookie-toggle-slider"></span>
                </label>
                <div class="cookie-option-header">
                  <span class="cookie-option-title">{{ t('cookie.analytics') }}</span>
                </div>
                <p class="cookie-option-desc">
                  {{ t('cookie.analytics_desc') }}
                </p>
              </div>

              <div class="cookie-option">
                <label class="cookie-toggle">
                  <input
                    type="checkbox"
                    v-model="consent.marketing"
                    :aria-label="t('cookie.marketing')"
                    @change="onOptionChange"
                  />
                  <span class="sr-only">{{ t('cookie.marketing') }}</span>
                  <span class="cookie-toggle-slider"></span>
                </label>
                <div class="cookie-option-header">
                  <span class="cookie-option-title">{{ t('cookie.marketing') }}</span>
                </div>
                <p class="cookie-option-desc">
                  {{ t('cookie.marketing_desc') }}
                </p>
              </div>
            </div>
          </div>

          <div class="cookie-banner-footer">
            <button class="btn btn-outline" @click="acceptEssential">
              {{ t('cookie.essential_only') }}
            </button>
            <button class="btn btn-primary" @click="acceptAll">
              {{ t('cookie.accept_all') }}
            </button>
            <button class="btn btn-secondary" @click="saveCustom">
              {{ t('cookie.save') }}
            </button>
          </div>

          <div class="cookie-banner-link">
            <a href="/privacy-policy">
              {{ t('common.privacy_policy') }}
            </a>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { useCookieConsent } from '../composables/useCookieConsent';
import { useI18n } from 'vue-i18n';

const { t } = useI18n();

const {
  showBanner,
  consent,
  acceptAll,
  acceptEssential,
  saveCustom,
} = useCookieConsent();

const onOptionChange = () => {
  // Just update the model, user will click save
};
</script>

<style scoped>
.cookie-banner-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
  padding: 1rem;
}

.cookie-banner {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border-radius: 12px;
  max-width: 520px;
  width: 100%;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
}

.cookie-banner-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1.25rem 1.5rem;
  border-bottom: 1px solid var(--border-color);
}

.cookie-banner-header h3 {
  margin: 0;
  font-size: 1.1rem;
  font-weight: 600;
}

.cookie-banner-close {
  width: 28px;
  height: 28px;
  border: none;
  background: #f5f5f5;
  border-radius: 50%;
  cursor: pointer;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.2s;
}

.cookie-banner-close:hover {
  background: #e5e5e5;
}

.cookie-banner-body {
  padding: 1.25rem 1.5rem;
}

.cookie-banner-body > p {
  margin: 0 0 1rem;
  font-size: 1rem;
  color: #555;
  line-height: 1.5;
}

.cookie-banner-options {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.cookie-option {
  display: flex;
  gap: 0.75rem;
  align-items: flex-start;
  padding: 0.75rem;
  border-radius: 8px;
  border: 1px solid var(--border-color);
}

.cookie-option-header {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.cookie-option-title {
  font-weight: 600;
  font-size: 0.95rem;
}

.cookie-option-badge {
  font-size: 0.7rem;
  padding: 2px 6px;
  border-radius: 4px;
  text-transform: uppercase;
}

.cookie-option-badge.essential {
  background: #e8f5e9;
  color: #2e7d32;
}

.cookie-option-desc {
  margin: 0.25rem 0 0;
  font-size: 0.875rem;
  color: var(--text-muted);
  line-height: 1.4;
}

/* Toggle switch */
.cookie-toggle {
  position: relative;
  width: 44px;
  height: 24px;
  flex-shrink: 0;
  cursor: pointer;
}

.cookie-toggle input {
  opacity: 0;
  width: 0;
  height: 0;
}

.cookie-toggle-slider {
  position: absolute;
  inset: 0;
  background: var(--border-color);
  border-radius: 24px;
  transition: background 0.2s;
}

.cookie-toggle-slider::before {
  content: '';
  position: absolute;
  width: 18px;
  height: 18px;
  left: 3px;
  bottom: 3px;
  background: var(--bg-secondary);
  border-radius: 50%;
  transition: transform 0.2s;
}

.cookie-toggle input:checked ~ .cookie-toggle-slider {
  background: var(--accent);
}

.cookie-toggle input:checked ~ .cookie-toggle-slider::before {
  transform: translateX(20px);
}

.cookie-banner-footer {
  display: flex;
  gap: 0.5rem;
  padding: 1rem 1.5rem;
  border-top: 1px solid var(--border-color);
  flex-wrap: wrap;
}

.cookie-banner-footer .btn {
  flex: 1;
  min-width: 120px;
  padding: 0.6rem 1rem;
  border: none;
  border-radius: 8px;
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
}

.cookie-banner-footer .btn-primary {
  background: var(--accent);
  color: white;
}

.cookie-banner-footer .btn-primary:hover {
  background: var(--accent-hover);
}

.cookie-banner-footer .btn-secondary {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.cookie-banner-footer .btn-secondary:hover {
  background: #e5e7eb;
}

.cookie-banner-footer .btn-outline {
  background: transparent;
  color: var(--accent);
  border: 1px solid var(--accent);
}

.cookie-banner-footer .btn-outline:hover {
  background: #fff7ed;
}

.cookie-banner-link {
  text-align: center;
  padding: 0.75rem 1.5rem;
  font-size: 0.8rem;
  color: #888;
}

.cookie-banner-link a {
  color: var(--accent);
  text-decoration: underline;
}

/* Animations */
.cookie-banner-enter-active,
.cookie-banner-leave-active {
  transition: opacity 0.3s ease;
}

.cookie-banner-enter-from,
.cookie-banner-leave-to {
  opacity: 0;
}

.cookie-banner-enter-from .cookie-banner {
  transform: translateY(20px);
}

@media (max-width: 480px) {
  .cookie-banner-footer {
    flex-direction: column;
  }

  .cookie-banner-footer .btn {
    width: 100%;
  }
}
</style>
