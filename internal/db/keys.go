package db

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unsafe"

	intHache "github.com/GenshIv/intHache"
	"github.com/GenshIv/makodb/v2"
	"github.com/GenshIv/makoshop/internal/model"
	"github.com/GenshIv/silentjson/v2"
)

var catReg = silentjson.BuildRegistry(reflect.TypeOf(model.Category{}))

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

// Key128 helpers for makodb/v2 compatibility

// DocIDKey128 converts a document key string (e.g., "category:123") to makodb.Key128.
func DocIDKey128(key string) makodb.Key128 {
	return makodb.HashKey(key)
}

// Int64ToDocIDKey128 converts an entity type prefix and int64 ID to makodb.Key128.
// For example: Int64ToDocIDKey128("category", 123) -> hash of "category:123"
func Int64ToDocIDKey128(prefix string, id int64) makodb.Key128 {
	return DocIDKey128(fmt.Sprintf("%s:%d", prefix, id))
}

// KeyCategoryKey128 converts a category ID to Key128 for turbo indexes.
func KeyCategoryKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("category", id)
}

// KeyUserKey128 converts a user ID to Key128 for turbo indexes.
func KeyUserKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("user", id)
}

// KeyProductKey128 converts a product ID to Key128 for turbo indexes.
func KeyProductKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("product", id)
}

// KeyCompanyKey128 converts a company ID to Key128 for turbo indexes.
func KeyCompanyKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("company", id)
}

// KeyBrandKey128 converts a brand ID to Key128 for turbo indexes.
func KeyBrandKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("brand", id)
}

// KeyOrderKey128 converts an order ID to Key128 for turbo indexes.
func KeyOrderKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("order", id)
}

// KeyPaymentKey128 converts a payment ID to Key128 for turbo indexes.
func KeyPaymentKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("payment", id)
}

// KeyReviewKey128 converts a review ID to Key128 for turbo indexes.
func KeyReviewKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("review", id)
}

// KeyPromoCampaignKey128 converts a promo campaign ID to Key128 for turbo indexes.
func KeyPromoCampaignKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("promo_campaign", id)
}

// KeyPromoPlanKey128 converts a promo plan ID to Key128 for turbo indexes.
func KeyPromoPlanKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("promo_plan", id)
}

// KeyPromoLogKey128 converts a promo log ID to Key128 for turbo indexes.
func KeyPromoLogKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("promo_log", id)
}

// KeyAttrDefKey128 converts an attribute definition ID to Key128 for turbo indexes.
func KeyAttrDefKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("attrdef", id)
}

// KeyCartKey128 converts a cart ID string to Key128 for turbo indexes.
func KeyCartKey128(id string) makodb.Key128 {
	return DocIDKey128(KeyCart(id))
}

// KeyLandingKey128 converts a landing page ID to Key128 for turbo indexes.
func KeyLandingKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("landing", id)
}

// KeySCUPageKey128 converts an SCU page ID to Key128 for turbo indexes.
func KeySCUPageKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("scupage", id)
}

// Uint64ToKey128 converts a uint64 value to Key128 for turbo indexes.
// This is useful for storing hash values as docIDs in turbo indexes.
func Uint64ToKey128(v uint64) makodb.Key128 {
	return makodb.Key128{v, 0}
}

// CompareKey128 compares two Key128 values.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// This is a copy of makodb.bytesCompareKey128 since it's not exported.
func CompareKey128(a, b makodb.Key128) int {
	if a[0] < b[0] {
		return -1
	}
	if a[0] > b[0] {
		return 1
	}
	if a[1] < b[1] {
		return -1
	}
	if a[1] > b[1] {
		return 1
	}
	return 0
}

// KeyDeliveryTimeKey128 converts a delivery time ID to Key128 for turbo indexes.
func KeyDeliveryTimeKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("delivery_time", id)
}

// KeyInstallmentPlanKey128 converts an installment plan ID to Key128 for turbo indexes.
func KeyInstallmentPlanKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("installment_plan", id)
}

// KeyPaymentMethodKey128 converts a payment method ID to Key128 for turbo indexes.
func KeyPaymentMethodKey128(id int64) makodb.Key128 {
	return Int64ToDocIDKey128("payment_method", id)
}

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
	if err := silentjson.ParseObject(data, catReg, unsafe.Pointer(&c)); err != nil {
		return nil, err
	}
	// Fix corrupted unicode escapes in stored data (u0026 -> &)
	c.NameRu = fixUnicodeEscapes(c.NameRu)
	c.NameUa = fixUnicodeEscapes(c.NameUa)
	c.NamePl = fixUnicodeEscapes(c.NamePl)
	c.NameEn = fixUnicodeEscapes(c.NameEn)
	c.Desc = fixUnicodeEscapes(c.Desc)
	c.DescRu = fixUnicodeEscapes(c.DescRu)
	c.DescUa = fixUnicodeEscapes(c.DescUa)
	c.DescPl = fixUnicodeEscapes(c.DescPl)
	c.DescEn = fixUnicodeEscapes(c.DescEn)
	return &c, nil
}

func fixUnicodeEscapes(s string) string {
	return strings.ReplaceAll(s, "u0026", "&")
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


