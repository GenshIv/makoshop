package db

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
)

const turboKeyAllOrders = "order_list"

type OrderRepo struct {
	store *Store
}

func NewOrderRepo(store *Store) *OrderRepo {
	return &OrderRepo{store: store}
}

// Create creates a new order.
func (r *OrderRepo) Create(o *model.Order) error {
	id, err := r.store.NextID("order")
	if err != nil {
		return fmt.Errorf("next_id order: %w", err)
	}
	o.ID = id
	o.CreatedAt = time.Now()
	o.UpdatedAt = time.Now()
	if o.Status == "" {
		o.Status = model.OrderStatusNew
	}
	if o.PaymentStatus == "" {
		o.PaymentStatus = model.PaymentStatusPending
	}
	if o.Currency == "" {
		o.Currency = "RUB"
	}

	data := MarshalOrder(*o)
	if err := r.store.DocPut(KeyOrder(o.ID), data); err != nil {
		return fmt.Errorf("save order: %w", err)
	}

	// Index: order by user (turbo)
	userKey := "order_user:" + strconv.FormatInt(o.UserID, 10)
	if _, err := r.store.db.TurboPutIndex(userKey, uint64(o.ID)); err != nil {
		_ = r.store.DocDelete(KeyOrder(o.ID))
		return fmt.Errorf("index order by user: %w", err)
	}

	// Index: order in global list (turbo)
	if _, err := r.store.db.TurboPutIndex(turboKeyAllOrders, uint64(o.ID)); err != nil {
		fmt.Printf("WARN: indexAllOrders error for order %d: %v\n", o.ID, err)
	}

	return nil
}

// Get returns an order by ID.
func (r *OrderRepo) Get(id int64) (*model.Order, error) {
	data, err := r.store.DocGet(KeyOrder(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("order %d not found", id)
		}
		return nil, fmt.Errorf("get order %d: %w", id, err)
	}
	return UnmarshalOrder(data)
}

// GetUserOrders returns all orders for a user.
func (r *OrderRepo) GetUserOrders(userID int64) ([]model.Order, error) {
	key := "order_user:" + strconv.FormatInt(userID, 10)
	data, err := r.store.db.TurboRawRead(key)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var orders []model.Order
	for _, id := range ids {
		o, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		orders = append(orders, *o)
	}

	// Sort by created_at desc (newest first)
	for i := len(orders)/2 - 1; i >= 0; i-- {
		j := len(orders) - 1 - i
		if orders[i].CreatedAt.Before(orders[j].CreatedAt) {
			orders[i], orders[j] = orders[j], orders[i]
		}
	}

	return orders, nil
}

// Update updates an order.
func (r *OrderRepo) Update(id int64, updater func(*model.Order)) error {
	o, err := r.Get(id)
	if err != nil {
		return err
	}

	updater(o)
	o.UpdatedAt = time.Now()

	data := MarshalOrder(*o)
	if err := r.store.DocPut(KeyOrder(o.ID), data); err != nil {
		return fmt.Errorf("update order: %w", err)
	}

	return nil
}

// UpdateStatus updates order status.
func (r *OrderRepo) UpdateStatus(id int64, status model.OrderStatus) error {
	return r.Update(id, func(o *model.Order) {
		o.Status = status
	})
}

// Delete removes an order.
func (r *OrderRepo) Delete(id int64) error {
	o, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove from user index (turbo)
	userKey := "order_user:" + strconv.FormatInt(o.UserID, 10)
	_, _ = r.store.db.TurboDeleteIndex(userKey, uint64(id))
	// Remove from global index (turbo)
	_, _ = r.store.db.TurboDeleteIndex(turboKeyAllOrders, uint64(id))

	if err := r.store.DocDelete(KeyOrder(id)); err != nil {
		return fmt.Errorf("delete order: %w", err)
	}
	return nil
}

// GetOrdersByCompanyID returns orders that contain items from the given company.
// Uses turbo index instead of scan.
func (r *OrderRepo) GetOrdersByCompanyID(companyID int64) ([]model.Order, error) {
	all, err := r.GetAllOrders()
	if err != nil {
		return nil, err
	}
	if all == nil {
		return nil, nil
	}

	var result []model.Order
	for _, o := range all {
		for _, item := range o.Items {
			if item.CompanyID == companyID {
				result = append(result, o)
				break
			}
		}
	}

	// Sort by created_at desc
	for i := len(result)/2 - 1; i >= 0; i-- {
		j := len(result) - 1 - i
		if result[i].CreatedAt.Before(result[j].CreatedAt) {
			result[i], result[j] = result[j], result[i]
		}
	}

	return result, nil
}

// GetAllOrders returns all orders (for analytics). Uses turbo index.
func (r *OrderRepo) GetAllOrders() ([]model.Order, error) {
	data, err := r.store.db.TurboRawRead(turboKeyAllOrders)
	if err != nil || len(data) == 0 {
		return nil, nil
	}

	ids := makodb.TurboUnsafeReadTokens(data)
	var result []model.Order
	for _, id := range ids {
		o, err := r.Get(int64(id))
		if err != nil {
			continue
		}
		result = append(result, *o)
	}

	return result, nil
}
