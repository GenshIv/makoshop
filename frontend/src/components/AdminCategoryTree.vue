<script setup>
import { ref, watch, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../api';
import { useToast } from '../composables/useToast';
import AdminCategoryTreeNode from './AdminCategoryTreeNode.vue';

const { t, locale } = useI18n();
const { toast } = useToast();

const props = defineProps({
  categories: {
    type: Array,
    default: () => [],
  },
});

const emit = defineEmits(['reordered']);

// Build tree from flat list
const buildTree = (cats) => {
  const map = new Map();
  const roots = [];

  for (const cat of cats) {
    map.set(cat.id, { ...cat, children: [] });
  }

  for (const cat of cats) {
    const node = map.get(cat.id);
    if (cat.parent_id && map.has(cat.parent_id)) {
      map.get(cat.parent_id).children.push(node);
    } else {
      roots.push(node);
    }
  }

  const sortNodes = (nodes) => {
    nodes.sort((a, b) => a.sort_order - b.sort_order || (a.name_en || '').localeCompare(b.name_en || ''));
    nodes.forEach(node => sortNodes(node.children));
  };
  sortNodes(roots);

  return roots;
};

const tree = ref([]);

watch(() => props.categories, (newCats) => {
  tree.value = buildTree(newCats || []);
}, { deep: true, immediate: true });

// Drag state
const draggedId = ref(null);
const dropTarget = ref(null);
const isDragging = ref(false);

// Expand/collapse state
const expanded = ref(new Set());

const toggleExpand = (id) => {
  if (expanded.value.has(id)) {
    expanded.value.delete(id);
  } else {
    expanded.value.add(id);
  }
};

// Get display name based on locale
const getDisplayName = (cat) => {
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || String(cat.id);
};

// Drag handlers
const onDragStart = (event, cat) => {
  draggedId.value = cat.id;
  isDragging.value = true;
  event.dataTransfer.effectAllowed = 'move';
  event.dataTransfer.setData('text/plain', cat.id);
};

const onDragEnd = () => {
  draggedId.value = null;
  dropTarget.value = null;
  isDragging.value = false;
};

const onDragOver = (event, cat) => {
  if (!draggedId.value || draggedId.value === cat.id) return;
  if (isDescendant(draggedId.value, cat.id)) return; // can't drop on own descendant

  event.preventDefault();
  event.dataTransfer.dropEffect = 'move';

  const rect = event.currentTarget.getBoundingClientRect();
  const y = event.clientY - rect.top;
  const height = rect.height;

  let position;
  if (y < height * 0.25) {
    position = 'before';
  } else if (y > height * 0.75) {
    position = 'after';
  } else {
    position = 'inside';
  }

  dropTarget.value = { id: cat.id, position };
};

const onDragLeave = () => {
  dropTarget.value = null;
};

// Check if catId is a descendant of ancestorId
const isDescendant = (ancestorId, catId) => {
  const catMap = new Map((props.categories || []).map(c => [c.id, c]));
  let current = catId;
  while (current) {
    if (current === ancestorId) return true;
    const cat = catMap.get(current);
    if (!cat || !cat.parent_id) return false;
    current = cat.parent_id;
  }
  return false;
};

const onDrop = async (event, cat) => {
  event.preventDefault();
  event.stopPropagation();

  if (!draggedId.value || !dropTarget.value) return;

  const draggedCatId = draggedId.value;
  const targetId = dropTarget.value.id;
  const position = dropTarget.value.position;

  if (draggedCatId === targetId) return;

  const allCats = props.categories || [];
  const catMap = new Map(allCats.map(c => [c.id, c]));

  const draggedCat = catMap.get(draggedCatId);
  const targetCat = catMap.get(targetId);

  if (!draggedCat || !targetCat) return;

  // Determine new parent
  let newParentId = null;

  if (position === 'inside') {
    newParentId = targetId;
  } else {
    newParentId = targetCat.parent_id || null;
  }

  // Build the new order of siblings for the target parent
  const siblings = allCats
    .filter(c => (c.parent_id || null) === newParentId)
    .sort((a, b) => a.sort_order - b.sort_order);

  // Remove dragged category from its old position
  const siblingsWithoutDragged = siblings.filter(c => c.id !== draggedCatId);

  // Determine where to insert the dragged category
  let insertIndex = 0;
  if (position === 'inside') {
    // Add to end
    insertIndex = siblingsWithoutDragged.length;
  } else {
    // Find target's position in the filtered list
    const targetIndex = siblingsWithoutDragged.findIndex(c => c.id === targetId);
    if (position === 'before') {
      insertIndex = targetIndex;
    } else {
      insertIndex = targetIndex + 1;
    }
  }

  // Insert dragged category at the new position
  siblingsWithoutDragged.splice(insertIndex, 0, draggedCat);

  // Build reorder payload - update ALL siblings with new sort orders
  const items = {};

  // Update dragged category with new parent
  items[draggedCatId] = {
    id: draggedCatId,
    parent_id: newParentId,
    sort_order: insertIndex,
  };

  // Update all other siblings in the new parent group
  siblingsWithoutDragged.forEach((sib, index) => {
    if (sib.id !== draggedCatId) {
      items[sib.id] = {
        id: sib.id,
        parent_id: newParentId,
        sort_order: index === insertIndex ? index + 1 : index,
      };
    }
  });

  // Also update siblings in the old parent group (if different)
  const oldParentId = draggedCat.parent_id || null;
  if (oldParentId !== newParentId) {
    const oldSiblings = allCats
      .filter(c => (c.parent_id || null) === oldParentId && c.id !== draggedCatId)
      .sort((a, b) => a.sort_order - b.sort_order);

    oldSiblings.forEach((sib, index) => {
      items[sib.id] = {
        id: sib.id,
        parent_id: oldParentId,
        sort_order: index,
      };
    });
  }

  const finalItems = Object.values(items);

  try {
    await api.post('/admin/categories/reorder', { items: finalItems });
    toast.success(t('admin.categories_reordered', 'Categories reordered'));
    emit('reordered');
  } catch (e) {
    const message = e.response?.data?.message || e.response?.data?.error?.message || t('admin.save_error', 'Failed to reorder');
    toast.error(message);
  }

  onDragEnd();
};

// Get drop indicator class
const getDropIndicatorClass = (cat) => {
  if (!isDragging.value || !dropTarget.value || dropTarget.value.id !== cat.id) {
    return '';
  }
  const { position } = dropTarget.value;
  if (position === 'inside') return 'drop-indicator-inside';
  if (position === 'before') return 'drop-indicator-before';
  return 'drop-indicator-after';
};

const isDragged = (cat) => isDragging.value && draggedId.value === cat.id;

// Make category a child of the previous sibling
const onMakeChild = async (node) => {
  const allCats = props.categories || [];
  const catMap = new Map(allCats.map(c => [c.id, c]));
  
  // Find the previous sibling
  const parent = node.parent_id ? catMap.get(node.parent_id) : null;
  const siblings = allCats
    .filter(c => (c.parent_id || null) === (node.parent_id || null))
    .sort((a, b) => a.sort_order - b.sort_order);
  
  const currentIndex = siblings.findIndex(c => c.id === node.id);
  if (currentIndex <= 0) {
    toast.error(t('admin.no_previous_sibling', 'Cannot make subcategory - no previous sibling'));
    return;
  }
  
  const prevSibling = siblings[currentIndex - 1];
  
  try {
    await api.post('/admin/categories/reorder', {
      items: [{
        id: node.id,
        parent_id: prevSibling.id,
        sort_order: 0,
      }]
    });
    toast.success(t('admin.category_moved', 'Category moved'));
    emit('reordered');
  } catch (e) {
    const message = e.response?.data?.message || e.response?.data?.error?.message || t('admin.save_error', 'Failed to move');
    toast.error(message);
  }
};

// Promote category (make it a sibling of its parent)
const onPromote = async (node) => {
  if (!node.parent_id) return;
  
  const allCats = props.categories || [];
  const catMap = new Map(allCats.map(c => [c.id, c]));
  const parent = catMap.get(node.parent_id);
  
  if (!parent) return;
  
  // Find position in parent's siblings
  const grandParentId = parent.parent_id || null;
  const siblings = allCats
    .filter(c => (c.parent_id || null) === grandParentId)
    .sort((a, b) => a.sort_order - b.sort_order);
  
  const parentIndex = siblings.findIndex(c => c.id === parent.id);
  
  try {
    await api.post('/admin/categories/reorder', {
      items: [{
        id: node.id,
        parent_id: grandParentId,
        sort_order: parentIndex + 1,
      }]
    });
    toast.success(t('admin.category_promoted', 'Category promoted'));
    emit('reordered');
  } catch (e) {
    const message = e.response?.data?.message || e.response?.data?.error?.message || t('admin.save_error', 'Failed to promote');
    toast.error(message);
  }
};

onMounted(() => {
  const expandAll = (nodes) => {
    for (const node of nodes) {
      if (node.children && node.children.length > 0) {
        expanded.value.add(node.id);
        expandAll(node.children);
      }
    }
  };
  expandAll(tree.value);
});
</script>

<template>
  <div class="category-tree-dnd">
    <div v-if="categories.length === 0" class="text-center py-8 text-ink-3">
      {{ t('admin.no_categories', 'No categories') }}
    </div>

    <ul v-else class="space-y-1">
      <AdminCategoryTreeNode
        v-for="cat in tree"
        :key="cat.id"
        :node="cat"
        :expanded="expanded"
        :is-dragged="isDragged"
        :get-drop-class="getDropIndicatorClass"
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
  </div>
</template>

<style>
.drop-indicator-inside {
  border: 3px dashed #9333ea !important;
  background-color: rgba(147, 51, 234, 0.15) !important;
  box-shadow: 0 0 0 4px rgba(147, 51, 234, 0.2);
  animation: pulse-border 1s ease-in-out infinite;
}

.drop-indicator-before {
  position: relative;
}

.drop-indicator-before::before {
  content: '';
  position: absolute;
  top: -2px;
  left: 0;
  right: 0;
  height: 4px;
  background: #9333ea;
  border-radius: 2px;
  animation: pulse-indicator 1s ease-in-out infinite;
}

.drop-indicator-after {
  position: relative;
}

.drop-indicator-after::after {
  content: '';
  position: absolute;
  bottom: -2px;
  left: 0;
  right: 0;
  height: 4px;
  background: #9333ea;
  border-radius: 2px;
  animation: pulse-indicator 1s ease-in-out infinite;
}

.dark .drop-indicator-inside {
  background-color: rgba(147, 51, 234, 0.25) !important;
  box-shadow: 0 0 0 4px rgba(147, 51, 234, 0.3);
}

@keyframes pulse-border {
  0%, 100% {
    box-shadow: 0 0 0 4px rgba(147, 51, 234, 0.2);
  }
  50% {
    box-shadow: 0 0 0 8px rgba(147, 51, 234, 0.1);
  }
}

@keyframes pulse-indicator {
  0%, 100% {
    opacity: 1;
  }
  50% {
    opacity: 0.6;
  }
}
</style>
