package db

import (
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type BrandRepo struct {
	store *Store
}

func NewBrandRepo(store *Store) *BrandRepo {
	return &BrandRepo{store: store}
}

func (r *BrandRepo) Create(b *model.Brand) error {
	id, err := r.store.NextID("brand")
	if err != nil {
		return fmt.Errorf("next_id brand: %w", err)
	}
	b.ID = id
	b.CreatedAt = time.Now().Unix()
	b.UpdatedAt = time.Now().Unix()

	data := MarshalBrand(*b)
	if err := r.store.DocPut(KeyBrand(b.ID), data); err != nil {
		return fmt.Errorf("save brand: %w", err)
	}
	return nil
}

func (r *BrandRepo) Get(id int64) (*model.Brand, error) {
	data, err := r.store.DocGet(KeyBrand(id))
	if err != nil {
		return nil, fmt.Errorf("get brand %d: %w", id, err)
	}
	return UnmarshalBrand(data)
}

func (r *BrandRepo) GetByName(name string) (*model.Brand, error) {
	brands, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	for _, b := range brands {
		if b.Name == name {
			return &b, nil
		}
	}
	return nil, nil
}

// ListAll returns all brands. Note: without a turbo index for brands,
// this is a placeholder. In production, maintain a brand_list turbo index.
func (r *BrandRepo) ListAll() ([]model.Brand, error) {
	return nil, nil
}

func (r *BrandRepo) Update(id int64, updater func(*model.Brand)) error {
	b, err := r.Get(id)
	if err != nil {
		return err
	}
	updater(b)
	b.UpdatedAt = time.Now().Unix()

	data := MarshalBrand(*b)
	if err := r.store.DocPut(KeyBrand(b.ID), data); err != nil {
		return fmt.Errorf("update brand: %w", err)
	}
	return nil
}

func (r *BrandRepo) Delete(id int64) error {
	if err := r.store.DocDelete(KeyBrand(id)); err != nil {
		return fmt.Errorf("delete brand: %w", err)
	}
	return nil
}
