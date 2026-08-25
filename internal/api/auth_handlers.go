package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/httpres"
	"github.com/GenshIv/makoshop/internal/model"
)

type AuthHandlers struct {
	userRepo    *db.UserRepo
	companyRepo *db.CompanyRepo
	cartRepo    *db.CartRepo
	turboSearch *db.TurboProductSearch
	jwt         *auth.JWTMiddleware
	secret      string
}

func NewAuthHandlers(userRepo *db.UserRepo, companyRepo *db.CompanyRepo, cartRepo *db.CartRepo, jwtMiddleware *auth.JWTMiddleware, secret string) *AuthHandlers {
	return &AuthHandlers{
		userRepo:    userRepo,
		companyRepo: companyRepo,
		cartRepo:    cartRepo,
		jwt:         jwtMiddleware,
		secret:      secret,
	}
}

// SetTurboSearch attaches a TurboProductSearch instance to AuthHandlers.
func (h *AuthHandlers) SetTurboSearch(t *db.TurboProductSearch) {
	h.turboSearch = t
}

// getTurboSearch returns the attached TurboProductSearch.
func (h *AuthHandlers) getTurboSearch() *db.TurboProductSearch {
	return h.turboSearch
}

// --- Request/Response types ---

type RegisterRequest struct {
	Email    string            `json:"email"`
	Password string            `json:"password"`
	Role     string            `json:"role,omitempty"`
	Profile  model.UserProfile `json:"profile,omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	UserID     int64          `json:"user_id"`
	Email      string         `json:"email"`
	Role       model.UserRole `json:"role"`
	Token      string         `json:"token"`
	FirstLogin bool           `json:"first_login,omitempty"`
}

type UpdateProfileRequest struct {
	Name        string `json:"name,omitempty"`
	Phone       string `json:"phone,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	Address     string `json:"address,omitempty"`
}

