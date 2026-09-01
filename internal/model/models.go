package model

import (
	"fmt"
	"strings"
)

// KeyValue is a generic key-value pair for attributes, constraints, contexts, etc.
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// attrValueMaxRunes is the maximum number of runes allowed in an attribute value.
// Values exceeding this limit are ignored everywhere: parsing, indexing, filtering.
const attrValueMaxRunes = 40

// IsAttrValueTooLong returns true if the attribute value has more than attrValueMaxRunes runes.
// Long values are considered noise and are excluded from all processing.
func IsAttrValueTooLong(v string) bool {
	return len([]rune(v)) > attrValueMaxRunes
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
	Keys         []string `json:"keys,omitempty"` // raw keys from HTML (e.g. "Moc", "Power")
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

// Normalize canonicalizes the Format field so import dispatch is deterministic:
// it trims and lowercases the value, maps the "xml" alias to "nokaut", and
// defaults an empty value to "nokaut". Call this before persisting a
// PriceSourceConfig so the stored format is always a valid, comparable value.
func (p *PriceSourceConfig) Normalize() {
	if p == nil {
		return
	}
	p.Format = strings.ToLower(strings.TrimSpace(p.Format))
	if p.Format == "xml" {
		p.Format = "nokaut"
	}
	if p.Format == "" {
		p.Format = "nokaut"
	}
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
	DeliveryMethodIds  []int64          `json:"delivery_method_ids,omitempty"`
	InstallmentPlanIds []int64          `json:"installment_plan_ids,omitempty"`

	// --- Price import (tasks 1, 3, 7) ---
	ImportURL    string            `json:"import_url,omitempty"`    // URL to download the price file from
	ImportFolder string            `json:"import_folder,omitempty"` // folder name in prices/ dir (legacy fallback)
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
	CompanyName   string        `json:"company_name,omitempty"` // transient: filled on read, not persisted
	Brand         string        `json:"brand,omitempty"`
	Price         float64       `json:"price"`
	PreviousPrice float64       `json:"previous_price,omitempty"` // old price; if > Price → discount
	Currency      string        `json:"currency"`
	StockQty      int64         `json:"stock_qty"`
	Status        ProductStatus `json:"status"`
	ProductURL    string        `json:"product_url,omitempty"`  // direct link to product on vendor site
	PurchaseURL   string        `json:"purchase_url,omitempty"` // affiliate/partner purchase link
	Attributes    []KeyValue    `json:"attributes,omitempty"`
	Images        []string      `json:"images,omitempty"`
	SEO           ProductSEO    `json:"seo,omitempty"`
	AvgRating     float64       `json:"avg_rating,omitempty"`   // average review rating (computed)
	ReviewCount   int           `json:"review_count,omitempty"` // number of approved reviews
	EANPageID     int64         `json:"ean_page_id,omitempty"`  // linked EAN page ID
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
	ShopCategory  string        `json:"shop_category"`
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
	ProductIDs  []int64  `json:"product_ids"` // cached list of product IDs with this EAN
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
	LikeCount    int        `json:"like_count"`           // likes for this page
	DislikeCount int        `json:"dislike_count"`        // dislikes for this page
	CreatedAt    int64      `json:"created_at,omitempty"`
	UpdatedAt    int64      `json:"updated_at,omitempty"`
	SeoURL       string     `json:"seo_url,omitempty"`
	Keywords     string     `json:"keywords,omitempty"` // keywords for catalogization (product name + shop category)
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

// ReviewStatus represents the moderation status of a review.
type ReviewStatus string

const (
	ReviewStatusPending  ReviewStatus = "pending"  // new, awaiting moderation
	ReviewStatusApproved ReviewStatus = "approved" // approved and visible
	ReviewStatusRejected ReviewStatus = "rejected" // rejected by moderator
	ReviewStatusHidden   ReviewStatus = "hidden"   // hidden (spam/complaints)
)

// Review represents a product review with moderation support.
type Review struct {
	ID         int64        `json:"id"`
	ProductID  int64        `json:"product_id"`
	EAN        string       `json:"ean,omitempty"`         // product's EAN (copied at creation)
	EANPageID  int64        `json:"ean_page_id,omitempty"` // EAN page ID (computed)
	UserID     int64        `json:"user_id"`
	Rating     int          `json:"rating"`
	Comment    string       `json:"comment,omitempty"`
	Status     ReviewStatus `json:"status"`
	IsFeatured bool         `json:"is_featured"`
	Verified   bool         `json:"verified"`
	CreatedAt  int64        `json:"created_at"`
	UpdatedAt  int64        `json:"updated_at"`
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

type DeliveryMethod struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Image     string `json:"image,omitempty"`
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
	DeliveryMethodIds  []int64 `json:"delivery_method_ids,omitempty"`
	InstallmentPlanIds []int64 `json:"installment_plan_ids,omitempty"`
}

// CommentTargetType represents the type of content a comment is attached to.
type CommentTargetType string

const (
	CommentTargetProduct  CommentTargetType = "product"
	CommentTargetCategory CommentTargetType = "category"
	CommentTargetEANPage  CommentTargetType = "eanpage"
)

// CommentStatus represents the moderation status of a comment.
type CommentStatus string

const (
	CommentStatusPending  CommentStatus = "pending"
	CommentStatusApproved CommentStatus = "approved"
	CommentStatusRejected CommentStatus = "rejected"
	CommentStatusHidden   CommentStatus = "hidden"
)

// Comment represents a user comment on any target (product, category, eanpage).
type Comment struct {
	ID           int64             `json:"id"`
	TargetType   CommentTargetType `json:"target_type"`
	TargetID     int64             `json:"target_id"`
	UserID       int64             `json:"user_id"`
	ParentID     int64             `json:"parent_id,omitempty"` // for nested replies
	Content      string            `json:"content"`
	Status       CommentStatus     `json:"status"`
	LikeCount    int               `json:"like_count"`
	DislikeCount int               `json:"dislike_count"`
	IsFeatured   bool              `json:"is_featured"`
	CreatedAt    int64             `json:"created_at"`
	UpdatedAt    int64             `json:"updated_at"`
}

// VoteType represents the type of vote (like or dislike).
type VoteType string

const (
	VoteLike    VoteType = "like"
	VoteDislike VoteType = "dislike"
)

// VoteTargetType represents the type of content being voted on.
type VoteTargetType string

const (
	VoteTargetComment VoteTargetType = "comment"
	VoteTargetReview  VoteTargetType = "review"
	VoteTargetEANPage VoteTargetType = "eanpage"
)

// Vote represents a like/dislike on a comment, review, or eanpage.
type Vote struct {
	ID         int64    `json:"id"`
	TargetType string   `json:"target_type"` // "comment", "review", or "eanpage"
	TargetID   int64    `json:"target_id"`
	UserID     int64    `json:"user_id"`
	VoteType   VoteType `json:"vote_type"`
	CreatedAt  int64    `json:"created_at"`
	UpdatedAt  int64    `json:"updated_at"`
}

// UserVote represents the current vote state for a user on a target.
type UserVote struct {
	TargetType string   `json:"target_type"`
	TargetID   int64    `json:"target_id"`
	VoteType   VoteType `json:"vote_type"` // "like", "dislike", or "" if not voted
}

// --- Branding: page decoration system (banners for different occasions) ---

// BrandSlot is a placement location for a branding element on the page.
type BrandSlot string

const (
	BrandSlotHeaderFullwidth BrandSlot = "header_fullwidth"  // full width, right under the header
	BrandSlotHomeBanner      BrandSlot = "home_banner"       // main page banner (hero area)
	BrandSlotCategoryBanner  BrandSlot = "category_banner"   // category page banner
	BrandSlotFooterFullwidth BrandSlot = "footer_fullwidth"  // full width, right above the footer
	BrandSlotSideLeftTop     BrandSlot = "side_left_top"     // left column, top
	BrandSlotSideLeftBottom  BrandSlot = "side_left_bottom"  // left column, bottom
	BrandSlotSideRightTop    BrandSlot = "side_right_top"    // right column, top
	BrandSlotSideRightBottom BrandSlot = "side_right_bottom" // right column, bottom
)

// BrandSlots lists all valid slots.
var BrandSlots = []BrandSlot{
	BrandSlotHeaderFullwidth,
	BrandSlotHomeBanner,
	BrandSlotCategoryBanner,
	BrandSlotFooterFullwidth,
	BrandSlotSideLeftTop,
	BrandSlotSideLeftBottom,
	BrandSlotSideRightTop,
	BrandSlotSideRightBottom,
}

// BrandSlotValid reports whether s is a known slot.
func BrandSlotValid(s BrandSlot) bool {
	for _, v := range BrandSlots {
		if v == s {
			return true
		}
	}
	return false
}

// Branding limits (server-side sanity guards, see docs/BRANDING_SYSTEM_PLAN.md).
const (
	BrandingMaxSetName    = 100  // max runes in a set name
	BrandingMaxSetDesc    = 500  // max runes in a set description
	BrandingMaxPatterns   = 10   // max page regex patterns per element
	BrandingMaxPatternLen = 200  // max runes per pattern
	BrandingMaxAltLen     = 200  // max runes in alt text
	BrandingMaxLinkLen    = 2048 // max runes in a link URL
)

// BrandElement is one branding element in a specific slot.
//
// PagePatterns are JS-regex patterns matched against the current page path
// (e.g. "/shop/telefony/samsung"). An empty list means "show on every page".
// Multiple patterns are OR-ed: the element shows if any of them matches.
type BrandElement struct {
	Slot         BrandSlot `json:"slot"`
	ImageURL     string    `json:"image_url"`                // light theme image
	ImageDarkURL string    `json:"image_dark_url,omitempty"` // dark theme image (optional)
	LinkURL      string    `json:"link_url,omitempty"`       // optional click-through link
	AltText      string    `json:"alt_text,omitempty"`
	PagePatterns []string  `json:"page_patterns,omitempty"`
}

// BrandSet is a named branding set (e.g. "New Year 2025"). It is the basic
// management unit: it can be enabled/disabled at any moment, and its elements
// are shown per the resolution rules (see docs/BRANDING_SYSTEM_PLAN.md 2.3).
type BrandSet struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Enabled     bool           `json:"enabled"`
	Priority    int            `json:"priority"` // higher wins on conflict
	Elements    []BrandElement `json:"elements"` // at most one element per slot
	CreatedAt   int64          `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
}

// BrandCategoryTheme is a per-category (section) image override for a slot:
// "different sections can have their own images in any place".
// It applies while the visitor browses the given category (or its subtree).
type BrandCategoryTheme struct {
	ID           int64     `json:"id"`
	CategoryID   int64     `json:"category_id"`
	Slot         BrandSlot `json:"slot"`
	ImageURL     string    `json:"image_url"`
	ImageDarkURL string    `json:"image_dark_url,omitempty"`
	LinkURL      string    `json:"link_url,omitempty"`
	CreatedAt    int64     `json:"created_at"`
	UpdatedAt    int64     `json:"updated_at"`
}

// BrandingActivePayload is the public response of GET /branding/active:
// only enabled sets plus all category overrides, with a version for caching.
type BrandingActivePayload struct {
	Version           int64                `json:"version"`
	Sets              []BrandSet           `json:"sets"`
	CategoryOverrides []BrandCategoryTheme `json:"category_overrides"`
}

// ValidateBrandElement checks a single element's fields.
func ValidateBrandElement(e *BrandElement) error {
	if !BrandSlotValid(e.Slot) {
		return fmt.Errorf("invalid slot %q", e.Slot)
	}
	if strings.TrimSpace(e.ImageURL) == "" {
		return fmt.Errorf("image_url is required for slot %s", e.Slot)
	}
	if len(e.ImageDarkURL) > BrandingMaxLinkLen {
		return fmt.Errorf("image_dark_url is too long")
	}
	if len(e.LinkURL) > BrandingMaxLinkLen {
		return fmt.Errorf("link_url is too long")
	}
	if len([]rune(e.AltText)) > BrandingMaxAltLen {
		return fmt.Errorf("alt_text is too long")
	}
	if len(e.PagePatterns) > BrandingMaxPatterns {
		return fmt.Errorf("too many page_patterns (max %d)", BrandingMaxPatterns)
	}
	for _, p := range e.PagePatterns {
		if len([]rune(p)) > BrandingMaxPatternLen {
			return fmt.Errorf("page pattern too long (max %d runes)", BrandingMaxPatternLen)
		}
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("empty page pattern")
		}
	}
	return nil
}

// ValidateBrandSet checks set-level invariants: name, and at most one
// element per slot.
func ValidateBrandSet(s *BrandSet) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len([]rune(s.Name)) > BrandingMaxSetName {
		return fmt.Errorf("name is too long (max %d runes)", BrandingMaxSetName)
	}
	if len([]rune(s.Description)) > BrandingMaxSetDesc {
		return fmt.Errorf("description is too long (max %d runes)", BrandingMaxSetDesc)
	}
	seen := make(map[BrandSlot]bool, len(s.Elements))
	for i := range s.Elements {
		if err := ValidateBrandElement(&s.Elements[i]); err != nil {
			return fmt.Errorf("element %d: %w", i, err)
		}
		slot := s.Elements[i].Slot
		if seen[slot] {
			return fmt.Errorf("duplicate element for slot %s", slot)
		}
		seen[slot] = true
	}
	return nil
}

// ValidateBrandCatTheme checks a category theme's fields.
func ValidateBrandCatTheme(t *BrandCategoryTheme) error {
	if t.CategoryID <= 0 {
		return fmt.Errorf("category_id is required")
	}
	if !BrandSlotValid(t.Slot) {
		return fmt.Errorf("invalid slot %q", t.Slot)
	}
	if strings.TrimSpace(t.ImageURL) == "" {
		return fmt.Errorf("image_url is required")
	}
	if len(t.ImageDarkURL) > BrandingMaxLinkLen {
		return fmt.Errorf("image_dark_url is too long")
	}
	if len(t.LinkURL) > BrandingMaxLinkLen {
		return fmt.Errorf("link_url is too long")
	}
	return nil
}

// ---------- SEO structured data (JSON-LD) ----------

// SEOSettings holds the configurable site-wide and product-page structured
// data (schema.org JSON-LD) used for SEO. A single settings document is stored
// in the DB (see db.KeySEOSettings); the server renders the JSON-LD into every
// landing page's <head>.
type SEOSettings struct {
	// Enabled toggles all JSON-LD injection. When false, no structured data
	// is emitted (except nothing — the site relies on plain HTML).
	Enabled bool `json:"enabled"`

	// Organization (schema.org Organization) — emitted on every page.
	OrgName       string   `json:"org_name"`
	OrgLegalName  string   `json:"org_legal_name,omitempty"`
	OrgLogo       string   `json:"org_logo,omitempty"`
	OrgPhone      string   `json:"org_phone,omitempty"`
	OrgEmail      string   `json:"org_email,omitempty"`
	OrgStreet     string   `json:"org_street,omitempty"`
	OrgCity       string   `json:"org_city,omitempty"`
	OrgPostalCode string   `json:"org_postal_code,omitempty"`
	OrgCountry    string   `json:"org_country,omitempty"`
	OrgSameAs     []string `json:"org_same_as,omitempty"` // social/profile URLs

	// WebSite (schema.org WebSite + SearchAction) — emitted on every page.
	// SiteName falls back to the site name derived from the base URL.
	// SearchURLTemplate is a path (relative to the base URL) that must contain
	// the {search_term_string} placeholder, e.g. "/shop?q={search_term_string}".
	SiteName          string `json:"site_name,omitempty"`
	SearchURLTemplate string `json:"search_url_template,omitempty"`

	// OnlineStore (schema.org OnlineStore) — emitted on every page and on
	// product pages. StoreName/StoreLogo fall back to the organization values.
	StoreName   string   `json:"store_name,omitempty"`
	StoreLogo   string   `json:"store_logo,omitempty"`
	StoreSameAs []string `json:"store_same_as,omitempty"`

	// Product offer defaults (schema.org Product/Offer on product pages).
	DefaultCurrency string `json:"default_currency,omitempty"` // fallback when EAN page has none
	PriceValidDays  int    `json:"price_valid_days,omitempty"` // priceValidUntil = now + N days (default 30)

	// Merchant return policy (schema.org Product.hasMerchantReturnPolicy).
	// Emitted on product pages when ReturnPolicyEnabled is true.
	ReturnPolicyEnabled bool   `json:"return_policy_enabled"`
	ReturnPolicyText    string `json:"return_policy_text,omitempty"`
	ReturnPolicyDays    int    `json:"return_policy_days,omitempty"`
	ReturnPolicyCountry string `json:"return_policy_country,omitempty"` // fallback: org country

	// Shipping details (schema.org Offer.shippingDetails). Emitted on product
	// page offers when ShippingEnabled is true.
	ShippingEnabled     bool    `json:"shipping_enabled"`
	ShippingCost        float64 `json:"shipping_cost,omitempty"`
	ShippingMinDays     int     `json:"shipping_min_days,omitempty"`
	ShippingMaxDays     int     `json:"shipping_max_days,omitempty"`
	ShippingDestination string  `json:"shipping_destination,omitempty"` // fallback: org country

	UpdatedAt int64 `json:"updated_at,omitempty"`
}

const (
	SEOMaxFieldLen   = 500 // max length for a single text field
	SEOMaxSameAs     = 20  // max number of sameAs URLs
	SEODefaultValid  = 30  // default priceValidUntil horizon in days
	SEODefaultSearch = "/shop?q={search_term_string}"
)

// ValidateSEOSettings checks field lengths and the search template invariant.
func ValidateSEOSettings(s *SEOSettings) error {
	check := func(name, v string) error {
		if len(v) > SEOMaxFieldLen {
			return fmt.Errorf("%s is too long (max %d)", name, SEOMaxFieldLen)
		}
		return nil
	}
	for name, v := range map[string]string{
		"org_name": s.OrgName, "org_legal_name": s.OrgLegalName, "org_logo": s.OrgLogo,
		"org_phone": s.OrgPhone, "org_email": s.OrgEmail, "org_street": s.OrgStreet,
		"org_city": s.OrgCity, "org_postal_code": s.OrgPostalCode, "org_country": s.OrgCountry,
		"site_name": s.SiteName, "search_url_template": s.SearchURLTemplate,
		"store_name": s.StoreName, "store_logo": s.StoreLogo, "default_currency": s.DefaultCurrency,
		"return_policy_text": s.ReturnPolicyText, "return_policy_country": s.ReturnPolicyCountry,
		"shipping_destination": s.ShippingDestination,
	} {
		if err := check(name, v); err != nil {
			return err
		}
	}
	if len(s.OrgSameAs) > SEOMaxSameAs || len(s.StoreSameAs) > SEOMaxSameAs {
		return fmt.Errorf("too many sameAs URLs (max %d)", SEOMaxSameAs)
	}
	for _, u := range append(append([]string{}, s.OrgSameAs...), s.StoreSameAs...) {
		if len(u) > SEOMaxFieldLen {
			return fmt.Errorf("sameAs URL too long")
		}
	}
	if s.SearchURLTemplate != "" && !strings.Contains(s.SearchURLTemplate, "{search_term_string}") {
		return fmt.Errorf("search_url_template must contain {search_term_string}")
	}
	if s.PriceValidDays < 0 || s.PriceValidDays > 365 {
		return fmt.Errorf("price_valid_days must be 0..365")
	}
	if s.ReturnPolicyDays < 0 || s.ReturnPolicyDays > 365 {
		return fmt.Errorf("return_policy_days must be 0..365")
	}
	if s.ShippingMinDays < 0 || s.ShippingMaxDays < 0 || s.ShippingMaxDays > 365 ||
		(s.ShippingMinDays > 0 && s.ShippingMaxDays > 0 && s.ShippingMinDays > s.ShippingMaxDays) {
		return fmt.Errorf("shipping days must be 0..365 and min <= max")
	}
	return nil
}
