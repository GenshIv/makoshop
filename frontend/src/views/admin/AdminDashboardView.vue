<script setup>
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useFormat } from '../../composables/useFormat';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';

const { t } = useI18n();
const { formatPrice } = useFormat();
const { toast } = useToast();

const stats = ref({ users: 0, companies: 0, products: 0, orders: 0, revenue: 0 });
const loading = ref(true);
const maintenance = ref({ enabled: false, auto_disable: false });
const maintenanceLoading = ref(false);

const fetchMaintenance = async () => {
  try {
    const res = await api.get('/admin/maintenance');
    maintenance.value = res.data;
  } catch (e) {
    console.error('fetch maintenance:', e);
  }
};

const autoDisable = ref(false);

const setMaintenance = async (enable) => {
  maintenanceLoading.value = true;
  try {
    await api.post('/admin/maintenance', { enable, auto_disable: enable ? autoDisable.value : false });
    maintenance.value.enabled = enable;
    maintenance.value.auto_disable = enable ? autoDisable.value : false;
  } catch (e) {
    console.error('set maintenance:', e);
    toast.error(t('admin.maintenance_update_error'));
  } finally {
    maintenanceLoading.value = false;
  }
};

// System rebuild buttons
const systemLoading = ref(null); // which button is loading

// Password change
const passwordForm = ref({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
});
const passwordLoading = ref(false);
const passwordError = ref('');
const passwordSuccess = ref('');

const changePassword = async () => {
  passwordError.value = '';
  passwordSuccess.value = '';

  const { oldPassword, newPassword, confirmPassword } = passwordForm.value;

  if (!oldPassword || !newPassword || !confirmPassword) {
    passwordError.value = t('admin.password_all_fields_required');
    return;
  }
  if (newPassword !== confirmPassword) {
    passwordError.value = t('admin.password_not_match');
    return;
  }
  if (newPassword.length < 6) {
    passwordError.value = t('admin.password_too_short');
    return;
  }

  passwordLoading.value = true;
  try {
    await api.post('/admin/change-password', {
      oldPassword,
      newPassword,
    });
    passwordSuccess.value = t('admin.password_changed');
    passwordForm.value = { oldPassword: '', newPassword: '', confirmPassword: '' };
  } catch (e) {
    console.error('change password error:', e);
    const err = e.response?.data?.message || e.message;
    passwordError.value = t('admin.password_change_failed', { error: err });
  } finally {
    passwordLoading.value = false;
  }
};

const rebuildEndpoints = {
  counts: '/admin/rebuild-product-counts',
  sort: '/admin/rebuild-sort-indexes',
  eanpage: '/admin/rebuild-eanpage-indexes',
  attrdef: '/admin/rebuild-attrdef-indexes',
  all: '/admin/eanpages/catalogize-all',
};

const rebuildKey = ref(null);

const askRebuild = (key) => {
  rebuildKey.value = key;
};

const rebuildLabel = (key) => {
  return {
    counts: t('admin.rebuild_product_counts'),
    sort: t('admin.rebuild_sort_indexes'),
    eanpage: t('admin.rebuild_eanpage_indexes'),
    attrdef: t('admin.rebuild_attrdef_indexes'),
    all: t('admin.rebuild_all_eanpage_indexes'),
  }[key];
};

const runRebuild = async (key) => {
  rebuildKey.value = null;
  systemLoading.value = key;
  try {
    const endpoint = rebuildEndpoints[key];
    const body = key === 'all' ? { apply: true, force: true } : undefined;
    await api.post(endpoint, body);
    toast.success(t('admin.rebuild_completed', { name: rebuildLabel(key) }));
  } catch (e) {
    console.error('rebuild error:', e);
    const err = e.response?.data?.message || e.message;
    toast.error(t('admin.rebuild_failed', { error: err }));
  } finally {
    systemLoading.value = null;
  }
};

const fetchStats = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/analytics/overview');
    const data = res.data;
    stats.value = {
      users: data.users || 0,
      companies: data.companies || 0,
      products: data.products || 0,
      orders: data.orders || 0,
      revenue: data.revenue || 0,
    };
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

