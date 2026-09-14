<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t } = useI18n();

// Lazy-load apexcharts
let apexPromise = null;
const loadApexCharts = () => {
  if (!apexPromise) apexPromise = import('apexcharts');
  return apexPromise;
};

// State
const loading = ref(false);
const error = ref('');
const eventCount = ref(0);
const byPage = ref({});
const byReferer = ref({});
const byUA = ref([]);
const byIP = ref([]);
const byTime = ref(new Array(24).fill(0));
const botCounts = ref({});

// Search and filter state
const pageSearch = ref('');
const refererSearch = ref('');
const uaSearch = ref('');
const ipSearch = ref('');

// Pagination state
const pageSize = 25;
const pagePage = ref(1);
const refererPage = ref(1);
const uaPage = ref(1);
const ipPage = ref(1);

const charts = ref({});

// Load data
const loadData = async () => {
  loading.value = true;
  error.value = '';
  try {
    const [countRes, pageRes, refererRes, uaRes, ipRes, timeRes, botRes] = await Promise.all([
      api.get('/admin/stats/events/count'),
      api.get('/admin/stats/events/by-page'),
      api.get('/admin/stats/events/by-referer'),
      api.get('/admin/stats/events/by-ua'),
      api.get('/admin/stats/events/by-ip'),
      api.get('/admin/stats/events/by-time'),
      api.get('/admin/stats/events/bot-counts'),
    ]);

    eventCount.value = countRes.data.event_count || 0;
    byPage.value = pageRes.data.by_page || {};
    byReferer.value = refererRes.data.by_referer || {};
    byUA.value = uaRes.data.by_ua || [];
    byIP.value = ipRes.data.by_ip || [];
    byTime.value = timeRes.data.by_hour || new Array(24).fill(0);
    botCounts.value = botRes.data.bots || {};

    await nextTick();
    renderCharts();
  } catch (e) {
    error.value = e.message;
  } finally {
    loading.value = false;
  }
};

// Render charts
const renderCharts = async () => {
  const ApexCharts = await loadApexCharts();

  // Time chart
  if (charts.value.time) charts.value.time.destroy();
  const timeEl = document.getElementById('chart-time');
  if (timeEl) {
    charts.value.time = new ApexCharts(timeEl, {
      chart: { type: 'bar', height: 300 },
      series: [{ name: 'Visits', data: byTime.value }],
      xaxis: { categories: Array.from({ length: 24 }, (_, i) => `${i}:00`) },
      title: { text: t('admin.detailed_stats_by_hour') },
    });
    charts.value.time.render();
  }

  // Top pages chart
  if (charts.value.pages) charts.value.pages.destroy();
  const pageEl = document.getElementById('chart-pages');
  if (pageEl) {
    const sortedPages = Object.entries(byPage.value)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 10);
    charts.value.pages = new ApexCharts(pageEl, {
      chart: { type: 'bar', height: 350 },
      series: [{ name: 'Visits', data: sortedPages.map(([_, v]) => v) }],
      xaxis: { categories: sortedPages.map(([k]) => k) },
      title: { text: t('admin.detailed_stats_top_pages') },
      plotOptions: { bar: { horizontal: true } },
    });
    charts.value.pages.render();
  }

  // Top referers chart
  if (charts.value.referers) charts.value.referers.destroy();
  const refEl = document.getElementById('chart-referers');
  if (refEl) {
    const sortedRefs = Object.entries(byReferer.value)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 10);
    charts.value.referers = new ApexCharts(refEl, {
      chart: { type: 'bar', height: 350 },
      series: [{ name: 'Visits', data: sortedRefs.map(([_, v]) => v) }],
      xaxis: { categories: sortedRefs.map(([k]) => k) },
      title: { text: t('admin.detailed_stats_top_referers') },
      plotOptions: { bar: { horizontal: true } },
    });
    charts.value.referers.render();
  }
};

// Formatted number
const nf = new Intl.NumberFormat();
const fmtNum = (n) => nf.format(n || 0);

// Page table data
const pageEntries = computed(() =>
  Object.entries(byPage.value).map(([page, count]) => ({ page, count }))
);

const filteredPages = computed(() => {
  const entries = pageEntries.value;
  if (!pageSearch.value) return entries;
  const search = pageSearch.value.toLowerCase();
  return entries.filter((e) => e.page.toLowerCase().includes(search));
});

const paginatedPages = computed(() => {
  const start = (pagePage.value - 1) * pageSize;
  return filteredPages.value.slice(start, start + pageSize);
});

const totalPages = computed(() =>
  Math.ceil(filteredPages.value.length / pageSize)
);

// Referer table data
const refererEntries = computed(() =>
  Object.entries(byReferer.value).map(([referer, count]) => ({ referer, count }))
);

