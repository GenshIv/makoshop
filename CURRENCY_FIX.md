# Currency Settings Fix

## Problem

1. Currency was not being saved in company settings from the admin panel
2. Default currency was RUB instead of PLN

## Root Cause

The frontend was sending `currency` as a top-level field in the PATCH request, but the backend handler didn't have a `currency` field in the request struct - it expected the currency to be in `settings.currency`.

## Changes Made

### 1. Backend Handler Fix (`internal/api/auth_handlers.go`)

Added `Currency` field to the request struct:
```go
Currency string `json:"currency,omitempty"` // top-level currency (mapped to settings)
```

Added logic to map the top-level currency to settings:
```go
// Handle top-level currency field (map to settings)
if req.Currency != "" {
    c.Settings.Currency = req.Currency
}
```

### 2. Default Currency Change (`internal/db/company_repo.go`)

Changed default currency from RUB to PLN:
```go
if c.Settings.Currency == "" {
    c.Settings.Currency = "PLN"
}
```

## Testing

To test the fix:
1. Go to Admin Panel → Companies
2. Click on a company → Settings
3. Change the currency (e.g., to "USD" or "EUR")
4. Save the settings
5. Reload the page and verify the currency was saved

## Files Modified

- `internal/api/auth_handlers.go` - Added currency field handling
- `internal/db/company_repo.go` - Changed default currency to PLN
