<template>
  <Transition name="search-overlay">
    <div v-if="modelValue" class="fixed inset-0 bg-black/50 z-[1000] flex justify-center pt-20" @click.self="close">
      <!-- Watermark -->
      <div class="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 pointer-events-none opacity-[0.1] select-none z-0">
        <span class="text-[16rem] font-black text-white tracking-tighter whitespace-nowrap">wszyst.pl</span>
      </div>
      <div class="bg-surface rounded-2xl shadow-2xl w-[90%] max-w-[640px] max-h-[70vh] flex flex-col overflow-hidden relative z-10">
        <!-- Search Input -->
        <div class="p-4 border-b border-line">
          <div class="search-input-wrapper flex items-center gap-3 bg-surface-2 rounded-xl px-4 py-3 transition-all duration-200">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 text-accent flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              ref="inputEl"
              type="text"
              class="flex-1 border-none outline-none bg-transparent text-lg text-ink placeholder:text-ink-3"
              :placeholder="t('common.search_overlay_placeholder')"
              :value="query"
              @input="$emit('update:query', $event.target.value)"
              @keyup.enter.prevent="selectFirst"
            />
            <button class="text-ink-3 hover:text-ink cursor-pointer p-1 transition-colors" @click="close">
              <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        <!-- Results -->
        <div class="overflow-y-auto flex-1 p-4">
          <div v-if="loading" class="text-center text-ink-3 py-8">Szukam...</div>

          <div v-else-if="error" class="text-center text-error py-8">{{ error }}</div>

          <div v-else-if="!query || query.length < 2" class="text-center text-ink-3 py-8">
            {{ t('common.search_overlay_min_chars') }}
          </div>

          <div v-else-if="Object.keys(groupedResults).length === 0" class="text-center text-ink-3 py-8">
            {{ t('common.search_overlay_no_results', { query }) }}
          </div>

          <div v-else>
            <template v-for="(group, catName) in groupedResults" :key="catName">
              <!-- Category Header -->
              <div
                class="flex items-center justify-between py-3 cursor-pointer select-none hover:bg-surface-2 rounded-lg px-2 transition-colors mb-1"
                @click="toggleCategory(catName)"
              >
                <span class="font-semibold text-sm uppercase tracking-wide text-accent">{{ catName }}</span>
                <svg
                  :class="['w-4 h-4 text-ink-3 transition-transform duration-200', expandedCategories[catName] ? 'rotate-180' : '']"
                  fill="none" viewBox="0 0 24 24" stroke="currentColor"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                </svg>
              </div>

              <!-- Category Items -->
              <template v-if="expandedCategories[catName]">
                <button
                  v-for="item in group.items"
                  :key="item.id"
                  class="w-full flex gap-3 items-center p-3 bg-surface border border-line rounded-xl mb-2 hover:border-orange-300 dark:hover:bg-surface-elevated transition-all duration-200 text-left cursor-pointer"
                  @click="selectItem(item)"
                >
                  <img
                    v-if="item.images && item.images.length > 0"
                    :src="item.images[0]"
                    :alt="item.title"
                    class="w-14 h-14 object-cover rounded-lg flex-shrink-0 bg-surface"
                    loading="lazy"
                    onerror="this.style.display='none'"
                  />
                  <div class="flex-1 min-w-0">
                    <span class="text-[15px] text-ink block overflow-hidden text-ellipsis whitespace-nowrap">{{ item.title }}</span>
                  </div>
                  <span v-if="item.min_price != null" class="font-bold text-accent text-lg flex-shrink-0">{{ formatPrice(item.min_price, item.currency) }}</span>
                </button>
                <a
                  :href="`/shop/${group.slug}?q=${encodeURIComponent(query)}&limit=60`"
                  class="block text-center py-2 px-3 text-sm font-bold text-accent hover:bg-surface-3 transition-colors rounded-lg dark:text-orange-400"
                >
                  {{ t('common.search_overlay_show_all', { catName }) }} →
                </a>
                <div class="border-t border-line mt-1 mb-1"></div>
              </template>
            </template>
          </div>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup>
import { ref, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import axios from 'axios'

const { t } = useI18n()

const props = defineProps({
  modelValue: Boolean,
  query: String,
})

const emit = defineEmits(['update:modelValue', 'update:query'])

const inputEl = ref(null)
const loading = ref(false)
const results = ref([])
const error = ref('')
const categories = ref([])
const expandedCategories = ref({})

function handleKeydown(e) {
  if (e.key === 'Escape' && props.modelValue) {
    close()
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})

watch(() => props.modelValue, (val) => {
  if (val) {
    nextTick(() => {
      inputEl.value?.focus()
    })
    loadCategories()
  }
})

async function loadCategories() {
  try {
    const res = await axios.get('/categories/tree', {
      headers: { Accept: 'application/json' },
    })
    categories.value = res.data.filter(c => !c.parent_id)
  } catch (e) {
    // Ignore errors loading categories
  }
}

async function searchProducts(q) {
  if (!q || q.length < 2 || categories.value.length === 0) {
    results.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const requests = categories.value.map(cat =>
      axios.get(`/shop/${cat.slug}?q=${encodeURIComponent(q)}&limit=5`, {
        headers: { Accept: 'application/json' },
      }).then(res => ({
        category: cat,
        items: res.data.items || [],
      })).catch(() => null)
    )

    const responses = await Promise.all(requests)
    results.value = []
    for (const resp of responses) {
      if (resp && resp.items.length > 0) {
        const catName = resp.category.name_pl || resp.category.name_ru || resp.category.name_en
        expandedCategories.value[catName] = true
        results.value.push(...resp.items.map(item => ({
          ...item,
          _category_name: catName,
          _category_slug: resp.category.slug,
        })))
      }
    }
  } catch (e) {
    error.value = 'Błąd wyszukiwania'
  } finally {
    loading.value = false
  }
}

watch(() => props.query, searchProducts)

const groupedResults = computed(() => {
  const groups = {}
  for (const item of results.value) {
    const catName = item._category_name || 'Bez kategorii'
    if (!groups[catName]) {
      groups[catName] = { items: [], slug: item._category_slug }
    }
    groups[catName].items.push(item)
  }
  return groups
})

function toggleCategory(catName) {
  expandedCategories.value[catName] = !expandedCategories.value[catName]
}

function formatPrice(price, currency) {
  if (price == null) return ''
  const fmt = new Intl.NumberFormat('pl-PL', {
    style: 'currency',
    currency: currency || 'PLN',
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })
  return fmt.format(price)
}

function selectItem(item) {
  window.location.href = item.seo_url || `/shop/${item.slug}`
}

function selectFirst() {
  if (results.value.length > 0) {
    selectItem(results.value[0])
  }
}

function close() {
  emit('update:modelValue', false)
  emit('update:query', '')
}
</script>

<style scoped>
.search-overlay-enter-active,
.search-overlay-leave-active {
  transition: opacity 0.2s ease;
}

.search-overlay-enter-from,
.search-overlay-leave-to {
  opacity: 0;
}

.search-input-wrapper:focus-within {
  border: 2px solid var(--accent, #6366f1);
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
}

.search-input-wrapper input:focus {
  outline: none;
  border: none;
}
</style>
