package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type PromoPlanRepo struct {
	store *Store
}

func NewPromoPlanRepo(store *Store) *PromoPlanRepo {
	return &PromoPlanRepo{store: store}
}

func (r *PromoPlanRepo) Create(p *model.PromoPlan) error {
	id, err := r.store.NextID("promo_plan")
	if err != nil {
		return fmt.Errorf("next_id promo_plan: %w", err)
	}
	p.ID = id
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()

	data := MarshalPromoPlan(*p)
	if err := r.store.DocPut(KeyPromoPlan(p.ID), data); err != nil {
		return fmt.Errorf("save promo_plan: %w", err)
	}
	return nil
}

func (r *PromoPlanRepo) Get(id int64) (*model.PromoPlan, error) {
	data, err := r.store.DocGet(KeyPromoPlan(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("promo_plan %d not found", id)
		}
		return nil, fmt.Errorf("get promo_plan %d: %w", id, err)
	}
	return UnmarshalPromoPlan(data)
}

// ListAll returns all promo plans. Note: without a turbo index for promo plans,
// this is a placeholder. In production, maintain a promo_plan_list turbo index.
func (r *PromoPlanRepo) ListAll() ([]model.PromoPlan, error) {
	return nil, nil
}

func (r *PromoPlanRepo) Update(id int64, updater func(*model.PromoPlan)) error {
	p, err := r.Get(id)
	if err != nil {
		return err
	}
	updater(p)
	p.UpdatedAt = time.Now()
	data := MarshalPromoPlan(*p)
	return r.store.DocPut(KeyPromoPlan(p.ID), data)
}

func (r *PromoPlanRepo) Delete(id int64) error {
	_, err := r.Get(id)
	if err != nil {
		return err
	}
	return r.store.DocDelete(KeyPromoPlan(id))
}