onMounted(() => {
  fetchStats();
  fetchMaintenance();
});
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <h1 class="text-2xl font-bold mb-6 text-purple-700">{{ t('admin.dashboard_title') }}</h1>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else>
      <!-- Maintenance mode control -->
      <div class="mb-6 flex items-center gap-3">
        <span class="text-sm font-medium text-ink-2">{{ t('admin.maintenance_mode') }}:</span>
        <span
          :class="maintenance.enabled ? 'text-red-600' : 'text-green-600'"
          class="text-sm font-semibold"
        >
          {{ maintenance.enabled ? t('admin.maintenance_on') : t('admin.maintenance_off') }}
        </span>
        <button
          @click="setMaintenance(!maintenance.enabled)"
          :disabled="maintenanceLoading"
          class="px-3 py-1 text-xs rounded-md border border-line bg-surface hover:bg-surface-2 disabled:opacity-50"
        >
          {{ maintenanceLoading ? '...' : (maintenance.enabled ? t('admin.maintenance_disable') : t('admin.maintenance_enable')) }}
        </button>
        <label class="inline-flex items-center gap-1 text-xs text-ink-2 cursor-pointer">
          <input
            v-model="autoDisable"
            type="checkbox"
            class="form-checkbox h-3 w-3"
          />
          {{ t('admin.maintenance_auto_disable') }}
        </label>
        <span class="text-xs text-ink-3">
          {{ t('admin.maintenance_hint') }}
        </span>
      </div>

      <!-- System rebuild buttons -->
      <div class="mb-6">
        <div class="text-sm font-medium text-ink-2 mb-2">{{ t('admin.system_title') }}</div>
        <div class="flex flex-wrap gap-2">
          <button
            @click="askRebuild('counts')"
            :disabled="systemLoading !== null"
            class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2 disabled:opacity-50"
          >
            {{ systemLoading === 'counts' ? '...' : t('admin.rebuild_product_counts') }}
          </button>
          <button
            @click="askRebuild('sort')"
            :disabled="systemLoading !== null"
            class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2 disabled:opacity-50"
          >
            {{ systemLoading === 'sort' ? '...' : t('admin.rebuild_sort_indexes') }}
          </button>
          <button
            @click="askRebuild('eanpage')"
            :disabled="systemLoading !== null"
            class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2 disabled:opacity-50"
          >
            {{ systemLoading === 'eanpage' ? '...' : t('admin.rebuild_eanpage_indexes') }}
          </button>
          <button
            @click="askRebuild('attrdef')"
            :disabled="systemLoading !== null"
            class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2 disabled:opacity-50"
          >
            {{ systemLoading === 'attrdef' ? '...' : t('admin.rebuild_attrdef_indexes') }}
          </button>
          <button
            @click="askRebuild('all')"
            :disabled="systemLoading !== null"
            class="px-3 py-1.5 text-xs rounded-md border border-purple-300 bg-purple-50 text-purple-700 hover:bg-purple-100 disabled:opacity-50"
          >
            {{ systemLoading === 'all' ? '...' : t('admin.rebuild_all_eanpage_indexes') }}
          </button>
        </div>
        <div class="text-[11px] text-ink-3 mt-1">
          {{ t('admin.rebuild_hint') }}
        </div>
      </div>

      <!-- Change password -->
      <div class="mb-6">
        <div class="text-sm font-medium text-ink-2 mb-2">{{ t('admin.change_password_title') }}</div>
        <div class="flex flex-col sm:flex-row gap-2 items-start">
          <input
            v-model="passwordForm.oldPassword"
            type="password"
            :placeholder="t('admin.password_old')"
            class="px-2 py-1.5 text-xs rounded-md border border-line flex-1"
          />
          <input
            v-model="passwordForm.newPassword"
            type="password"
            :placeholder="t('admin.password_new')"
            class="px-2 py-1.5 text-xs rounded-md border border-line flex-1"
          />
          <input
            v-model="passwordForm.confirmPassword"
            type="password"
            :placeholder="t('admin.password_confirm')"
            class="px-2 py-1.5 text-xs rounded-md border border-line flex-1"
          />
          <button
            @click="changePassword"
            :disabled="passwordLoading"
            class="px-3 py-1.5 text-xs rounded-md border border-purple-300 bg-purple-50 text-purple-700 hover:bg-purple-100 disabled:opacity-50 whitespace-nowrap"
          >
            {{ passwordLoading ? '...' : t('admin.password_change_btn') }}
          </button>
        </div>
        <div v-if="passwordError" class="text-xs text-red-600 mt-1">{{ passwordError }}</div>
        <div v-if="passwordSuccess" class="text-xs text-green-600 mt-1">{{ passwordSuccess }}</div>
      </div>

      <!-- Stats -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-4 mb-8">
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('admin.users') }}</div>
          <div class="text-2xl font-bold">{{ stats.users }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('admin.companies') }}</div>
          <div class="text-2xl font-bold">{{ stats.companies }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('admin.products') }}</div>
          <div class="text-2xl font-bold">{{ stats.products }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('admin.orders') }}</div>
          <div class="text-2xl font-bold">{{ stats.orders }}</div>
        </div>
        <div class="bg-surface rounded-lg shadow-sm p-4">
          <div class="text-sm text-ink-3">{{ t('admin.revenue') }}</div>
          <div class="text-2xl font-bold text-green-600">{{ formatPrice(stats.revenue) }}</div>
        </div>
      </div>

      <!-- Quick links -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <router-link to="/admin/settings" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.settings') || 'Settings' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.settings_desc') || 'Global settings: currency, etc.' }}</div>
        </router-link>
        <router-link to="/admin/users" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.users_manage') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.users_manage_desc') }}</div>
        </router-link>
        <router-link to="/admin/companies" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.companies_manage') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.companies_manage_desc') }}</div>
        </router-link>
        <router-link to="/admin/categories" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.categories_attrs') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.categories_attrs_desc') }}</div>
        </router-link>
        <router-link to="/admin/analytics" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.analytics') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.analytics_desc') }}</div>
        </router-link>
        <router-link to="/admin/promo" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.promo') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.promo_desc') }}</div>
        </router-link>
        <router-link to="/admin/eanpages" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.eanpage_title') || 'EAN Pages' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.eanpage_manage_desc') || 'Manage SEO product pages' }}</div>
        </router-link>
        <router-link to="/admin/catalogizer" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.catalogizer_title') || 'Auto-Catalogizer' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.catalogizer_desc_short') || 'Auto-assign products to categories' }}</div>
        </router-link>
        <router-link to="/admin/stats" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.visits_stats') }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.visits_stats_desc') }}</div>
        </router-link>
        <router-link to="/admin/delivery-times" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.delivery_times_title') || 'Delivery Times' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.delivery_times_desc') || 'Manage delivery time options' }}</div>
        </router-link>
        <router-link to="/admin/delivery-methods" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.delivery_methods_title') || 'Delivery Methods' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.delivery_methods_desc') || 'Manage delivery method options' }}</div>
        </router-link>
        <router-link to="/admin/installment-plans" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">{{ t('admin.installment_plans_title') || 'Installment Plans' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.installment_plans_desc') || 'Manage installment plans' }}</div>
        </router-link>
        <router-link to="/admin/reviews" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">⭐ {{ t('admin.reviews_manage') || 'Reviews' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.reviews_manage_desc') || 'Manage product reviews & ratings' }}</div>
        </router-link>
        <router-link to="/admin/comments" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">💬 {{ t('admin.comments_manage') || 'Comments' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.comments_manage_desc') || 'Manage user comments & likes' }}</div>
        </router-link>
        <router-link to="/admin/branding" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">🎨 {{ t('admin.branding.title') || 'Branding' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.branding.desc') || 'Page decoration sets & banners' }}</div>
        </router-link>
        <router-link to="/admin/seo" class="bg-surface rounded-lg shadow-sm p-4 hover:shadow-md transition">
          <div class="font-medium">🔍 {{ t('admin.seo.title') || 'SEO' }}</div>
          <div class="text-sm text-ink-3 mt-1">{{ t('admin.seo.desc') || 'Structured data (JSON-LD) for search engines' }}</div>
        </router-link>
      </div>
    </div>

    <ConfirmDialog
      :open="rebuildKey !== null"
      :title="t('admin.system_title')"
      :message="t('admin.rebuild_confirm', { name: rebuildKey ? rebuildLabel(rebuildKey) : '' })"
      :confirm-text="t('admin.save')"
      :cancel-text="t('admin.cancel')"
      @confirm="runRebuild(rebuildKey)"
      @cancel="rebuildKey = null"
    />
  </div>
</template>
