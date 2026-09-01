<script setup>
// Admin SEO: configurable structured data (schema.org JSON-LD) settings.
// These settings drive the JSON-LD blocks injected into every landing page
// (Organization / WebSite / OnlineStore on all pages; Product / BreadcrumbList
// / OnlineStore on product pages). See internal/api/jsonld.go.
import { ref, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import api from '../../api';
import { useToast } from '../../composables/useToast';

const { t } = useI18n();
const { toast } = useToast();

const loading = ref(true);
const saving = ref(false);
const saved = ref(false);

// The settings form. `sameAs` fields are edited as newline-separated text and
// converted to/from arrays on load/save.
const form = ref(defaultForm());

function defaultForm() {
  return {
    enabled: true,
    // Organization
    org_name: '',
    org_legal_name: '',
    org_logo: '',
    org_phone: '',
    org_email: '',
    org_street: '',
    org_city: '',
    org_postal_code: '',
    org_country: '',
    org_same_as: [],
    // WebSite
    site_name: '',
    search_url_template: '/shop?q={search_term_string}',
    // OnlineStore
    store_name: '',
    store_logo: '',
    store_same_as: [],
    // Product offer defaults
    default_currency: '',
    price_valid_days: 30,
    // Merchant return policy
    return_policy_enabled: false,
    return_policy_text: '',
    return_policy_days: 14,
    return_policy_country: '',
    // Shipping details
    shipping_enabled: false,
    shipping_cost: 0,
    shipping_min_days: 1,
    shipping_max_days: 3,
    shipping_destination: '',
  };
}

// Textarea <-> array helpers for sameAs URL lists.
const orgSameAsText = ref('');
const storeSameAsText = ref('');

const splitLines = (s) =>
  (s || '')
    .split('\n')
    .map((x) => x.trim())
    .filter((x) => x.length > 0);

const joinLines = (arr) => (arr || []).join('\n');

const load = async () => {
  loading.value = true;
  try {
    const res = await api.get('/admin/seo/settings');
    const s = res.data || {};
    form.value = {
      enabled: s.enabled !== false,
      org_name: s.org_name || '',
      org_legal_name: s.org_legal_name || '',
      org_logo: s.org_logo || '',
      org_phone: s.org_phone || '',
      org_email: s.org_email || '',
      org_street: s.org_street || '',
      org_city: s.org_city || '',
      org_postal_code: s.org_postal_code || '',
      org_country: s.org_country || '',
      org_same_as: s.org_same_as || [],
      site_name: s.site_name || '',
      search_url_template: s.search_url_template || '/shop?q={search_term_string}',
      store_name: s.store_name || '',
      store_logo: s.store_logo || '',
      store_same_as: s.store_same_as || [],
      default_currency: s.default_currency || '',
      price_valid_days: s.price_valid_days || 30,
      return_policy_enabled: s.return_policy_enabled === true,
      return_policy_text: s.return_policy_text || '',
      return_policy_days: s.return_policy_days || 14,
      return_policy_country: s.return_policy_country || '',
      shipping_enabled: s.shipping_enabled === true,
      shipping_cost: s.shipping_cost || 0,
      shipping_min_days: s.shipping_min_days || 1,
      shipping_max_days: s.shipping_max_days || 3,
      shipping_destination: s.shipping_destination || '',
    };
    orgSameAsText.value = joinLines(s.org_same_as);
    storeSameAsText.value = joinLines(s.store_same_as);
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.seo.load_error'));
  } finally {
    loading.value = false;
  }
};

const save = async () => {
  saving.value = true;
  saved.value = false;
  try {
    const payload = {
      ...form.value,
      org_same_as: splitLines(orgSameAsText.value),
      store_same_as: splitLines(storeSameAsText.value),
    };
    const res = await api.put('/admin/seo/settings', payload);
    // Reflect server-side normalization (e.g. updated_at).
    if (res.data) {
      form.value.price_valid_days = res.data.price_valid_days ?? form.value.price_valid_days;
    }
    toast.success(t('admin.seo.saved'));
    saved.value = true;
    setTimeout(() => (saved.value = false), 2500);
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.seo.save_error'));
  } finally {
    saving.value = false;
  }
};

const inputCls = 'w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface';
const labelCls = 'block text-sm font-medium mb-1';

// ---------- Logo upload ----------
// Logos are uploaded as files (subdir "seo") and the returned relative URL
// (e.g. /uploads/seo/logo.png) is stored. The JSON-LD builder turns it into an
// absolute URL at render time.
const LOGO_MAX_DIM = 512;
const uploading = ref({});

const uploadLogo = async (field, file) => {
  if (!file) return;
  const validTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/webp', 'image/gif', 'image/svg+xml'];
  if (!validTypes.includes(file.type)) {
    toast.error(t('admin.seo.invalid_file'));
    return;
  }
  if (file.size > 10 * 1024 * 1024) {
    toast.error(t('admin.seo.file_too_large'));
    return;
  }
  uploading.value[field] = true;
  try {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('subdir', 'seo');
    formData.append('max_dim', String(LOGO_MAX_DIM));
    const res = await api.post('/admin/upload-image', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    });
    form.value[field] = res.data.url;
    toast.success(t('admin.seo.logo_uploaded'));
  } catch (e) {
    toast.error(e.response?.data?.message || t('admin.seo.upload_error'));
  } finally {
    uploading.value[field] = false;
  }
};

