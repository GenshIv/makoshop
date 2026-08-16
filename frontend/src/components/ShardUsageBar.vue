<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../api';

const { t } = useI18n();

const shards = ref([]);
const totalShards = ref(0);
const totalFreeOffset = ref(0);
const totalMaxSize = ref(0);
const loading = ref(false);
const loadingActive = ref(false);
const compacting = ref(false);
const compactResult = ref(null);
const error = ref(null);
const showDetails = ref(false);
const mode = ref('fast'); // 'fast' or 'active'

const fetchShards = async (active = false) => {
  loading.value = true;
  error.value = null;
  try {
    const endpoint = active ? '/admin/db/shards/active' : '/admin/db/shards';
    const res = await api.get(endpoint);
    shards.value = res.data || [];
    mode.value = active ? 'active' : 'fast';
    computeSummary();
  } catch (e) {
    error.value = e.message || String(e);
  } finally {
    loading.value = false;
    loadingActive.value = false;
  }
};

const fetchActive = async () => {
  if (loadingActive.value) return;
  loadingActive.value = true;
  await fetchShards(true);
};

const computeSummary = () => {
  totalShards.value = shards.value.length;
  totalFreeOffset.value = shards.value.reduce((sum, s) => sum + (s.FreeOffset || 0), 0);
  totalMaxSize.value = shards.value.reduce((sum, s) => sum + (s.MaxSize || 0), 0);
};

const usageRatio = () => {
  if (totalMaxSize.value === 0) return 0;
  return totalFreeOffset.value / totalMaxSize.value;
};

const usagePercent = () => {
  return (usageRatio() * 100).toFixed(1);
};

const getShardColor = (shard) => {
  if (shard.MaxSize === 0) return 'bg-gray-300';
  const ratio = shard.FreeOffset / shard.MaxSize;
  if (ratio < 0.5) return 'bg-green-500';
  if (ratio < 0.75) return 'bg-yellow-500';
  if (ratio < 0.9) return 'bg-orange-500';
  return 'bg-red-600';
};

const getShardTooltip = (shard) => {
  const parts = [
    `Shard ${shard.ShardIndex}`,
    `FreeOffset: ${formatBytes(shard.FreeOffset)}`,
    `MaxSize: ${formatBytes(shard.MaxSize)}`,
    `FileSize: ${formatBytes(shard.FileSize)}`,
  ];
  if (shard.ActiveKeys > 0) {
    parts.push(`ActiveKeys: ${shard.ActiveKeys}`);
    parts.push(`ActiveData: ${formatBytes(shard.ActiveDataBytes)}`);
  }
  return parts.join('\n');
};

const formatBytes = (bytes) => {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return (bytes / Math.pow(1024, i)).toFixed(1) + ' ' + units[i];
};

const formatPercent = (shard) => {
  if (shard.MaxSize === 0) return '0%';
  return ((shard.FreeOffset / shard.MaxSize) * 100).toFixed(0) + '%';
};

const runCompact = async () => {
  if (compacting.value) return;
  compacting.value = true;
  compactResult.value = null;
  error.value = null;
  try {
    await api.post('/admin/db/compact');
    compactResult.value = { ok: true, message: 'Compact completed successfully' };
    // Refresh stats after compaction
    await fetchShards(true);
  } catch (e) {
    compactResult.value = { ok: false, message: e.message || String(e) };
  } finally {
    compacting.value = false;
  }
};

// Auto-refresh fast stats every 15 seconds
let interval = null;
onMounted(() => {
  fetchShards(false);
  interval = setInterval(() => fetchShards(false), 15000);
});

onUnmounted(() => {
  if (interval) clearInterval(interval);
});
</script>