type AdminUpdateUserRequest struct {
	Role    model.UserRole     `json:"role,omitempty"`
	Status  model.UserStatus   `json:"status,omitempty"`
	Profile *model.UserProfile `json:"profile,omitempty"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// --- Handlers ---

func (h *AuthHandlers) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req RegisterRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.Email == "" || req.Password == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "email and password are required")
		return
	}

	role := model.UserRole(req.Role)
	if role == "" || (role != model.RoleBuyer && role != model.RoleSeller && role != model.RoleAdmin) {
		role = model.RoleBuyer
	}

	user := &model.User{
		Email:   req.Email,
		Role:    role,
		Profile: req.Profile,
	}

	if err := h.userRepo.Create(user, req.Password); err != nil {
		if err.Error() == "email already registered" {
			httpres.WriteError(w, http.StatusConflict, "EMAIL_EXISTS", err.Error())
			return
		}
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	token, err := auth.GenerateToken(user, h.secret)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "TOKEN_ERROR", "failed to generate token")
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, AuthResponse{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		Token:  token,
	})
}

func (h *AuthHandlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req LoginRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.Email == "" || req.Password == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "email and password are required")
		return
	}

	user, err := h.userRepo.GetByEmail(req.Email)
	if err != nil {
		httpres.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	if user.Status == model.UserStatusBlocked {
		httpres.WriteError(w, http.StatusForbidden, "ACCOUNT_BLOCKED", "account is blocked")
		return
	}

	if !h.userRepo.VerifyPassword(user, req.Password) {
		httpres.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	token, err := auth.GenerateToken(user, h.secret)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "TOKEN_ERROR", "failed to generate token")
		return
	}

	// Merge guest cart with user cart if session_id is provided.
	// Session ID can be passed via:
	//   - Header: X-Session-ID
	//   - Query parameter: session_id
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session_id")
	}
	if sessionID != "" && h.cartRepo != nil {
		// Try to find a guest cart by session ID
		guestCart, err := h.cartRepo.GetBySessionID(sessionID)
		if err == nil && guestCart != nil {
			// Merge guest cart into user's cart (or assign if user has no cart)
			_, _ = h.cartRepo.AssignToUser(guestCart.ID, user.ID)
		}
	}

	httpres.WriteJSON(w, http.StatusOK, AuthResponse{
		UserID:     user.ID,
		Email:      user.Email,
		Role:       user.Role,
		Token:      token,
		FirstLogin: user.IsFirstLogin,
	})
}

func (h *AuthHandlers) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, ok := auth.ContextUserFrom(r)
	if !ok {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	user, err := h.userRepo.GetByID(ctxUser.ID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":             user.ID,
		"email":          user.Email,
		"role":           user.Role,
		"status":         user.Status,
		"profile":        user.Profile,
		"is_first_login": user.IsFirstLogin,
	})
}

func (h *AuthHandlers) HandleUpdateMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, ok := auth.ContextUserFrom(r)
	if !ok {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	var req UpdateProfileRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.userRepo.Update(ctxUser.ID, func(u *model.User) {
		if req.Name != "" {
			u.Profile.Name = req.Name
		}
		if req.Phone != "" {
			u.Profile.Phone = req.Phone
		}
		if req.CompanyName != "" {
			u.Profile.CompanyName = req.CompanyName
		}
		if req.Address != "" {
			u.Profile.Address = req.Address
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	user, _ := h.userRepo.GetByID(ctxUser.ID)
	httpres.WriteJSON(w, http.StatusOK, user)
}

// --- Admin user handlers ---

func (h *AuthHandlers) HandleAdminUsersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	q := r.URL.Query()
	page, _ := parseQueryInt(q.Get("page"), 1)
	limit, _ := parseQueryInt(q.Get("limit"), 50)

	params := db.ListUsersParams{
		Page:   page,
		Limit:  limit,
		Role:   q.Get("role"),
		Status: q.Get("status"),
		Q:      q.Get("q"),
	}

	users, total, err := h.userRepo.List(params)
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if users == nil {
		users = []model.User{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"items": users,
	})
}

func (h *AuthHandlers) HandleAdminUserGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "user_id")
	if !ok {
		return
	}

	user, err := h.userRepo.GetByID(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, user)
}

func (h *AuthHandlers) HandleAdminUserUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "user_id")
	if !ok {
		return
	}

	var req AdminUpdateUserRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.userRepo.Update(id, func(u *model.User) {
		if req.Role != "" {
			u.Role = req.Role
		}
		if req.Status != "" {
			u.Status = req.Status
		}
		if req.Profile != nil {
			if req.Profile.Name != "" {
				u.Profile.Name = req.Profile.Name
			}
			if req.Profile.Phone != "" {
				u.Profile.Phone = req.Profile.Phone
			}
			if req.Profile.CompanyName != "" {
				u.Profile.CompanyName = req.Profile.CompanyName
			}
			if req.Profile.Address != "" {
				u.Profile.Address = req.Profile.Address
			}
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	user, _ := h.userRepo.GetByID(id)
	httpres.WriteJSON(w, http.StatusOK, user)
}

// --- Company handlers (admin + public read) ---

type CreateCompanyRequest struct {
	Name        string                 `json:"name"`
	Slug        string                 `json:"slug,omitempty"`
	Description string                 `json:"description,omitempty"`
	LogoURL     string                 `json:"logo_url,omitempty"`
	LegalInfo   model.CompanyLegalInfo `json:"legal_info,omitempty"`
	Contacts    model.CompanyContacts  `json:"contacts,omitempty"`
	Settings    model.CompanySettings  `json:"settings,omitempty"`
	OwnerUserID int64                  `json:"owner_user_id"`
}

func (h *AuthHandlers) HandleAdminCompaniesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	companies, err := h.companyRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if companies == nil {
		companies = []model.Company{}
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": companies,
		"total": len(companies),
	})
}

func (h *AuthHandlers) HandleAdminCompanyCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateCompanyRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.Name == "" || req.OwnerUserID == 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "name and owner_user_id are required")
		return
	}

	c := &model.Company{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		LogoURL:     req.LogoURL,
		LegalInfo:   req.LegalInfo,
		Contacts:    req.Contacts,
		Settings:    req.Settings,
		OwnerUserID: req.OwnerUserID,
	}

	if err := h.companyRepo.Create(c); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusCreated, c)
}

func (h *AuthHandlers) HandleAdminCompanyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "company_id")
	if !ok {
		return
	}

	c, err := h.companyRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, c)
}

func (h *AuthHandlers) HandleAdminCompanyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "company_id")
	if !ok {
		return
	}

	var req struct {
		Name               string                  `json:"name,omitempty"`
		LegalInfo          *model.CompanyLegalInfo `json:"legal_info,omitempty"`
		Settings           *model.CompanySettings  `json:"settings,omitempty"`
		Status             model.CompanyStatus     `json:"status,omitempty"`
		PaymentMethodIds   []int64                 `json:"payment_method_ids,omitempty"`
		DeliveryTimeIds    []int64                 `json:"delivery_time_ids,omitempty"`
		InstallmentPlanIds []int64                 `json:"installment_plan_ids,omitempty"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if err := h.companyRepo.Update(id, func(c *model.Company) {
		if req.Name != "" {
			c.Name = req.Name
		}
		if req.LegalInfo != nil {
			c.LegalInfo = *req.LegalInfo
		}
		if req.Settings != nil {
			c.Settings = *req.Settings
		}
		if req.Status != "" {
			c.Status = req.Status
		}
		// Update company settings IDs (will be persisted as a batch in Update)
		if req.PaymentMethodIds != nil {
			c.PaymentMethodIds = req.PaymentMethodIds
		}
		if req.DeliveryTimeIds != nil {
			c.DeliveryTimeIds = req.DeliveryTimeIds
		}
		if req.InstallmentPlanIds != nil {
			c.InstallmentPlanIds = req.InstallmentPlanIds
		}
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ := h.companyRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, c)
}

// HandleAdminCompanyVerify verifies or blocks a company.
// PATCH /admin/companies/{id}/verify
// Body: {"status": "verified" | "blocked"}
func (h *AuthHandlers) HandleAdminCompanyVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /admin/companies/{id}/verify
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "admin", "companies", "{id}", "verify"]
	if len(parts) < 5 || parts[4] != "verify" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || id <= 0 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	var newStatus model.CompanyStatus
	switch req.Status {
	case "verified":
		newStatus = model.CompanyStatusVerified
	case "blocked":
		newStatus = model.CompanyStatusBlocked
	default:
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "status must be 'verified' or 'blocked'")
		return
	}

	if err := h.companyRepo.Update(id, func(c *model.Company) {
		c.Status = newStatus
	}); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ := h.companyRepo.Get(id)
	httpres.WriteJSON(w, http.StatusOK, c)
}

