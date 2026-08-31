package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type CommentRepo struct {
	store *Store
}

func NewCommentRepo(store *Store) *CommentRepo {
	return &CommentRepo{store: store}
}

// Create creates a new comment.
func (r *CommentRepo) Create(comment *model.Comment) error {
	id, err := r.store.NextID("comment")
	if err != nil {
		return fmt.Errorf("next_id comment: %w", err)
	}
	comment.ID = id
	comment.CreatedAt = time.Now().Unix()
	comment.UpdatedAt = comment.CreatedAt
	if comment.Status == "" {
		comment.Status = model.CommentStatusApproved
	}
	if comment.LikeCount == 0 {
		comment.LikeCount = 0
	}
	if comment.DislikeCount == 0 {
		comment.DislikeCount = 0
	}

	data := MarshalComment(*comment)
	if err := r.store.DocPut(KeyComment(comment.ID), data); err != nil {
		return fmt.Errorf("save comment: %w", err)
	}

	// Index: comment by target
	targetKey := fmt.Sprintf("comment_target:%s:%d", comment.TargetType, comment.TargetID)
	if _, err := r.store.db.TurboPutIndexString(targetKey, KeyComment(comment.ID)); err != nil {
		_ = r.store.DocDelete(KeyComment(comment.ID))
		return fmt.Errorf("turbo index comment by target: %w", err)
	}

	// Index: comment by user
	userKey := fmt.Sprintf("comment_user:%d", comment.UserID)
	if _, err := r.store.db.TurboPutIndexString(userKey, KeyComment(comment.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndexString(targetKey, KeyComment(comment.ID))
		_ = r.store.DocDelete(KeyComment(comment.ID))
		return fmt.Errorf("turbo index comment by user: %w", err)
	}

	// Index: comment by status
	statusKey := fmt.Sprintf("comment_status:%s", comment.Status)
	if _, err := r.store.db.TurboPutIndexString(statusKey, KeyComment(comment.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndexString(targetKey, KeyComment(comment.ID))
		_, _ = r.store.db.TurboDeleteIndexString(userKey, KeyComment(comment.ID))
		_ = r.store.DocDelete(KeyComment(comment.ID))
		return fmt.Errorf("turbo index comment by status: %w", err)
	}

	return nil
}

// Get returns a comment by ID.
func (r *CommentRepo) Get(id int64) (*model.Comment, error) {
	data, err := r.store.DocGet(KeyComment(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("comment %d not found", id)
		}
		return nil, fmt.Errorf("get comment %d: %w", id, err)
	}
	return UnmarshalComment(data)
}

// ListByTarget returns comments for a target with pagination.
func (r *CommentRepo) ListByTarget(targetType string, targetID int64, page, limit int, statusFilter string) ([]model.Comment, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	key := fmt.Sprintf("comment_target:%s:%d", targetType, targetID)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil, 0, nil
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, 0, fmt.Errorf("multi get comments: %w", err)
	}

	var comments []model.Comment
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		comment, err := UnmarshalComment(doc)
		if err != nil {
			continue
		}
		if statusFilter != "" && string(comment.Status) != statusFilter {
			continue
		}
		comments = append(comments, *comment)
	}

	// Sort by created_at desc (newest first)
	for i := len(comments)/2 - 1; i >= 0; i-- {
		j := len(comments) - 1 - i
		if comments[i].CreatedAt < comments[j].CreatedAt {
			comments[i], comments[j] = comments[j], comments[i]
		}
	}

	total := int64(len(comments))

	// Pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(comments) {
		return nil, total, nil
	}
	if end > len(comments) {
		end = len(comments)
	}

	return comments[start:end], total, nil
}

// ListAll returns all comments with pagination and optional filters.
func (r *CommentRepo) ListAll(page, limit int, statusFilter, targetTypeFilter string) ([]model.Comment, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Collect all comment IDs from status index
	var allIDs []int64

	if statusFilter != "" {
		key := fmt.Sprintf("comment_status:%s", statusFilter)
		tokens, err := r.store.db.TurboGetIndexTokens(key)
		if err == nil && len(tokens) > 0 {
			for _, token := range tokens {
				s, ok := token.(string)
				if !ok {
					continue
				}
				var id int64
				fmt.Sscanf(s, "comment:%d", &id)
				allIDs = append(allIDs, id)
			}
		}
	} else {
		for _, status := range []string{
			string(model.CommentStatusApproved),
			string(model.CommentStatusPending),
			string(model.CommentStatusRejected),
			string(model.CommentStatusHidden),
		} {
			key := fmt.Sprintf("comment_status:%s", status)
			tokens, err := r.store.db.TurboGetIndexTokens(key)
			if err == nil && len(tokens) > 0 {
				for _, token := range tokens {
					s, ok := token.(string)
					if !ok {
						continue
					}
					var id int64
					fmt.Sscanf(s, "comment:%d", &id)
					allIDs = append(allIDs, id)
				}
			}
		}
	}

	// Deduplicate
	seen := make(map[int64]bool)
	var uniqueIDs []any
	for _, id := range allIDs {
		if !seen[id] {
			seen[id] = true
			uniqueIDs = append(uniqueIDs, id)
		}
	}

	// Get comment documents
	docs, err := r.store.db.MultiGetByDocIDs(uniqueIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("multi get comments: %w", err)
	}

	var comments []model.Comment
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		comment, err := UnmarshalComment(doc)
		if err != nil {
			continue
		}
		if targetTypeFilter != "" && string(comment.TargetType) != targetTypeFilter {
			continue
		}
		comments = append(comments, *comment)
	}

	// Sort by created_at desc
	for i := len(comments)/2 - 1; i >= 0; i-- {
		j := len(comments) - 1 - i
		if comments[i].CreatedAt < comments[j].CreatedAt {
			comments[i], comments[j] = comments[j], comments[i]
		}
	}

	total := int64(len(comments))

	// Pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(comments) {
		return nil, total, nil
	}
	if end > len(comments) {
		end = len(comments)
	}

	return comments[start:end], total, nil
}

