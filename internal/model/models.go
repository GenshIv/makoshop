package model

// KeyValue is a generic key-value pair for attributes, constraints, contexts, etc.
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

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
	IsFirstLogin bool        `json:"is_first_login"` // for superadmin initial setup
	CreatedAt    int64       `json:"created_at"`
	UpdatedAt    int64       `json:"updated_at"`
}

// AttrDef — attribute definition linked to categories.
// Code is unique (from normalized data, e.g. "diagonal-ekrana").
type AttrDef struct {
	ID           int64    `json:"id"`
	Code         string   `json:"code"`
	NameRu       string   `json:"name_ru,omitempty"`
	NameUa       string   `json:"name_ua,omitempty"`
	NamePl       string   `json:"name_pl,omitempty"`
	NameEn       string   `json:"name_en,omitempty"`
	Categories   []int64  `json:"categories"`
	Type         AttrType `json:"type"`
	IsActive     bool     `json:"is_active"`
	IsFilterable bool     `json:"is_filterable"`
	IsSortable   bool     `json:"is_sortable"`
	SortOrder    int      `json:"sort_order"`
	RangeParams  []string `json:"range_params,omitempty"`
	Unit         string   `json:"unit,omitempty"`
	CreatedAt    int64    `json:"created_at"`
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

type CompanyContacts struct {
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Website  string `json:"website,omitempty"`
	Telegram string `json:"telegram,omitempty"`
}

type CompanySettings struct {
	Currency            string `json:"currency"`
	VatEnabled          bool   `json:"vat_enabled"`
	DefaultPaymentTerms string `json:"default_payment_terms,omitempty"`
}

type Company struct {
	ID                 int64            `json:"id"`
	Name               string           `json:"name"`
	Slug               string           `json:"slug"`                  // URL-friendly name
	Description        string           `json:"description,omitempty"` // company description
	LogoURL            string           `json:"logo_url,omitempty"`    // company logo
	LegalInfo          CompanyLegalInfo `json:"legal_info,omitempty"`
	Contacts           CompanyContacts  `json:"contacts,omitempty"`
	Settings           CompanySettings  `json:"settings"`
	Status             CompanyStatus    `json:"status"`
	OwnerUserID        int64            `json:"owner_user_id"`
	PaymentMethodIds   []int64          `json:"payment_method_ids,omitempty"`
	DeliveryTimeIds    []int64          `json:"delivery_time_ids,omitempty"`
	InstallmentPlanIds []int64          `json:"installment_plan_ids,omitempty"`
	CreatedAt          int64            `json:"created_at"`
	UpdatedAt          int64            `json:"updated_at"`
}

// Brand

type Brand struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Category

type Category struct {
	ID             int64    `json:"id"`
	ParentID       *int64   `json:"parent_id,omitempty"`
	NameRu         string   `json:"name_ru"`
	NameUa         string   `json:"name_ua"`
	NamePl         string   `json:"name_pl"`
	NameEn         string   `json:"name_en"`
	Slug           string   `json:"slug"`
	Desc           string   `json:"description,omitempty"` // legacy, kept for compat
	DescRu         string   `json:"description_ru,omitempty"`
	DescUa         string   `json:"description_ua,omitempty"`
	DescPl         string   `json:"description_pl,omitempty"`
	DescEn         string   `json:"description_en,omitempty"`
	ImageLightURL  string   `json:"image_light_url,omitempty"` // light theme image
	ImageDarkURL   string   `json:"image_dark_url,omitempty"`  // dark theme image
	IsActive       bool     `json:"is_active"`
	SortOrder      int      `json:"sort_order"`
	AnchorKeywords []string `json:"anchor_keywords,omitempty"` // keywords for auto-catalogization
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
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
	ID           int64    `json:"id"`
	CategoryID   int64    `json:"category_id"`
	NameRu       string   `json:"name_ru"`
	NameUa       string   `json:"name_ua,omitempty"`
	NamePl       string   `json:"name_pl,omitempty"`
	NameEn       string   `json:"name_en,omitempty"`
	Code         string   `json:"code"`
	Type         AttrType `json:"type"`
	Options      []string `json:"options,omitempty"`
	IsRequired   bool     `json:"is_required"`
	IsFilterable bool     `json:"is_filterable"`
	IsSortable   bool     `json:"is_sortable"`
	IsSearchable bool     `json:"is_searchable"`
	SortOrder    int      `json:"sort_order"`
	CreatedAt    int64    `json:"created_at"`
	UpdatedAt    int64    `json:"updated_at"`
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
	ID          int64         `json:"id"`
	SKU         string        `json:"sku"`
	SCU         string        `json:"scu,omitempty"` // Standard Catalog Unit — links to landing page
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	CategoryID  int64         `json:"category_id"`
	BrandID     int64         `json:"brand_id,omitempty"`
	CompanyID   int64         `json:"company_id"`
	Brand       string        `json:"brand,omitempty"`
	Price       float64       `json:"price"`
	Currency    string        `json:"currency"`
	StockQty    int64         `json:"stock_qty"`
	Status      ProductStatus `json:"status"`
	Attributes  []KeyValue    `json:"attributes,omitempty"`
	Images      []string      `json:"images,omitempty"`
	SEO         ProductSEO    `json:"seo,omitempty"`
	CreatedAt   int64         `json:"created_at"`
	UpdatedAt   int64         `json:"updated_at"`
}

// LandingPage — посадочная страница для группы товаров с одинаковым SCU.
// Все товары с этим SCU редиректятся на эту страницу.

type LandingPage struct {
	ID          int64    `json:"id"`
	SCU         string   `json:"scu"`         // unique identifier
	Slug        string   `json:"slug"`        // URL-friendly path
	Title       string   `json:"title"`       // page title
	Description string   `json:"description"` // meta description
	Content     string   `json:"content"`     // HTML/markdown content
	Images      []string `json:"images"`      // page images
	IsActive    bool     `json:"is_active"`
	ProductIDs  []int64  `json:"product_ids"` // cached list of product IDs with this SCU
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

// SCUPage — SEO-страница для группы товаров с одинаковым SCU.
// Основная сущность каталога: каталог и поиск работают по SCUPage, не по товарам.
// Путь: /shop/{category_tree}/{slug}

type SCUPage struct {
	ID           int64      `json:"id"`
	SCU          string     `json:"scu"`         // unique identifier from supplier
	Slug         string     `json:"slug"`        // URL-friendly slug (unique)
	Title        string     `json:"title"`       // SEO title (from first product name)
	Description  string     `json:"description"` // SEO meta description
	Content      string     `json:"content"`     // full HTML content
	Images       []string   `json:"images"`      // unique product images (limited to maxSCUPageImages)
	CategoryID   int64      `json:"category_id"` // main category
	Brand        string     `json:"brand"`       // brand name
	BrandID      int64      `json:"brand_id"`    // brand ID
	IsActive     bool       `json:"is_active"`
	MinPrice     float64    `json:"min_price"`            // minimum price among all products
	Currency     string     `json:"currency"`             // currency (default: RUB)
	Attributes   []KeyValue `json:"attributes,omitempty"` // merged attributes (no duplicates)
	ProductCount int        `json:"product_count"`        // number of products with this SCU
	CreatedAt    int64      `json:"created_at"`
	UpdatedAt    int64      `json:"updated_at"`
}

// NOTE: ProductIDs removed from SCUPage to prevent DB bloat.
// Product→SCU link is stored in Product.SCU field.
// SCU→Products query via turbo index "scu:{scu}" in TurboProductSearch.

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
	CreatedAt   int64      `json:"created_at"`
	UpdatedAt   int64      `json:"updated_at"`
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
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
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
	CreatedAt         int64         `json:"created_at"`
}

// Review

type Review struct {
	ID        int64  `json:"id"`
	ProductID int64  `json:"product_id"`
	UserID    int64  `json:"user_id"`
	Rating    int    `json:"rating"`
	Comment   string `json:"comment,omitempty"`
	CreatedAt int64  `json:"created_at"`
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
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	Type         PromoPlanType `json:"type"`
	DurationDays int           `json:"duration_days"`
	Price        float64       `json:"price"`
	Currency     string        `json:"currency"`
	Description  string        `json:"description,omitempty"`
	Constraints  []KeyValue    `json:"constraints,omitempty"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
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
	CategoryIDs      []int64    `json:"category_ids,omitempty"`
	AttributeFilters []KeyValue `json:"attribute_filters,omitempty"`
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
	StartAt        int64               `json:"start_at"`
	EndAt          int64               `json:"end_at"`
	CreatedAt      int64               `json:"created_at"`
	UpdatedAt      int64               `json:"updated_at"`
}

type PromoEventType string

const (
	PromoEventImpression PromoEventType = "impression"
	PromoEventClick      PromoEventType = "click"
	PromoEventConversion PromoEventType = "conversion"
)

type PromoLog struct {
	ID         int64          `json:"id"`
	CampaignID int64          `json:"campaign_id"`
	EventType  PromoEventType `json:"event_type"`
	Context    []KeyValue     `json:"context,omitempty"`
	Cost       float64        `json:"cost"`
	CreatedAt  int64          `json:"created_at"`
}

// --- Company settings: payment methods, delivery times, installment plans ---

type CompanyPaymentMethod struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type DeliveryTime struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type InstallmentPlan struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsActive  bool   `json:"is_active"`
	SortOrder int    `json:"sort_order"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// CompanySettingsV2 holds company-specific options (payment, delivery, installment).
// Embedded into Company via separate fields to avoid breaking changes.
type CompanySettingsV2 struct {
	PaymentMethodIds   []int64 `json:"payment_method_ids,omitempty"`
	DeliveryTimeIds    []int64 `json:"delivery_time_ids,omitempty"`
	InstallmentPlanIds []int64 `json:"installment_plan_ids,omitempty"`
}
