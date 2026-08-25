<script setup>
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';

const { t, locale } = useI18n();

// ─────────────────────────────────────────────────────────────
// Lazy-load apexcharts (~850 kB) only when charts are rendered.
// ─────────────────────────────────────────────────────────────
let apexPromise = null;
const loadApexCharts = () => {
  if (!apexPromise) apexPromise = import('apexcharts');
  return apexPromise;
};

// ─────────────────────────────────────────────────────────────
// State
// ─────────────────────────────────────────────────────────────
const loading = ref(false);
const error = ref('');
const enabled = ref(false);
const summary = ref(null);
const referrers = ref({});
const paths = ref({});

const charts = ref({});

// ─────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────
const sum = (arr) => (arr || []).reduce((a, b) => a + (b || 0), 0);

const currentHour = () => new Date().getHours();
const currentDow = () => new Date().getDay();
const currentDom = () => new Date().getDate();

const nf = new Intl.NumberFormat();
const fmtNum = (n) => nf.format(n || 0);

const totalHuman = computed(() => sum(summary.value?.HumanVisitsByHour));
const totalBot = computed(() => sum(summary.value?.BotVisitsByHour));
const totalAll = computed(() => totalHuman.value + totalBot.value);
const humanShare = computed(() =>
  totalAll.value > 0 ? Math.round((totalHuman.value / totalAll.value) * 100) : 0
);

const hourLabels = computed(() => {
  const base = [
    '00', '01', '02', '03', '04', '05',
    '06', '07', '08', '09', '10', '11',
    '12', '13', '14', '15', '16', '17',
    '18', '19', '20', '21', '22', '23',
  ];
  // Rotate so the current hour is in the middle
  const cur = currentHour();
  const out = [];
  for (let i = 11; i >= 0; i--) {
    out.push(base[(cur - i + 24) % 24]);
  }
  for (let i = 0; i <= 12; i++) {
    out.push(base[(cur + i) % 24]);
  }
  return out;
});

const rotateArr = (arr) => {
  if (!arr || arr.length !== 24) return (arr || []).slice(0, 24);
  const cur = currentHour();
  const out = [];
  for (let i = 11; i >= 0; i--) {
    out.push(arr[(cur - i + 24) % 24]);
  }
  for (let i = 0; i <= 12; i++) {
    out.push(arr[(cur + i) % 24]);
  }
  return out;
};

const dowLabels = computed(() => {
  const cur = currentDow();
  const base = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const out = [];
  for (let i = 0; i < 7; i++) {
    out.push(base[(cur + i) % 7]);
  }
  return out;
});

const rotateDow = (arr) => {
  if (!arr || arr.length !== 7) return (arr || []).slice(0, 7);
  const cur = currentDow();
  const out = [];
  for (let i = 0; i < 7; i++) {
    out.push(arr[(cur + i) % 7]);
  }
  return out;
};

const domLabels = computed(() => {
  const cur = currentDom();
  const out = [];
  for (let i = 0; i < 31; i++) {
    out.push(String((cur + i) % 31 || 31));
  }
  return out;
});

const rotateDom = (arr) => {
  if (!arr || arr.length !== 31) return (arr || []).slice(0, 31);
  const cur = currentDom();
  const out = [];
  for (let i = 0; i < 31; i++) {
    out.push(arr[(cur + i - 1) % 31]);
  }
  return out;
};

const referrerRows = computed(() => {
  return Object.entries(referrers.value || {})
    .map(([domain, r]) => ({
      domain,
      total: sum(r.visits),
      current: r.visits?.[currentHour()] || 0,
    }))
    .sort((a, b) => b.total - a.total);
});

const pathRows = computed(() => {
  return Object.entries(paths.value || {})
    .map(([id, p]) => ({
      id,
      total: sum(p.visits),
      current: p.visits?.[currentHour()] || 0,
    }))
    .filter(p => p.total > 0)
    .sort((a, b) => b.total - a.total);
});

const topReferrers = computed(() => referrerRows.value.slice(0, 10));
const topPaths = computed(() => pathRows.value.slice(0, 10));

const totalReferrerVisits = computed(() => sum(referrerRows.value.map(r => r.total)));
const maxReferrerTotal = computed(() => Math.max(1, ...referrerRows.value.map(r => r.total)));
const maxPathTotal = computed(() => Math.max(1, ...pathRows.value.map(r => r.total)));