// Delete removes a comment.
func (r *CommentRepo) Delete(id int64) error {
	comment, err := r.Get(id)
	if err != nil {
		return err
	}

	targetKey := fmt.Sprintf("comment_target:%s:%d", comment.TargetType, comment.TargetID)
	userKey := fmt.Sprintf("comment_user:%d", comment.UserID)
	statusKey := fmt.Sprintf("comment_status:%s", comment.Status)
	_, _ = r.store.db.TurboDeleteIndexString(targetKey, KeyComment(id))
	_, _ = r.store.db.TurboDeleteIndexString(userKey, KeyComment(id))
	_, _ = r.store.db.TurboDeleteIndexString(statusKey, KeyComment(id))

	if err := r.store.DocDelete(KeyComment(id)); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

// UpdateStatus updates a comment's status.
func (r *CommentRepo) UpdateStatus(id int64, newStatus model.CommentStatus) error {
	comment, err := r.Get(id)
	if err != nil {
		return err
	}

	oldStatus := comment.Status
	comment.Status = newStatus
	comment.UpdatedAt = time.Now().Unix()

	oldStatusKey := fmt.Sprintf("comment_status:%s", oldStatus)
	newStatusKey := fmt.Sprintf("comment_status:%s", newStatus)
	_, _ = r.store.db.TurboDeleteIndexString(oldStatusKey, KeyComment(id))
	if _, err := r.store.db.TurboPutIndexString(newStatusKey, KeyComment(id)); err != nil {
		return fmt.Errorf("turbo index update status: %w", err)
	}

	data := MarshalComment(*comment)
	if err := r.store.DocPut(KeyComment(id), data); err != nil {
		return fmt.Errorf("update comment: %w", err)
	}

	return nil
}

// UpdateFeatured updates the is_featured flag on a comment.
func (r *CommentRepo) UpdateFeatured(id int64, featured bool) error {
	comment, err := r.Get(id)
	if err != nil {
		return err
	}

	comment.IsFeatured = featured
	comment.UpdatedAt = time.Now().Unix()

	data := MarshalComment(*comment)
	if err := r.store.DocPut(KeyComment(id), data); err != nil {
		return fmt.Errorf("update comment featured: %w", err)
	}

	return nil
}

// UpdateLikeCount updates the like_count and dislike_count for a comment.
func (r *CommentRepo) UpdateLikeCount(id int64, likeCount, dislikeCount int) error {
	comment, err := r.Get(id)
	if err != nil {
		return err
	}

	comment.LikeCount = likeCount
	comment.DislikeCount = dislikeCount
	comment.UpdatedAt = time.Now().Unix()

	data := MarshalComment(*comment)
	if err := r.store.DocPut(KeyComment(id), data); err != nil {
		return fmt.Errorf("update comment like count: %w", err)
	}

	return nil
}

// Stats returns comment statistics.
type CommentStats struct {
	TotalComments int            `json:"total_comments"`
	Pending       int            `json:"pending"`
	Approved      int            `json:"approved"`
	Rejected      int            `json:"rejected"`
	Hidden        int            `json:"hidden"`
	ByTargetType  map[string]int `json:"by_target_type"`
}

func (r *CommentRepo) GetStats() (*CommentStats, error) {
	stats := &CommentStats{
		ByTargetType: make(map[string]int),
	}

	for _, status := range []model.CommentStatus{
		model.CommentStatusPending,
		model.CommentStatusApproved,
		model.CommentStatusRejected,
		model.CommentStatusHidden,
	} {
		key := fmt.Sprintf("comment_status:%s", status)
		tokens, err := r.store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			continue
		}

		switch status {
		case model.CommentStatusPending:
			stats.Pending = len(tokens)
		case model.CommentStatusApproved:
			stats.Approved = len(tokens)
		case model.CommentStatusRejected:
			stats.Rejected = len(tokens)
		case model.CommentStatusHidden:
			stats.Hidden = len(tokens)
		}
		stats.TotalComments += len(tokens)

		// Get documents for target type breakdown
		docs, err := r.store.db.MultiGetByDocIDs(tokens)
		if err != nil {
			continue
		}

		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			comment, err := UnmarshalComment(doc)
			if err != nil {
				continue
			}
			stats.ByTargetType[string(comment.TargetType)]++
		}
	}

	return stats, nil
}
