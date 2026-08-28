<script setup>
import { ref, onMounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';
import ConfirmDialog from '../../components/ConfirmDialog.vue';
import EmptyState from '../../components/EmptyState.vue';

const { t } = useI18n();
const { toast } = useToast();

const companies = ref([]);
const loading = ref(true);
const error = ref(null);

// Settings modal
const showSettingsModal = ref(false);
const settingsLoading = ref(false);
const selectedCompany = ref(null);
const companySettings = ref(null);

const paymentMethods = ref([]);
const deliveryTimes = ref([]);
const installmentPlans = ref([]);

// Available currencies for company settings
const currencies = ['PLN', 'EUR', 'USD', 'RUB', 'UAH', 'GBP', 'CHF'];

const fetchCompanies = async () => {
  loading.value = true;
  error.value = null;
  try {
    const response = await api.get('/admin/companies');
    companies.value = response.data.items || response.data || [];
  } catch (e) {
    error.value = t('admin.companies_load_error');
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const verifyCompany = async (id) => {
  try {
    await api.patch(`/admin/companies/${id}`, { status: 'verified' });
    const c = companies.value.find(x => x.id === id);
    if (c) c.status = 'verified';
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.error'));
  }
};

const blockCompanyId = ref(null);

const askBlock = (id) => {
  blockCompanyId.value = id;
};

const blockCompany = async (id) => {
  blockCompanyId.value = null;
  try {
    await api.patch(`/admin/companies/${id}`, { status: 'blocked' });
    const c = companies.value.find(x => x.id === id);
    if (c) c.status = 'blocked';
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.error'));
  }
};

// --- Create company ---
const showCreateModal = ref(false);
const createForm = ref({ name: '', slug: '', owner_user_id: '' });
const createOwnerOptions = ref([]);

const openCreate = async () => {
  createForm.value = { name: '', slug: '', owner_user_id: '' };
  showCreateModal.value = true;
  // Load users as owner options
  try {
    const res = await api.get('/admin/users');
    createOwnerOptions.value = res.data.items || res.data || [];
  } catch (e) {
    createOwnerOptions.value = [];
  }
};

const saveCreate = async () => {
  if (!createForm.value.name || !createForm.value.owner_user_id) {
    toast.error(t('admin.name_and_owner_required') || 'Name and owner are required');
    return;
  }
  try {
    await api.post('/admin/companies', {
      name: createForm.value.name,
      slug: createForm.value.slug || undefined,
      owner_user_id: Number(createForm.value.owner_user_id),
    });
    showCreateModal.value = false;
    toast.success(t('admin.company_created') || 'Company created');
    await fetchCompanies();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.error'));
  }
};

// --- Import company from JSON ---
const importFileInput = ref(null);

const triggerImportFile = () => {
  importFileInput.value?.click();
};

const onImportFile = async (e) => {
  const file = e.target.files?.[0];
  if (!file) return;
  try {
    const text = await file.text();
    const config = JSON.parse(text);
    const res = await api.post('/admin/companies/import', config);
    toast.success(`${t('admin.company_imported') || 'Company imported'}: ${config.name || res.data?.name || ''}`);
    await fetchCompanies();
  } catch (err) {
    toast.error(err.message || t('admin.import_failed') || 'Import failed');
  } finally {
    e.target.value = '';
  }
};

// --- Edit company details (name, multilang, images, descriptions) ---
const showEditModal = ref(false);
const editSaving = ref(false);
const editForm = ref({
  name: '',
  name_ru: '',
  name_ua: '',
  name_pl: '',
  name_en: '',
  logo_url: '',
  website_url: '',
  hero_image: '',
  description: '',
  desc_ru: '',
  desc_ua: '',
  desc_pl: '',
  desc_en: '',
  is_visible: false,
});

const openEdit = async (company) => {
  selectedCompany.value = company;
  editForm.value = {
    name: company.name || '',
    name_ru: '',
    name_ua: '',
    name_pl: '',
    name_en: '',
    logo_url: company.logo_url || '',
    website_url: company.website_url || '',
    hero_image: company.hero_image || '',
    description: company.description || '',
    desc_ru: company.desc_ru || '',
    desc_ua: company.desc_ua || '',
    desc_pl: company.desc_pl || '',
    desc_en: company.desc_en || '',
    is_visible: !!company.is_visible,
  };
  try {
    const res = await api.get(`/admin/companies/${company.id}`);
    const c = res.data;
    editForm.value.name = c.name || '';
    editForm.value.name_ru = c.name_ru || '';
    editForm.value.name_ua = c.name_ua || '';
    editForm.value.name_pl = c.name_pl || '';
    editForm.value.name_en = c.name_en || '';
    editForm.value.logo_url = c.logo_url || '';
    editForm.value.website_url = c.website_url || '';
    editForm.value.hero_image = c.hero_image || '';
    editForm.value.description = c.description || '';
    editForm.value.desc_ru = c.desc_ru || '';
    editForm.value.desc_ua = c.desc_ua || '';
    editForm.value.desc_pl = c.desc_pl || '';
    editForm.value.desc_en = c.desc_en || '';
    editForm.value.is_visible = !!c.is_visible;
  } catch (e) {
    console.error('load company for edit:', e);
  }
  showEditModal.value = true;
};

const saveEdit = async () => {
  if (!selectedCompany.value) return;
  editSaving.value = true;
  try {
    await api.patch(`/admin/companies/${selectedCompany.value.id}`, {
      name: editForm.value.name,
      name_ru: editForm.value.name_ru,
      name_ua: editForm.value.name_ua,
      name_pl: editForm.value.name_pl,
      name_en: editForm.value.name_en,
      logo_url: editForm.value.logo_url,
      website_url: editForm.value.website_url,
      hero_image: editForm.value.hero_image,
      description: editForm.value.description,
      desc_ru: editForm.value.desc_ru,
      desc_ua: editForm.value.desc_ua,
      desc_pl: editForm.value.desc_pl,
      desc_en: editForm.value.desc_en,
      is_visible: editForm.value.is_visible,
    });
    showEditModal.value = false;
    toast.success(t('admin.settings_saved') || 'Saved');
    await fetchCompanies();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.error'));
  } finally {
    editSaving.value = false;
  }
};

// --- Bulk export/import all companies ---
const exportAll = async () => {
  try {
    const res = await api.get('/admin/companies/export-all');
    const blob = new Blob([JSON.stringify(res.data, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `companies-export-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  } catch (e) {
    toast.error(e.response?.data?.message || 'Export failed');
  }
};

const importAllFileInput = ref(null);
const triggerImportAllFile = () => {
  importAllFileInput.value?.click();
};

const onImportAllFile = async (e) => {
  const file = e.target.files?.[0];
  if (!file) return;
  try {
    const text = await file.text();
    const data = JSON.parse(text);
    // Accept either {companies: [...]} or a bare array
    const companies = Array.isArray(data) ? data : (data.companies || []);
    if (companies.length === 0) {
      toast.error('No companies found in file');
      return;
    }
    const res = await api.post('/admin/companies/import-all', { companies });
    toast.success(`${t('admin.companies_imported') || 'Imported'}: +${res.data.created}, ~${res.data.updated}`);
    await fetchCompanies();
  } catch (err) {
    toast.error(err.message || t('admin.import_failed') || 'Import failed');
  } finally {
    e.target.value = '';
  }
};

// --- Delete company ---
const deleteCompanyId = ref(null);

const askDelete = (id) => {
  deleteCompanyId.value = id;
};

const deleteCompany = async (id) => {
  deleteCompanyId.value = null;
  try {
    await api.delete(`/admin/companies/${id}`);
    toast.success(t('admin.company_deleted') || 'Company deleted');
    await fetchCompanies();
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.error'));
  }
};

const openSettings = async (company) => {
  selectedCompany.value = company;
  settingsLoading.value = true;
  try {
    // Fetch full lists
    const [pmRes, dtRes, ipRes] = await Promise.all([
      api.get('/admin/payment-methods'),
      api.get('/admin/delivery-times'),
      api.get('/admin/installment-plans'),
    ]);
    paymentMethods.value = pmRes.data || [];
    deliveryTimes.value = dtRes.data || [];
    installmentPlans.value = ipRes.data || [];

    // Fetch company settings
    const res = await api.get(`/admin/companies/${company.id}/settings`);
    companySettings.value = {
      payment_method_ids: (res.data.company?.payment_method_ids || []).map(Number),
      delivery_time_ids: (res.data.company?.delivery_time_ids || []).map(Number),
      installment_plan_ids: (res.data.company?.installment_plan_ids || []).map(Number),
    };
  } catch (e) {
    console.error('load settings:', e);
    companySettings.value = {
      payment_method_ids: [],
      delivery_time_ids: [],
      installment_plan_ids: [],
    };
  } finally {
    settingsLoading.value = false;
    showSettingsModal.value = true;
  }
};

const saveSettings = async () => {
  if (!selectedCompany.value || !companySettings.value) return;
  try {
    await api.patch(`/admin/companies/${selectedCompany.value.id}`, {
      payment_method_ids: companySettings.value.payment_method_ids,
      delivery_time_ids: companySettings.value.delivery_time_ids,
      installment_plan_ids: companySettings.value.installment_plan_ids,
    });
    showSettingsModal.value = false;
    toast.success(t('admin.settings_saved') || 'Settings saved');
  } catch (e) {
    toast.error(e.response?.data?.message || 'Save error');
  }
};

const toggleSelection = (listKey, id) => {
  const list = companySettings.value[listKey];
  const idx = list.indexOf(id);
  if (idx >= 0) {
    list.splice(idx, 1);
  } else {
    list.push(id);
  }
};

const isSelected = (listKey, id) => {
  return companySettings.value[listKey].includes(id);
};

// --- Price import management (tasks 1, 3, 4, 7) ---
const showPriceModal = ref(false);
const priceLoading = ref(false);
const priceSaving = ref(false);
const importing = ref(false);
const importResult = ref(null);
const priceForm = ref(null);

const emptyPriceForm = () => ({
  import_url: '',
  import_folder: '',
  currency: '',
  is_visible: false,
  hero_image: '',
  desc_ru: '',
  desc_ua: '',
  desc_pl: '',
  desc_en: '',
  price_source: {
    format: 'nokaut',
    ean_field: 'EAN',
    previous_price_field: 'PreviousPrice',
    image_field: 'ImageOriginalUrl',
    product_url_field: 'ProductUrl',
    brand_field: 'Producent',
    shop_category_field: 'ShopProductCategory',
    availability_map: {},
    attr_fields: [],
  },
});

const openPriceModal = async (company) => {
  selectedCompany.value = company;
  priceLoading.value = true;
  importResult.value = null;
  priceForm.value = emptyPriceForm();
  try {
    const res = await api.get(`/admin/companies/${company.id}`);
    const c = res.data;
    priceForm.value.import_url = c.import_url || '';
    priceForm.value.import_folder = c.import_folder || '';
    priceForm.value.currency = c.settings?.currency || '';
    priceForm.value.is_visible = !!c.is_visible;
    priceForm.value.hero_image = c.hero_image || '';
    priceForm.value.desc_ru = c.desc_ru || '';
    priceForm.value.desc_ua = c.desc_ua || '';
    priceForm.value.desc_pl = c.desc_pl || '';
    priceForm.value.desc_en = c.desc_en || '';
    if (c.price_source) {
      // Don't copy currency from price_source (it's in settings.currency)
      const { currency, ...priceSourceWithoutCurrency } = c.price_source;
      priceForm.value.price_source = { ...priceForm.value.price_source, ...priceSourceWithoutCurrency };
      if (!priceForm.value.price_source.attr_fields) priceForm.value.price_source.attr_fields = [];
      if (!priceForm.value.price_source.availability_map) priceForm.value.price_source.availability_map = {};
    }
  } catch (e) {
    console.error('load price config:', e);
  } finally {
    priceLoading.value = false;
    showPriceModal.value = true;
  }
};

const addAttrField = () => {
  priceForm.value.price_source.attr_fields.push({ field: '', code: '' });
};

const removeAttrField = (idx) => {
  priceForm.value.price_source.attr_fields.splice(idx, 1);
};

const addAvailabilityEntry = () => {
  // Add an empty entry; user fills raw value + mapping
  const keys = Object.keys(priceForm.value.price_source.availability_map);
  priceForm.value.price_source.availability_map[`raw_${keys.length + 1}`] = 'in_stock';
};

const removeAvailabilityEntry = (key) => {
  delete priceForm.value.price_source.availability_map[key];
};

const savePriceConfig = async () => {
  if (!selectedCompany.value || !priceForm.value) return;
  priceSaving.value = true;
  try {
    // Remove currency from price_source (it's stored in settings.currency)
    const priceSource = { ...priceForm.value.price_source };
    delete priceSource.currency;
    
    await api.patch(`/admin/companies/${selectedCompany.value.id}`, {
      import_url: priceForm.value.import_url,
      import_folder: priceForm.value.import_folder,
      currency: priceForm.value.currency,
      is_visible: priceForm.value.is_visible,
      hero_image: priceForm.value.hero_image,
      desc_ru: priceForm.value.desc_ru,
      desc_ua: priceForm.value.desc_ua,
      desc_pl: priceForm.value.desc_pl,
      desc_en: priceForm.value.desc_en,
      price_source: priceSource,
    });
    showPriceModal.value = false;
    toast.success(t('admin.settings_saved') || 'Saved');
  } catch (e) {
    toast.error(e.response?.data?.message || 'Save error');
  } finally {
    priceSaving.value = false;
  }
};

const triggerImport = async () => {
  if (!selectedCompany.value) return;
  importing.value = true;
  importResult.value = null;
  try {
    const res = await api.post(`/admin/import-nokaut?company=${selectedCompany.value.id}`);
    importResult.value = res.data;
  } catch (e) {
    importResult.value = { status: 'error', message: e.response?.data?.message || 'Import failed' };
  } finally {
    importing.value = false;
  }
};

const exportCompany = async (company) => {
  try {
    const res = await api.get(`/admin/companies/${company.id}/export`);
    const blob = new Blob([JSON.stringify(res.data, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${company.name || 'company'}.json`;
    a.click();
    URL.revokeObjectURL(url);
  } catch (e) {
    toast.error(e.response?.data?.message || 'Export failed');
  }
};

// --- Unified settings management ---
const showUnifiedSettingsModal = ref(false);
const unifiedSettingsLoading = ref(false);
const unifiedSettingsSaving = ref(false);
const unifiedImporting = ref(false);
const unifiedImportResult = ref(null);
const unifiedSettingsForm = ref(null);

const emptyUnifiedSettingsForm = () => ({
  payment_method_ids: [],
  delivery_time_ids: [],
  installment_plan_ids: [],
  price_source: {
    import_url: '',
    import_folder: '',
    currency: '',
    html_attr_rules: [],
  },
});

const openUnifiedSettings = async (company) => {
  selectedCompany.value = company;
  unifiedSettingsLoading.value = true;
  unifiedImportResult.value = null;
  unifiedSettingsForm.value = emptyUnifiedSettingsForm();
  try {
    const res = await api.get(`/admin/companies/${company.id}`);
    const c = res.data;
    unifiedSettingsForm.value.payment_method_ids = c.payment_method_ids || [];
    unifiedSettingsForm.value.delivery_time_ids = c.delivery_time_ids || [];
    unifiedSettingsForm.value.installment_plan_ids = c.installment_plan_ids || [];
    // Load import_url and import_folder from company root level
    unifiedSettingsForm.value.price_source.import_url = c.import_url || '';
    unifiedSettingsForm.value.price_source.import_folder = c.import_folder || '';
    // Load currency from settings
    unifiedSettingsForm.value.price_source.currency = c.settings?.currency || '';
    // Load html_attr_rules from price_source
    if (c.price_source) {
      unifiedSettingsForm.value.price_source.html_attr_rules = c.price_source.html_attr_rules || [];
    }
  } catch (e) {
    console.error('load unified settings:', e);
  } finally {
    unifiedSettingsLoading.value = false;
    showUnifiedSettingsModal.value = true;
  }
};

const isUnifiedSelected = (field, id) => {
  return unifiedSettingsForm.value?.[field]?.includes(id) || false;
};

const toggleUnifiedSelection = (field, id) => {
  if (!unifiedSettingsForm.value[field]) unifiedSettingsForm.value[field] = [];
  const idx = unifiedSettingsForm.value[field].indexOf(id);
  if (idx > -1) {
    unifiedSettingsForm.value[field].splice(idx, 1);
  } else {
    unifiedSettingsForm.value[field].push(id);
  }
};

const addParserRule = () => {
  if (!unifiedSettingsForm.value.price_source.html_attr_rules) {
    unifiedSettingsForm.value.price_source.html_attr_rules = [];
  }
  unifiedSettingsForm.value.price_source.html_attr_rules.push({
    code: '',
    pattern: '',
    group: 1,
    transform: 'trim',
  });
};

const removeParserRule = (idx) => {
  unifiedSettingsForm.value.price_source.html_attr_rules.splice(idx, 1);
};

const saveUnifiedSettings = async () => {
  if (!selectedCompany.value || !unifiedSettingsForm.value) return;
  unifiedSettingsSaving.value = true;
  try {
    // Save currency before removing from price_source
    const currency = unifiedSettingsForm.value.price_source.currency;
    
    // Remove currency from price_source (it's stored in settings.currency)
    const priceSource = { ...unifiedSettingsForm.value.price_source };
    delete priceSource.currency;
    
    await api.patch(`/admin/companies/${selectedCompany.value.id}`, {
      import_url: unifiedSettingsForm.value.price_source.import_url,
      import_folder: unifiedSettingsForm.value.price_source.import_folder,
      currency: currency,
      payment_method_ids: unifiedSettingsForm.value.payment_method_ids,
      delivery_time_ids: unifiedSettingsForm.value.delivery_time_ids,
      installment_plan_ids: unifiedSettingsForm.value.installment_plan_ids,
      price_source: priceSource,
    });
    showUnifiedSettingsModal.value = false;
    toast.success(t('admin.settings_saved') || 'Saved');
  } catch (e) {
    toast.error(e.response?.data?.message || 'Save error');
  } finally {
    unifiedSettingsSaving.value = false;
  }
};

const triggerUnifiedImport = async () => {
  if (!selectedCompany.value) return;
  unifiedImporting.value = true;
  unifiedImportResult.value = null;
  try {
    const res = await api.post(`/admin/import-nokaut?company=${selectedCompany.value.id}`);
    unifiedImportResult.value = res.data;
  } catch (e) {
    unifiedImportResult.value = { status: 'error', message: e.response?.data?.message || 'Import failed' };
  } finally {
    unifiedImporting.value = false;
  }
};

// Lock body scroll while the settings modal is open
watch(showSettingsModal, (open) => {
  if (typeof document === 'undefined') return;
  document.body.style.overflow = open ? 'hidden' : '';
});

onMounted(fetchCompanies);
</script>

<template>
  <div class="max-w-app mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6 gap-3 flex-wrap">
      <h1 class="text-2xl font-bold text-purple-700">{{ t('admin.companies') }}</h1>
      <div class="flex items-center gap-2 flex-wrap">
        <button @click="openCreate" class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm font-medium hover:bg-purple-700 transition">
          + {{ t('admin.add_company') || 'Add Company' }}
        </button>
        <button @click="exportAll" class="px-4 py-2 bg-sky-600 text-white rounded-lg text-sm font-medium hover:bg-sky-700 transition">
          ⬇ {{ t('admin.export_all') || 'Export All' }}
        </button>
        <button @click="triggerImportAllFile" class="px-4 py-2 bg-sky-600 text-white rounded-lg text-sm font-medium hover:bg-sky-700 transition">
          ⬆ {{ t('admin.import_all') || 'Import All' }}
        </button>
        <input ref="importAllFileInput" type="file" accept=".json,application/json" class="hidden" @change="onImportAllFile" />
      </div>
    </div>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <div v-else-if="error" class="p-4 bg-red-100 text-red-700 rounded-lg">
      {{ error }}
    </div>

    <div v-else-if="companies.length === 0" class="bg-surface rounded-lg shadow-sm">
      <EmptyState icon="users" :title="t('admin.no_companies')" />
    </div>

    <div v-else class="bg-surface rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <caption class="sr-only">{{ t('tables.admin_companies') }}</caption>
        <thead class="bg-surface-2">
          <tr>
            <th scope="col" class="px-4 py-3 text-left">ID</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.name') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.owner') }}</th>
            <th scope="col" class="px-4 py-3 text-left">{{ t('admin.status') }}</th>
            <th scope="col" class="px-4 py-3 text-right">{{ t('admin.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in companies" :key="c.id" class="border-t hover:bg-surface-2">
            <td class="px-4 py-3">{{ c.id }}</td>
            <td class="px-4 py-3">{{ c.name || '—' }}</td>
            <td class="px-4 py-3 text-ink-3">{{ c.owner_user_id }}</td>
            <td class="px-4 py-3">
              <span :class="{
                'text-green-600': c.status === 'verified',
                'text-yellow-600': c.status === 'pending',
                'text-red-600': c.status === 'blocked',
              }">
                {{ c.status }}
              </span>
            </td>
            <td class="px-4 py-3 text-right space-x-2">
              <button v-if="c.status === 'pending'" @click="verifyCompany(c.id)" class="text-green-600 hover:underline text-xs">
                {{ t('admin.verify') }}
              </button>
              <button @click="askBlock(c.id)" class="text-red-600 hover:underline text-xs">
                {{ t('admin.block') }}
              </button>
              <button @click="openEdit(c)" class="text-blue-600 hover:underline text-xs">
                {{ t('admin.edit') || 'Edit' }}
              </button>
              <button @click="openUnifiedSettings(c)" class="text-purple-700 hover:underline text-xs">
                {{ t('admin.settings') || 'Settings' }}
              </button>
              <button @click="exportCompany(c)" class="text-sky-600 hover:underline text-xs">
                {{ t('admin.export') || 'Export' }}
              </button>
              <button @click="askDelete(c.id)" class="text-red-700 hover:underline text-xs">
                {{ t('admin.delete') || 'Delete' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Settings modal -->
    <div v-if="showSettingsModal" class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" @click.self="showSettingsModal = false">
      <div role="dialog" aria-modal="true" class="bg-surface rounded-xl shadow-lg w-full max-w-3xl max-h-[90vh] overflow-y-auto p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold text-purple-700">
            {{ t('admin.company_settings_title') || 'Company Settings' }}: {{ selectedCompany?.name }}
          </h2>
          <button @click="showSettingsModal = false" class="text-ink-3 hover:text-ink-2 text-xl" :aria-label="t('common.close')">&times;</button>
        </div>

        <div v-if="settingsLoading" class="text-sm text-ink-3">Loading...</div>
        <div v-else class="space-y-4">
          <!-- Payment methods -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-1">{{ t('admin.payment_methods') || 'Payment Methods' }}</div>
            <div class="flex flex-wrap gap-2">
              <label v-for="pm in paymentMethods" :key="pm.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isSelected('payment_method_ids', pm.id)" @change="toggleSelection('payment_method_ids', pm.id)" />
                {{ pm.name }}
              </label>
              <span v-if="paymentMethods.length === 0" class="text-xs text-ink-3">No payment methods defined.</span>
            </div>
          </div>

          <!-- Delivery times -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-1">{{ t('admin.delivery_times') || 'Delivery Times' }}</div>
            <div class="flex flex-wrap gap-2">
              <label v-for="dt in deliveryTimes" :key="dt.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isSelected('delivery_time_ids', dt.id)" @change="toggleSelection('delivery_time_ids', dt.id)" />
                {{ dt.name }}
              </label>
              <span v-if="deliveryTimes.length === 0" class="text-xs text-ink-3">No delivery times defined.</span>
            </div>
          </div>

          <!-- Installment plans -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-1">{{ t('admin.installment_plans') || 'Installment Plans' }}</div>
            <div class="flex flex-wrap gap-2">
              <label v-for="ip in installmentPlans" :key="ip.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isSelected('installment_plan_ids', ip.id)" @change="toggleSelection('installment_plan_ids', ip.id)" />
                {{ ip.name }}
              </label>
              <span v-if="installmentPlans.length === 0" class="text-xs text-ink-3">No installment plans defined.</span>
            </div>
          </div>
        </div>

        <div class="mt-4 flex justify-end gap-2">
          <button @click="showSettingsModal = false" class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2">
            {{ t('admin.cancel') }}
          </button>
          <button @click="saveSettings" class="px-3 py-1.5 text-xs rounded-md bg-purple-600 text-white hover:bg-purple-700">
            {{ t('admin.save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Price import modal -->
    <div v-if="showPriceModal" class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" @click.self="showPriceModal = false">
      <div role="dialog" aria-modal="true" class="bg-surface rounded-xl shadow-lg w-full max-w-3xl max-h-[90vh] overflow-y-auto p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold text-orange-600">
            {{ t('admin.price_import_title') || 'Price Import' }}: {{ selectedCompany?.name }}
          </h2>
          <button @click="showPriceModal = false" class="text-ink-3 hover:text-ink-2 text-xl" :aria-label="t('common.close')">&times;</button>
        </div>

        <div v-if="priceLoading" class="text-sm text-ink-3">Loading...</div>
        <div v-else-if="priceForm" class="space-y-4">
          <!-- Import URL -->
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.import_url') || 'Import URL' }}</label>
            <input v-model="priceForm.import_url" type="text" class="w-full px-2 py-1 text-sm rounded-md border border-line bg-surface" placeholder="https://example.com/prices/company.xml" />
            <p class="text-xs text-ink-3 mt-1">{{ t('admin.import_url_hint') || 'URL to download the price file from. Saved to prices/<company>.xml on import.' }}</p>
          </div>

          <!-- Currency + visibility -->
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.currency') || 'Currency' }}</label>
              <select v-model="priceForm.currency" class="w-full px-2 py-1 text-sm rounded-md border border-line bg-surface">
                <option v-for="cur in currencies" :key="cur" :value="cur">{{ cur }}</option>
              </select>
            </div>
            <div class="flex items-end">
              <label class="inline-flex items-center gap-2 text-sm cursor-pointer">
                <input v-model="priceForm.is_visible" type="checkbox" />
                {{ t('admin.landing_visible') || 'Landing page visible' }}
              </label>
            </div>
          </div>

          <!-- Hero image -->
          <div>
            <label class="text-sm font-medium text-ink-2 block mb-1">{{ t('admin.hero_image') || 'Hero Image URL' }}</label>
            <input v-model="priceForm.hero_image" type="text" class="w-full px-2 py-1 text-sm rounded-md border border-line bg-surface" placeholder="https://..." />
          </div>

          <!-- Multilang descriptions -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-1">{{ t('admin.landing_desc') || 'Landing Description' }}</div>
            <div class="space-y-2">
              <div v-for="lang in ['ru', 'ua', 'pl', 'en']" :key="lang">
                <label class="text-xs text-ink-3 block">{{ lang.toUpperCase() }}</label>
                <textarea v-model="priceForm['desc_' + lang]" rows="2" class="w-full px-2 py-1 text-sm rounded-md border border-line bg-surface"></textarea>
              </div>
            </div>
          </div>

          <!-- Price source config -->
          <div class="border-t border-line pt-3">
            <div class="text-sm font-semibold text-ink-2 mb-2">{{ t('admin.price_source') || 'Price Source Config' }}</div>
            <div class="grid grid-cols-2 sm:grid-cols-3 gap-2">
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_format') || 'Format' }}</label>
                <input v-model="priceForm.price_source.format" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_ean') || 'EAN Field' }}</label>
                <input v-model="priceForm.price_source.ean_field" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_prev_price') || 'Prev Price Field' }}</label>
                <input v-model="priceForm.price_source.previous_price_field" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_image') || 'Image Field' }}</label>
                <input v-model="priceForm.price_source.image_field" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_product_url') || 'Product URL Field' }}</label>
                <input v-model="priceForm.price_source.product_url_field" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_brand') || 'Brand Field' }}</label>
                <input v-model="priceForm.price_source.brand_field" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block">{{ t('admin.field_shop_cat') || 'Shop Category Field' }}</label>
                <input v-model="priceForm.price_source.shop_category_field" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
              </div>
            </div>
          </div>

          <!-- Availability map -->
          <div class="border-t border-line pt-3">
            <div class="flex items-center justify-between mb-2">
              <div class="text-sm font-semibold text-ink-2">{{ t('admin.availability_map') || 'Availability Map' }}</div>
              <button @click="addAvailabilityEntry" class="text-xs text-purple-700 hover:underline">+ Add</button>
            </div>
            <div class="space-y-1">
              <div v-for="(val, key) in priceForm.price_source.availability_map" :key="key" class="flex items-center gap-2">
                <input :value="key" type="text" readonly class="flex-1 px-2 py-1 text-xs rounded-md border border-line bg-surface-2" />
                <select v-model="priceForm.price_source.availability_map[key]" class="flex-1 px-2 py-1 text-xs rounded-md border border-line bg-surface">
                  <option value="in_stock">in_stock</option>
                  <option value="out_of_stock">out_of_stock</option>
                </select>
                <button @click="removeAvailabilityEntry(key)" class="text-red-600 text-xs">&times;</button>
              </div>
              <p v-if="Object.keys(priceForm.price_source.availability_map).length === 0" class="text-xs text-ink-3">
                {{ t('admin.availability_map_hint') || 'Optional: map raw availability values. Default: contains "out" → out_of_stock' }}
              </p>
            </div>
          </div>

          <!-- Attr fields -->
          <div class="border-t border-line pt-3">
            <div class="flex items-center justify-between mb-2">
              <div class="text-sm font-semibold text-ink-2">{{ t('admin.attr_fields') || 'Extra Attribute Fields' }}</div>
              <button @click="addAttrField" class="text-xs text-purple-700 hover:underline">+ Add</button>
            </div>
            <div class="space-y-1">
              <div v-for="(af, idx) in priceForm.price_source.attr_fields" :key="idx" class="flex items-center gap-2">
                <input v-model="af.field" type="text" placeholder="XML property (e.g. Material)" class="flex-1 px-2 py-1 text-xs rounded-md border border-line bg-surface" />
                <span class="text-xs text-ink-3">→</span>
                <input v-model="af.code" type="text" placeholder="attr code (e.g. material)" class="flex-1 px-2 py-1 text-xs rounded-md border border-line bg-surface" />
                <button @click="removeAttrField(idx)" class="text-red-600 text-xs">&times;</button>
              </div>
            </div>
          </div>

          <!-- Import result -->
          <div v-if="importResult" class="border-t border-line pt-3 text-sm">
            <div class="text-ink-2 mb-1">{{ t('admin.import_result') || 'Import Result' }}:</div>
            <div v-if="importResult.status === 'error'" class="text-red-600">{{ importResult.message }}</div>
            <div v-else class="text-ink-3">
              {{ t('admin.import_parsed') || 'Parsed' }}: {{ importResult.offers_parsed }} |
              {{ t('admin.import_created') || 'Created' }}: {{ importResult.products_created }} |
              {{ t('admin.import_updated') || 'Updated' }}: {{ importResult.products_updated }} |
              {{ t('admin.import_skipped') || 'Skipped' }}: {{ importResult.products_skipped }}
            </div>
          </div>
        </div>

        <div class="mt-4 flex justify-end gap-2">
          <button @click="showPriceModal = false" class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2">
            {{ t('admin.cancel') }}
          </button>
          <button @click="triggerImport" :disabled="importing" class="px-3 py-1.5 text-xs rounded-md bg-orange-600 text-white hover:bg-orange-700 disabled:opacity-50">
            {{ importing ? (t('admin.importing') || 'Importing...') : (t('admin.run_import') || 'Run Import') }}
          </button>
          <button @click="savePriceConfig" :disabled="priceSaving" class="px-3 py-1.5 text-xs rounded-md bg-purple-600 text-white hover:bg-purple-700 disabled:opacity-50">
            {{ priceSaving ? (t('admin.saving') || 'Saving...') : (t('admin.save') || 'Save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Unified settings modal -->
    <div v-if="showUnifiedSettingsModal" class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" @click.self="showUnifiedSettingsModal = false">
      <div role="dialog" aria-modal="true" class="bg-surface rounded-xl shadow-lg w-full max-w-4xl max-h-[90vh] overflow-y-auto p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold text-purple-700">
            {{ t('admin.unified_settings_title') || 'Company Settings' }}: {{ selectedCompany?.name }}
          </h2>
          <button @click="showUnifiedSettingsModal = false" class="text-ink-3 hover:text-ink-2 text-xl" :aria-label="t('common.close')">&times;</button>
        </div>

        <div v-if="unifiedSettingsLoading" class="text-sm text-ink-3">Loading...</div>
        <div v-else-if="unifiedSettingsForm" class="space-y-6">
          <!-- Section: Payment Methods -->
          <div class="border-b border-line pb-4">
            <h3 class="text-sm font-semibold text-ink-2 mb-2">{{ t('admin.payment_methods') || 'Payment Methods' }}</h3>
            <div class="flex flex-wrap gap-2">
              <label v-for="pm in paymentMethods" :key="pm.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isUnifiedSelected('payment_method_ids', pm.id)" @change="toggleUnifiedSelection('payment_method_ids', pm.id)" />
                {{ pm.name }}
              </label>
              <span v-if="paymentMethods.length === 0" class="text-xs text-ink-3">No payment methods defined.</span>
            </div>
          </div>

          <!-- Section: Delivery Times -->
          <div class="border-b border-line pb-4">
            <h3 class="text-sm font-semibold text-ink-2 mb-2">{{ t('admin.delivery_times') || 'Delivery Times' }}</h3>
            <div class="flex flex-wrap gap-2">
              <label v-for="dt in deliveryTimes" :key="dt.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isUnifiedSelected('delivery_time_ids', dt.id)" @change="toggleUnifiedSelection('delivery_time_ids', dt.id)" />
                {{ dt.name }}
              </label>
              <span v-if="deliveryTimes.length === 0" class="text-xs text-ink-3">No delivery times defined.</span>
            </div>
          </div>

          <!-- Section: Installment Plans -->
          <div class="border-b border-line pb-4">
            <h3 class="text-sm font-semibold text-ink-2 mb-2">{{ t('admin.installment_plans') || 'Installment Plans' }}</h3>
            <div class="flex flex-wrap gap-2">
              <label v-for="ip in installmentPlans" :key="ip.id" class="inline-flex items-center gap-1 text-xs cursor-pointer">
                <input type="checkbox" :checked="isUnifiedSelected('installment_plan_ids', ip.id)" @change="toggleUnifiedSelection('installment_plan_ids', ip.id)" />
                {{ ip.name }}
              </label>
              <span v-if="installmentPlans.length === 0" class="text-xs text-ink-3">No installment plans defined.</span>
            </div>
          </div>

          <!-- Section: Price Import Settings -->
          <div class="border-b border-line pb-4">
            <h3 class="text-sm font-semibold text-ink-2 mb-2">{{ t('admin.price_import_title') || 'Price Import' }}</h3>
            <div class="grid grid-cols-2 gap-3">
              <div class="col-span-2">
                <label class="text-xs text-ink-3 block mb-1">{{ t('admin.import_url') || 'Import URL' }}</label>
                <input v-model="unifiedSettingsForm.price_source.import_url" type="text" class="w-full px-2 py-1 text-sm rounded-md border border-line bg-surface" placeholder="https://example.com/prices/company.xml" />
              </div>
              <div>
                <label class="text-xs text-ink-3 block mb-1">{{ t('admin.currency') || 'Currency' }}</label>
                <select v-model="unifiedSettingsForm.price_source.currency" class="w-full px-2 py-1 text-sm rounded-md border border-line bg-surface">
                  <option v-for="cur in currencies" :key="cur" :value="cur">{{ cur }}</option>
                </select>
              </div>
            </div>
          </div>

          <!-- Section: Parser Rules -->
          <div class="border-b border-line pb-4">
            <div class="flex items-center justify-between mb-2">
              <h3 class="text-sm font-semibold text-ink-2">{{ t('admin.parser_rules') || 'Parser Rules' }}</h3>
              <button @click="addParserRule" class="text-xs text-purple-700 hover:underline">+ Add Rule</button>
            </div>
            <div class="space-y-2">
              <div v-for="(rule, idx) in unifiedSettingsForm.price_source.html_attr_rules" :key="idx" class="grid grid-cols-2 gap-2 p-2 bg-surface-2 rounded-md">
                <div>
                  <label class="text-xs text-ink-3 block mb-1">Code</label>
                  <input v-model="rule.code" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
                </div>
                <div>
                  <label class="text-xs text-ink-3 block mb-1">Pattern</label>
                  <input v-model="rule.pattern" type="text" class="w-full px-2 py-1 text-xs rounded-md border border-line bg-surface" />
                </div>
                <div class="col-span-2 flex items-center justify-between">
                  <span class="text-xs text-ink-3">Group: {{ rule.group || 1 }}</span>
                  <button @click="removeParserRule(idx)" class="text-red-600 text-xs">&times; Remove</button>
                </div>
              </div>
              <p v-if="!unifiedSettingsForm.price_source.html_attr_rules || unifiedSettingsForm.price_source.html_attr_rules.length === 0" class="text-xs text-ink-3">
                No parser rules defined.
              </p>
            </div>
          </div>

          <!-- Import button -->
          <div class="flex justify-between items-center">
            <button @click="triggerUnifiedImport" :disabled="unifiedImporting" class="px-3 py-1.5 text-xs rounded-md bg-orange-600 text-white hover:bg-orange-700 disabled:opacity-50">
              {{ unifiedImporting ? (t('admin.importing') || 'Importing...') : (t('admin.run_import') || 'Run Import') }}
            </button>
            <div v-if="unifiedImportResult" class="text-sm text-ink-3">
              {{ unifiedImportResult.status === 'error' ? unifiedImportResult.message : 
                 `Parsed: ${unifiedImportResult.offers_parsed} | Created: ${unifiedImportResult.products_created} | Updated: ${unifiedImportResult.products_updated}` }}
            </div>
          </div>
        </div>

        <div class="mt-4 flex justify-end gap-2">
          <button @click="showUnifiedSettingsModal = false" class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2">
            {{ t('admin.cancel') }}
          </button>
          <button @click="saveUnifiedSettings" :disabled="unifiedSettingsSaving" class="px-3 py-1.5 text-xs rounded-md bg-purple-600 text-white hover:bg-purple-700 disabled:opacity-50">
            {{ unifiedSettingsSaving ? (t('admin.saving') || 'Saving...') : (t('admin.save') || 'Save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Edit company details modal -->
    <div v-if="showEditModal" class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" @click.self="showEditModal = false">
      <div role="dialog" aria-modal="true" class="bg-surface rounded-xl shadow-lg w-full max-w-2xl max-h-[90vh] overflow-y-auto p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold text-blue-700">{{ t('admin.edit_company') || 'Edit Company' }}: {{ selectedCompany?.name }}</h2>
          <button @click="showEditModal = false" class="text-ink-3 hover:text-ink-2 text-xl" :aria-label="t('common.close')">&times;</button>
        </div>

        <div class="space-y-4">
          <!-- Name (base) -->
          <div>
            <label class="block text-sm text-ink-2 mb-1">{{ t('admin.name') }}</label>
            <input v-model="editForm.name" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" />
          </div>

          <!-- Multilang names -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-2">{{ t('admin.name_translations') || 'Name Translations' }}</div>
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs text-ink-3 mb-1">RU</label>
                <input v-model="editForm.name_ru" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" />
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">UA</label>
                <input v-model="editForm.name_ua" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" />
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">PL</label>
                <input v-model="editForm.name_pl" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" />
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">EN</label>
                <input v-model="editForm.name_en" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" />
              </div>
            </div>
          </div>

          <!-- Images -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-2">{{ t('admin.images') || 'Images' }}</div>
            <div class="space-y-2">
              <div>
                <label class="block text-xs text-ink-3 mb-1">{{ t('admin.website_url') || 'Website URL' }}</label>
                <input v-model="editForm.website_url" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" placeholder="https://company-website.com" />
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">{{ t('admin.logo_url') || 'Logo URL' }}</label>
                <input v-model="editForm.logo_url" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" placeholder="https://..." />
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">{{ t('admin.hero_image') || 'Hero Image URL' }}</label>
                <input v-model="editForm.hero_image" type="text" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm" placeholder="https://..." />
              </div>
              <img v-if="editForm.logo_url" :src="editForm.logo_url" alt="logo" class="h-10 rounded border border-line" @error="$event.target.style.display='none'" />
            </div>
          </div>

          <!-- Description (base) -->
          <div>
            <label class="block text-sm text-ink-2 mb-1">{{ t('admin.description') || 'Description' }}</label>
            <textarea v-model="editForm.description" rows="2" class="w-full px-3 py-2 border border-line rounded-lg text-sm"></textarea>
          </div>

          <!-- Multilang descriptions -->
          <div>
            <div class="text-sm font-medium text-ink-2 mb-2">{{ t('admin.description_translations') || 'Description Translations' }}</div>
            <div class="space-y-2">
              <div>
                <label class="block text-xs text-ink-3 mb-1">RU</label>
                <textarea v-model="editForm.desc_ru" rows="2" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm"></textarea>
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">UA</label>
                <textarea v-model="editForm.desc_ua" rows="2" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm"></textarea>
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">PL</label>
                <textarea v-model="editForm.desc_pl" rows="2" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm"></textarea>
              </div>
              <div>
                <label class="block text-xs text-ink-3 mb-1">EN</label>
                <textarea v-model="editForm.desc_en" rows="2" class="w-full px-3 py-1.5 border border-line rounded-lg text-sm"></textarea>
              </div>
            </div>
          </div>

          <!-- Visibility -->
          <label class="inline-flex items-center gap-2 text-sm text-ink-2 cursor-pointer">
            <input v-model="editForm.is_visible" type="checkbox" />
            {{ t('admin.is_visible') || 'Visible on landing page' }}
          </label>
        </div>

        <div class="flex justify-end gap-2 mt-4">
          <button @click="showEditModal = false" class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2">
            {{ t('admin.cancel') }}
          </button>
          <button @click="saveEdit" :disabled="editSaving" class="px-3 py-1.5 text-xs rounded-md bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50">
            {{ editSaving ? (t('admin.saving') || 'Saving...') : (t('admin.save') || 'Save') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Create company modal -->
    <div v-if="showCreateModal" class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" @click.self="showCreateModal = false">
      <div role="dialog" aria-modal="true" class="bg-surface rounded-xl shadow-lg w-full max-w-md p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-lg font-semibold text-purple-700">{{ t('admin.add_company') || 'Add Company' }}</h2>
          <button @click="showCreateModal = false" class="text-ink-3 hover:text-ink-2 text-xl" :aria-label="t('common.close')">&times;</button>
        </div>
        <div class="space-y-3">
          <div>
            <label class="block text-sm text-ink-2 mb-1">{{ t('admin.name') }}</label>
            <input v-model="createForm.name" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" placeholder="Company name" />
          </div>
          <div>
            <label class="block text-sm text-ink-2 mb-1">{{ t('admin.slug') || 'Slug (optional)' }}</label>
            <input v-model="createForm.slug" type="text" class="w-full px-3 py-2 border border-line rounded-lg text-sm" placeholder="company-slug" />
          </div>
          <div>
            <label class="block text-sm text-ink-2 mb-1">{{ t('admin.owner') }}</label>
            <select v-model="createForm.owner_user_id" class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface">
              <option value="">— select user —</option>
              <option v-for="u in createOwnerOptions" :key="u.id" :value="u.id">{{ u.email }} (id: {{ u.id }})</option>
            </select>
          </div>
        </div>
        <div class="flex justify-end gap-2 mt-4">
          <button @click="showCreateModal = false" class="px-3 py-1.5 text-xs rounded-md border border-line bg-surface hover:bg-surface-2">
            {{ t('admin.cancel') }}
          </button>
          <button @click="saveCreate" class="px-3 py-1.5 text-xs rounded-md bg-purple-600 text-white hover:bg-purple-700">
            {{ t('admin.create') || 'Create' }}
          </button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :open="blockCompanyId !== null"
      :title="t('admin.companies')"
      :message="t('admin.block_confirm')"
      variant="danger"
      :confirm-text="t('admin.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="blockCompany(blockCompanyId)"
      @cancel="blockCompanyId = null"
    />

    <ConfirmDialog
      :open="deleteCompanyId !== null"
      :title="t('admin.companies')"
      :message="t('admin.delete_company_confirm') || 'Delete this company? This cannot be undone.'"
      variant="danger"
      :confirm-text="t('admin.delete')"
      :cancel-text="t('admin.cancel')"
      @confirm="deleteCompany(deleteCompanyId)"
      @cancel="deleteCompanyId = null"
    />
  </div>
</template>
