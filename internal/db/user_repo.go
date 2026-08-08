package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
	"golang.org/x/crypto/bcrypt"
)

const turboKeyAllUsers = "user_list"

type UserRepo struct {
	store *Store
}

func NewUserRepo(store *Store) *UserRepo {
	return &UserRepo{store: store}
}

func (r *UserRepo) Create(u *model.User, plainPassword string) error {
	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	u.PasswordHash = string(hash)

	// Check email uniqueness
	emailKey := AuthKeyEmail(strings.ToLower(u.Email))
	existing, err := r.store.DocGet(emailKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("check email uniqueness: %w", err)
	}
	if existing != nil && len(existing) > 0 {
		return fmt.Errorf("email already registered")
	}

	// Generate ID
	id, err := r.store.NextID("user")
	if err != nil {
		return fmt.Errorf("generate user id: %w", err)
	}
	u.ID = id
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	if u.Status == "" {
		u.Status = model.UserStatusActive
	}

	// Store user document
	key := KeyUser(id)
	data := MarshalUser(*u)
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("store user: %w", err)
	}

	// Store email index
	emailData := []byte(fmt.Sprintf("%d", id))
	if err := r.store.DocPut(emailKey, emailData); err != nil {
		// Rollback user document
		_ = r.store.DocDelete(key)
		return fmt.Errorf("store email index: %w", err)
	}

	// Index user ID in global list
	if err := r.indexUserID(id); err != nil {
		_ = r.store.DocDelete(key)
		_ = r.store.DocDelete(emailKey)
		return fmt.Errorf("index user id: %w", err)
	}

	return nil
}

func (r *UserRepo) GetByID(id int64) (*model.User, error) {
	key := KeyUser(id)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("user %d not found", id)
		}
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	u, err := UnmarshalUser(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal user %d: %w", id, err)
	}
	return u, nil
}

func (r *UserRepo) GetByEmail(email string) (*model.User, error) {
	emailKey := AuthKeyEmail(strings.ToLower(email))
	data, err := r.store.DocGet(emailKey)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("user with email %s not found", email)
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.GetByID(id)
}

func (r *UserRepo) Update(id int64, updateFn func(*model.User)) error {
	u, err := r.GetByID(id)
	if err != nil {
		return err
	}

	updateFn(u)
	u.UpdatedAt = time.Now()

	key := KeyUser(id)
	data := MarshalUser(*u)
	if err := r.store.DocPut(key, data); err != nil {
		return fmt.Errorf("update user %d: %w", id, err)
	}

	return nil
}

func (r *UserRepo) VerifyPassword(u *model.User, plainPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(plainPassword))
	return err == nil
}

// ListUsersParams holds parameters for user listing (admin).
type ListUsersParams struct {
	Page   int
	Limit  int
	Role   string
	Status string
	Q      string // search by email/name
}

// List returns a paginated list of users (for admin).
func (r *UserRepo) List(params ListUsersParams) ([]model.User, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 1 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	// Collect all users from turbo index.
	allUsers, err := r.GetAllUsers()
	if err != nil {
		return nil, 0, fmt.Errorf("get all users: %w", err)
	}
	if allUsers == nil {
		allUsers = []model.User{}
	}

	// Apply filters
	var filtered []model.User
	for _, u := range allUsers {
		if params.Role != "" && u.Role != model.UserRole(params.Role) {
			continue
		}
		if params.Status != "" && u.Status != model.UserStatus(params.Status) {
			continue
		}
		if params.Q != "" {
			q := strings.ToLower(params.Q)
			match := strings.Contains(strings.ToLower(u.Email), q) ||
				strings.Contains(strings.ToLower(u.Profile.Name), q)
			if !match {
				continue
			}
		}
		filtered = append(filtered, u)
	}

	total := int64(len(filtered))

	// Pagination
	start := (params.Page - 1) * params.Limit
	end := start + params.Limit
	if start > len(filtered) {
		return []model.User{}, total, nil
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], total, nil
}

// GetAllUsers returns all users (for analytics). Uses turbo index.
func (r *UserRepo) GetAllUsers() ([]model.User, error) {
	data, err := r.store.db.TurboRawRead(turboKeyAllUsers)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.User
	for _, id := range ids {
		u, err := r.GetByID(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *u)
	}

	return result, nil
}

// indexUserID adds a user ID to the turbo user list.
func (r *UserRepo) indexUserID(id int64) error {
	_, err := r.store.db.TurboPutIndex(turboKeyAllUsers, uint64(id))
	return err
}

// IndexMultiworld is a helper wrapper around makodb's Index function for multi-word indexing.
func IndexMultiworld(store *Store, key string, text string) error {
	if text == "" {
		return nil
	}
	// Simple tokenization: split by non-alphanumeric characters.
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})

	for _, word := range words {
		if len(word) < 2 {
			continue
		}
		word = strings.ToLower(word)
		// Use makodb's Index to create inverted index entries.
		// We'll wrap it via store.db directly.
		_ = store.db.Index(IndexKeySearch(word), key)
	}
	return nil
}
