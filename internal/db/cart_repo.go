package db

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

type CartRepo struct {
	store *Store
}

func NewCartRepo(store *Store) *CartRepo {
	return &CartRepo{store: store}
}

func generateCartID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create creates a new cart.
func (r *CartRepo) Create(userID *int64, sessionID string) (*model.Cart, error) {
	id := generateCartID()

	cart := &model.Cart{
		ID:        id,
		UserID:    userID,
		SessionID: sessionID,
		Items:     []model.CartItem{},
		Currency:  "RUB",
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}

	if err := r.save(cart); err != nil {
		return nil, fmt.Errorf("save cart: %w", err)
	}

	// Index: cart by user
	if userID != nil {
		if err := r.indexCartByUser(*userID, id); err != nil {
			_ = r.Delete(id)
			return nil, fmt.Errorf("index cart by user: %w", err)
		}
	}

	// Index: cart by session (for guest carts)
	if sessionID != "" {
		if err := r.indexCartBySession(sessionID, id); err != nil {
			_ = r.Delete(id)
			return nil, fmt.Errorf("index cart by session: %w", err)
		}
	}

	return cart, nil
}

// CreateForUser creates a new cart for an authenticated user.
func (r *CartRepo) CreateForUser(userID int64) (*model.Cart, error) {
	return r.Create(&userID, "")
}

// GetBySessionID returns the cart associated with a session ID (guest cart).
func (r *CartRepo) GetBySessionID(sessionID string) (*model.Cart, error) {
	key := fmt.Sprintf("cart:session:%s", sessionID)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("no cart found for session %s", sessionID)
		}
		return nil, fmt.Errorf("get cart by session: %w", err)
	}

	cartID := string(data)
	return r.Get(cartID)
}

// Get returns a cart by ID.
func (r *CartRepo) Get(id string) (*model.Cart, error) {
	data, err := r.store.DocGet(KeyCart(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("cart %s not found", id)
		}
		return nil, fmt.Errorf("get cart %s: %w", id, err)
	}
	return UnmarshalCart(data)
}

// GetUserCart returns the active cart for a user.
func (r *CartRepo) GetUserCart(userID int64) (*model.Cart, error) {
	key := fmt.Sprintf("cart:user:%d", userID)
	data, err := r.store.DocGet(key)
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("no cart found for user %d", userID)
		}
		return nil, fmt.Errorf("get user cart: %w", err)
	}

	cartID := string(data)
	return r.Get(cartID)
}

// AddItem adds a product to the cart or updates quantity if already present.
func (r *CartRepo) AddItem(cartID string, productID int64, productName string, qty int, price float64) (*model.Cart, error) {
	cart, err := r.Get(cartID)
	if err != nil {
		return nil, err
	}

	if qty <= 0 {
		return nil, fmt.Errorf("qty must be positive")
	}

	// Check if item already exists
	for i, item := range cart.Items {
		if item.ProductID == productID {
			cart.Items[i].Qty += qty
			cart.Items[i].Price = price // update price to latest
			cart.UpdatedAt = time.Now().Unix()
			cart.TotalAmount = r.calcTotal(cart)
			if err := r.save(cart); err != nil {
				return nil, fmt.Errorf("save cart after add item: %w", err)
			}
			return cart, nil
		}
	}

	// Add new item
	cart.Items = append(cart.Items, model.CartItem{
		ProductID:   productID,
		ProductName: productName,
		Qty:         qty,
		Price:       price,
	})
	cart.UpdatedAt = time.Now().Unix()
	cart.TotalAmount = r.calcTotal(cart)

	if err := r.save(cart); err != nil {
		return nil, fmt.Errorf("save cart after add item: %w", err)
	}

	return cart, nil
}

// UpdateItem updates quantity of an item in the cart. If qty=0, removes the item.
func (r *CartRepo) UpdateItem(cartID string, productID int64, qty int) (*model.Cart, error) {
	cart, err := r.Get(cartID)
	if err != nil {
		return nil, err
	}

	found := false
	var newItems []model.CartItem
	for _, item := range cart.Items {
		if item.ProductID == productID {
			found = true
			if qty <= 0 {
				// Remove item
				continue
			}
			item.Qty = qty
			newItems = append(newItems, item)
		} else {
			newItems = append(newItems, item)
		}
	}

	if !found {
		return nil, fmt.Errorf("product %d not found in cart", productID)
	}

	cart.Items = newItems
	cart.UpdatedAt = time.Now().Unix()
	cart.TotalAmount = r.calcTotal(cart)

	if err := r.save(cart); err != nil {
		return nil, fmt.Errorf("save cart after update item: %w", err)
	}

	return cart, nil
}

// Delete removes a cart.
func (r *CartRepo) Delete(id string) error {
	cart, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove user index
	if cart.UserID != nil {
		_ = r.store.DocDelete(fmt.Sprintf("cart:user:%d", *cart.UserID))
	}

	// Remove session index
	if cart.SessionID != "" {
		_ = r.store.DocDelete(fmt.Sprintf("cart:session:%s", cart.SessionID))
	}

	if err := r.store.DocDelete(KeyCart(id)); err != nil {
		return fmt.Errorf("delete cart: %w", err)
	}
	return nil
}

// AssignToUser assigns a guest cart to a user (used during login).
func (r *CartRepo) AssignToUser(cartID string, userID int64) (*model.Cart, error) {
	cart, err := r.Get(cartID)
	if err != nil {
		return nil, err
	}

	// If user already has a cart, merge items
	existing, err := r.GetUserCart(userID)
	if err == nil && existing != nil {
		// Merge cart items into existing
		for _, item := range cart.Items {
			existing, err = r.AddItem(existing.ID, item.ProductID, item.ProductName, item.Qty, item.Price)
			if err != nil {
				return nil, fmt.Errorf("merge cart items: %w", err)
			}
		}
		// Delete guest cart
		_ = r.Delete(cartID)
		return existing, nil
	}

	// No existing cart for user, just assign
	cart.UserID = &userID
	cart.SessionID = ""
	cart.UpdatedAt = time.Now().Unix()

	if err := r.save(cart); err != nil {
		return nil, fmt.Errorf("save cart: %w", err)
	}

	if err := r.indexCartByUser(userID, cartID); err != nil {
		return nil, fmt.Errorf("index cart by user: %w", err)
	}

	return cart, nil
}

func (r *CartRepo) save(cart *model.Cart) error {
	data := MarshalCart(*cart)
	return r.store.DocPut(KeyCart(cart.ID), data)
}

func (r *CartRepo) indexCartByUser(userID int64, cartID string) error {
	key := fmt.Sprintf("cart:user:%d", userID)
	return r.store.DocPut(key, []byte(cartID))
}

func (r *CartRepo) indexCartBySession(sessionID string, cartID string) error {
	key := fmt.Sprintf("cart:session:%s", sessionID)
	return r.store.DocPut(key, []byte(cartID))
}

func (r *CartRepo) calcTotal(cart *model.Cart) float64 {
	var total float64
	for _, item := range cart.Items {
		total += item.Price * float64(item.Qty)
	}
	return total
}
