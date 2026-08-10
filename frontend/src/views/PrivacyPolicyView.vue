<template>
  <div class="max-w-3xl mx-auto px-4 py-8">
    <h1 class="text-2xl font-bold mb-6">{{ t('privacy.title') }}</h1>

    <p class="text-gray-700 mb-6">{{ t('privacy.intro') }}</p>

    <section class="mb-6">
      <h2 class="text-lg font-semibold mb-2">{{ t('privacy.data_collected') }}</h2>
      <p class="text-gray-700">{{ t('privacy.data_collected_desc') }}</p>
    </section>

    <section class="mb-6">
      <h2 class="text-lg font-semibold mb-2">{{ t('privacy.cookies') }}</h2>
      <p class="text-gray-700">{{ t('privacy.cookies_desc') }}</p>
    </section>

    <section class="mb-6">
      <h2 class="text-lg font-semibold mb-2">{{ t('privacy.data_usage') }}</h2>
      <p class="text-gray-700">{{ t('privacy.data_usage_desc') }}</p>
    </section>

    <section class="mb-6">
      <h2 class="text-lg font-semibold mb-2">{{ t('privacy.rights') }}</h2>
      <p class="text-gray-700">{{ t('privacy.rights_desc') }}</p>
    </section>

    <section class="mb-6">
      <h2 class="text-lg font-semibold mb-2">{{ t('privacy.contact') }}</h2>
      <p class="text-gray-700">{{ t('privacy.contact_desc') }} privacy@mako.com</p>
    </section>

    <p class="text-sm text-gray-500">
      {{ t('privacy.last_updated', { date: lastUpdated }) }}
    </p>
  </div>
</template>

<script setup>
import { onMounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';

const { t, locale } = useI18n();

const lastUpdated = computed(() => {
  return new Date().toLocaleDateString(locale.value);
});

onMounted(() => {
  document.title = `${t('privacy.title')} — MakoShop`;
});
</script>
