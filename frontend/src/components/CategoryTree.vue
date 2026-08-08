<script setup>
import { ref, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import api from '../api';
import CategoryTreeNode from './CategoryTreeNode.vue';

const route = useRoute();
const router = useRouter();

const rootCategories = ref([]);
const loading = ref(false);
const expanded = ref(new Set());
const activeCategoryId = ref(null);

watch(() => route.query.category_id, (val) => {
  if (!val) {
    activeCategoryId.value = null;
  } else {
    const id = Number(val);
    activeCategoryId.value = Number.isFinite(id) ? id : null;
  }
});

const fetchTree = async () => {
  try {
    const response = await api.get('/categories/tree');
    return Array.isArray(response.data) ? response.data : [];
  } catch (e) {
    console.error('Fetch category tree error:', e);
    return [];
  }
};

const goToCategory = (cat) => {
  const query = { ...route.query };
  query.category_id = String(cat.id);
  delete query.page;
  router.push({ path: '/', query });
};

onMounted(async () => {
  loading.value = true;
  rootCategories.value = await fetchTree();
  loading.value = false;
});

defineOptions({ name: 'CategoryTree' });
</script>

<template>
  <nav class="space-y-0.5 text-sm">
    <div v-if="loading" class="text-gray-400 py-2">Загрузка...</div>
    <template v-else>
      <!-- Root "All" category -->
      <CategoryTreeNode
        :category="{ id: '', name: 'Все товары' }"
        :expanded="expanded"
        :active-id="activeCategoryId"
        @toggle="() => {}"
        @go="() => {
          const query = { ...route.query };
          delete query.category_id;
          delete query.page;
          router.push({ path: '/', query });
        }"
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