<template>
  <div class="flex items-center gap-2 text-xs">
    <!-- Summary -->
    <div
      class="flex items-center gap-1.5 px-2 py-1 rounded-full cursor-pointer select-none"
      :class="{
        'bg-green-100 text-green-800': usageRatio() < 0.5,
        'bg-yellow-100 text-yellow-800': usageRatio() >= 0.5 && usageRatio() < 0.75,
        'bg-orange-100 text-orange-800': usageRatio() >= 0.75 && usageRatio() < 0.9,
        'bg-red-100 text-red-800': usageRatio() >= 0.9,
      }"
      @click="showDetails = !showDetails"
      :title="t('db_monitor.usage_title', { percent: usagePercent(), used: formatBytes(totalFreeOffset), max: formatBytes(totalMaxSize) })"
    >
      <!-- Icon -->
      <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 7v10c0 1.1.9 2 2 2h12a2 2 0 002-2V7a2 2 0 00-2-2H6a2 2 0 00-2 2zm0 0h16M4 11h16" />
      </svg>
      <span class="font-medium">{{ usagePercent() }}%</span>
      <span class="text-[11px] opacity-70">{{ mode === 'active' ? '(active)' : '' }}</span>
    </div>

    <!-- Active scan button -->
    <button
      @click="fetchActive"
      :disabled="loadingActive"
      class="px-1.5 py-0.5 text-[11px] rounded-full bg-surface-2 text-ink-3 hover:bg-surface-3 disabled:opacity-50"
      :title="t('db_monitor.active_scan')"
    >
      {{ loadingActive ? '...' : 'Active' }}
    </button>

    <!-- Error indicator -->
    <span v-if="error" class="text-[11px] text-red-500" :title="error">!</span>

    <!-- Details dropdown -->
    <div
      v-if="showDetails"
      class="absolute top-10 right-4 z-50 bg-surface border border-line rounded-lg shadow-xl p-3 w-[340px] max-h-[60vh] overflow-y-auto text-xs"
    >
      <div class="flex items-center justify-between mb-1">
        <span class="font-semibold text-ink-2">{{ t('db_monitor.shards_usage') }}</span>
        <button @click="showDetails = false" class="text-ink-3 hover:text-ink-2" :aria-label="t('common.close')">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>
      <div class="text-[11px] text-ink-3 mb-2">
        {{ formatBytes(totalFreeOffset) }} / {{ formatBytes(totalMaxSize) }} ({{ usagePercent() }}%)
      </div>

      <div v-if="loading" class="text-ink-3">{{ t('common.loading') }}</div>

      <div v-else-if="shards.length === 0" class="text-ink-3">{{ t('common.no_data') }}</div>

      <div v-else class="space-y-1">
        <div
          v-for="shard in shards"
          :key="shard.ShardIndex"
          class="flex items-center gap-2"
          :title="getShardTooltip(shard)"
        >
          <!-- Number -->
          <span class="w-5 text-right text-ink-3 text-[11px]">{{ shard.ShardIndex }}</span>

          <!-- Bar -->
          <div class="flex-1 h-2 bg-surface-2 rounded-full overflow-hidden">
            <div
              class="h-full rounded-full transition-all"
              :class="getShardColor(shard)"
              :style="{ width: formatPercent(shard) }"
            ></div>
          </div>

          <!-- Percent -->
          <span class="w-10 text-right text-[11px] text-ink-3">{{ formatPercent(shard) }}</span>
        </div>

        <!-- Legend -->
        <div class="mt-3 pt-2 border-t border-line flex flex-wrap gap-2 text-[11px] text-ink-3">
          <span class="flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-green-500"></span> &lt;50%</span>
          <span class="flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-yellow-500"></span> 50-75%</span>
          <span class="flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-orange-500"></span> 75-90%</span>
          <span class="flex items-center gap-1"><span class="w-2 h-2 rounded-full bg-red-600"></span> &gt;90%</span>
        </div>

        <div v-if="mode === 'active'" class="mt-2 pt-2 border-t border-line space-y-1">
          <div class="text-[11px] text-ink-3">
            {{ t('db_monitor.mode_active') }}
          </div>
          <button
            @click="runCompact"
            :disabled="compacting || loading"
            class="w-full mt-1 px-2 py-1.5 text-[11px] rounded-md bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {{ compacting ? t('db_monitor.compact_running') : t('db_monitor.compact_button') }}
          </button>
          <div v-if="compactResult" class="text-[11px]" :class="compactResult.ok ? 'text-green-600' : 'text-red-600'">
            {{ compactResult.message }}
          </div>
        </div>
        <div v-else class="mt-1 text-[11px] text-ink-3">
          {{ t('db_monitor.mode_fast') }}
        </div>
      </div>
    </div>
  </div>
</template>
