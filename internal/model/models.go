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

// AttrFieldMap maps an XML property field to a catalog attribute code.
type AttrFieldMap struct {
	Field string `json:"field"` // property name in the price file (e.g. "Material")
	Code  string `json:"code"`  // attribute code in the catalog (e.g. "material")
}

// HTMLAttrRule defines how to extract an attribute from HTML description
type HTMLAttrRule struct {
	Code      string `json:"code"`      // attribute code in the catalog (e.g. "power")
	Pattern   string `json:"pattern"`   // regex pattern to match (e.g. "Moc\\s*[-:]\\s*([0-9]+\\s*W)")
	Group     int    `json:"group"`     // capture group to extract (default 1)
	Transform string `json:"transform"` // optional: "trim", "lowercase", "uppercase", "clean_html"
}

// PriceSourceConfig describes how to parse a specific company's price file.
// Different companies provide attributes differently, so this is configurable.
type PriceSourceConfig struct {
	Format             string            `json:"format,omitempty"`               // "nokaut" (default)
	Currency           string            `json:"currency,omitempty"`             // e.g. "PLN" (default from company settings)
	EANField           string            `json:"ean_field,omitempty"`            // property name for EAN (default "EAN")
	PreviousPriceField string            `json:"previous_price_field,omitempty"` // property name (default "PreviousPrice")
	ImageField         string            `json:"image_field,omitempty"`          // property name (default "ImageOriginalUrl", fallback <image>)
	ProductURLField    string            `json:"product_url_field,omitempty"`    // property name (default "ProductUrl", fallback <url>)
	BrandField         string            `json:"brand_field,omitempty"`          // property name (default "Producent", fallback <producer>)
	ShopCategoryField  string            `json:"shop_category_field,omitempty"`  // property name (default "ShopProductCategory")
	AvailabilityMap    map[string]string `json:"availability_map,omitempty"`     // raw value -> "in_stock"|"out_of_stock"
	AttrFields         []AttrFieldMap    `json:"attr_fields,omitempty"`          // extra attributes to extract
	HTMLAttrRules      []HTMLAttrRule    `json:"html_attr_rules,omitempty"`      // rules to extract attributes from HTML description
}

type Company struct {
	ID                 int64            `json:"id"`
	Name               string           `json:"name"`
	Slug               string           `json:"slug"`                  // URL-friendly name
	Description        string           `json:"description,omitempty"` // company description
	LogoURL            string           `json:"logo_url,omitempty"`    // company logo
	WebsiteURL         string           `json:"website_url,omitempty"` // company's external website
	LegalInfo          CompanyLegalInfo `json:"legal_info,omitempty"`
	Contacts           CompanyContacts  `json:"contacts,omitempty"`
	Settings           CompanySettings  `json:"settings"`
	Status             CompanyStatus    `json:"status"`
	OwnerUserID        int64            `json:"owner_user_id"`
	PaymentMethodIds   []int64          `json:"payment_method_ids,omitempty"`
	DeliveryTimeIds    []int64          `json:"delivery_time_ids,omitempty"`
	InstallmentPlanIds []int64          `json:"installment_plan_ids,omitempty"`

	// --- Price import (tasks 1, 3, 7) ---
	ImportFolder string            `json:"import_folder,omitempty"` // folder name in prices/ dir
	PriceSource  PriceSourceConfig `json:"price_source,omitempty"`  // parsing config

	// --- Company landing page (task 4) ---
	NameRu    string `json:"name_ru,omitempty"`
	NameUa    string `json:"name_ua,omitempty"`
	NamePl    string `json:"name_pl,omitempty"`
	NameEn    string `json:"name_en,omitempty"`
	DescRu    string `json:"desc_ru,omitempty"`
	DescUa    string `json:"desc_ua,omitempty"`
	DescPl    string `json:"desc_pl,omitempty"`
	DescEn    string `json:"desc_en,omitempty"`
	HeroImage string `json:"hero_image,omitempty"`
	IsVisible bool   `json:"is_visible"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
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
	CreatedAt      int64    `json:"created_at,omitempty"`
	UpdatedAt      int64    `json:"updated_at,omitempty"`
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
	ID            int64         `json:"id"`
	SKU           string        `json:"sku"`
	EAN           string        `json:"ean,omitempty"` // European barcode — links to landing page
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	CategoryID    int64         `json:"category_id"`
	BrandID       int64         `json:"brand_id,omitempty"`
	CompanyID     int64         `json:"company_id"`
	Brand         string        `json:"brand,omitempty"`
	Price         float64       `json:"price"`
	PreviousPrice float64       `json:"previous_price,omitempty"` // old price; if > Price → discount
	Currency      string        `json:"currency"`
	StockQty      int64         `json:"stock_qty"`
	Status        ProductStatus `json:"status"`
	Attributes    []KeyValue    `json:"attributes,omitempty"`
	Images        []string      `json:"images,omitempty"`
	SEO           ProductSEO    `json:"seo,omitempty"`
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
}

// LandingPage — посадочная страница для группы товаров с одинаковым EAN.
// Все товары с этим EAN редиректятся на эту страницу.

type LandingPage struct {
	ID          int64    `json:"id"`
	EAN         string   `json:"ean"`         // unique identifier
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

// EANPage — SEO-страница для группы товаров с одинаковым EAN.
// Основная сущность каталога: каталог и поиск работают по EANPage, не по товарам.
// Путь: /shop/{category_tree}/{slug}

type EANPage struct {
	ID           int64      `json:"id"`
	EAN          string     `json:"ean"`                // European barcode (or normalized name if no EAN)
	Slug         string     `json:"slug"`               // URL-friendly slug (unique)
	Title        string     `json:"title"`              // SEO title (from first product name)
	Description  string     `json:"description"`        // SEO meta description
	Content      string     `json:"content,omitempty"`  // full HTML content (omitted in list responses)
	Images       []string   `json:"images"`             // unique product images (limited to maxEANPageImages)
	CategoryID   int64      `json:"category_id"`        // main category
	Brand        string     `json:"brand"`              // brand name
	BrandID      int64      `json:"brand_id,omitempty"` // brand ID
	IsActive     bool       `json:"is_active"`
	MinPrice     float64    `json:"min_price"`            // minimum price among all products
	Currency     string     `json:"currency"`             // currency (default: RUB)
	Attributes   []KeyValue `json:"attributes,omitempty"` // merged attributes (no duplicates)
	ProductCount int        `json:"product_count"`        // number of products with this EAN
	CreatedAt    int64      `json:"created_at,omitempty"`
	UpdatedAt    int64      `json:"updated_at,omitempty"`
	SeoURL       string     `json:"seo_url,omitempty"`
}

// NOTE: ProductIDs removed from EANPage to prevent DB bloat.
// Product→EAN link is stored in Product.EAN field.
// EAN→Products query via turbo index "ean:{ean}" in TurboProductSearch.

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
