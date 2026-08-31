# Система комментариев и лайков/дизлайков — Дизайн

## Цель

Пользователи могут:
1. **Комментировать** любые страницы (товары, категории, EAN-страницы)
2. **Лайкать/дизлайкать** отзывы и комментарии
3. **Администраторы** модерировать комментарии

## 1. Модель данных

### 1.1 Comment

```go
type Comment struct {
    ID         int64        `json:"id"`
    TargetType string       `json:"target_type"` // "product", "category", "eanpage"
    TargetID   int64        `json:"target_id"`
    UserID     int64        `json:"user_id"`
    ParentID   int64        `json:"parent_id"`   // для вложенных ответов
    Content    string       `json:"content"`
    Status     CommentStatus `json:"status"`     // pending, approved, rejected, hidden
    LikeCount  int          `json:"like_count"`
    DislikeCount int        `json:"dislike_count"`
    IsFeatured bool         `json:"is_featured"`
    CreatedAt  int64        `json:"created_at"`
    UpdatedAt  int64        `json:"updated_at"`
}

type CommentStatus string

const (
    CommentStatusPending  CommentStatus = "pending"
    CommentStatusApproved CommentStatus = "approved"
    CommentStatusRejected CommentStatus = "rejected"
    CommentStatusHidden   CommentStatus = "hidden"
)
```

### 1.2 Vote (лайк/дизлайк)

```go
type VoteType string

const (
    VoteLike    VoteType = "like"
    VoteDislike VoteType = "dislike"
)

type Vote struct {
    ID         int64      `json:"id"`
    TargetType string     `json:"target_type"` // "comment" или "review"
    TargetID   int64      `json:"target_id"`
    UserID     int64      `json:"user_id"`
    VoteType   VoteType   `json:"vote_type"`
    CreatedAt  int64      `json:"created_at"`
}
```

### 1.3 UserVote (для фронтенда — что голосовал ли пользователь)

```go
type UserVote struct {
    TargetType string   `json:"target_type"`
    TargetID   int64    `json:"target_id"`
    VoteType   VoteType `json:"vote_type"` // "like", "dislike" или "" если не голосовал
}
```

## 2. API

### 2.1 Комментарии

| Метод | Endpoint | Описание |
|-------|----------|----------|
| POST | `/comments` | Создать комментарий |
| GET | `/comments?target_type=product&target_id=123` | Список комментариев |
| PATCH | `/comments/{id}` | Обновить (админ) |
| DELETE | `/comments/{id}` | Удалить (админ/автор) |
| POST | `/admin/comments/bulk-actions` | Массовые действия |
| GET | `/admin/comments/stats` | Статистика |
| POST | `/admin/comments/recalculate` | Пересчёт лайков |

### 2.2 Голосование

| Метод | Endpoint | Описание |
|-------|----------|----------|
| POST | `/votes` | Голосовать (like/dislike) |
| GET | `/votes/check?target_type=comment&target_id=123` | Проверить голос пользователя |

### 2.3 Админ

| Метод | Endpoint | Описание |
|-------|----------|----------|
| GET | `/admin/comments` | Список комментариев |
| GET | `/admin/comments/{id}` | Детали |
| PATCH | `/admin/comments/{id}` | Обновить статус |
| DELETE | `/admin/comments/{id}` | Удалить |
| POST | `/admin/comments/bulk-actions` | Массовые действия |
| GET | `/admin/comments/stats` | Статистика |

## 3. Фронтенд

### 3.1 Компонент комментариев

```vue
<CommentSection
  :target-type="'product'"
  :target-id="123"
/>
```

Компонент включает:
- Форму добавления комментария
- Список комментариев с пагинацией
- Лайки/дизлайки для каждого комментария
- Ответы на комментарии (вложенные)
- Сортировка (популярные, новые, старые)

### 3.2 Админ-панель

Новая страница `/admin/comments` с:
- Таблицей комментариев
- Фильтрами по типу цели, статусу
- Действиями: одобрить, отклонить, удалить
- Статистикой

## 4. Индексация

Turbo-индексы:
- `comment_target:{target_type}:{target_id}` — комментарии по цели
- `comment_user:{user_id}` — комментарии пользователя
- `comment_status:{status}` — комментарии по статусу
- `vote_target:{target_type}:{target_id}` — голоса по цели
- `vote_user:{user_id}` — голоса пользователя

## 5. Приоритеты

### Phase 1 (MVP)
- [x] Модель Comment и Vote
- [x] API для комментариев и голосов
- [x] Фронтенд компонент CommentSection
- [x] Админ-панель комментариев

### Phase 2
- [ ] Вложенные комментарии (ответы)
- [ ] Уведомления о новых комментариях
- [ ] Analytics