const filteredReferers = computed(() => {
  const entries = refererEntries.value;
  if (!refererSearch.value) return entries;
  const search = refererSearch.value.toLowerCase();
  return entries.filter((e) => e.referer.toLowerCase().includes(search));
});

const paginatedReferers = computed(() => {
  const start = (refererPage.value - 1) * pageSize;
  return filteredReferers.value.slice(start, start + pageSize);
});

const totalRefererPages = computed(() =>
  Math.ceil(filteredReferers.value.length / pageSize)
);

// UA table data
const filteredUAs = computed(() => {
  if (!uaSearch.value) return byUA.value;
  const search = uaSearch.value.toLowerCase();
  return byUA.value.filter((e) => e.ua.toLowerCase().includes(search));
});

const paginatedUAs = computed(() => {
  const start = (uaPage.value - 1) * pageSize;
  return filteredUAs.value.slice(start, start + pageSize);
});

const totalUAPages = computed(() =>
  Math.ceil(filteredUAs.value.length / pageSize)
);

// IP table data
const filteredIPs = computed(() => {
  if (!ipSearch.value) return byIP.value;
  const search = ipSearch.value.toLowerCase();
  return byIP.value.filter((e) => e.ip.toLowerCase().includes(search));
});

const paginatedIPs = computed(() => {
  const start = (ipPage.value - 1) * pageSize;
  return filteredIPs.value.slice(start, start + pageSize);
});

const totalIPPages = computed(() =>
  Math.ceil(filteredIPs.value.length / pageSize)
);

// Bot entries sorted by count
const botEntries = computed(() =>
  Object.entries(botCounts.value)
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
);

onMounted(() => {
  loadData();
});

onBeforeUnmount(() => {
  Object.values(charts.value).forEach((c) => c.destroy());
});
</script>

