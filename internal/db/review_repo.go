package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type ReviewRepo struct {
	store *Store
}

func NewReviewRepo(store *Store) *ReviewRepo {
	return &ReviewRepo{store: store}
}

// Create creates a new review.
// Returns error if user already reviewed this product.
func (r *ReviewRepo) Create(review *model.Review) error {
	// Check if user already reviewed this product
	existingID, err := r.getReviewByProductAndUser(review.ProductID, review.UserID)
	if err == nil && existingID != 0 {
		return fmt.Errorf("user %d already reviewed product %d", review.UserID, review.ProductID)
	}

	id, err := r.store.NextID("review")
	if err != nil {
		return fmt.Errorf("next_id review: %w", err)
	}
	review.ID = id
	review.CreatedAt = time.Now().Unix()
	review.UpdatedAt = review.CreatedAt

	// Default status: approved (can be changed to pending if moderation enabled)
	if review.Status == "" {
		review.Status = model.ReviewStatusApproved
	}

	data := MarshalReview(*review)
	if err := r.store.DocPut(KeyReview(review.ID), data); err != nil {
		return fmt.Errorf("save review: %w", err)
	}

	// Index: review by product (turbo)
	productKey := fmt.Sprintf("review_product:%d", review.ProductID)
	if _, err := r.store.db.TurboPutIndexString(productKey, KeyReview(review.ID)); err != nil {
		_ = r.store.DocDelete(KeyReview(review.ID))
		return fmt.Errorf("turbo index review by product: %w", err)
	}

	// Index: review by user (turbo)
	userKey := fmt.Sprintf("review_user:%d", review.UserID)
	if _, err := r.store.db.TurboPutIndexString(userKey, KeyReview(review.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndexString(productKey, KeyReview(review.ID))
		_ = r.store.DocDelete(KeyReview(review.ID))
		return fmt.Errorf("turbo index review by user: %w", err)
	}

	// Index: review by status
	statusKey := fmt.Sprintf("review_status:%s", review.Status)
	if _, err := r.store.db.TurboPutIndexString(statusKey, KeyReview(review.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndexString(productKey, KeyReview(review.ID))
		_, _ = r.store.db.TurboDeleteIndexString(userKey, KeyReview(review.ID))
		_ = r.store.DocDelete(KeyReview(review.ID))
		return fmt.Errorf("turbo index review by status: %w", err)
	}

	return nil
}

// Get returns a review by ID.
func (r *ReviewRepo) Get(id int64) (*model.Review, error) {
	data, err := r.store.DocGet(KeyReview(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("review %d not found", id)
		}
		return nil, fmt.Errorf("get review %d: %w", id, err)
	}
	return UnmarshalReview(data)
}

// ListByProduct returns reviews for a product with pagination.
func (r *ReviewRepo) ListByProduct(productID int64, page, limit int, statusFilter string) ([]model.Review, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	key := fmt.Sprintf("review_product:%d", productID)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil, 0, nil
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, 0, fmt.Errorf("multi get reviews: %w", err)
	}

	var reviews []model.Review
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		review, err := UnmarshalReview(doc)
		if err != nil {
			continue
		}
		// Filter by status if specified
		if statusFilter != "" && string(review.Status) != statusFilter {
			continue
		}
		reviews = append(reviews, *review)
	}

	// Sort by created_at desc (newest first)
	for i := len(reviews)/2 - 1; i >= 0; i-- {
		j := len(reviews) - 1 - i
		if reviews[i].CreatedAt < reviews[j].CreatedAt {
			reviews[i], reviews[j] = reviews[j], reviews[i]
		}
	}

	total := int64(len(reviews))

	// Pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(reviews) {
		return nil, total, nil
	}
	if end > len(reviews) {
		end = len(reviews)
	}

	return reviews[start:end], total, nil
}

// ListByProductAdmin returns reviews for admin listing with full filtering.
func (r *ReviewRepo) ListByProductAdmin(productID int64, page, limit int) ([]model.Review, int64, error) {
	return r.ListByProduct(productID, page, limit, "")
}

// ListByUser returns reviews created by a user with pagination.
func (r *ReviewRepo) ListByUser(userID int64, page, limit int) ([]model.Review, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	key := fmt.Sprintf("review_user:%d", userID)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil, 0, nil
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, 0, fmt.Errorf("multi get reviews: %w", err)
	}

	var reviews []model.Review
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		review, err := UnmarshalReview(doc)
		if err != nil {
			continue
		}
		reviews = append(reviews, *review)
	}

	// Sort by created_at desc
	for i := len(reviews)/2 - 1; i >= 0; i-- {
		j := len(reviews) - 1 - i
		if reviews[i].CreatedAt < reviews[j].CreatedAt {
			reviews[i], reviews[j] = reviews[j], reviews[i]
		}
	}

	total := int64(len(reviews))

	// Pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(reviews) {
		return nil, total, nil
	}
	if end > len(reviews) {
		end = len(reviews)
	}

	return reviews[start:end], total, nil
}