// ─────────────────────────────────────────────────────────────
// Data fetching
// ─────────────────────────────────────────────────────────────
const fetchData = async () => {
  loading.value = true;
  error.value = '';
  try {
    const [statusRes, summaryRes, refRes, pathsRes] = await Promise.allSettled([
      api.get('/admin/stats/visits/status'),
      api.get('/admin/stats/visits/summary'),
      api.get('/admin/stats/visits/referrers'),
      api.get('/admin/stats/visits/paths'),
    ]);

    if (statusRes.status === 'fulfilled') enabled.value = !!statusRes.value.data?.enabled;
    if (summaryRes.status === 'fulfilled') summary.value = summaryRes.value.data;
    if (refRes.status === 'fulfilled') referrers.value = refRes.value.data?.referrers || {};
    if (pathsRes.status === 'fulfilled') paths.value = pathsRes.value.data?.paths || {};

    if (summaryRes.status === 'rejected' && refRes.status === 'rejected' && pathsRes.status === 'rejected') {
      error.value = summaryRes.reason?.response?.data?.message || summaryRes.reason?.message || 'Error';
    }

    await nextTick();
    await renderCharts();
  } finally {
    loading.value = false;
  }
};

const toggleEnabled = async () => {
  try {
    const res = await api.post('/admin/stats/visits/toggle', { enabled: !enabled.value });
    enabled.value = !!res.data?.enabled;
  } catch (e) {
    error.value = e.response?.data?.message || e.message;
  }
};

// ─────────────────────────────────────────────────────────────
// Charts
// ─────────────────────────────────────────────────────────────
const isDark = () =>
  typeof document !== 'undefined' &&
  document.documentElement.classList.contains('theme-dark');

const baseTheme = () => {
  const dark = isDark();
  return {
    foreColor: dark ? '#94a3b8' : '#6b7280',
    fontFamily: 'inherit',
  };
};

const baseChartOpts = (type, height) => ({
  chart: {
    type,
    height,
    toolbar: { show: false },
    zoom: { enabled: false },
    animations: { enabled: !isDark() && true, speed: 500 },
  },
  theme: { mode: isDark() ? 'dark' : 'light' },
  colors: ['#f97316', '#8b5cf6', '#06b6d4', '#10b981', '#f59e0b', '#ef4444', '#ec4899', '#14b8a6'],
  stroke: { curve: 'smooth', width: 2 },
  grid: {
    borderColor: isDark() ? 'rgba(148,163,184,0.15)' : 'rgba(107,114,128,0.15)',
    strokeDashArray: 4,
    padding: { left: 8, right: 8 },
  },
  tooltip: {
    theme: isDark() ? 'dark' : 'light',
    shared: true,
    intersect: false,
  },
});

const destroyCharts = () => {
  Object.values(charts.value).forEach((c) => {
    try { c.destroy(); } catch {}
  });
  charts.value = {};
};

