package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/GenshIv/makoshop/internal/auth"
	"github.com/GenshIv/makoshop/internal/db"
	"github.com/GenshIv/makoshop/internal/model"
)

type AuthHandlers struct {
	userRepo    *db.UserRepo
	companyRepo *db.CompanyRepo
	cartRepo    *db.CartRepo
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
	UserID int64          `json:"user_id"`
	Email  string         `json:"email"`
	Role   model.UserRole `json:"role"`
	Token  string         `json:"token"`
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

// --- Handlers ---

func (h *AuthHandlers) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req RegisterRequest
	if !readJSON(w, r, &req) {
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "email and password are required")
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
			writeError(w, http.StatusConflict, "EMAIL_EXISTS", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	token, err := auth.GenerateToken(user, h.secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_ERROR", "failed to generate token")
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		Token:  token,
	})
}

func (h *AuthHandlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req LoginRequest
	if !readJSON(w, r, &req) {
		return
	}

	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "email and password are required")
		return
	}

	user, err := h.userRepo.GetByEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	if user.Status == model.UserStatusBlocked {
		writeError(w, http.StatusForbidden, "ACCOUNT_BLOCKED", "account is blocked")
		return
	}

	if !h.userRepo.VerifyPassword(user, req.Password) {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	token, err := auth.GenerateToken(user, h.secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_ERROR", "failed to generate token")
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

	writeJSON(w, http.StatusOK, AuthResponse{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		Token:  token,
	})
}

func (h *AuthHandlers) HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, ok := auth.ContextUserFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	user, err := h.userRepo.GetByID(ctxUser.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      user.ID,
		"email":   user.Email,
		"role":    user.Role,
		"status":  user.Status,
		"profile": user.Profile,
	})
}

func (h *AuthHandlers) HandleUpdateMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	ctxUser, ok := auth.ContextUserFrom(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	var req UpdateProfileRequest
	if !readJSON(w, r, &req) {
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	user, _ := h.userRepo.GetByID(ctxUser.ID)
	writeJSON(w, http.StatusOK, user)
}

// --- Admin user handlers ---

func (h *AuthHandlers) HandleAdminUsersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if users == nil {
		users = []model.User{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"total": total,
		"items": users,
	})
}

func (h *AuthHandlers) HandleAdminUserGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "user_id")
	if !ok {
		return
	}

	user, err := h.userRepo.GetByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *AuthHandlers) HandleAdminUserUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "user_id")
	if !ok {
		return
	}

	var req AdminUpdateUserRequest
	if !readJSON(w, r, &req) {
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	user, _ := h.userRepo.GetByID(id)
	writeJSON(w, http.StatusOK, user)
}

// --- Company handlers (admin) ---

type CreateCompanyRequest struct {
	Name        string                 `json:"name"`
	LegalInfo   model.CompanyLegalInfo `json:"legal_info,omitempty"`
	Settings    model.CompanySettings  `json:"settings,omitempty"`
	OwnerUserID int64                  `json:"owner_user_id"`
}

func (h *AuthHandlers) HandleAdminCompaniesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	companies, err := h.companyRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	if companies == nil {
		companies = []model.Company{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": companies,
		"total": len(companies),
	})
}

func (h *AuthHandlers) HandleAdminCompanyCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	var req CreateCompanyRequest
	if !readJSON(w, r, &req) {
		return
	}

	if req.Name == "" || req.OwnerUserID == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "name and owner_user_id are required")
		return
	}

	c := &model.Company{
		Name:        req.Name,
		LegalInfo:   req.LegalInfo,
		Settings:    req.Settings,
		OwnerUserID: req.OwnerUserID,
	}

	if err := h.companyRepo.Create(c); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, c)
}

func (h *AuthHandlers) HandleAdminCompanyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "company_id")
	if !ok {
		return
	}

	c, err := h.companyRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "company not found")
		return
	}

	writeJSON(w, http.StatusOK, c)
}

func (h *AuthHandlers) HandleAdminCompanyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	id, ok := parseID(w, r, "company_id")
	if !ok {
		return
	}

	var req struct {
		Name      string                  `json:"name,omitempty"`
		LegalInfo *model.CompanyLegalInfo `json:"legal_info,omitempty"`
		Settings  *model.CompanySettings  `json:"settings,omitempty"`
		Status    model.CompanyStatus     `json:"status,omitempty"`
	}
	if !readJSON(w, r, &req) {
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
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ := h.companyRepo.Get(id)
	writeJSON(w, http.StatusOK, c)
}

// HandleAdminCompanyVerify verifies or blocks a company.
// PATCH /admin/companies/{id}/verify
// Body: {"status": "verified" | "blocked"}
func (h *AuthHandlers) HandleAdminCompanyVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "")
		return
	}

	// Path: /admin/companies/{id}/verify
	parts := strings.Split(r.URL.Path, "/")
	// Expected: ["", "admin", "companies", "{id}", "verify"]
	if len(parts) < 5 || parts[4] != "verify" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid path")
		return
	}
	id, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid company id")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !readJSON(w, r, &req) {
		return
	}

	var newStatus model.CompanyStatus
	switch req.Status {
	case "verified":
		newStatus = model.CompanyStatusVerified
	case "blocked":
		newStatus = model.CompanyStatusBlocked
	default:
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "status must be 'verified' or 'blocked'")
		return
	}

	if err := h.companyRepo.Update(id, func(c *model.Company) {
		c.Status = newStatus
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	c, _ := h.companyRepo.Get(id)
	writeJSON(w, http.StatusOK, c)
}
