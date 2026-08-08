<script setup>
import { ref, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import api from '../../api';

const route = useRoute();
const router = useRouter();

const categoryId = computed(() => parseInt(route.params.id, 10));
const category = ref(null);
const attributes = ref([]);
const loading = ref(true);
const error = ref(null);

// Search filters per attribute
const searchFilters = ref({});

const fetchCategory = async () => {
  try {
    const response = await api.get(`/categories/${categoryId.value}`);
    category.value = response.data;
  } catch (e) {
    console.error('Failed to fetch category:', e);
  }
};

const fetchAttributes = async () => {
  loading.value = true;
  error.value = null;
  try {
    const response = await api.get(`/admin/categories/${categoryId.value}/attributes`);
    const items = response.data.attributes || response.data.items || response.data || [];
    attributes.value = Array.isArray(items) ? items : [];
  } catch (e) {
    error.value = e.response?.data?.message || 'Ошибка загрузки атрибутов';
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const setSearch = (code, value) => {
  searchFilters.value[code] = value;
};

// Group attributes by type
const groupedAttrs = computed(() => {
  const groups = {};
  for (const attr of attributes.value) {
    const type = attr.type || 'text';
    if (!groups[type]) {
      groups[type] = [];
    }
    groups[type].push(attr);
  }
  return groups;
});

// Get options/values for an attribute (backend returns "values", not "options")
const getOptions = (attr) => {
  return attr.options || attr.values || [];
};

// Filter options for an attribute
const filteredOptions = (attr) => {
  const opts = getOptions(attr);
  if (!Array.isArray(opts)) return [];
  const search = (searchFilters.value[attr.code] || '').toLowerCase();
  if (!search) return opts;
  return opts.filter(opt => String(opt).toLowerCase().includes(search));
};

// Max tags to show without scroll
const MAX_TAGS = 7;

const visibleTags = (attr) => {
  const opts = filteredOptions(attr);
  return opts.slice(0, MAX_TAGS);
};

const hiddenTags = (attr) => {
  const opts = filteredOptions(attr);
  return opts.slice(MAX_TAGS);
};

const hasMoreTags = (attr) => hiddenTags(attr).length > 0;

const hasOptions = (attr) => getOptions(attr).length > 0;

const humanizeAttrName = (code) => {
  if (!code) return '';
  let s = code.replace(/_/g, ' ').replace(/-/g, ' ');
  return s.replace(/\b\w/g, c => c.toUpperCase());
};

const typeLabels = {
  text: 'Текст',
  number: 'Число',
  boolean: 'Да/Нет',
  select: 'Выбор',
  multiselect: 'Множественный выбор',
  date: 'Дата',
  enum: 'Значения',
};

onMounted(() => {
  fetchCategory();
  fetchAttributes();
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Header -->
    <div class="flex items-center gap-4 mb-6">
      <button
        @click="router.push({ name: 'admin-categories' })"
        class="p-2 text-gray-500 hover:text-gray-700 hover:bg-gray-100 rounded-lg transition"
      >
        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
        </svg>
      </button>
      <div>
        <h1 class="text-2xl font-bold text-gray-800">
          {{ category?.name || `Категория #${categoryId}` }} — Атрибуты
        </h1>
        <p class="text-sm text-gray-500 mt-0.5">
          {{ attributes.length }} атрибутов
        </p>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="mb-4 p-3 bg-red-50 text-red-700 rounded-lg text-sm">
      {{ error }}
    </div>

    <!-- Empty -->
    <div v-else-if="attributes.length === 0" class="text-center py-12 text-gray-500">
      <p>Атрибутов у этой категории нет</p>
    </div>

    <!-- Attributes by type -->
    <div v-else class="space-y-6">
      <div
        v-for="(attrs, type) in groupedAttrs"
        :key="type"
        class="bg-white rounded-lg shadow-sm border border-gray-200 overflow-hidden"
      >
        <!-- Group header -->
        <div class="px-4 py-3 bg-gray-50 border-b border-gray-200 flex items-center gap-2">
          <span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-700">
            {{ typeLabels[type] || type }}
          </span>
          <span class="text-xs text-gray-500">{{ attrs.length }} шт.</span>
        </div>

        <!-- Attributes list -->
        <div class="p-4 space-y-4">
          <div v-for="attr in attrs" :key="attr.code" class="space-y-2">
            <!-- Attribute name + search -->
            <div class="flex items-center gap-3">
              <label class="text-sm font-medium text-gray-700">
                {{ attr.name || humanizeAttrName(attr.code) }}
              </label>
              <span class="text-xs text-gray-400 font-mono">{{ attr.code }}</span>
              <span v-if="attr.is_required" class="text-[10px] px-1.5 py-0.5 rounded bg-red-100 text-red-600">
                обязательно
              </span>
              <span v-if="attr.is_filterable" class="text-[10px] px-1.5 py-0.5 rounded bg-green-100 text-green-600">
                фильтр
              </span>
              <!-- Search input for this attribute -->
              <input
                v-if="getOptions(attr).length > MAX_TAGS"
                v-model="searchFilters[attr.code]"
                type="text"
                placeholder="Поиск тегов..."
                class="ml-auto px-2 py-1 border border-gray-300 rounded text-xs w-40 focus:outline-none focus:ring-2 focus:ring-purple-500"
              />
            </div>

            <!-- Tags -->
            <div v-if="hasOptions(attr)" class="space-y-2">
              <!-- Visible tags -->
              <div class="flex flex-wrap gap-1.5">
                <span
                  v-for="tag in visibleTags(attr)"
                  :key="tag"
                  class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-purple-50 text-purple-700 border border-purple-200 hover:bg-purple-100 transition cursor-default"
                >
                  {{ tag }}
                </span>
              </div>

              <!-- Scrollable area for hidden tags -->
              <div
                v-if="hasMoreTags(attr)"
                class="border border-gray-200 rounded-lg p-2 max-h-48 overflow-y-auto bg-gray-50"
              >
                <div class="flex flex-wrap gap-1.5">
                  <span
                    v-for="tag in hiddenTags(attr)"
                    :key="tag"
                    class="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-medium bg-purple-50 text-purple-700 border border-purple-200 hover:bg-purple-100 transition cursor-default"
                  >
                    {{ tag }}
                  </span>
                </div>
              </div>
            </div>

            <!-- No options -->
            <div v-else class="text-xs text-gray-400 italic">
              Значений нет
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
