<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue';
import api from '../../api';
import ApexCharts from 'apexcharts';

const loading = ref(false);
const error = ref('');
const stats = ref(null);

const charts = ref({});

const fetchStats = async (force = false) => {
  loading.value = true;
  error.value = '';
  try {
    const url = force ? '/admin/stats?refresh=1' : '/admin/stats';
    const res = await api.get(url);
    stats.value = res.data;
    renderCharts();
  } catch (e) {
    error.value = e.response?.data?.message || e.message;
  } finally {
    loading.value = false;
  }
};

const destroyCharts = () => {
  Object.values(charts.value).forEach(c => {
    try { c.destroy(); } catch {}
  });
  charts.value = {};
};

const renderCharts = () => {
  destroyCharts();
  if (!stats.value) return;

  const s = stats.value;

  // 1) RPS Over Time
  if (s.rps_over_time?.length > 1 && document.getElementById('chart-rps')) {
    const rpsData = s.rps_over_time.map(b => ({
      x: b.ts,
      y: Math.round(b.count / (60000 / 1000)) // requests per second
    }));

    charts.value.rps = new ApexCharts(document.getElementById('chart-rps'), {
      chart: { type: 'area', height: 260, toolbar: { show: false } },
      title: { text: 'RPS over time', align: 'left', style: { fontSize: '14px' } },
      xaxis: {
        type: 'datetime',
        labels: { datetimeUTC: false }
      },
      yaxis: { title: { text: 'req/s' } },
      series: [{ name: 'RPS', data: rpsData }],
      stroke: { curve: 'smooth', width: 2 },
      fill: { type: 'gradient', gradient: { shadeIntensity: 1, opacityFrom: 0.35, opacityTo: 0.05 } },
      tooltip: { x: { format: 'dd MMM HH:mm' } }
    });
    charts.value.rps.render();
  }

  // 2) Latency Over Time (avg ms)
  if (s.latency_over_time?.length > 1 && document.getElementById('chart-latency')) {
    const latData = s.latency_over_time.map(b => ({
      x: b.ts,
      y: parseFloat(b.avg_ms.toFixed(2))
    }));

    charts.value.latency = new ApexCharts(document.getElementById('chart-latency'), {
      chart: { type: 'line', height: 260, toolbar: { show: false } },
      title: { text: 'Avg latency over time', align: 'left', style: { fontSize: '14px' } },
      xaxis: {
        type: 'datetime',
        labels: { datetimeUTC: false }
      },
      yaxis: { title: { text: 'ms' } },
      series: [{ name: 'Avg ms', data: latData }],
      stroke: { curve: 'smooth', width: 2 },
      tooltip: { x: { format: 'dd MMM HH:mm' } }
    });
    charts.value.latency.render();
  }

  // 3) Top routes by count (bar)
  if (s.top_routes?.length && document.getElementById('chart-top-routes')) {
    charts.value.topRoutes = new ApexCharts(document.getElementById('chart-top-routes'), {
      chart: { type: 'bar', height: 300, toolbar: { show: false } },
      title: { text: 'Top routes by requests', align: 'left', style: { fontSize: '14px' } },
      plotOptions: { bar: { horizontal: true } },
      xaxis: { categories: s.top_routes.map(r => r.route), title: { text: 'count' } },
      yaxis: { labels: { maxWidth: 260 } },
      series: [{ name: 'Requests', data: s.top_routes.map(r => r.count) }]
    });
    charts.value.topRoutes.render();
  }

  // 4) Slow routes by avg ms (bar)
  if (s.slow_routes?.length && document.getElementById('chart-slow-routes')) {
    charts.value.slowRoutes = new ApexCharts(document.getElementById('chart-slow-routes'), {
      chart: { type: 'bar', height: 300, toolbar: { show: false } },
      title: { text: 'Slowest routes by avg latency', align: 'left', style: { fontSize: '14px' } },
      plotOptions: { bar: { horizontal: true } },
      xaxis: { categories: s.slow_routes.map(r => r.route), title: { text: 'avg ms' } },
      yaxis: { labels: { maxWidth: 260 } },
      series: [{ name: 'Avg ms', data: s.slow_routes.map(r => parseFloat((r.avg_ns / 1e6).toFixed(2))) }]
    });
    charts.value.slowRoutes.render();
  }

  // 5) Response codes (pie)
  if (s.by_code && Object.keys(s.by_code).length && document.getElementById('chart-codes')) {
    const codes = Object.entries(s.by_code).map(([code, count]) => ({
      code,
      count
    }));
    charts.value.codes = new ApexCharts(document.getElementById('chart-codes'), {
      chart: { type: 'pie', height: 300, toolbar: { show: false } },
      title: { text: 'Response codes', align: 'left', style: { fontSize: '14px' } },
      series: codes.map(c => c.count),
      labels: codes.map(c => c.code),
      legend: { position: 'right' }
    });
    charts.value.codes.render();
  }
};

