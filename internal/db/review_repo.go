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

	data := MarshalReview(*review)
	if err := r.store.DocPut(KeyReview(review.ID), data); err != nil {
		return fmt.Errorf("save review: %w", err)
	}

	// Index: review by product (turbo)
	productKey := fmt.Sprintf("review_product:%d", review.ProductID)
	if _, err := r.store.db.TurboPutIndex(productKey, KeyReviewKey128(review.ID)); err != nil {
		_ = r.store.DocDelete(KeyReview(review.ID))
		return fmt.Errorf("turbo index review by product: %w", err)
	}

	// Index: review by user (turbo)
	userKey := fmt.Sprintf("review_user:%d", review.UserID)
	if _, err := r.store.db.TurboPutIndex(userKey, KeyReviewKey128(review.ID)); err != nil {
		_, _ = r.store.db.TurboDeleteIndex(productKey, KeyReviewKey128(review.ID))
		_ = r.store.DocDelete(KeyReview(review.ID))
		return fmt.Errorf("turbo index review by user: %w", err)
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
func (r *ReviewRepo) ListByProduct(productID int64, page, limit int) ([]model.Review, int64, error) {
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

	docs, err := r.store.db.MultiGetByDocIDsWithPrefix(tokens, "review:")
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

	docs, err := r.store.db.MultiGetByDocIDsWithPrefix(tokens, "review:")
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

// GetReviewByProductAndUser checks if a user has reviewed a product.
// Returns the review ID if exists, 0 otherwise.
func (r *ReviewRepo) getReviewByProductAndUser(productID, userID int64) (int64, error) {
	// Get all reviews for this product and check user
	key := fmt.Sprintf("review_product:%d", productID)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return 0, nil
	}

	docs, err := r.store.db.MultiGetByDocIDsWithPrefix(tokens, "review:")
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
	_, _ = r.store.db.TurboDeleteIndex(productKey, KeyReviewKey128(id))
	_, _ = r.store.db.TurboDeleteIndex(userKey, KeyReviewKey128(id))

	if err := r.store.DocDelete(KeyReview(id)); err != nil {
		return fmt.Errorf("delete review: %w", err)
	}
	return nil
}