const renderCharts = async () => {
  destroyCharts();
  if (!summary.value) return;

  const { default: ApexCharts } = await loadApexCharts();
  const s = summary.value;
  const theme = baseTheme();
  const curHour = currentHour();

  const mount = (id, options) => {
    const el = document.getElementById(id);
    if (!el) return null;
    const chart = new ApexCharts(el, options);
    chart.render();
    return chart;
  };

  // 1) Traffic by hour (area, rotated so current hour is centered)
  const hourData = rotateArr(s.HumanVisitsByHour).map((v, i) => ({
    x: hourLabels.value[i],
    y: v,
  }));
  const botHourData = rotateArr(s.BotVisitsByHour).map((v, i) => ({
    x: hourLabels.value[i],
    y: v,
  }));

  charts.value.hours = mount('chart-hours', {
    ...baseChartOpts('area', 300),
    theme: { mode: isDark() ? 'dark' : 'light' },
    series: [
      { name: t('admin.visits_human'), data: hourData },
      { name: t('admin.visits_bot'), data: botHourData },
    ],
    dataLabels: { enabled: false },
    xaxis: {
      categories: hourLabels.value,
      labels: { ...theme, style: { colors: [theme.foreColor] } },
      axisBorder: { color: theme.foreColor, opacity: 0.3 },
      axisTicks: { color: theme.foreColor, opacity: 0.3 },
    },
    yaxis: {
      labels: { ...theme, style: { colors: [theme.foreColor] } },
    },
    legend: {
      position: 'top',
      horizontalAlign: 'right',
      labels: { colors: [theme.foreColor] },
    },
    fill: {
      type: 'gradient',
      gradient: {
        shadeIntensity: 1,
        opacityFrom: 0.4,
        opacityTo: 0.05,
        stops: [0, 95, 100],
      },
    },
    markers: {
      size: 0,
      hover: { size: 5 },
    },
    annotations: {
      xaxis: [
        {
          x: hourLabels.value[12],
          label: {
            text: 'now',
            style: { background: '#f97316', borderRadius: 4, fontSize: '10px', color: '#fff' },
          },
        },
      ],
    },
  });

  // 2) Traffic by day of week (bar)
  const dowData = rotateDow(s.HumanVisitsByDay);
  const botDowData = rotateDow(s.BotVisitsByDay);
  charts.value.days = mount('chart-days', {
    ...baseChartOpts('bar', 300),
    series: [
      { name: t('admin.visits_human'), data: dowData },
      { name: t('admin.visits_bot'), data: botDowData },
    ],
    plotOptions: {
      bar: {
        horizontal: false,
        columnWidth: '55%',
        borderRadius: 6,
        dataLabels: { position: 'top' },
      },
    },
    dataLabels: { enabled: false },
    xaxis: {
      categories: dowLabels.value,
      labels: { ...theme, style: { colors: [theme.foreColor] } },
    },
    yaxis: { labels: { ...theme, style: { colors: [theme.foreColor] } } },
    legend: {
      position: 'top',
      horizontalAlign: 'right',
      labels: { colors: [theme.foreColor] },
    },
    states: { hover: { filter: { type: 'darken', value: 0.85 } } },
  });

  // 3) Traffic by day of month (line)
  const domData = rotateDom(s.HumanVisitsByMonthDay).map((v, i) => ({
    x: domLabels.value[i],
    y: v,
  }));
  const botDomData = rotateDom(s.BotVisitsByMonthDay).map((v, i) => ({
    x: domLabels.value[i],
    y: v,
  }));
  charts.value.monthDays = mount('chart-month-days', {
    ...baseChartOpts('line', 300),
    series: [
      { name: t('admin.visits_human'), data: domData },
      { name: t('admin.visits_bot'), data: botDomData },
    ],
    xaxis: {
      categories: domLabels.value,
      labels: {
        ...theme,
        style: { colors: [theme.foreColor], fontSize: '10px' },
        rotate: -45,
      },
    },
    yaxis: { labels: { ...theme, style: { colors: [theme.foreColor] } } },
    legend: {
      position: 'top',
      horizontalAlign: 'right',
      labels: { colors: [theme.foreColor] },
    },
    markers: { size: 2, colors: ['#f97316', '#f59e0b'], strokeColors: '#fff', strokeWidth: 1 },
  });

  // 4) Human vs Bot donut
  charts.value.donut = mount('chart-donut', {
    chart: {
      type: 'donut',
      height: 300,
      toolbar: { show: false },
    },
    theme: { mode: isDark() ? 'dark' : 'light' },
    series: [totalHuman.value, totalBot.value],
    labels: [t('admin.visits_human'), t('admin.visits_bot')],
    colors: ['#f97316', '#f59e0b'],
    stroke: {
      colors: [isDark() ? '#0f172a' : '#ffffff', isDark() ? '#0f172a' : '#ffffff'],
      width: 2,
    },
    legend: {
      show: true,
      position: 'bottom',
      labels: { colors: [theme.foreColor] },
    },
    dataLabels: {
      enabled: true,
      style: { fontSize: '14px', fontWeight: 600 },
      background: { enabled: false },
      formatter: (val, opts) => {
        const total = totalHuman.value + totalBot.value;
        if (!total) return '';
        const pct = Math.round((Number(val) / total) * 100);
        return `${pct}%`;
      },
    },
    plotOptions: {
      pie: {
        donut: {
          size: '70%',
          labels: {
            show: true,
            total: {
              show: true,
              label: t('admin.visits_total'),
              formatter: () => fmtNum(totalAll.value),
              style: {
                fontSize: '18px',
                fontWeight: 700,
                color: isDark() ? '#f1f5f9' : '#111827',
              },
            },
          },
        },
      },
    },
    tooltip: {
      theme: isDark() ? 'dark' : 'light',
      y: { formatter: (val) => fmtNum(val) },
    },
  });
};

// ─────────────────────────────────────────────────────────────
// Lifecycle
// ─────────────────────────────────────────────────────────────
let themeWatcher = null;

