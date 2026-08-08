package model

import "time"

// User

type UserRole string

const (
	RoleAdmin     UserRole = "admin"
	RoleSeller    UserRole = "seller"
	RoleBuyer     UserRole = "buyer"
	RoleModerator UserRole = "moderator"
)

type UserStatus string

const (
	UserStatusActive  UserStatus = "active"
	UserStatusBlocked UserStatus = "blocked"
	UserStatusPending UserStatus = "pending"
)

type UserProfile struct {
	Name        string `json:"name,omitempty"`
	Phone       string `json:"phone,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	Address     string `json:"address,omitempty"`
}

type User struct {
	ID           int64       `json:"id"`
	Email        string      `json:"email"`
	PasswordHash string      `json:"-"`
	Role         UserRole    `json:"role"`
	Status       UserStatus  `json:"status"`
	Profile      UserProfile `json:"profile,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// AttrDef — attribute definition linked to categories.
// Code is unique (from normalized data, e.g. "diagonal-ekrana").
type AttrDef struct {
	ID         int64     `json:"id"`
	Code       string    `json:"code"`       // unique attribute code
	Categories []int64   `json:"categories"` // category IDs where this attribute is used
	CreatedAt  time.Time `json:"created_at"`
}

// Company

type CompanyStatus string

const (
	CompanyStatusPending  CompanyStatus = "pending"
	CompanyStatusVerified CompanyStatus = "verified"
	CompanyStatusBlocked  CompanyStatus = "blocked"
)

type CompanyLegalInfo struct {
	INN     string `json:"inn,omitempty"`
	OGRN    string `json:"ogrn,omitempty"`
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
	Address string `json:"address,omitempty"`
}

type CompanySettings struct {
	Currency            string `json:"currency"`
	VatEnabled          bool   `json:"vat_enabled"`
	DefaultPaymentTerms string `json:"default_payment_terms,omitempty"`
}

type Company struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	LegalInfo   CompanyLegalInfo `json:"legal_info,omitempty"`
	Settings    CompanySettings  `json:"settings"`
	Status      CompanyStatus    `json:"status"`
	OwnerUserID int64            `json:"owner_user_id"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// Brand

type Brand struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Category

type Category struct {
	ID        int64     `json:"id"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Desc      string    `json:"description,omitempty"`
	IsActive  bool      `json:"is_active"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AttributeDefinition

type AttrType string

const (
	AttrTypeString    AttrType = "string"
	AttrTypeInt       AttrType = "int"
	AttrTypeFloat     AttrType = "float"
	AttrTypeBool      AttrType = "bool"
	AttrTypeEnum      AttrType = "enum"
	AttrTypeMultiEnum AttrType = "multi_enum"
	AttrTypeDate      AttrType = "date"
	AttrTypeRange     AttrType = "range"
)

type AttributeDefinition struct {
	ID           int64     `json:"id"`
	CategoryID   int64     `json:"category_id"`
	Name         string    `json:"name"`
	Code         string    `json:"code"`
	Type         AttrType  `json:"type"`
	Options      []string  `json:"options,omitempty"`
	IsRequired   bool      `json:"is_required"`
	IsFilterable bool      `json:"is_filterable"`
	IsSortable   bool      `json:"is_sortable"`
	IsSearchable bool      `json:"is_searchable"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Product

type ProductStatus string

const (
	ProductStatusDraft    ProductStatus = "draft"
	ProductStatusActive   ProductStatus = "active"
	ProductStatusHidden   ProductStatus = "hidden"
	ProductStatusArchived ProductStatus = "archived"
)

type ProductSEO struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Keywords    string `json:"keywords,omitempty"`
}

type Product struct {
	ID          int64                  `json:"id"`
	SKU         string                 `json:"sku"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	CategoryID  int64                  `json:"category_id"`
	BrandID     int64                  `json:"brand_id,omitempty"`
	CompanyID   int64                  `json:"company_id"`
	Brand       string                 `json:"brand,omitempty"`
	Price       float64                `json:"price"`
	Currency    string                 `json:"currency"`
	StockQty    int64                  `json:"stock_qty"`
	Status      ProductStatus          `json:"status"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Images      []string               `json:"images,omitempty"`
	SEO         ProductSEO             `json:"seo,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Cart

type CartItem struct {
	ProductID   int64   `json:"product_id"`
	ProductName string  `json:"product_name,omitempty"`
	Qty         int     `json:"qty"`
	Price       float64 `json:"price"`
}

type Cart struct {
	ID          string     `json:"id"`
	UserID      *int64     `json:"user_id,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	Items       []CartItem `json:"items"`
	TotalAmount float64    `json:"total_amount"`
	Currency    string     `json:"currency"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Order

type OrderStatus string

const (
	OrderStatusNew        OrderStatus = "new"
	OrderStatusConfirmed  OrderStatus = "confirmed"
	OrderStatusProcessing OrderStatus = "processing"
	OrderStatusShipped    OrderStatus = "shipped"
	OrderStatusDelivered  OrderStatus = "delivered"
	OrderStatusCancelled  OrderStatus = "cancelled"
	OrderStatusRefunded   OrderStatus = "refunded"
)

type PaymentStatus string

const (
	PaymentStatusPending  PaymentStatus = "pending"
	PaymentStatusPaid     PaymentStatus = "paid"
	PaymentStatusFailed   PaymentStatus = "failed"
	PaymentStatusRefunded PaymentStatus = "refunded"
)

type OrderItem struct {
	ProductID int64   `json:"product_id"`
	CompanyID int64   `json:"company_id"`
	Qty       int     `json:"qty"`
	Price     float64 `json:"price"`
	Total     float64 `json:"total"`
}

type ShippingInfo struct {
	Name       string `json:"name"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	Address    string `json:"address"`
	City       string `json:"city"`
	Country    string `json:"country"`
	PostalCode string `json:"postal_code,omitempty"`
}

