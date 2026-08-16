<script setup>
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';

const { locale, t } = useI18n();

const props = defineProps({
  // Array of { slug, name } for category path
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

// Get localized category name based on current locale
const catDisplayName = (cat) => {
  if (!cat) return '';
  const langField = `name_${locale.value}`;
  return cat[langField] || cat.name_en || cat.name_ru || cat.name_ua || cat.name_pl || '';
};

const crumbs = computed(() => {
  const items = [];
  
  // Always start with catalog
  items.push({ name: t('catalog.all_products'), to: { path: '/' } });
  
  // Add category path using slugs
  let path = '/shop';
  for (const cat of props.categories) {
    path += '/' + (cat.slug || cat.name_en?.toLowerCase().replace(/[^a-z0-9]+/g, '-') || '');
    items.push({
      name: catDisplayName(cat),
      to: { path },
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
  <nav v-if="crumbs.length > 1" class="flex items-center gap-1 text-sm text-ink-3 mb-4 overflow-hidden">
    <template v-for="(crumb, idx) in crumbs" :key="idx">
      <template v-if="idx > 0">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 text-ink-3 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7" />
        </svg>
      </template>
      <router-link
        v-if="crumb.to"
        :to="crumb.to"
        class="hover:text-indigo-600 transition min-w-0 truncate max-w-[10rem] sm:max-w-[16rem]"
        :title="crumb.name"
      >
        {{ crumb.name }}
      </router-link>
      <span v-else class="text-ink-2 min-w-0 truncate max-w-[10rem] sm:max-w-[16rem]" :title="crumb.name">
        {{ crumb.name }}
      </span>
    </template>
  </nav>
</template>
