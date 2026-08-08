<script setup>
import { computed } from 'vue';
import { useRouter } from 'vue-router';

const router = useRouter();

const props = defineProps({
  // Array of { id, name } for category path
  categories: {
    type: Array,
    default: () => [],
  },
  // Optional product name for product pages
  productName: {
    type: String,
    default: '',
  },
});

const crumbs = computed(() => {
  const items = [];
  
  // Always start with catalog
  items.push({ name: 'Каталог', to: { name: 'catalog' } });
  
  // Add category path
  for (const cat of props.categories) {
    items.push({
      name: cat.name,
      to: {
        name: 'catalog',
        query: { category_id: String(cat.id) },
      },
    });
  }
  
  // Add product name if provided
  if (props.productName) {
    items.push({ name: props.productName });
  }
  
  return items;
});
</script>

<template>
  <nav v-if="crumbs.length > 1" class="flex items-center gap-1 text-sm text-gray-500 mb-4">
    <template v-for="(crumb, idx) in crumbs" :key="idx">
      <template v-if="idx > 0">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
      </template>
      <router-link
        v-if="crumb.to"
        :to="crumb.to"
        class="hover:text-indigo-600 transition"
      >
        {{ crumb.name }}
      </router-link>
      <span v-else class="text-gray-700">
        {{ crumb.name }}
      </span>
    </template>
  </nav>
</template>
