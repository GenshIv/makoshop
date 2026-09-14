package db

import (
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

const turboKeySearchAliasList = "search_alias_list"

// SearchAliasRepo manages search aliases — landing pages that redirect to
// pre-filtered search results.
type SearchAliasRepo struct {
	store *Store
}

func NewSearchAliasRepo(store *Store) *SearchAliasRepo {
	return &SearchAliasRepo{store: store}
}

// Create validates and stores a new search alias.
func (r *SearchAliasRepo) Create(a *model.SearchAlias) error {
	if a.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	if a.Title == "" {
		return fmt.Errorf("title is required")
	}

	id, err := r.store.NextID("search_alias")
	if err != nil {
		return fmt.Errorf("next_id search_alias: %w", err)
	}
	a.ID = id
	a.CreatedAt = time.Now().Unix()
	a.UpdatedAt = a.CreatedAt

	if err := r.store.DocPut(KeySearchAlias(id), MarshalSearchAlias(*a)); err != nil {
		return fmt.Errorf("save search alias: %w", err)
	}
	if _, err := r.store.db.TurboPutIndexString(turboKeySearchAliasList, KeySearchAlias(id)); err != nil {
		_ = r.store.DocDelete(KeySearchAlias(id))
		return fmt.Errorf("turbo index search_alias_list: %w", err)
	}
	return nil
}

// Get returns a search alias by ID.
func (r *SearchAliasRepo) Get(id int64) (*model.SearchAlias, error) {
	data, err := r.store.DocGet(KeySearchAlias(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("search alias %d not found", id)
		}
		return nil, fmt.Errorf("get search alias %d: %w", id, err)
	}
	return UnmarshalSearchAlias(data)
}

// GetBySlug returns a search alias by slug.
func (r *SearchAliasRepo) GetBySlug(slug string) (*model.SearchAlias, error) {
	aliases, err := r.ListAll()
	if err != nil {
		return nil, err
	}
	for _, a := range aliases {
		if a.Slug == slug {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("search alias with slug %q not found", slug)
}

// ListAll returns all search aliases.
func (r *SearchAliasRepo) ListAll() ([]model.SearchAlias, error) {
	tokens, err := r.store.db.TurboGetIndexTokens(turboKeySearchAliasList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get search aliases: %w", err)
	}
	aliases := make([]model.SearchAlias, 0, len(docs))
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		a, err := UnmarshalSearchAlias(doc)
		if err != nil {
			return nil, fmt.Errorf("unmarshal search alias: %w", err)
		}
		aliases = append(aliases, *a)
	}
	return aliases, nil
}

// Update modifies an existing search alias.
func (r *SearchAliasRepo) Update(id int64, updater func(*model.SearchAlias)) error {
	a, err := r.Get(id)
	if err != nil {
		return err
	}
	updater(a)
	a.UpdatedAt = time.Now().Unix()

	if err := r.store.DocPut(KeySearchAlias(a.ID), MarshalSearchAlias(*a)); err != nil {
		return fmt.Errorf("update search alias: %w", err)
	}
	return nil
}

// Delete removes a search alias by ID.
func (r *SearchAliasRepo) Delete(id int64) error {
	if err := r.store.DocDelete(KeySearchAlias(id)); err != nil {
		return fmt.Errorf("delete search alias: %w", err)
	}
	_, _ = r.store.db.TurboDeleteIndexString(turboKeySearchAliasList, KeySearchAlias(id))
	return nil
}