onMounted(async () => {
  await fetchData();

  // Re-render charts on theme change
  themeWatcher = new MutationObserver(() => {
    if (charts.value && Object.keys(charts.value).length) {
      renderCharts();
    }
  });
  themeWatcher.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class'],
  });
});

onBeforeUnmount(() => {
  destroyCharts();
  if (themeWatcher) themeWatcher.disconnect();
});

// Re-render charts when locale changes (labels)
watch(locale, () => {
  if (charts.value && Object.keys(charts.value).length) {
    renderCharts();
  }
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <!-- Header -->
    <div class="flex flex-wrap items-center justify-between gap-3 mb-6">
      <div>
        <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.visits_stats') }}</h1>
        <p class="text-sm text-ink-3 mt-1">{{ t('admin.visits_stats_desc') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <!-- Toggle switch -->
        <button
          @click="toggleEnabled"
          :class="[
            'relative inline-flex h-6 w-11 items-center rounded-full transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-purple-400 focus:ring-offset-2',
            enabled ? 'bg-purple-600' : 'bg-gray-300 dark:bg-gray-600'
          ]"
          :title="enabled ? t('admin.visits_disable') : t('admin.visits_enable')"
        >
          <span
            :class="[
              'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform duration-200',
              enabled ? 'translate-x-6' : 'translate-x-1'
            ]"
          ></span>
        </button>
        <span :class="['text-sm font-medium', enabled ? 'text-green-600' : 'text-ink-3']">
          {{ enabled ? t('admin.visits_enabled') : t('admin.visits_disabled') }}
        </span>
        <button
          @click="fetchData"
          :disabled="loading"
          class="btn btn-secondary btn-sm"
        >
          <svg v-if="loading" class="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          <svg v-else class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"/>
          </svg>
          {{ t('admin.refresh') }}
        </button>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="mb-4 p-3 rounded-lg bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-900 text-sm text-red-600 dark:text-red-400">
      {{ error }}
    </div>

    <!-- Loading skeleton -->
    <div v-if="loading && !summary" class="space-y-4">
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div v-for="i in 4" :key="i" class="bg-surface rounded-xl p-5 animate-pulse">
          <div class="h-3 bg-surface-2 rounded w-20 mb-3"></div>
          <div class="h-8 bg-surface-2 rounded w-24"></div>
        </div>
      </div>
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div v-for="i in 2" :key="i" class="bg-surface rounded-xl p-5 h-80 animate-pulse"></div>
      </div>
    </div>

    <!-- Content -->
    <div v-else-if="summary" class="stats-chart-container space-y-4">
      <!-- KPI cards -->
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-ink-3 uppercase tracking-wide">{{ t('admin.visits_total') }}</span>
            <div class="h-8 w-8 rounded-lg bg-purple-100 dark:bg-purple-900/40 flex items-center justify-center">
              <svg class="h-4 w-4 text-purple-600 dark:text-purple-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/>
              </svg>
            </div>
          </div>
          <div class="mt-2 text-3xl font-bold tracking-tight">{{ fmtNum(totalAll) }}</div>
          <div class="mt-1 text-xs text-ink-3">{{ t('admin.visits_all_time') }}</div>
        </div>

        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-ink-3 uppercase tracking-wide">{{ t('admin.visits_human') }}</span>
            <div class="h-8 w-8 rounded-lg bg-orange-100 dark:bg-orange-900/40 flex items-center justify-center">
              <svg class="h-4 w-4 text-orange-600 dark:text-orange-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"/>
              </svg>
            </div>
          </div>
          <div class="mt-2 text-3xl font-bold tracking-tight text-orange-600 dark:text-orange-400">{{ fmtNum(totalHuman) }}</div>
          <div class="mt-1 text-xs text-ink-3">{{ humanShare }}% {{ t('admin.visits_of_total') }}</div>
        </div>

        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-ink-3 uppercase tracking-wide">{{ t('admin.visits_bot') }}</span>
            <div class="h-8 w-8 rounded-lg bg-amber-100 dark:bg-amber-900/40 flex items-center justify-center">
              <svg class="h-4 w-4 text-amber-600 dark:text-amber-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
              </svg>
            </div>
          </div>
          <div class="mt-2 text-3xl font-bold tracking-tight text-amber-600 dark:text-amber-400">{{ fmtNum(totalBot) }}</div>
          <div class="mt-1 text-xs text-ink-3">{{ 100 - humanShare }}% {{ t('admin.visits_of_total') }}</div>
        </div>

        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-ink-3 uppercase tracking-wide">{{ t('admin.visits_now') }}</span>
            <div class="h-8 w-8 rounded-lg bg-emerald-100 dark:bg-emerald-900/40 flex items-center justify-center">
              <svg class="h-4 w-4 text-emerald-600 dark:text-emerald-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/>
              </svg>
            </div>
          </div>
          <div class="mt-2 text-3xl font-bold tracking-tight text-emerald-600 dark:text-emerald-400">
            {{ fmtNum((summary.HumanVisitsByHour?.[new Date().getHours()] || 0) + (summary.BotVisitsByHour?.[new Date().getHours()] || 0)) }}
          </div>
          <div class="mt-1 text-xs text-ink-3">{{ new Date().getHours() }}:00 - {{ (new Date().getHours() + 1) % 24 }}:00</div>
        </div>
      </div>

      <!-- Hour + Donut -->
      <div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div class="lg:col-span-2 bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between mb-2">
            <h2 class="font-semibold text-sm text-ink-2">{{ t('admin.visits_by_hour') }}</h2>
            <span class="text-xs text-ink-3">{{ t('admin.visits_hour_hint') }}</span>
          </div>
          <div id="chart-hours"></div>
        </div>
        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <h2 class="font-semibold text-sm text-ink-2 mb-2">{{ t('admin.visits_split') }}</h2>
          <div id="chart-donut"></div>
        </div>
      </div>

      <!-- Days of week + Days of month -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between mb-2">
            <h2 class="font-semibold text-sm text-ink-2">{{ t('admin.visits_by_weekday') }}</h2>
            <span class="text-xs text-ink-3">{{ t('admin.visits_weekday_hint') }}</span>
          </div>
          <div id="chart-days"></div>
        </div>
        <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
          <div class="flex items-center justify-between mb-2">
            <h2 class="font-semibold text-sm text-ink-2">{{ t('admin.visits_by_monthday') }}</h2>
            <span class="text-xs text-ink-3">{{ t('admin.visits_monthday_hint') }}</span>
          </div>
          <div id="chart-month-days"></div>
        </div>
      </div>

      <!-- Referrers -->
      <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-semibold text-sm text-ink-2">{{ t('admin.visits_referrers') }}</h2>
          <span class="text-xs text-ink-3">{{ referrerRows.length }} {{ t('admin.visits_domains') }}</span>
        </div>

        <div v-if="referrerRows.length === 0" class="text-center py-8 text-sm text-ink-3">
          {{ t('admin.visits_no_data') }}
        </div>

        <div v-else>
          <!-- Top 10 referrers with progress bars -->
          <div class="grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-3">
            <div
              v-for="r in topReferrers"
              :key="r.domain"
              class="group"
            >
              <div class="flex items-center justify-between text-sm mb-1">
                <span class="font-medium truncate max-w-[180px]" :title="r.domain">{{ r.domain }}</span>
                <span class="text-ink-3 text-xs tabular-nums">
                  {{ fmtNum(r.total) }}
                  <span class="text-ink-3/60">({{ totalReferrerVisits ? Math.round((r.total / totalReferrerVisits) * 100) : 0 }}%)</span>
                </span>
              </div>
              <div class="h-2 bg-surface-2 rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full bg-gradient-to-r from-purple-500 to-orange-500 transition-all duration-500"
                  :style="{ width: (r.total / maxReferrerTotal * 100) + '%' }"
                ></div>
              </div>
            </div>
          </div>

          <!-- Full table (scrollable) -->
          <div class="mt-5 max-h-64 overflow-y-auto rounded-lg border border-line">
            <table class="w-full text-sm">
              <caption class="sr-only">{{ t('tables.visits_referrers') }}</caption>
              <thead class="bg-surface-2 sticky top-0">
                <tr>
                  <th scope="col" class="px-3 py-2 text-left font-medium text-ink-2">{{ t('admin.visits_domain') }}</th>
                  <th scope="col" class="px-3 py-2 text-right font-medium text-ink-2">{{ t('admin.visits_total') }}</th>
                  <th scope="col" class="px-3 py-2 text-right font-medium text-ink-2">{{ t('admin.visits_now') }}</th>
                  <th scope="col" class="px-3 py-2 text-right font-medium text-ink-2">{{ t('admin.visits_share') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-line">
                <tr v-for="r in referrerRows" :key="r.domain" class="hover:bg-surface-2/50 transition">
                  <td class="px-3 py-2 truncate max-w-[200px]" :title="r.domain">{{ r.domain }}</td>
                  <td class="px-3 py-2 text-right tabular-nums">{{ fmtNum(r.total) }}</td>
                  <td class="px-3 py-2 text-right tabular-nums text-emerald-600 dark:text-emerald-400">{{ fmtNum(r.current) }}</td>
                  <td class="px-3 py-2 text-right tabular-nums text-ink-3">
                    {{ totalReferrerVisits ? (r.total / totalReferrerVisits * 100).toFixed(1) : '0.0' }}%
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- Paths / Categories -->
      <div class="bg-surface rounded-xl shadow-sm p-5 border border-line">
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-semibold text-sm text-ink-2">{{ t('admin.visits_paths') }}</h2>
          <span class="text-xs text-ink-3">{{ pathRows.length }} {{ t('admin.visits_categories') }}</span>
        </div>

        <div v-if="pathRows.length === 0" class="text-center py-8 text-sm text-ink-3">
          {{ t('admin.visits_no_data') }}
        </div>

        <div v-else>
          <!-- Top 10 paths with progress bars -->
          <div class="grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-3">
            <div
              v-for="p in topPaths"
              :key="p.id"
              class="group"
            >
              <div class="flex items-center justify-between text-sm mb-1">
                <span class="font-medium text-ink-2">
                  <span class="text-ink-3">#</span>{{ p.id }}
                </span>
                <span class="text-ink-3 text-xs tabular-nums">
                  {{ fmtNum(p.total) }}
                  <span class="text-ink-3/60">({{ Math.round((p.total / maxPathTotal) * 100) }}%)</span>
                </span>
              </div>
              <div class="h-2 bg-surface-2 rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full bg-gradient-to-r from-amber-400 to-orange-500 transition-all duration-500"
                  :style="{ width: (p.total / maxPathTotal * 100) + '%' }"
                ></div>
              </div>
            </div>
          </div>

          <!-- Full table (scrollable) -->
          <div class="mt-5 max-h-64 overflow-y-auto rounded-lg border border-line">
            <table class="w-full text-sm">
              <caption class="sr-only">{{ t('tables.visits_paths') }}</caption>
              <thead class="bg-surface-2 sticky top-0">
                <tr>
                  <th scope="col" class="px-3 py-2 text-left font-medium text-ink-2">{{ t('admin.visits_category_id') }}</th>
                  <th scope="col" class="px-3 py-2 text-right font-medium text-ink-2">{{ t('admin.visits_total') }}</th>
                  <th scope="col" class="px-3 py-2 text-right font-medium text-ink-2">{{ t('admin.visits_now') }}</th>
                  <th scope="col" class="px-3 py-2 text-right font-medium text-ink-2">{{ t('admin.visits_share') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-line">
                <tr v-for="p in pathRows" :key="p.id" class="hover:bg-surface-2/50 transition">
                  <td class="px-3 py-2 text-ink-2">
                    <span class="text-ink-3">#</span>{{ p.id }}
                  </td>
                  <td class="px-3 py-2 text-right tabular-nums">{{ fmtNum(p.total) }}</td>
                  <td class="px-3 py-2 text-right tabular-nums text-emerald-600 dark:text-emerald-400">{{ fmtNum(p.current) }}</td>
                  <td class="px-3 py-2 text-right tabular-nums text-ink-3">
                    {{ Math.round((p.total / maxPathTotal) * 100) }}%
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>

    <!-- Empty state -->
    <div v-else class="text-center py-16">
      <div class="text-4xl mb-3">📊</div>
      <div class="text-lg font-medium text-ink-2">{{ t('admin.visits_no_data') }}</div>
      <div class="text-sm text-ink-3 mt-1">{{ t('admin.visits_no_data_hint') }}</div>
    </div>
  </div>
</template>

<style scoped>
/* Smooth chart container */
.stats-chart-container {
  animation: fadeIn 0.3s ease-out;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* Chart tooltip styling */
:deep(.apexcharts-tooltip) {
  border-radius: 8px !important;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15) !important;
}

:deep(.apexcharts-tooltip-title) {
  font-weight: 600 !important;
  font-size: 12px !important;
}

/* Ensure charts are responsive */
:deep(.apexcharts-svg) {
  max-width: 100%;
}
</style>
