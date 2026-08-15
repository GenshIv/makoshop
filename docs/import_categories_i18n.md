# Импорт i18n-каталогов категорий в makoshop

Дата: 2026-08-13

## Стратегия (Variant B)

- **Name**: русский текст (default/fallback)
- **Translations**: все 4 языка (ru, uk, en, pl) в `Category.Translations`
- **AnchorKeywords**: русские ключевые слова для catalogizer
- **categories-export-i18n.json**: технический экспорт с placeholders (не используется напрямую)
- **translations.json**: источник переводов

## Исходные файлы

- `_tmp/i18n/categories-export-i18n.json` — категории с placeholders
- `_tmp/i18n/translations.json` — переводы для ru, uk, en, pl

## Скрипты

### Импорт

```bash
cd /home/ihar/IdeaProjects/makoshop

# Запуск
go run ./cmd/import_categories_i18n/

# Или бинарник
go build -o _tmp/import/import_categories_i18n_bin ./cmd/import_categories_i18n/
./_tmp/import/import_categories_i18n_bin
```

Действия:
1. Загружает категории и переводы
2. Валидирует наличие переводов для всех категорий
3. Делает бэкап существующих категорий (`_tmp/import/backup_categories_<timestamp>.json`)
4. Обновляет/создаёт категории (upsert по ID)
5. Перестраивает токены catalogizer

### Восстановление

```bash
./_tmp/import/restore_categories_bin -backup _tmp/import/backup_categories_<timestamp>.json
```

## Результат импорта

```
Total categories: 305
With anchor_keywords: 304
Total anchor_keywords: 8892
With full translations (ru+uk+en+pl): 305
```

## Примеры

### id=2 (Авто и мото)

```json
{
  "id": 2,
  "name": "Авто и мото",
  "slug": "avto-i-moto",
  "translations": {
    "ru": "Авто и мото",
    "uk": "Авто і мото",
    "en": "Auto and Moto",
    "pl": "Motoryzacja"
  },
  "anchor_keywords": [
    "Грузовые шины", "Диски", "Домкраты", "Мотошины",
    "Насосы и компрессоры", "Шины", "Шины и диски"
  ]
}
```

### id=12 (Батуты)

```json
{
  "id": 12,
  "parent_id": 11,
  "name": "Батуты",
  "slug": "batuty",
  "translations": {
    "ru": "Батуты",
    "uk": "Батути",
    "en": "Trampolines",
    "pl": "Trampoliny"
  },
  "anchor_keywords": [
    "батут", "happy", "safety", "10ft", "sport", "fitness",
    "hasttings", "12ft", "eclipse", "kogee", "optifit", "tramp",
    "14ft", "16ft", "jump", "berg", "elite", "moove", "like",
    "unix", "line", "star", "with", "diamond", "triumph", "nord",
    "slide", "external", "comfort", "hegen"
  ]
}
```

### id=62 (Карандаши)

```json
{
  "id": 62,
  "parent_id": 61,
  "name": "Карандаши",
  "slug": "karandashi",
  "translations": {
    "ru": "Карандаши",
    "uk": "Карандаші",
    "en": "Pencils",
    "pl": "Ołówki"
  },
  "anchor_keywords": []
}
```

## Изменения в коде

- `internal/db/category_repo.go`: добавлен публичный метод `UpdateIndexes(cat)`

## Обновление переводов в будущем

1. Перегенерировать `translations.json` (скрипт `_tmp/i18n/generate_i18n.py` или аналог)
2. Запустить импорт повторно — он обновит существующие категории по ID

## Откат

```bash
./_tmp/import/restore_categories_bin -backup _tmp/import/backup_categories_<timestamp>.json
```