// --- Public company endpoints ---

// HandleCompaniesList returns all companies (public).
// GET /companies
func (h *AuthHandlers) HandleCompaniesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	companies, err := h.companyRepo.List()
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if companies == nil {
		companies = []model.Company{}
	}

	httpres.WriteJSON(w, http.StatusOK, companies)
}

// HandleCompanyGet returns a company by ID (public).
// GET /companies/{id}
func (h *AuthHandlers) HandleCompanyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "company_id")
	if !ok {
		return
	}

	c, err := h.companyRepo.Get(id)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, c)
}

// HandleCompanyGetBySlug returns a company by slug (public).
// GET /companies/slug/{slug}
func (h *AuthHandlers) HandleCompanyGetBySlug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Extract slug from path: /companies/slug/{slug}
	path := r.URL.Path
	prefix := "/companies/slug/"
	if !strings.HasPrefix(path, prefix) {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	slug := strings.TrimPrefix(path, prefix)
	if slug == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "missing slug")
		return
	}

	c, err := h.companyRepo.GetBySlug(slug)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	httpres.WriteJSON(w, http.StatusOK, c)
}

// HandleCompanyProducts returns products for a company (public).
// GET /companies/{id}/products
func (h *AuthHandlers) HandleCompanyProducts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "company_id")
	if !ok {
		return
	}

	// Verify company exists
	if _, err := h.companyRepo.Get(id); err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	q := r.URL.Query().Get("q")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	sortStr := r.URL.Query().Get("sort")

	page := 1
	if pageStr != "" {
		p, _ := strconv.Atoi(pageStr)
		if p < 1 {
			p = 1
		}
		page = p
	}

	limit := 50
	if limitStr != "" {
		l, _ := strconv.Atoi(limitStr)
		if l < 1 {
			l = 1
		}
		if l > 200 {
			l = 200
		}
		limit = l
	}

	// Use turbo search with company filter
	turboSearch := h.getTurboSearch()
	if turboSearch == nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "turbo search not initialized")
		return
	}

	result, err := turboSearch.ListWithTurbo(db.TurboListParams{
		Q:         q,
		CompanyID: id,
		Sort:      sortStr,
		Page:      page,
		Limit:     limit,
	})
	if err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": result.Items,
		"total": result.Total,
		"page":  result.Page,
		"limit": result.Limit,
	})
}

