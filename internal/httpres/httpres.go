// Package httpres provides shared helpers for writing HTTP responses.
package httpres

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
// HTML escaping is disabled so that characters like <, >, & are not
// escaped to \u003c, \u003e, \u0026 in the output.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteError writes a JSON error response using the nested format:
//
//	{"error": {"code": code, "message": message}}
//
// This is the format used by the main API.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

// WriteErrorFlat writes a JSON error response using the flat format:
//
//	{"error": code, "message": message}
//
// This is the format used by the stats endpoints.
func WriteErrorFlat(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]interface{}{
		"error":   code,
		"message": message,
	})
}

// ReadJSON decodes a JSON request body into v. On failure it writes a
// 400 BAD_REQUEST error response and returns false.
func ReadJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return false
	}
	return true
}
