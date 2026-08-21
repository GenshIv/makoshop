<script setup>
// Reusable skeleton rows for list-style pages (orders, reviews, etc.).
// Props:
//   count - number of rows to render (default 4)
defineProps({
  count: { type: Number, default: 4 },
});
</script>

<template>
  <div class="space-y-3" aria-hidden="true">
    <div
      v-for="i in count"
      :key="i"
      class="bg-surface rounded-lg border border-line p-4 flex items-center gap-4"
    >
      <div class="w-14 h-14 rounded-lg bg-surface-3 animate-pulse flex-shrink-0"></div>
      <div class="flex-1 space-y-2">
        <div class="h-3 w-1/3 bg-surface-3 rounded animate-pulse"></div>
        <div class="h-3 w-1/4 bg-surface-3 rounded animate-pulse"></div>
      </div>
      <div class="h-4 w-16 bg-surface-3 rounded animate-pulse"></div>
    </div>
  </div>
</template>