onMounted(() => {
  fetchStats();
});

onBeforeUnmount(() => {
  destroyCharts();
});

const fmtNs = (ns) => {
  if (ns == null || ns === 0) return '0 ms';
  const ms = ns / 1e6;
  if (ms < 1) return `${(ns / 1e3).toFixed(1)} µs`;
  if (ms < 1000) return `${ms.toFixed(2)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
};
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex justify-between items-center mb-6">
      <h1 class="text-2xl font-bold text-purple-700">Request Stats</h1>
      <div class="flex gap-2">
        <button
          @click="fetchStats(true)"
          :disabled="loading"
          class="px-3 py-1.5 text-sm bg-blue-600 text-white rounded hover:bg-blue-700 disabled:opacity-50"
        >
          {{ loading ? 'Loading...' : 'Refresh' }}
        </button>
      </div>
    </div>

    <div v-if="error" class="mb-4 text-sm text-red-600">{{ error }}</div>

    <!-- Summary -->
    <div v-if="stats" class="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div class="text-xs text-gray-500">Total requests</div>
        <div class="text-2xl font-bold">{{ stats.total_requests || 0 }}</div>
      </div>
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div class="text-xs text-gray-500">Avg latency</div>
        <div class="text-2xl font-bold">{{ fmtNs(stats.avg_ns) }}</div>
      </div>
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div class="text-xs text-gray-500">P95 latency</div>
        <div class="text-2xl font-bold text-orange-600">{{ fmtNs(stats.p95_ns) }}</div>
      </div>
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div class="text-xs text-gray-500">P99 latency</div>
        <div class="text-2xl font-bold text-red-600">{{ fmtNs(stats.p99_ns) }}</div>
      </div>
    </div>

    <!-- Time series -->
    <div v-if="stats" class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-6">
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div id="chart-rps"></div>
      </div>
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div id="chart-latency"></div>
      </div>
    </div>

    <!-- Routes -->
    <div v-if="stats" class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-6">
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div id="chart-top-routes"></div>
      </div>
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div id="chart-slow-routes"></div>
      </div>
    </div>

    <!-- Codes + tables -->
    <div v-if="stats" class="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-6">
      <div class="bg-white rounded-lg shadow-sm p-4">
        <div id="chart-codes"></div>
      </div>

      <!-- Top URLs table -->
      <div class="bg-white rounded-lg shadow-sm p-4 overflow-auto max-h-80">
        <h3 class="font-semibold mb-2 text-sm">Top URLs</h3>
        <table class="w-full text-xs">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-2 py-1 text-left">URL</th>
              <th class="px-2 py-1 text-right">Count</th>
              <th class="px-2 py-1 text-right">Avg ms</th>
              <th class="px-2 py-1 text-right">P95 ms</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in stats.top_urls || []" :key="u.url">
              <td class="px-2 py-1 truncate max-w-[160px]" :title="u.url">{{ u.url }}</td>
              <td class="px-2 py-1 text-right">{{ u.count }}</td>
              <td class="px-2 py-1 text-right">{{ (u.avg_ns / 1e6).toFixed(2) }}</td>
              <td class="px-2 py-1 text-right">{{ (u.p95_ns / 1e6).toFixed(2) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Slow URLs table -->
      <div class="bg-white rounded-lg shadow-sm p-4 overflow-auto max-h-80">
        <h3 class="font-semibold mb-2 text-sm">Slowest URLs</h3>
        <table class="w-full text-xs">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-2 py-1 text-left">URL</th>
              <th class="px-2 py-1 text-right">Count</th>
              <th class="px-2 py-1 text-right">Avg ms</th>
              <th class="px-2 py-1 text-right">Max ms</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="u in stats.slow_urls || []" :key="u.url">
              <td class="px-2 py-1 truncate max-w-[160px]" :title="u.url">{{ u.url }}</td>
              <td class="px-2 py-1 text-right">{{ u.count }}</td>
              <td class="px-2 py-1 text-right">{{ (u.avg_ns / 1e6).toFixed(2) }}</td>
              <td class="px-2 py-1 text-right">{{ (u.max_ns / 1e6).toFixed(2) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
