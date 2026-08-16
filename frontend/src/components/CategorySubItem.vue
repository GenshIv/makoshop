<script setup>
import { useI18n } from 'vue-i18n';

const { locale } = useI18n();

const props = defineProps({
  category: { type: Object, required: true },
  level: { type: Number, default: 1 },
  expandedIds: { type: Array, required: true },
});

const emit = defineEmits(['toggle', 'go']);

const catName = (cat) => {
  if (!cat) return '';
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || '';
};

const children = props.category.children || [];
const hasChildren = children.length > 0;
const isExpanded = props.expandedIds.includes(props.category.id);

// Image size decreases with depth
const getImageSize = () => {
  if (props.level === 1) return { w: 'w-10', h: 'h-10', icon: 'h-4 w-4' };
  if (props.level === 2) return { w: 'w-8', h: 'h-8', icon: 'h-3.5 w-3.5' };
  return { w: 'w-7', h: 'h-7', icon: 'h-3 w-3' };
};

const imgSize = getImageSize();

const handleToggle = (e) => {
  e.stopPropagation();
  emit('toggle', props.category.id);
};

const handleGo = () => {
  emit('go', props.category);
};
</script>

<template>
  <div class="relative">
    <!-- Category item -->
    <div
      class="flex items-center gap-2 p-2 rounded-lg border border-line cursor-pointer hover:bg-surface-2 hover:border-indigo-200 transition-all duration-150"
      @click="handleGo"
    >
      <!-- Toggle button (if has children) -->
      <button
        v-if="hasChildren"
        @click="handleToggle"
        class="flex-shrink-0 w-5 h-5 flex items-center justify-center text-ink-3 hover:text-ink-2 hover:bg-surface-2 rounded text-xs transition"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          class="h-3 w-3 transition-transform duration-150"
          :class="{ 'rotate-90': isExpanded }"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2.5"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
        </svg>
      </button>
      <span v-else class="flex-shrink-0 w-5"></span>

      <!-- Subcategory image -->
      <div :class="['flex-shrink-0', imgSize.w, imgSize.h, 'rounded-lg bg-surface-3 overflow-hidden']">
        <img
          v-if="category.image_light || category.image_dark"
          :src="category.image_light || category.image_dark"
          :alt="catName(category)"
          loading="lazy"
          decoding="async"
          class="w-full h-full object-cover"
        />
        <div
          v-else
          class="w-full h-full flex items-center justify-center text-ink-3"
        >
          <svg xmlns="http://www.w3.org/2000/svg" :class="imgSize.icon" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z" />
          </svg>
        </div>
      </div>

      <!-- Subcategory name -->
      <div class="min-w-0 flex-1">
        <div class="text-[11px] font-medium text-ink truncate">
          {{ catName(category) }}
        </div>
        <div
          v-if="category.products_count != null && category.products_count > 0"
          class="text-[11px] text-ink-3"
        >
          {{ Number(category.products_count).toLocaleString() }}
        </div>
      </div>
    </div>

    <!-- Children (nested) -->
    <div
      v-if="isExpanded && hasChildren"
      class="mt-1 ml-5 pl-2 border-l border-line space-y-1"
    >
      <CategorySubItem
        v-for="child in children"
        :key="child.id"
        :category="child"
        :level="level + 1"
        :expanded-ids="expandedIds"
        @toggle="(id) => $emit('toggle', id)"
        @go="(cat) => $emit('go', cat)"
      />
    </div>
  </div>
</template>

<style scoped>
/* Smooth expand/collapse */
.space-y-1 {
  transition: max-height 0.2s ease-out, opacity 0.15s ease-out;
}
</style>
