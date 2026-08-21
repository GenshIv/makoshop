<script setup>
import { ref, onMounted, watch, computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import api from '../api';
import CategoryTreeNode from './CategoryTreeNode.vue';

const { t } = useI18n();

const allProductsLabel = computed(() => t('catalog.all_products', 'All products'));

const route = useRoute();
const router = useRouter();

const rootCategories = ref([]);
const loading = ref(false);
const expanded = ref(new Set());
const activeCategoryId = ref(null);

watch(() => route.path, () => {
  if (rootCategories.value.length > 0) {
    resolveActiveCategory();
  }
});

const findCategoryBySlugs = (slugs, nodes) => {
  if (slugs.length === 0) return null;
  for (const node of nodes) {
    if (node.slug === slugs[0]) {
      if (slugs.length === 1) return node;
      if (node.children) {
        const result = findCategoryBySlugs(slugs.slice(1), node.children);
        if (result) return result;
      }
    }
  }
  return null;
};

// Expand all parent categories for a given category ID
const expandParents = (targetId) => {
  const findAndExpand = (nodes) => {
    for (const node of nodes) {
      if (node.id === targetId) return true;
      if (node.children && node.children.length > 0) {
        if (findAndExpand(node.children)) {
          expanded.value.add(node.id);
          return true;
        }
      }
    }
    return false;
  };
  findAndExpand(rootCategories.value);
};

const fetchTree = async () => {
  try {
    const response = await api.get('/categories/tree');
    return Array.isArray(response.data) ? response.data : [];
  } catch (e) {
    console.error('Fetch category tree error:', e);
    return [];
  }
};

// Build path from root to category by walking the tree
const buildCategoryPath = (targetId, nodes, path = []) => {
  for (const node of nodes) {
    path.push(node.slug);
    if (node.id === targetId) {
      return path;
    }
    if (node.children && node.children.length > 0) {
      const result = buildCategoryPath(targetId, node.children, path);
      if (result) return result;
    }
    path.pop();
  }
  return null;
};

const resetFiltersInQuery = (query) => {
  delete query.q;
  delete query.price_min;
  delete query.price_max;
  delete query.sort;
  // Remove all attribute filters (they start with attr_)
  Object.keys(query).forEach(key => {
    if (key.startsWith('attr_')) {
      delete query[key];
    }
  });
};

const goToAllCategory = () => {
  const query = { ...route.query };
  delete query.page;
  // Reset filters if switching from a category to "All"
  if (activeCategoryId.value !== '') {
    resetFiltersInQuery(query);
  }
  router.push({ path: '/shop', query });
};

const goToCategory = (cat) => {
  const slugs = buildCategoryPath(cat.id, rootCategories.value);
  const query = { ...route.query };
  delete query.page;

  // Reset filters if switching to a different category
  if (activeCategoryId.value !== cat.id) {
    resetFiltersInQuery(query);
  }

  let path = '/shop';
  if (slugs && slugs.length > 0) {
    path = '/shop/' + slugs.join('/');
  }

  router.push({ path, query });
};

// Resolve active category from current route path
const resolveActiveCategory = () => {
  if (!route.path.startsWith('/shop/')) {
    activeCategoryId.value = null;
    return;
  }
  const pathSlugs = route.path.slice(6).split('/').filter(Boolean);
  // Try full path first
  let found = findCategoryBySlugs(pathSlugs, rootCategories.value);
  // If not found, last slug might be product/SCU - try without it
  if (!found && pathSlugs.length > 1) {
    found = findCategoryBySlugs(pathSlugs.slice(0, -1), rootCategories.value);
  }
  if (found) {
    activeCategoryId.value = found.id;
    expandParents(found.id);
  } else {
    activeCategoryId.value = null;
  }
};

onMounted(async () => {
  loading.value = true;
  rootCategories.value = await fetchTree();
  loading.value = false;
  // Resolve active category after tree is loaded
  resolveActiveCategory();
});

defineOptions({ name: 'CategoryTree' });
</script>

<template>
  <nav class="space-y-0.5 text-sm category-tree-nav">
    <div v-if="loading" class="text-ink-3 py-2">{{ t('common.loading') }}</div>
    <template v-else>
      <!-- Root "All" category -->
      <CategoryTreeNode
        :category="{ id: '', name_ru: allProductsLabel, name_ua: allProductsLabel, name_pl: allProductsLabel, name_en: allProductsLabel }"
        :expanded="expanded"
        :active-id="activeCategoryId"
        @toggle="() => {}"
        @go="goToAllCategory"
      />

      <ul class="space-y-0.5">
        <CategoryTreeNode
          v-for="cat in rootCategories"
          :key="cat.id"
          :category="cat"
          :expanded="expanded"
          :active-id="activeCategoryId"
          @toggle="(cat) => {
            if (expanded.has(cat.id)) {
              expanded.delete(cat.id);
            } else {
              expanded.add(cat.id);
            }
          }"
          @go="goToCategory"
        />
      </ul>
    </template>
  </nav>
</template>

<style scoped>
/* Smooth expand/collapse for children */
.category-tree-nav ul {
  transition: max-height 0.15s ease-out;
  overflow: hidden;
}
</style>
