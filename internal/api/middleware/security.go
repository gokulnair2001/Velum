package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/velum/internal/config"
)

const (
	// SecurityHeaderKey is the header key for the API key
	SecurityHeaderKey = "X-Infra-Key"
)

// SecurityMiddleware creates a middleware that validates API keys
func SecurityMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If security is not enabled, skip validation
			if !cfg.Security.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Get API key from header
			apiKey := r.Header.Get(SecurityHeaderKey)
			if apiKey == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"message":"Missing API key. Please provide the X-Infra-Key header."}`))
				return
			}

			// Hash the provided key and compare with stored hash
			if !validateAPIKey(apiKey, cfg.Security.APIKeyHash) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"message":"Invalid API key."}`))
				return
			}

			// Key is valid, proceed to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// validateAPIKey hashes the provided key and compares it with the stored hash
// Uses constant-time comparison to prevent timing attacks
func validateAPIKey(providedKey, storedHash string) bool {
	// Compute SHA256 hash of the provided key
	hash := sha256.Sum256([]byte(providedKey))
	computedHash := hex.EncodeToString(hash[:])

	// Normalize both hashes to lowercase for comparison
	storedHash = strings.ToLower(storedHash)
	computedHash = strings.ToLower(computedHash)

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) == 1
}
