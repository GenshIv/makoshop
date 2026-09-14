# Lenovo PSREF Import

Интеграция с Lenovo Product Specifications Reference (PSREF) для импорта спецификаций продуктов.

## API Endpoint

**POST /admin/import-lenovo**

Импортирует модели Lenovo из официального справочника PSREF.

### Request Body

```json
{
  "product_key": "ThinkPad_E470",
  "page": 1,
  "pagesize": 30
}
```

| Поле | Тип | Обязательное | Описание |
|------|-----|--------------|----------|
| `product_key` | string | Да | Ключ продукта (например, `ThinkPad_E470`, `IdeaPad_5_2_in_1_14IWC11`) |
| `page` | int | Нет | Номер страницы (по умолчанию 1) |
| `pagesize` | int | Нет | Количество моделей на странице (по умолчанию 30, максимум ~100) |

### Response

```json
{
  "status": "success",
  "product_key": "ThinkPad_E470",
  "total": 198,
  "received": 5,
  "imported": 5
}
```

| Поле | Описание |
|------|----------|
| `status` | Статус операции (`success`) |
| `product_key` | Запрошенный ключ продукта |
| `total` | Общее количество моделей для этого продукта |
| `received` | Количество моделей, полученных в этом запросе |
| `imported` | Количество успешно импортированных моделей |

## Примеры использования

### Импорт одной страницы моделей

```bash
curl -X POST http://localhost:9090/admin/import-lenovo \
  -H "Content-Type: application/json" \
  -d '{"product_key": "ThinkPad_E470", "page": 1, "pagesize": 30}'
```

### Импорт всех моделей (батч)

Для импорта всех моделей продукта нужно сделать несколько запросов с разными номерами страниц:

```bash
# Страница 1
curl -X POST http://localhost:9090/admin/import-lenovo \
  -H "Content-Type: application/json" \
  -d '{"product_key": "ThinkPad_E470", "page": 1, "pagesize": 30}'

# Страница 2
curl -X POST http://localhost:9090/admin/import-lenovo \
  -H "Content-Type: application/json" \
  -d '{"product_key": "ThinkPad_E470", "page": 2, "pagesize": 30}'

# ... и так далее пока received > 0
```

## Маппинг атрибутов

Данные из API Lenovo автоматически маппятся в атрибуты EAN-страниц:

| Lenovo API Column | Атрибут EAN-страницы |
|-------------------|---------------------|
| Processor | processor |
| Graphics | graphics |
| Memory | memory |
| Storage | storage |
| Display | display |
| Operating System | os |
| Battery | battery |
| Power Adapter | power_adapter |
| Keyboard | keyboard |
| Case Material | case_material |
| Color | color |
| Camera | camera |
| WLAN + Bluetooth | wireless |
| WWAN | wwan |
| Fingerprint Reader | fingerprint |
| TPM | tpm |
| Warranty | warranty |
| Machine Type | machine_type |
| Region | region |

## Ограничения

- EAN код не предоставляется API — используется формат `LEN-{model_number}`
- Изображения продуктов не импортируются (API не предоставляет URL)
- Максимум ~100 моделей за один запрос
- Требуется интернет-соединение для каждого запроса

## Технические детали

- Используется официальный API PSREF: `/api/search/DefinitionFilterAndSearch/ShowModel`
- Отправляются браузерные заголовки (User-Agent, Origin, Referer) для авторизации
- Ответ парсится как JSON с UTF-8 BOM обработкой
- Данные импортируются через `UpsertFromProduct()` — существующие EAN-страницы обновляются