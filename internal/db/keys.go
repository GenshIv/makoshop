package db

import (
	"encoding/json"
	"fmt"

	intHache "github.com/GenshIv/intHache"
	"github.com/GenshIv/makoshop/internal/model"
)

// Key builders

func KeyUser(id int64) string          { return fmt.Sprintf("user:%d", id) }
func KeyCompany(id int64) string       { return fmt.Sprintf("company:%d", id) }
func KeyCategory(id int64) string      { return fmt.Sprintf("category:%d", id) }
func KeyBrand(id int64) string         { return fmt.Sprintf("brand:%d", id) }
func KeyAttrDef(id int64) string       { return fmt.Sprintf("attrdef:%d", id) }
func KeyProduct(id int64) string       { return fmt.Sprintf("product:%d", id) }
func KeyCart(id string) string         { return fmt.Sprintf("cart:%s", id) }
func KeyOrder(id int64) string         { return fmt.Sprintf("order:%d", id) }
func KeyPayment(id int64) string       { return fmt.Sprintf("payment:%d", id) }
func KeyReview(id int64) string        { return fmt.Sprintf("review:%d", id) }
func KeyPromoPlan(id int64) string     { return fmt.Sprintf("promo_plan:%d", id) }
func KeyPromoCampaign(id int64) string { return fmt.Sprintf("promo_campaign:%d", id) }
func KeyPromoLog(id int64) string      { return fmt.Sprintf("promo_log:%d", id) }
func KeyLandingPage(id int64) string   { return fmt.Sprintf("landing:%d", id) }
func KeySCUPage(id int64) string       { return fmt.Sprintf("scupage:%d", id) }

// Index keys — all turbo-based. Helpers for hashing used by turbo_search.go.

// attrValueHash returns a stable hash for an attribute value string.
func attrValueHash(value string) int64 {
	return intHache.Sum([]byte(value))
}

// attrCodeHash returns a stable hash for an attribute code.
func attrCodeHash(code string) int64 {
	return intHache.Sum([]byte(code))
}

// brandHash returns a stable hash for a brand string.
func brandHash(brand string) int64 {
	return intHache.Sum([]byte(brand))
}

// Auth keys

func AuthKeyEmail(email string) string {
	return fmt.Sprintf("auth:user:email:%s", email)
}

func AuthKeySession(tokenHash string) string {
	return fmt.Sprintf("auth:session:%s", tokenHash)
}

// Cart keys

func CartKeyByUser(userID int64) string {
	return fmt.Sprintf("cart:user:%d", userID)
}

// Order keys

func OrderKeyByUser(userID int64) string {
	return fmt.Sprintf("order:user:%d", userID)
}

// Payment keys

func PaymentKeyByOrder(orderID int64) string {
	return fmt.Sprintf("payment:order:%d", orderID)
}

// Serialization helpers (using standard json for stability)

func MarshalUser(u model.User) []byte {
	// Use a custom struct to include PasswordHash in serialization
	type UserWithHash struct {
		ID           int64             `json:"id"`
		Email        string            `json:"email"`
		PasswordHash string            `json:"password_hash"`
		Role         string            `json:"role"`
		Status       string            `json:"status"`
		Profile      model.UserProfile `json:"profile,omitempty"`
		CreatedAt    int64             `json:"created_at"`
		UpdatedAt    int64             `json:"updated_at"`
	}
	b, _ := json.Marshal(UserWithHash{
		ID:           u.ID,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Role:         string(u.Role),
		Status:       string(u.Status),
		Profile:      u.Profile,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	})
	return b
}

func UnmarshalUser(data []byte) (*model.User, error) {
	type UserWithHash struct {
		ID           int64             `json:"id"`
		Email        string            `json:"email"`
		PasswordHash string            `json:"password_hash"`
		Role         string            `json:"role"`
		Status       string            `json:"status"`
		Profile      model.UserProfile `json:"profile,omitempty"`
		CreatedAt    int64             `json:"created_at"`
		UpdatedAt    int64             `json:"updated_at"`
	}
	var uwh UserWithHash
	if err := json.Unmarshal(data, &uwh); err != nil {
		return nil, err
	}
	return &model.User{
		ID:           uwh.ID,
		Email:        uwh.Email,
		PasswordHash: uwh.PasswordHash,
		Role:         model.UserRole(uwh.Role),
		Status:       model.UserStatus(uwh.Status),
		Profile:      uwh.Profile,
		CreatedAt:    uwh.CreatedAt,
		UpdatedAt:    uwh.UpdatedAt,
	}, nil
}

func MarshalCompany(c model.Company) []byte {
	b, _ := json.Marshal(c)
	return b
}

func UnmarshalCompany(data []byte) (*model.Company, error) {
	var c model.Company
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func MarshalCategory(c model.Category) []byte {
	b, _ := json.Marshal(c)
	return b
}

func UnmarshalCategory(data []byte) (*model.Category, error) {
	var c model.Category
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func MarshalAttrDef(a model.AttributeDefinition) []byte {
	b, _ := json.Marshal(a)
	return b
}

func UnmarshalAttrDef(data []byte) (*model.AttributeDefinition, error) {
	var a model.AttributeDefinition
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func MarshalProduct(p model.Product) []byte {
	b, _ := json.Marshal(p)
	return b
}

func UnmarshalProduct(data []byte) (*model.Product, error) {
	var p model.Product
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func MarshalCart(c model.Cart) []byte {
	b, _ := json.Marshal(c)
	return b
}

func UnmarshalCart(data []byte) (*model.Cart, error) {
	var c model.Cart
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func MarshalOrder(o model.Order) []byte {
	b, _ := json.Marshal(o)
	return b
}

func UnmarshalOrder(data []byte) (*model.Order, error) {
	var o model.Order
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func MarshalPayment(p model.Payment) []byte {
	b, _ := json.Marshal(p)
	return b
}

func UnmarshalPayment(data []byte) (*model.Payment, error) {
	var p model.Payment
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func MarshalReview(r model.Review) []byte {
	b, _ := json.Marshal(r)
	return b
}

func UnmarshalReview(data []byte) (*model.Review, error) {
	var r model.Review
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func MarshalPromoPlan(p model.PromoPlan) []byte {
	b, _ := json.Marshal(p)
	return b
}

func UnmarshalPromoPlan(data []byte) (*model.PromoPlan, error) {
	var p model.PromoPlan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func MarshalPromoCampaign(p model.PromoCampaign) []byte {
	b, _ := json.Marshal(p)
	return b
}

func UnmarshalPromoCampaign(data []byte) (*model.PromoCampaign, error) {
	var p model.PromoCampaign
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func MarshalPromoLog(p model.PromoLog) []byte {
	b, _ := json.Marshal(p)
	return b
}

func UnmarshalPromoLog(data []byte) (*model.PromoLog, error) {
	var p model.PromoLog
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func MarshalBrand(b model.Brand) []byte {
	data, _ := json.Marshal(b)
	return data
}

func UnmarshalBrand(data []byte) (*model.Brand, error) {
	var b model.Brand
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func MarshalLandingPage(l model.LandingPage) []byte {
	b, _ := json.Marshal(l)
	return b
}

func UnmarshalLandingPage(data []byte) (*model.LandingPage, error) {
	var l model.LandingPage
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func MarshalSCUPage(s model.SCUPage) []byte {
	b, _ := json.Marshal(s)
	return b
}

func UnmarshalSCUPage(data []byte) (*model.SCUPage, error) {
	var s model.SCUPage
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
