package db

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

const turboKeyAllPayments = "payment_list:"

type PaymentRepo struct {
	store *Store
}

func NewPaymentRepo(store *Store) *PaymentRepo {
	return &PaymentRepo{store: store}
}

// Create creates a new payment.
func (r *PaymentRepo) Create(p *model.Payment) error {
	id, err := r.store.NextID("payment")
	if err != nil {
		return fmt.Errorf("next_id payment: %w", err)
	}
	p.ID = id
	p.CreatedAt = time.Now().Unix()
	if p.Status == "" {
		p.Status = model.PaymentStatusPending
	}

	data := MarshalPayment(*p)
	if err := r.store.DocPut(KeyPayment(p.ID), data); err != nil {
		return fmt.Errorf("save payment: %w", err)
	}

	// Index: payment by order (turbo)
	orderKey := "payment_order:" + strconv.FormatInt(p.OrderID, 10)
	if _, err := r.store.db.TurboPutIndexString(orderKey, KeyPayment(p.ID)); err != nil {
		_ = r.store.DocDelete(KeyPayment(p.ID))
		return fmt.Errorf("index payment by order: %w", err)
	}

	// Index: payment in global list (turbo)
	if _, err := r.store.db.TurboPutIndexString(turboKeyAllPayments, KeyPayment(p.ID)); err != nil {
		fmt.Printf("WARN: indexAllPayments error for payment %d: %v\n", p.ID, err)
	}

	return nil
}

// Get returns a payment by ID.
func (r *PaymentRepo) Get(id int64) (*model.Payment, error) {
	data, err := r.store.DocGet(KeyPayment(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("payment %d not found", id)
		}
		return nil, fmt.Errorf("get payment %d: %w", id, err)
	}
	return UnmarshalPayment(data)
}

// GetByOrderID returns the payment for an order.
func (r *PaymentRepo) GetByOrderID(orderID int64) (*model.Payment, error) {
	key := "payment_order:" + strconv.FormatInt(orderID, 10)
	tokens, err := r.store.db.TurboGetIndexTokens(key)
	if err != nil || len(tokens) == 0 {
		return nil, fmt.Errorf("no payment found for order %d", orderID)
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get payment docs: %w", err)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no payment found for order %d", orderID)
	}
	return UnmarshalPayment(docs[0])
}

// Update updates a payment.
func (r *PaymentRepo) Update(id int64, updater func(*model.Payment)) error {
	p, err := r.Get(id)
	if err != nil {
		return err
	}

	updater(p)

	data := MarshalPayment(*p)
	if err := r.store.DocPut(KeyPayment(p.ID), data); err != nil {
		return fmt.Errorf("update payment: %w", err)
	}

	return nil
}

// UpdateStatus updates payment status.
func (r *PaymentRepo) UpdateStatus(id int64, status model.PaymentStatus) error {
	return r.Update(id, func(p *model.Payment) {
		p.Status = status
	})
}

// Delete removes a payment.
func (r *PaymentRepo) Delete(id int64) error {
	p, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove from order index (turbo)
	orderKey := "payment_order:" + strconv.FormatInt(p.OrderID, 10)
	_, _ = r.store.db.TurboDeleteIndexString(orderKey, KeyPayment(id))
	// Remove from global index (turbo)
	_, _ = r.store.db.TurboDeleteIndexString(turboKeyAllPayments, KeyPayment(id))
	if err := r.store.DocDelete(KeyPayment(id)); err != nil {
		return fmt.Errorf("delete payment: %w", err)
	}
	return nil
}

// TimeoutCleanupResult holds the result of a timeout cleanup operation.
type TimeoutCleanupResult struct {
	CheckedPayments  int      `json:"checked_payments"`
	TimedOutPayments int      `json:"timed_out_payments"`
	CancelledOrders  int      `json:"cancelled_orders"`
	Details          []string `json:"details,omitempty"`
}

// CleanupTimedOutPayments scans pending payments older than maxPendingMinutes
// and cancels their associated orders.
func (r *PaymentRepo) CleanupTimedOutPayments(maxPendingMinutes int) (*TimeoutCleanupResult, error) {
	if maxPendingMinutes <= 0 {
		maxPendingMinutes = 30
	}

	cutoff := time.Now().Unix() - int64(maxPendingMinutes)*60

	result := &TimeoutCleanupResult{}

	// Get all payment IDs from turbo index
	tokens, err := r.store.db.TurboGetIndexTokens(turboKeyAllPayments)
	if err != nil || len(tokens) == 0 {
		return result, nil
	}

	docs, err := r.store.db.MultiGetByDocIDs(tokens)
	if err != nil {
		return result, fmt.Errorf("multi get payments: %w", err)
	}

	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		p, err := UnmarshalPayment(doc)
		if err != nil {
			continue
		}

		result.CheckedPayments++

		// Only process pending payments older than cutoff
		if p.Status != model.PaymentStatusPending {
			continue
		}
		if p.CreatedAt > cutoff {
			continue
		}

		// This payment has timed out
		result.TimedOutPayments++

		// Update payment status to failed
		if err := r.UpdateStatus(p.ID, model.PaymentStatusFailed); err != nil {
			result.Details = append(result.Details, fmt.Sprintf("payment %d: failed to update status: %v", p.ID, err))
			continue
		}

		// Cancel the associated order
		order, err := r.GetOrderByID(p.OrderID)
		if err != nil {
			result.Details = append(result.Details, fmt.Sprintf("payment %d (order %d): order not found", p.ID, p.OrderID))
			continue
		}

		// Only cancel if order is in 'new' status
		if order.Status == model.OrderStatusNew {
			if err := r.UpdateOrderStatus(p.OrderID, model.OrderStatusCancelled); err != nil {
				result.Details = append(result.Details, fmt.Sprintf("order %d: failed to cancel: %v", p.OrderID, err))
			} else {
				result.CancelledOrders++
				result.Details = append(result.Details, fmt.Sprintf("order %d cancelled (payment %d timed out after >%d min)", p.OrderID, p.ID, maxPendingMinutes))
			}
		} else {
			result.Details = append(result.Details, fmt.Sprintf("order %d: not cancelled (status=%s)", p.OrderID, order.Status))
		}
	}

	return result, nil
}

// GetOrderByID is a helper to get an order directly (used by cleanup).
func (r *PaymentRepo) GetOrderByID(id int64) (*model.Order, error) {
	data, err := r.store.DocGet(KeyOrder(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("order %d not found", id)
		}
		return nil, fmt.Errorf("get order %d: %w", id, err)
	}
	return UnmarshalOrder(data)
}

// UpdateOrderStatus is a helper to update order status (used by cleanup).
func (r *PaymentRepo) UpdateOrderStatus(id int64, status model.OrderStatus) error {
	order, err := r.GetOrderByID(id)
	if err != nil {
		return err
	}

	order.Status = status
	order.UpdatedAt = time.Now().Unix()

	data := MarshalOrder(*order)
	if err := r.store.DocPut(KeyOrder(order.ID), data); err != nil {
		return fmt.Errorf("update order status: %w", err)
	}

	return nil
}