<template>
  <div class="detailed-stats">
    <h1>{{ t('admin.detailed_stats') }}</h1>

    <div v-if="loading" class="loading">{{ t('common.loading') }}</div>
    <div v-if="error" class="error">{{ error }}</div>

    <template v-if="!loading">
      <!-- KPI Cards -->
      <div class="kpi-row">
        <div class="kpi-card">
          <div class="kpi-value">{{ fmtNum(eventCount) }}</div>
          <div class="kpi-label">{{ t('admin.detailed_stats_events') }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-value">{{ fmtNum(pageEntries.length) }}</div>
          <div class="kpi-label">{{ t('admin.detailed_stats_unique_pages') }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-value">{{ fmtNum(byIP.length) }}</div>
          <div class="kpi-label">{{ t('admin.detailed_stats_unique_ips') }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-value">{{ fmtNum(botEntries.length) }}</div>
          <div class="kpi-label">{{ t('admin.detailed_stats_bots') }}</div>
        </div>
      </div>

      <!-- Charts Row -->
      <div class="charts-row">
        <div class="chart-container">
          <div id="chart-time"></div>
        </div>
        <div class="chart-container">
          <div id="chart-pages"></div>
        </div>
      </div>

      <div class="charts-row">
        <div class="chart-container">
          <div id="chart-referers"></div>
        </div>
        <div class="chart-container bot-chart">
          <h3>{{ t('admin.detailed_stats_bots') }}</h3>
          <ul v-if="botEntries.length > 0">
            <li v-for="bot in botEntries" :key="bot.name">
              <span class="bot-name">{{ bot.name }}</span>
              <span class="bot-count">{{ fmtNum(bot.count) }}</span>
            </li>
          </ul>
          <p v-else>{{ t('admin.detailed_stats_no_bots') }}</p>
        </div>
      </div>

      <!-- Pages Table -->
      <section class="data-section">
        <h2>{{ t('admin.detailed_stats_pages') }}</h2>
        <div class="search-bar">
          <input
            v-model="pageSearch"
            type="text"
            :placeholder="t('admin.detailed_stats_search')"
          />
        </div>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('admin.detailed_stats_page') }}</th>
              <th>{{ t('admin.detailed_stats_visits') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in paginatedPages" :key="entry.page">
              <td class="page-cell">{{ entry.page }}</td>
              <td>{{ fmtNum(entry.count) }}</td>
            </tr>
          </tbody>
        </table>
        <div class="pagination">
          <button @click="pagePage = 1" :disabled="pagePage === 1">&laquo;</button>
          <button @click="pagePage--" :disabled="pagePage === 1">&lsaquo;</button>
          <span>{{ pagePage }} / {{ totalPages }}</span>
          <button @click="pagePage++" :disabled="pagePage === totalPages">&rsaquo;</button>
          <button @click="pagePage = totalPages" :disabled="pagePage === totalPages">&raquo;</button>
        </div>
      </section>

      <!-- Referers Table -->
      <section class="data-section">
        <h2>{{ t('admin.detailed_stats_referers') }}</h2>
        <div class="search-bar">
          <input
            v-model="refererSearch"
            type="text"
            :placeholder="t('admin.detailed_stats_search')"
          />
        </div>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('admin.detailed_stats_referer') }}</th>
              <th>{{ t('admin.detailed_stats_visits') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in paginatedReferers" :key="entry.referer">
              <td class="page-cell">{{ entry.referer || '(direct)' }}</td>
              <td>{{ fmtNum(entry.count) }}</td>
            </tr>
          </tbody>
        </table>
        <div class="pagination">
          <button @click="refererPage = 1" :disabled="refererPage === 1">&laquo;</button>
          <button @click="refererPage--" :disabled="refererPage === 1">&lsaquo;</button>
          <span>{{ refererPage }} / {{ totalRefererPages }}</span>
          <button @click="refererPage++" :disabled="refererPage === totalRefererPages">&rsaquo;</button>
          <button @click="refererPage = totalRefererPages" :disabled="refererPage === totalRefererPages">&raquo;</button>
        </div>
      </section>

      <!-- User Agents Table -->
      <section class="data-section">
        <h2>{{ t('admin.detailed_stats_user_agents') }}</h2>
        <div class="search-bar">
          <input
            v-model="uaSearch"
            type="text"
            :placeholder="t('admin.detailed_stats_search')"
          />
        </div>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('admin.detailed_stats_ua') }}</th>
              <th>{{ t('admin.detailed_stats_visits') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in paginatedUAs" :key="entry.ua_id">
              <td class="page-cell">{{ entry.ua || '(unknown)' }}</td>
              <td>{{ fmtNum(entry.visits) }}</td>
            </tr>
          </tbody>
        </table>
        <div class="pagination">
          <button @click="uaPage = 1" :disabled="uaPage === 1">&laquo;</button>
          <button @click="uaPage--" :disabled="uaPage === 1">&lsaquo;</button>
          <span>{{ uaPage }} / {{ totalUAPages }}</span>
          <button @click="uaPage++" :disabled="uaPage === totalUAPages">&rsaquo;</button>
          <button @click="uaPage = totalUAPages" :disabled="uaPage === totalUAPages">&raquo;</button>
        </div>
      </section>

      <!-- IPs Table -->
      <section class="data-section">
        <h2>{{ t('admin.detailed_stats_ips') }}</h2>
        <div class="search-bar">
          <input
            v-model="ipSearch"
            type="text"
            :placeholder="t('admin.detailed_stats_search')"
          />
        </div>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ t('admin.detailed_stats_ip') }}</th>
              <th>{{ t('admin.detailed_stats_visits') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in paginatedIPs" :key="entry.ip_id">
              <td>{{ entry.ip || '(unknown)' }}</td>
              <td>{{ fmtNum(entry.visits) }}</td>
            </tr>
          </tbody>
        </table>
        <div class="pagination">
          <button @click="ipPage = 1" :disabled="ipPage === 1">&laquo;</button>
          <button @click="ipPage--" :disabled="ipPage === 1">&lsaquo;</button>
          <span>{{ ipPage }} / {{ totalIPPages }}</span>
          <button @click="ipPage++" :disabled="ipPage === totalIPPages">&rsaquo;</button>
          <button @click="ipPage = totalIPPages" :disabled="ipPage === totalIPPages">&raquo;</button>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.detailed-stats {
  padding: 20px;
}

.kpi-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.kpi-card {
  background: var(--card-bg);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}

.kpi-value {
  font-size: 2em;
  font-weight: bold;
  color: var(--primary-color);
}

.kpi-label {
  color: var(--text-muted);
  margin-top: 4px;
}

.charts-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.chart-container {
  background: var(--card-bg);
  border-radius: 8px;
  padding: 16px;
}

.bot-chart ul {
  list-style: none;
  padding: 0;
}

.bot-chart li {
  display: flex;
  justify-content: space-between;
  padding: 4px 0;
  border-bottom: 1px solid var(--border-color);
}

.data-section {
  margin-bottom: 32px;
}

.data-section h2 {
  margin-bottom: 12px;
}

.search-bar {
  margin-bottom: 12px;
}

.search-bar input {
  width: 100%;
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  border-radius: 4px;
}

.data-table {
  width: 100%;
  border-collapse: collapse;
}

.data-table th,
.data-table td {
  padding: 8px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border-color);
}

.page-cell {
  max-width: 400px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pagination {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.pagination button {
  padding: 4px 8px;
  border: 1px solid var(--border-color);
  background: var(--card-bg);
  cursor: pointer;
}

.pagination button:disabled {
  opacity: 0.5;
  cursor: default;
}

.loading,
.error {
  padding: 20px;
  text-align: center;
}

.error {
  color: var(--error-color);
}
</style>
