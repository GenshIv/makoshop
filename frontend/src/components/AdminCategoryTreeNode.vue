<script setup>
defineOptions({ name: 'AdminCategoryTreeNode' });

const props = defineProps({
  node: {
    type: Object,
    required: true,
  },
  expanded: {
    type: Object,
    required: true,
  },
  isDragged: {
    type: Function,
    required: true,
  },
  getDropClass: {
    type: Function,
    required: true,
  },
  getDisplayName: {
    type: Function,
    required: true,
  },
  toggleExpand: {
    type: Function,
    required: true,
  },
  onDragStart: {
    type: Function,
    required: true,
  },
  onDragEnd: {
    type: Function,
    required: true,
  },
  onDragOver: {
    type: Function,
    required: true,
  },
  onDragLeave: {
    type: Function,
    required: true,
  },
  onDrop: {
    type: Function,
    required: true,
  },
  onMakeChild: {
    type: Function,
    required: true,
  },
  onPromote: {
    type: Function,
    required: true,
  },
  t: {
    type: Function,
    required: true,
  },
});
</script>

<template>
  <li>
    <div
      :class="[
        'flex items-center gap-2 px-3 py-2 rounded-lg cursor-pointer transition-all',
        isDragged(node) ? 'opacity-50 bg-purple-100 dark:bg-purple-900/30' : 'hover:bg-surface-2',
        getDropClass(node)
      ]"
      draggable="true"
      @dragstart="onDragStart($event, node)"
      @dragend="onDragEnd"
      @dragover="onDragOver($event, node)"
      @dragleave="onDragLeave"
      @drop="onDrop($event, node)"
    >
      <!-- Expand/collapse button -->
      <button
        v-if="node.children && node.children.length > 0"
        @click.stop="toggleExpand(node.id)"
        class="w-5 h-5 flex items-center justify-center text-ink-3 hover:text-ink-2 transition-transform"
        :class="{ 'rotate-90': expanded.has(node.id) }"
      >
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
      </button>
      <span v-else class="w-5 h-5"></span>

      <!-- Drag handle -->
      <svg class="w-4 h-4 text-ink-3 cursor-grab" fill="currentColor" viewBox="0 0 20 20">
        <path d="M7 3a1 1 0 110-2 1 1 0 010 2zm7 0a1 1 0 110-2 1 1 0 010 2zM7 7a1 1 0 110-2 1 1 0 010 2zm7 0a1 1 0 110-2 1 1 0 010 2zm-7 4a1 1 0 110-2 1 1 0 010 2zm7 0a1 1 0 110-2 1 1 0 010 2zm-7 4a1 1 0 110-2 1 1 0 010 2zm7 0a1 1 0 110-2 1 1 0 010 2z" />
      </svg>

      <!-- Category name -->
      <span class="flex-1 text-sm font-medium">{{ getDisplayName(node) }}</span>

      <!-- Sort order badge -->
      <span class="text-xs text-ink-3 bg-surface-2 px-2 py-0.5 rounded">
        #{{ node.sort_order }}
      </span>

      <!-- Active status -->
      <span
        v-if="!node.is_active"
        class="text-xs text-ink-3 bg-surface-2 px-2 py-0.5 rounded"
      >
        {{ t('admin.inactive', 'Inactive') }}
      </span>

      <!-- Level controls -->
      <div class="flex gap-1 ml-2">
        <!-- Make subcategory (move right) -->
        <button
          @click.stop="onMakeChild(node)"
          class="p-1 rounded hover:bg-surface-3 text-ink-3 hover:text-ink-1"
          :title="t('admin.make_subcategory', 'Make subcategory')"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
          </svg>
        </button>
        
        <!-- Promote (move left) - only if has parent -->
        <button
          v-if="node.parent_id"
          @click.stop="onPromote(node)"
          class="p-1 rounded hover:bg-surface-3 text-ink-3 hover:text-ink-1"
          :title="t('admin.promote', 'Promote')"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
          </svg>
        </button>
      </div>
    </div>

    <!-- Children -->
    <ul
      v-if="node.children && node.children.length > 0 && expanded.has(node.id)"
      class="ml-6 space-y-1"
    >
      <AdminCategoryTreeNode
        v-for="child in node.children"
        :key="child.id"
        :node="child"
        :expanded="expanded"
        :is-dragged="isDragged"
        :get-drop-class="getDropClass"
        :get-display-name="getDisplayName"
        :toggle-expand="toggleExpand"
        :on-drag-start="onDragStart"
        :on-drag-end="onDragEnd"
        :on-drag-over="onDragOver"
        :on-drag-leave="onDragLeave"
        :on-drop="onDrop"
        :on-make-child="onMakeChild"
        :on-promote="onPromote"
        :t="t"
      />
    </ul>
  </li>
</template>
