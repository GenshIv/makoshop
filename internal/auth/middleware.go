package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/GenshIv/makoshop/internal/model"
)

type contextKey string

const (
	userContextKey contextKey = "user"
)

type ContextUser struct {
	ID   int64
	Role model.UserRole
}

func ContextUserFrom(r *http.Request) (*ContextUser, bool) {
	v := r.Context().Value(userContextKey)
	if v == nil {
		return nil, false
	}
	u, ok := v.(*ContextUser)
	return u, ok
}

type JWTMiddleware struct {
	secret string
}

func NewJWTMiddleware(secret string) *JWTMiddleware {
	return &JWTMiddleware{secret: secret}
}

// RequireAuth validates JWT and puts user into context.
func (m *JWTMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		tokenString := extractToken(r)
		if tokenString == "" {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"missing authorization token"}}`, http.StatusUnauthorized)
			return
		}

		claims, err := ValidateToken(tokenString, m.secret)
		if err != nil {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"invalid or expired token"}}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, &ContextUser{
			ID:   claims.UserID,
			Role: claims.Role,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole validates JWT and ensures user has one of the required roles.
func (m *JWTMiddleware) RequireRole(next http.Handler, roles ...model.UserRole) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := extractToken(r)
		if tokenString == "" {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"missing authorization token"}}`, http.StatusUnauthorized)
			return
		}

		claims, err := ValidateToken(tokenString, m.secret)
		if err != nil {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"invalid or expired token"}}`, http.StatusUnauthorized)
			return
		}

		allowed := false
		for _, role := range roles {
			if claims.Role == role {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, `{"error":{"code":"FORBIDDEN","message":"insufficient permissions"}}`, http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, &ContextUser{
			ID:   claims.UserID,
			Role: claims.Role,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth validates JWT if present and puts user into context.
// If no token or invalid token, continues without user context.
func (m *JWTMiddleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenString := extractToken(r)
		if tokenString == "" {
			// No token — proceed without user context
			next.ServeHTTP(w, r)
			return
		}

		claims, err := ValidateToken(tokenString, m.secret)
		if err != nil {
			// Invalid token — proceed without user context
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, &ContextUser{
			ID:   claims.UserID,
			Role: claims.Role,
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}
	return parts[1]
}
