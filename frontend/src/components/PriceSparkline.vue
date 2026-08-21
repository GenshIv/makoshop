<script setup>
import { computed } from 'vue';

const props = defineProps({
  values: {
    type: Array,
    default: () => [],
  },
  width: { type: Number, default: 96 },
  height: { type: Number, default: 32 },
  strokeWidth: { type: Number, default: 1.5 },
});

const PAD = 3;

const points = computed(() => {
  const vals = (props.values || []).map(Number).filter((v) => Number.isFinite(v));
  if (vals.length < 2) return null;
  const min = Math.min(...vals);
  const max = Math.max(...vals);
  const range = max - min || 1;
  const w = props.width;
  const h = props.height;
  const step = (w - PAD * 2) / (vals.length - 1);
  return vals
    .map((v, i) => {
      const x = PAD + i * step;
      const y = PAD + (h - PAD * 2) * (1 - (v - min) / range);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
});

const hasData = computed(() => points.value !== null);

const areaPoints = computed(() => {
  if (!points.value) return '';
  return `${PAD},${props.height - PAD} ${points.value} ${props.width - PAD},${props.height - PAD}`;
});
</script>

<template>
  <svg
    v-if="hasData"
    :width="width"
    :height="height"
    :viewBox="`0 0 ${width} ${height}`"
    class="block"
    aria-hidden="true"
  >
    <!-- Subtle area fill under the line -->
    <polygon :points="areaPoints" fill="currentColor" opacity="0.12" />
    <polyline
      :points="points"
      fill="none"
      stroke="currentColor"
      :stroke-width="strokeWidth"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  </svg>
</template>