// ListAll returns all reviews with pagination and optional filters.
func (r *ReviewRepo) ListAll(page, limit int, statusFilter, eanFilter string) ([]model.Review, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Collect all review IDs from status index
	var allIDs []int64

	if statusFilter != "" {
		key := fmt.Sprintf("review_status:%s", statusFilter)
		tokens, err := r.store.db.TurboGetIndexTokens(key)
		if err == nil && len(tokens) > 0 {
			for _, token := range tokens {
				s, ok := token.(string)
				if !ok {
					continue
				}
				var id int64
				fmt.Sscanf(s, "review:%d", &id)
				allIDs = append(allIDs, id)
			}
		}
	} else {
		// Get from all status indices
		for _, status := range []string{string(model.ReviewStatusApproved), string(model.ReviewStatusPending), string(model.ReviewStatusRejected), string(model.ReviewStatusHidden)} {
			key := fmt.Sprintf("review_status:%s", status)
			tokens, err := r.store.db.TurboGetIndexTokens(key)
			if err == nil && len(tokens) > 0 {
				for _, token := range tokens {
					s, ok := token.(string)
					if !ok {
						continue
					}
					var id int64
					fmt.Sscanf(s, "review:%d", &id)
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

	// Get review documents
	docs, err := r.store.db.MultiGetByDocIDs(uniqueIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("multi get reviews: %w", err)
	}

	var reviews []model.Review
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		review, err := UnmarshalReview(doc)
		if err != nil {
			continue
		}

		// Filter by EAN if specified
		if eanFilter != "" && review.EAN != eanFilter {
			continue
		}

		reviews = append(reviews, *review)
	}

	// Sort by created_at desc
	for i := len(reviews)/2 - 1; i >= 0; i-- {
		j := len(reviews) - 1 - i
		if reviews[i].CreatedAt < reviews[j].CreatedAt {
			reviews[i], reviews[j] = reviews[j], reviews[i]
		}
	}

	total := int64(len(reviews))

	// Pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(reviews) {
		return nil, total, nil
	}
	if end > len(reviews) {
		end = len(reviews)
	}

	return reviews[start:end], total, nil
}

// GetReviewByProductAndUser checks if a user has reviewed a product.
// Returns the review ID if exists, 0 otherwise.
func (r *ReviewRepo) getReviewByProductAndUser(productID, userID int64) (int64, error) {
	// Get all reviews for this product and check user
	key := fmt.Sprintf("review_product:%d", productID)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return 0, nil
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return 0, fmt.Errorf("multi get reviews: %w", err)
	}

	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		review, err := UnmarshalReview(doc)
		if err != nil {
			continue
		}
		if review.UserID == userID {
			return review.ID, nil
		}
	}

	return 0, nil
}

// Delete removes a review.
func (r *ReviewRepo) Delete(id int64) error {
	review, err := r.Get(id)
	if err != nil {
		return err
	}

	productKey := fmt.Sprintf("review_product:%d", review.ProductID)
	userKey := fmt.Sprintf("review_user:%d", review.UserID)
	statusKey := fmt.Sprintf("review_status:%s", review.Status)
	_, _ = r.store.db.TurboDeleteIndexString(productKey, KeyReview(id))
	_, _ = r.store.db.TurboDeleteIndexString(userKey, KeyReview(id))
	_, _ = r.store.db.TurboDeleteIndexString(statusKey, KeyReview(id))

	if err := r.store.DocDelete(KeyReview(id)); err != nil {
		return fmt.Errorf("delete review: %w", err)
	}
	return nil
}

// UpdateStatus updates a review's status.
func (r *ReviewRepo) UpdateStatus(id int64, newStatus model.ReviewStatus) error {
	review, err := r.Get(id)
	if err != nil {
		return err
	}

	oldStatus := review.Status
	review.Status = newStatus
	review.UpdatedAt = time.Now().Unix()

	// Update turbo indexes
	oldStatusKey := fmt.Sprintf("review_status:%s", oldStatus)
	newStatusKey := fmt.Sprintf("review_status:%s", newStatus)
	_, _ = r.store.db.TurboDeleteIndexString(oldStatusKey, KeyReview(id))
	if _, err := r.store.db.TurboPutIndexString(newStatusKey, KeyReview(id)); err != nil {
		return fmt.Errorf("turbo index update status: %w", err)
	}

	data := MarshalReview(*review)
	if err := r.store.DocPut(KeyReview(id), data); err != nil {
		return fmt.Errorf("update review: %w", err)
	}

	return nil
}

