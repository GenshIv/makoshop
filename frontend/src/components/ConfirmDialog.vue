<script setup>
import { ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';

const { t } = useI18n();

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: '' },
  message: { type: String, default: '' },
  confirmText: { type: String, default: '' },
  cancelText: { type: String, default: '' },
  // 'danger' renders the confirm button in red
  variant: { type: String, default: 'default' },
});

const emit = defineEmits(['confirm', 'cancel']);

const confirmRef = ref(null);

watch(
  () => props.open,
  (open) => {
    if (open) {
      // Lock body scroll while the dialog is open
      if (typeof document !== 'undefined') document.body.style.overflow = 'hidden';
      // Focus the confirm button for keyboard users
      requestAnimationFrame(() => confirmRef.value?.focus());
    } else if (typeof document !== 'undefined') {
      document.body.style.overflow = '';
    }
  }
);

const handleConfirm = () => {
  emit('confirm');
};

const handleCancel = () => {
  emit('cancel');
};

const onKeydown = (e) => {
  if (e.key === 'Escape') handleCancel();
};
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="fixed inset-0 z-[90] flex items-center justify-center bg-black/40 p-4"
      @click.self="handleCancel"
      @keydown.esc="handleCancel"
    >
      <div
        role="dialog"
        aria-modal="true"
        :aria-labelledby="title ? 'confirm-dialog-title' : undefined"
        class="bg-surface rounded-xl shadow-xl w-full max-w-md p-5"
      >
        <h2 id="confirm-dialog-title" class="text-lg font-semibold mb-2">
          {{ title }}
        </h2>
        <p v-if="message" class="text-sm text-ink-2 mb-4 whitespace-pre-line">
          {{ message }}
        </p>
        <div class="flex justify-end gap-2">
          <button
            @click="handleCancel"
            class="px-4 py-2 text-sm rounded-md border border-line bg-surface hover:bg-surface-2"
          >
            {{ cancelText || t('common.cancel') }}
          </button>
          <button
            ref="confirmRef"
            @click="handleConfirm"
            :class="variant === 'danger'
              ? 'px-4 py-2 text-sm rounded-md bg-red-600 text-white hover:bg-red-700'
              : 'px-4 py-2 text-sm rounded-md bg-purple-600 text-white hover:bg-purple-700'"
          >
            {{ confirmText || (variant === 'danger' ? t('admin.delete') : t('common.save')) }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
