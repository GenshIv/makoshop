<script setup>
import { useI18n } from 'vue-i18n';

const { locale, t } = useI18n();

const props = defineProps({
  category: { type: Object, required: true },
  expanded: { type: Set, required: true },
  activeId: { type: [Number, null, String], required: true },
});

const emit = defineEmits(['toggle', 'go']);

const catName = (cat) => {
  if (!cat) return '—';
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || '—';
};

const children = props.category.children || [];
const hasChildren = children.length > 0;

const isActive = () => {
  // "All" category has id='', activeId is null when no category selected
  if (props.category.id === '') {
    return props.activeId === null || props.activeId === '';
  }
  return props.activeId === props.category.id;
};

defineOptions({ name: 'CategoryTreeNode' });
</script>

<template>
  <li>
    <div class="flex items-center gap-1">
      <!-- Toggle button: only shown if category has children -->
      <button
        v-if="hasChildren"
        @click.stop="emit('toggle', category)"
        class="w-5 h-5 flex items-center justify-center text-ink-3 hover:text-ink-2 hover:bg-surface-2 rounded text-xs transition"
        :aria-label="expanded.has(category.id) ? t('common.collapse') : t('common.expand')"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          class="h-3 w-3 transition-transform"
          :class="{ 'rotate-90': expanded.has(category.id) }"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
        </svg>
      </button>
      <span v-else class="w-5"></span>

      <!-- Category name link -->
      <button
        @click="emit('go', category)"
        class="flex-1 flex items-center justify-between px-2 py-1.5 rounded-md text-left text-sm transition cursor-pointer
          hover:bg-orange-50 dark:hover:bg-slate-800
          text-ink-2
          focus:outline-none focus:ring-2 focus:ring-orange-500 focus:ring-offset-1"
        :class="{
          'bg-orange-600 text-white font-semibold shadow-sm': isActive(),
          'bg-orange-50 text-orange-700 font-medium dark:bg-slate-800 dark:text-white': isActive()
        }"
      >
        <span class="truncate">{{ catName(category) }}</span>
        <span
          v-if="category.products_count != null && category.products_count > 0"
          class="ml-1.5 text-[11px] opacity-80"
        >
          ({{ Number(category.products_count).toLocaleString() }})
        </span>
      </button>
    </div>

    <!-- Children list -->
    <ul
      v-if="expanded.has(category.id) && children.length > 0"
      class="ml-5 mt-0.5 space-y-0.5 border-l border-line pl-2"
    >
      <CategoryTreeNode
        v-for="child in children"
        :key="child.id"
        :category="child"
        :expanded="expanded"
        :active-id="activeId"
        @toggle="(cat) => emit('toggle', cat)"
        @go="(cat) => emit('go', cat)"
      />
    </ul>
  </li>
</template>
