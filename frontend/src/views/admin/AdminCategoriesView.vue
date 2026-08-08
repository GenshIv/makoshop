<script setup>
import { ref, reactive, onMounted } from 'vue';
import api from '../../api';

const categories = ref([]);
const loading = ref(true);
const showForm = ref(false);

const form = reactive({
  name: '',
  parent_id: 0,
  description: '',
  is_active: true,
});

const fetchCategories = async () => {
  loading.value = true;
  try {
    const response = await api.get('/categories');
    categories.value = response.data.items || response.data || [];
  } catch (e) {
    console.error(e);
  } finally {
    loading.value = false;
  }
};

const createCategory = async () => {
  if (!form.name) return;
  try {
    await api.post('/admin/categories', form);
    showForm.value = false;
    Object.assign(form, { name: '', parent_id: 0, description: '', is_active: true });
    await fetchCategories();
  } catch (e) {
    alert(e.response?.data?.message || 'Ошибка');
  }
};

onMounted(fetchCategories);
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold text-purple-700">Категории</h1>
      <button @click="showForm = true" class="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700">
        + Категория
      </button>
    </div>

    <div v-if="loading" class="flex justify-center py-12">
      <div class="animate-spin h-8 w-8 border-4 border-purple-600 border-t-transparent rounded-full"></div>
    </div>

    <!-- Form -->
    <div v-if="showForm" class="mb-6 bg-white rounded-lg shadow-sm p-4">
      <h3 class="font-medium mb-3">Новая категория</h3>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <input v-model="form.name" type="text" placeholder="Название" class="px-3 py-2 border border-gray-300 rounded-lg text-sm" />
        <input v-model.number="form.parent_id" type="number" placeholder="Parent ID (0 = root)" class="px-3 py-2 border border-gray-300 rounded-lg text-sm" />
        <textarea v-model="form.description" placeholder="Описание" rows="2" class="sm:col-span-2 px-3 py-2 border border-gray-300 rounded-lg text-sm"></textarea>
      </div>
      <div class="flex gap-2 mt-3">
        <button @click="createCategory" class="px-4 py-2 bg-purple-600 text-white rounded-lg text-sm hover:bg-purple-700">
          Создать
        </button>
        <button @click="showForm = false" class="px-4 py-2 border rounded-lg text-sm hover:bg-gray-50">
          Отмена
        </button>
      </div>
    </div>

    <!-- List -->
    <div v-else-if="categories.length === 0" class="text-center py-12 text-gray-500">
      Категорий пока нет
    </div>

    <div v-else class="bg-white rounded-lg shadow-sm overflow-hidden">
      <table class="w-full text-sm">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-4 py-3 text-left">ID</th>
            <th class="px-4 py-3 text-left">Название</th>
            <th class="px-4 py-3 text-left">Parent</th>
            <th class="px-4 py-3 text-left">Статус</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="cat in categories" :key="cat.id" class="border-t hover:bg-gray-50">
            <td class="px-4 py-3">{{ cat.id }}</td>
            <td class="px-4 py-3">{{ cat.name }}</td>
            <td class="px-4 py-3 text-gray-500">{{ cat.parent_id || '—' }}</td>
            <td class="px-4 py-3">
              <span :class="cat.is_active ? 'text-green-600' : 'text-gray-400'">
                {{ cat.is_active ? 'Активна' : 'Неактивна' }}
              </span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
