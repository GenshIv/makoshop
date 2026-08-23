package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/GenshIv/makoshop/internal/model"
)

const (
	turboKeyCompanyList = "company_list"
	turboKeyCompanyName = "company_name:" // prefix for name lookup
)

type CompanyRepo struct {
	Store *Store
}

func NewCompanyRepo(store *Store) *CompanyRepo {
	return &CompanyRepo{Store: store}
}

// Create creates a new company.
func (r *CompanyRepo) Create(c *model.Company) error {
	id, err := r.Store.NextID("company")
	if err != nil {
		return fmt.Errorf("next_id company: %w", err)
	}
	c.ID = id
	c.CreatedAt = time.Now().Unix()
	c.UpdatedAt = time.Now().Unix()
	if c.Status == "" {
		c.Status = model.CompanyStatusPending
	}
	if c.Settings.Currency == "" {
		c.Settings.Currency = "RUB"
	}
	if c.Slug == "" {
		c.Slug = toSlug(c.Name)
	}

	data := MarshalCompany(*c)
	if err := r.Store.DocPut(KeyCompany(c.ID), data); err != nil {
		return fmt.Errorf("save company: %w", err)
	}

	// Turbo index: company_list
	if _, err := r.Store.db.TurboPutIndexString(turboKeyCompanyList, strconv.Itoa(int(id))); err != nil {
		_ = r.Store.DocDelete(KeyCompany(c.ID))
		return fmt.Errorf("turbo index company_list: %w", err)
	}

	// Turbo index: company_name:<slug>
	nameKey := turboKeyCompanyName + c.Slug
	if err := r.Store.TurboWrite(nameKey, []byte(fmt.Sprintf("%d", id))); err != nil {
		_, _ = r.Store.db.TurboDeleteIndexString(turboKeyCompanyList, strconv.Itoa(int(id)))
		_ = r.Store.DocDelete(KeyCompany(c.ID))
		return fmt.Errorf("turbo index company_name: %w", err)
	}

	return nil
}

// Get returns a company by ID.
func (r *CompanyRepo) Get(id int64) (*model.Company, error) {
	data, err := r.Store.DocGet(KeyCompany(id))
	if err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			return nil, fmt.Errorf("company %d not found", id)
		}
		return nil, fmt.Errorf("get company %d: %w", id, err)
	}
	return UnmarshalCompany(data)
}

// GetBySlug returns a company by slug.
func (r *CompanyRepo) GetBySlug(slug string) (*model.Company, error) {
	nameKey := turboKeyCompanyName + slug
	data, err := r.Store.db.TurboRawRead(nameKey)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("company with slug %q not found", slug)
	}
	var id int64
	_, _ = fmt.Sscanf(string(data), "%d", &id)
	return r.Get(id)
}

// GetCompanyIDByUserID returns the company ID for a given user (seller).
// Uses the first company where owner_user_id matches.
func (r *CompanyRepo) GetCompanyIDByUserID(userID int64) (int64, error) {
	companies, err := r.List()
	if err != nil {
		return 0, err
	}
	for _, c := range companies {
		if c.OwnerUserID == userID {
			return c.ID, nil
		}
	}
	return 0, fmt.Errorf("no company found for user %d", userID)
}

// Update updates a company.
// If PaymentMethodIds/DeliveryTimeIds/InstallmentPlanIds are changed, settings are saved as a batch.
func (r *CompanyRepo) Update(id int64, updater func(*model.Company)) error {
	c, err := r.Get(id)
	if err != nil {
		return err
	}

	oldSlug := c.Slug
	updater(c)
	c.UpdatedAt = time.Now().Unix()

	data := MarshalCompany(*c)
	if err := r.Store.DocPut(KeyCompany(c.ID), data); err != nil {
		return fmt.Errorf("update company: %w", err)
	}

	// Update company_name index if slug changed
	if oldSlug != c.Slug {
		_ = r.Store.TurboWrite(turboKeyCompanyName+oldSlug, []byte{}) // clear old
		if err := r.Store.TurboWrite(turboKeyCompanyName+c.Slug, []byte(strconv.FormatInt(id, 10))); err != nil {
			return fmt.Errorf("update company_name index: %w", err)
		}
	}

	// Save company settings (payment methods, delivery times, installment plans) as a batch
	settings := &model.CompanySettingsV2{
		PaymentMethodIds:   c.PaymentMethodIds,
		DeliveryTimeIds:    c.DeliveryTimeIds,
		InstallmentPlanIds: c.InstallmentPlanIds,
	}
	if err := r.SaveCompanySettings(id, settings); err != nil {
		return fmt.Errorf("save company settings: %w", err)
	}

	return nil
}