// HandleAdminCreateTestCompanies creates 6-7 test companies for import testing.
// POST /admin/companies/create-test
func (h *AuthHandlers) HandleAdminCreateTestCompanies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Find or create admin user
	adminUser, err := h.userRepo.GetByEmail("admin@mako.com")
	if err != nil {
		adminUser = &model.User{
			Email: "admin@mako.com",
			Role:  model.RoleAdmin,
		}
		if err := h.userRepo.Create(adminUser, "admin123"); err != nil {
			httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}

	testCompanies := []struct {
		Name        string
		Description string
		Slug        string
		LogoURL     string
	}{
		{"Magazilla", "Крупнейший интернет-магазин электроники и товаров для дома", "magazilla", ""},
		{"DNS", "Цепочка магазинов цифровой техники и электроники", "dns-shop", ""},
		{"Ситилинк", "Онлайн-магазин электроники и бытовой техники", "citilink", ""},
		{"М.Видео", "Крупнейшая розничная сеть бытовой техники", "mvideo", ""},
		{"Эльдорадо", "Магазин бытовой техники и электроники", "eldorado", ""},
		{"Ozon", "Маркетплейс с широким ассортиментом товаров", "ozon", ""},
		{"Wildberries", "Крупнейший маркетплейс одежды и товаров для дома", "wildberries", ""},
	}

	var created []model.Company
	var existing []string

	for _, tc := range testCompanies {
		// Check if exists
		companies, _ := h.companyRepo.List()
		exists := false
		for _, c := range companies {
			if c.Name == tc.Name {
				existing = append(existing, tc.Name)
				exists = true
				break
			}
		}
		if exists {
			continue
		}

		c := &model.Company{
			Name:        tc.Name,
			Slug:        tc.Slug,
			Description: tc.Description,
			LogoURL:     tc.LogoURL,
			Contacts: model.CompanyContacts{
				Email:   "info@" + tc.Slug + ".ru",
				Website: "https://" + tc.Slug + ".ru",
			},
			Settings: model.CompanySettings{
				Currency:   "RUB",
				VatEnabled: false,
			},
			OwnerUserID: adminUser.ID,
			Status:      model.CompanyStatusVerified,
		}

		if err := h.companyRepo.Create(c); err != nil {
			fmt.Printf("WARN: create test company %s: %v\n", tc.Name, err)
			continue
		}
		created = append(created, *c)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"created":  created,
		"existing": existing,
		"total":    len(created) + len(existing),
	})
}

// HandleChangePassword changes the current user's password.
// POST /admin/change-password
// Body: { "oldPassword": "...", "newPassword": "..." }
// Available to any authenticated user (changes their own password).
func (h *AuthHandlers) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpres.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, ok := auth.ContextUserFrom(r)
	if !ok {
		httpres.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	var req ChangePasswordRequest
	if !httpres.ReadJSON(w, r, &req) {
		return
	}

	if req.OldPassword == "" || req.NewPassword == "" {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "oldPassword and newPassword are required")
		return
	}

	if len(req.NewPassword) < 6 {
		httpres.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "new password must be at least 6 characters")
		return
	}

	user, err := h.userRepo.GetByID(ctxUser.ID)
	if err != nil {
		httpres.WriteError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	// Verify old password
	if !h.userRepo.VerifyPassword(user, req.OldPassword) {
		httpres.WriteError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "current password is incorrect")
		return
	}

	// Update password
	if err := h.userRepo.UpdatePassword(user, req.NewPassword); err != nil {
		httpres.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Clear first_login flag
	if err := h.userRepo.Update(ctxUser.ID, func(u *model.User) {
		u.IsFirstLogin = false
	}); err != nil {
		fmt.Printf("WARN: failed to clear is_first_login: %v\n", err)
	}

	httpres.WriteJSON(w, http.StatusOK, map[string]string{"message": "password changed successfully"})
}

// turboSearch field and helpers
var _ = db.TurboListParams{}