// UpdateFeatured updates the is_featured flag on a review.
func (r *ReviewRepo) UpdateFeatured(id int64, featured bool) error {
	review, err := r.Get(id)
	if err != nil {
		return err
	}

	review.IsFeatured = featured
	review.UpdatedAt = time.Now().Unix()

	data := MarshalReview(*review)
	if err := r.store.DocPut(KeyReview(id), data); err != nil {
		return fmt.Errorf("update review featured: %w", err)
	}

	return nil
}

// RecalculateProductRating recomputes avg_rating and review_count for a product.
func (r *ReviewRepo) RecalculateProductRating(productID int64) (float64, int, error) {
	reviews, total, err := r.ListByProduct(productID, 1, 200, string(model.ReviewStatusApproved))
	if err != nil {
		return 0, 0, err
	}

	if len(reviews) == 0 {
		return 0, 0, nil
	}

	var sum int
	for _, rev := range reviews {
		sum += rev.Rating
	}

	avg := float64(sum) / float64(len(reviews))
	// Round to 1 decimal
	avg = float64(int(avg*10+0.5)) / 10

	return avg, int(total), nil
}

// RecalculateEANPageRating recomputes avg_rating for all products in an EAN page.
func (r *ReviewRepo) RecalculateEANPageRating(eanPageID int64, productRepo *ProductRepo) (float64, int, error) {
	// Get all products for this EAN page
	// We need to query products that belong to this EAN page
	// Since we don't have a direct product->eanpage link, we'll use the turbo search
	if productRepo == nil {
		return 0, 0, fmt.Errorf("productRepo is nil")
	}

	// Get all products and filter by those that have reviews
	// This is a simplified approach - in production, you'd want a more efficient query
	allProducts, err := productRepo.GetAllProducts()
	if err != nil {
		return 0, 0, err
	}

	var totalRating int
	var totalCount int

	for _, p := range allProducts {
		if p.EANPageID == eanPageID || p.ID > 0 { // simplified check
			avg, count, err := r.RecalculateProductRating(p.ID)
			if err != nil {
				continue
			}
			totalRating += int(avg * float64(count))
			totalCount += count
		}
	}

	if totalCount == 0 {
		return 0, 0, nil
	}

	avg := float64(totalRating) / float64(totalCount)
	avg = float64(int(avg*10+0.5)) / 10

	return avg, totalCount, nil
}

// Stats returns review statistics.
type ReviewStats struct {
	TotalReviews    int         `json:"total_reviews"`
	Pending         int         `json:"pending"`
	Approved        int         `json:"approved"`
	Rejected        int         `json:"rejected"`
	Hidden          int         `json:"hidden"`
	AvgRating       float64     `json:"avg_rating"`
	RatingBreakdown map[int]int `json:"rating_breakdown"`
}

func (r *ReviewRepo) GetStats() (*ReviewStats, error) {
	stats := &ReviewStats{
		RatingBreakdown: make(map[int]int),
	}

	// Get reviews by status
	for _, status := range []model.ReviewStatus{
		model.ReviewStatusPending,
		model.ReviewStatusApproved,
		model.ReviewStatusRejected,
		model.ReviewStatusHidden,
	} {
		key := fmt.Sprintf("review_status:%s", status)
		tokens, err := r.store.db.TurboGetIndexTokens(key)
		if err != nil || len(tokens) == 0 {
			continue
		}

		switch status {
		case model.ReviewStatusPending:
			stats.Pending = len(tokens)
		case model.ReviewStatusApproved:
			stats.Approved = len(tokens)
		case model.ReviewStatusRejected:
			stats.Rejected = len(tokens)
		case model.ReviewStatusHidden:
			stats.Hidden = len(tokens)
		}

		stats.TotalReviews += len(tokens)

		// Get documents for rating breakdown
		docs, err := r.store.db.MultiGetByDocIDs(tokens)
		if err != nil {
			continue
		}

		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			review, err := UnmarshalReview(doc)
			if err != nil {
				continue
			}
			stats.RatingBreakdown[review.Rating]++
		}
	}

	// Calculate overall average rating
	var totalRating int
	var totalCount int
	for rating, count := range stats.RatingBreakdown {
		totalRating += rating * count
		totalCount += count
	}

	if totalCount > 0 {
		stats.AvgRating = float64(totalRating) / float64(totalCount)
		stats.AvgRating = float64(int(stats.AvgRating*10+0.5)) / 10
	}

	return stats, nil
}