// List returns all companies via turbo index.
func (r *CompanyRepo) List() ([]model.Company, error) {
	// Get company IDs as Key128
	tokens, err := r.Store.DB().TurboGetIndexTokens(turboKeyCompanyList)
	if err != nil || len(tokens) == 0 {
		return nil, nil
	}
	// Use MultiGetByDocIDs to retrieve all companies at once (tokens already contain full keys)
	docs, err := r.Store.DB().MultiGetByDocIDs(tokens)
	if err != nil {
		return nil, fmt.Errorf("multi get companies: %w", err)
	}
	var result []model.Company
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}
		c, err := UnmarshalCompany(doc)
		if err != nil {
			continue
		}
		result = append(result, *c)
	}
	return result, nil
}

// Delete removes a company.
func (r *CompanyRepo) Delete(id int64) error {
	c, err := r.Get(id)
	if err != nil {
		return err
	}

	// Remove turbo indexes
	_, _ = r.Store.db.TurboDeleteIndexString(turboKeyCompanyList, strconv.Itoa(int(id)))
	_ = r.Store.TurboWrite(turboKeyCompanyName+c.Slug, []byte{})

	if err := r.Store.DocDelete(KeyCompany(id)); err != nil {
		return fmt.Errorf("delete company: %w", err)
	}
	return nil
}

// --- Company settings: payment methods, delivery times, installment plans ---

const (
	keyCompanyPaymentMethods   = "company_pm:" // company_id -> JSON array of method_ids
	keyCompanyDeliveryTimes    = "company_dt:" // company_id -> JSON array of time_ids
	keyCompanyInstallmentPlans = "company_ip:" // company_id -> JSON array of plan_ids
)

// SaveCompanySettings saves all company settings (payment, delivery, installment) as a batch.
// This replaces all existing settings for the company in one operation.
func (r *CompanyRepo) SaveCompanySettings(companyID int64, settings *model.CompanySettingsV2) error {
	if settings == nil {
		settings = &model.CompanySettingsV2{}
	}

	// Save payment method IDs
	pmData, _ := json.Marshal(settings.PaymentMethodIds)
	if err := r.Store.DocPut(keyCompanyPaymentMethods+strconv.FormatInt(companyID, 10), pmData); err != nil {
		return fmt.Errorf("save company payment methods: %w", err)
	}

	// Save delivery time IDs
	dtData, _ := json.Marshal(settings.DeliveryTimeIds)
	if err := r.Store.DocPut(keyCompanyDeliveryTimes+strconv.FormatInt(companyID, 10), dtData); err != nil {
		return fmt.Errorf("save company delivery times: %w", err)
	}

	// Save installment plan IDs
	ipData, _ := json.Marshal(settings.InstallmentPlanIds)
	if err := r.Store.DocPut(keyCompanyInstallmentPlans+strconv.FormatInt(companyID, 10), ipData); err != nil {
		return fmt.Errorf("save company installment plans: %w", err)
	}

	return nil
}

// GetCompanySettings returns all settings for a company.
func (r *CompanyRepo) GetCompanySettings(companyID int64) (*model.CompanySettingsV2, error) {
	settings := &model.CompanySettingsV2{}

	// Load payment method IDs
	pmData, err := r.Store.DocGet(keyCompanyPaymentMethods + strconv.FormatInt(companyID, 10))
	if err == nil && len(pmData) > 0 {
		_ = json.Unmarshal(pmData, &settings.PaymentMethodIds)
	}

	// Load delivery time IDs
	dtData, err := r.Store.DocGet(keyCompanyDeliveryTimes + strconv.FormatInt(companyID, 10))
	if err == nil && len(dtData) > 0 {
		_ = json.Unmarshal(dtData, &settings.DeliveryTimeIds)
	}

	// Load installment plan IDs
	ipData, err := r.Store.DocGet(keyCompanyInstallmentPlans + strconv.FormatInt(companyID, 10))
	if err == nil && len(ipData) > 0 {
		_ = json.Unmarshal(ipData, &settings.InstallmentPlanIds)
	}

	return settings, nil
}

// DeleteCompanySettings removes all settings for a company.
func (r *CompanyRepo) DeleteCompanySettings(companyID int64) error {
	idStr := strconv.FormatInt(companyID, 10)
	_ = r.Store.DocDelete(keyCompanyPaymentMethods + idStr)
	_ = r.Store.DocDelete(keyCompanyDeliveryTimes + idStr)
	_ = r.Store.DocDelete(keyCompanyInstallmentPlans + idStr)
	return nil
}

// toSlug creates a URL-friendly slug from a string.
func toSlug(s string) string {
	// Simple slug: lowercase, replace spaces with hyphens, remove special chars
	result := []rune{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result = append(result, r)
		} else if r == ' ' || r == '-' {
			result = append(result, '-')
		}
	}
	// Collapse multiple hyphens
	collapsed := []rune{}
	for i, r := range result {
		if r == '-' && i > 0 && result[i-1] == '-' {
			continue
		}
		collapsed = append(collapsed, r)
	}
	// Trim leading/trailing hyphens
	start, end := 0, len(collapsed)
	for start < end && collapsed[start] == '-' {
		start++
	}
	for end > start && collapsed[end-1] == '-' {
		end--
	}
	return string(collapsed[start:end])
}