type Order struct {
	ID            int64         `json:"id"`
	UserID        int64         `json:"user_id"`
	Status        OrderStatus   `json:"status"`
	Items         []OrderItem   `json:"items"`
	TotalAmount   float64       `json:"total_amount"`
	Currency      string        `json:"currency"`
	PaymentStatus PaymentStatus `json:"payment_status"`
	ShippingInfo  ShippingInfo  `json:"shipping_info"`
	Comment       string        `json:"comment,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// Payment

type PaymentMethod string

const (
	PaymentMethodCard         PaymentMethod = "card"
	PaymentMethodInvoice      PaymentMethod = "invoice"
	PaymentMethodBankTransfer PaymentMethod = "bank_transfer"
)

type Payment struct {
	ID                int64         `json:"id"`
	OrderID           int64         `json:"order_id"`
	Amount            float64       `json:"amount"`
	Currency          string        `json:"currency"`
	Method            PaymentMethod `json:"method"`
	Status            PaymentStatus `json:"status"`
	ExternalPaymentID string        `json:"external_payment_id,omitempty"`
	PaymentURL        string        `json:"payment_url,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
}

// Review

type Review struct {
	ID        int64     `json:"id"`
	ProductID int64     `json:"product_id"`
	UserID    int64     `json:"user_id"`
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Promotion

type PromoPlanType string

const (
	PromoPlanTypePosition    PromoPlanType = "position"
	PromoPlanTypeHighlight   PromoPlanType = "highlight"
	PromoPlanTypeBanner      PromoPlanType = "banner"
	PromoPlanTypeFilterBoost PromoPlanType = "filter_boost"
)

type PromoPlan struct {
	ID           int64                  `json:"id"`
	Name         string                 `json:"name"`
	Type         PromoPlanType          `json:"type"`
	DurationDays int                    `json:"duration_days"`
	Price        float64                `json:"price"`
	Currency     string                 `json:"currency"`
	Description  string                 `json:"description,omitempty"`
	Constraints  map[string]interface{} `json:"constraints,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type PromoCampaignStatus string

const (
	PromoCampaignStatusActive    PromoCampaignStatus = "active"
	PromoCampaignStatusPaused    PromoCampaignStatus = "paused"
	PromoCampaignStatusExpired   PromoCampaignStatus = "expired"
	PromoCampaignStatusCancelled PromoCampaignStatus = "cancelled"
	PromoCampaignStatusPending   PromoCampaignStatus = "pending"
)

type TargetFilters struct {
	CategoryIDs      []int64                `json:"category_ids,omitempty"`
	AttributeFilters map[string]interface{} `json:"attribute_filters,omitempty"`
}

type PromoCampaign struct {
	ID             int64               `json:"id"`
	CompanyID      int64               `json:"company_id"`
	PromoPlanID    int64               `json:"promotion_plan_id"`
	Status         PromoCampaignStatus `json:"status"`
	TargetFilters  TargetFilters       `json:"target_filters"`
	TargetPosition string              `json:"target_position"`
	ProductIDs     []int64             `json:"product_ids,omitempty"` // if empty, all company products matching TargetFilters are promoted
	BudgetTotal    float64             `json:"budget_total"`
	BudgetUsed     float64             `json:"budget_used"`
	StartAt        time.Time           `json:"start_at"`
	EndAt          time.Time           `json:"end_at"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

type PromoEventType string

const (
	PromoEventImpression PromoEventType = "impression"
	PromoEventClick      PromoEventType = "click"
	PromoEventConversion PromoEventType = "conversion"
)

type PromoLog struct {
	ID         int64                  `json:"id"`
	CampaignID int64                  `json:"campaign_id"`
	EventType  PromoEventType         `json:"event_type"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Cost       float64                `json:"cost"`
	CreatedAt  time.Time              `json:"created_at"`
}
