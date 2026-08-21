<script setup>
import { useI18n } from 'vue-i18n';

const { t } = useI18n();

const props = defineProps({
  modelValue: {
    type: String,
    required: true,
    validator: (v) => ['grid', 'list'].includes(v),
  },
});

const emit = defineEmits(['update:modelValue']);

const setView = (view) => {
  emit('update:modelValue', view);
};
</script>

<template>
  <div
    class="inline-flex items-center rounded-lg border border-line bg-surface p-0.5"
    role="group"
  >
    <button
      type="button"
      :class="[
        'flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition',
        modelValue === 'grid'
          ? 'bg-accent text-white shadow-sm'
          : 'text-ink-2 hover:bg-surface-2 hover:text-ink'
      ]"
      :aria-pressed="modelValue === 'grid'"
      :aria-label="t('common.view_grid')"
      :title="t('common.view_grid')"
      @click="setView('grid')"
    >
      <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
      </svg>
    </button>
    <button
      type="button"
      :class="[
        'flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition',
        modelValue === 'list'
          ? 'bg-accent text-white shadow-sm'
          : 'text-ink-2 hover:bg-surface-2 hover:text-ink'
      ]"
      :aria-pressed="modelValue === 'list'"
      :aria-label="t('common.view_list')"
      :title="t('common.view_list')"
      @click="setView('list')"
    >
      <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 10h16M4 14h10M4 18h6" />
      </svg>
    </button>
  </div>
</template>