const onLogoFile = (field, event) => {
  const file = event.target.files?.[0];
  if (file) uploadLogo(field, file);
  event.target.value = ''; // allow re-selecting the same file
};

onMounted(load);
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-xl font-semibold">{{ t('admin.seo.title') }}</h1>
        <p class="text-sm text-ink-3 mt-1">{{ t('admin.seo.subtitle') }}</p>
      </div>
      <button
        @click="save"
        :disabled="saving"
        class="px-4 py-2 bg-accent text-white rounded-lg text-sm font-medium hover:opacity-90 disabled:opacity-50"
      >
        {{ saving ? t('admin.seo.saving') : t('admin.save') }}
      </button>
    </div>

    <div v-if="loading" class="bg-surface rounded-xl border border-line p-8 text-center text-ink-3">
      {{ t('admin.seo.loading') }}
    </div>

    <template v-else>
      <!-- Master toggle -->
      <div class="bg-surface rounded-xl shadow-sm border border-line p-5">
        <label class="flex items-center gap-3 cursor-pointer">
          <input type="checkbox" v-model="form.enabled" class="w-4 h-4 accent-accent" />
          <span class="font-medium">{{ t('admin.seo.enabled') }}</span>
        </label>
        <p class="text-sm text-ink-3 mt-2 ml-7">{{ t('admin.seo.enabled_hint') }}</p>
      </div>

      <div v-if="form.enabled" class="space-y-6">
        <!-- Organization -->
        <div class="bg-surface rounded-xl shadow-sm border border-line p-5 space-y-4">
          <h2 class="font-semibold">{{ t('admin.seo.org_section') }}</h2>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_name') }}</label>
              <input v-model.trim="form.org_name" type="text" :class="inputCls" placeholder="HDWR" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_legal_name') }}</label>
              <input v-model.trim="form.org_legal_name" type="text" :class="inputCls" placeholder="HDWR Sp. z o.o." />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_logo') }}</label>
              <div class="flex items-center gap-3">
                <img v-if="form.org_logo" :src="form.org_logo" class="h-12 w-auto max-w-[160px] object-contain rounded border border-line bg-surface" alt="logo" />
                <div class="flex flex-col gap-1">
                  <label class="inline-block px-3 py-1.5 bg-accent text-white rounded-lg text-sm cursor-pointer hover:opacity-90 disabled:opacity-50" :class="{ 'opacity-50': uploading.org_logo }">
                    {{ uploading.org_logo ? t('admin.seo.uploading') : (form.org_logo ? t('admin.seo.replace_logo') : t('admin.seo.upload_logo')) }}
                    <input type="file" accept="image/*" class="hidden" :disabled="uploading.org_logo" @change="onLogoFile('org_logo', $event)" />
                  </label>
                  <button v-if="form.org_logo" @click="form.org_logo = ''" class="text-xs text-ink-3 hover:text-red-500 text-left">
                    {{ t('admin.seo.remove_logo') }}
                  </button>
                </div>
              </div>
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_phone') }}</label>
              <input v-model.trim="form.org_phone" type="text" :class="inputCls" placeholder="+48-456-456-001" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_email') }}</label>
              <input v-model.trim="form.org_email" type="text" :class="inputCls" placeholder="biuro@hdwr.pl" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_country') }}</label>
              <input v-model.trim="form.org_country" type="text" :class="inputCls" placeholder="PL" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_street') }}</label>
              <input v-model.trim="form.org_street" type="text" :class="inputCls" placeholder="ul. Dmowskiego 28" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_city') }}</label>
              <input v-model.trim="form.org_city" type="text" :class="inputCls" placeholder="Środa Wielkopolska" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_postal_code') }}</label>
              <input v-model.trim="form.org_postal_code" type="text" :class="inputCls" placeholder="63-000" />
            </div>
          </div>
          <div>
            <label class="text-sm font-medium mb-1">{{ t('admin.seo.org_same_as') }}</label>
            <textarea
              v-model="orgSameAsText"
              rows="3"
              :class="inputCls"
              :placeholder="t('admin.seo.same_as_hint')"
            ></textarea>
          </div>
        </div>

        <!-- WebSite -->
        <div class="bg-surface rounded-xl shadow-sm border border-line p-5 space-y-4">
          <h2 class="font-semibold">{{ t('admin.seo.website_section') }}</h2>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.site_name') }}</label>
              <input v-model.trim="form.site_name" type="text" :class="inputCls" placeholder="HDWR - Sprzęt" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.search_template') }}</label>
              <input v-model.trim="form.search_url_template" type="text" :class="inputCls" placeholder="/shop?q={search_term_string}" />
              <p class="text-xs text-ink-3 mt-1">{{ t('admin.seo.search_template_hint') }}</p>
            </div>
          </div>
        </div>

        <!-- OnlineStore -->
        <div class="bg-surface rounded-xl shadow-sm border border-line p-5 space-y-4">
          <h2 class="font-semibold">{{ t('admin.seo.store_section') }}</h2>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.store_name') }}</label>
              <input v-model.trim="form.store_name" type="text" :class="inputCls" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.store_logo') }}</label>
              <div class="flex items-center gap-3">
                <img v-if="form.store_logo" :src="form.store_logo" class="h-12 w-auto max-w-[160px] object-contain rounded border border-line bg-surface" alt="logo" />
                <div class="flex flex-col gap-1">
                  <label class="inline-block px-3 py-1.5 bg-accent text-white rounded-lg text-sm cursor-pointer hover:opacity-90 disabled:opacity-50" :class="{ 'opacity-50': uploading.store_logo }">
                    {{ uploading.store_logo ? t('admin.seo.uploading') : (form.store_logo ? t('admin.seo.replace_logo') : t('admin.seo.upload_logo')) }}
                    <input type="file" accept="image/*" class="hidden" :disabled="uploading.store_logo" @change="onLogoFile('store_logo', $event)" />
                  </label>
                  <button v-if="form.store_logo" @click="form.store_logo = ''" class="text-xs text-ink-3 hover:text-red-500 text-left">
                    {{ t('admin.seo.remove_logo') }}
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div>
            <label class="text-sm font-medium mb-1">{{ t('admin.seo.store_same_as') }}</label>
            <textarea
              v-model="storeSameAsText"
              rows="3"
              :class="inputCls"
              :placeholder="t('admin.seo.same_as_hint')"
            ></textarea>
          </div>
        </div>

        <!-- Product offer defaults -->
        <div class="bg-surface rounded-xl shadow-sm border border-line p-5 space-y-4">
          <h2 class="font-semibold">{{ t('admin.seo.product_section') }}</h2>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.default_currency') }}</label>
              <input v-model.trim="form.default_currency" type="text" :class="inputCls" placeholder="PLN" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.price_valid_days') }}</label>
              <input v-model.number="form.price_valid_days" type="number" min="0" max="365" :class="inputCls" />
              <p class="text-xs text-ink-3 mt-1">{{ t('admin.seo.price_valid_days_hint') }}</p>
            </div>
          </div>
        </div>

        <!-- Merchant return policy -->
        <div class="bg-surface rounded-xl shadow-sm border border-line p-5 space-y-4">
          <div class="flex items-center justify-between">
            <h2 class="font-semibold">{{ t('admin.seo.return_section') }}</h2>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" v-model="form.return_policy_enabled" class="w-4 h-4 accent-accent" />
              <span class="text-sm">{{ t('admin.seo.enabled') }}</span>
            </label>
          </div>
          <div v-if="form.return_policy_enabled" class="space-y-4">
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.return_text') }}</label>
              <textarea v-model="form.return_policy_text" rows="2" :class="inputCls" :placeholder="t('admin.seo.return_text_hint')"></textarea>
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label class="text-sm font-medium mb-1">{{ t('admin.seo.return_days') }}</label>
                <input v-model.number="form.return_policy_days" type="number" min="0" max="365" :class="inputCls" />
              </div>
              <div>
                <label class="text-sm font-medium mb-1">{{ t('admin.seo.return_country') }}</label>
                <input v-model.trim="form.return_policy_country" type="text" :class="inputCls" placeholder="PL" />
              </div>
            </div>
          </div>
        </div>

        <!-- Shipping details -->
        <div class="bg-surface rounded-xl shadow-sm border border-line p-5 space-y-4">
          <div class="flex items-center justify-between">
            <h2 class="font-semibold">{{ t('admin.seo.shipping_section') }}</h2>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" v-model="form.shipping_enabled" class="w-4 h-4 accent-accent" />
              <span class="text-sm">{{ t('admin.seo.enabled') }}</span>
            </label>
          </div>
          <div v-if="form.shipping_enabled" class="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.shipping_cost') }}</label>
              <input v-model.number="form.shipping_cost" type="number" min="0" step="0.01" :class="inputCls" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.shipping_min_days') }}</label>
              <input v-model.number="form.shipping_min_days" type="number" min="0" max="365" :class="inputCls" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.shipping_max_days') }}</label>
              <input v-model.number="form.shipping_max_days" type="number" min="0" max="365" :class="inputCls" />
            </div>
            <div>
              <label class="text-sm font-medium mb-1">{{ t('admin.seo.shipping_destination') }}</label>
              <input v-model.trim="form.shipping_destination" type="text" :class="inputCls" placeholder="PL" />
            </div>
          </div>
        </div>
      </div>

      <div class="flex items-center gap-3">
        <button
          @click="save"
          :disabled="saving"
          class="px-4 py-2 bg-accent text-white rounded-lg text-sm font-medium hover:opacity-90 disabled:opacity-50"
        >
          {{ saving ? t('admin.seo.saving') : t('admin.save') }}
        </button>
        <span v-if="saved" class="text-sm text-green-600">{{ t('admin.seo.saved_ok') }}</span>
      </div>
    </template>
  </div>
</template>
