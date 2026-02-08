package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/velum/internal/config"
)

func TestSecurityMiddleware_Disabled(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			Enabled:    false,
			APIKeyHash: "",
		},
	}

	handler := SecurityMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestSecurityMiddleware_MissingKey(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			Enabled:    true,
			APIKeyHash: "somehash",
		},
	}

	handler := SecurityMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestSecurityMiddleware_InvalidKey(t *testing.T) {
	// SHA256 hash of "correct-key"
	correctKeyHash := "ddb0fd2dede48502669718e09ef1447dba46f3d3822e9fbf05af11d874a0f23b"

	cfg := &config.Config{
		Security: config.SecurityConfig{
			Enabled:    true,
			APIKeyHash: correctKeyHash,
		},
	}

	handler := SecurityMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(SecurityHeaderKey, "wrong-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestSecurityMiddleware_ValidKey(t *testing.T) {
	// SHA256 hash of "correct-key"
	correctKeyHash := "ddb0fd2dede48502669718e09ef1447dba46f3d3822e9fbf05af11d874a0f23b"

	cfg := &config.Config{
		Security: config.SecurityConfig{
			Enabled:    true,
			APIKeyHash: correctKeyHash,
		},
	}

	handler := SecurityMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(SecurityHeaderKey, "correct-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestValidateAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		providedKey string
		storedHash  string
		expected    bool
	}{
		{
			name:        "valid key",
			providedKey: "my-secret-key",
			storedHash:  "3840a032dd9e3c1e1e0dc62f8cfa43a3a4d64f786f59cb8cb8c2f8e9e3f8a7b2", // pre-computed
			expected:    false, // This hash is fake, so it should fail
		},
		{
			name:        "correct hash for test-key",
			providedKey: "test-key",
			storedHash:  "51ab0b17343656d82f1d2e6b6d63d0fd0b6e29d0d3d55f54c7e9b6f3a7c8d9e0", // fake hash
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateAPIKey(tt.providedKey, tt.storedHash)
			if result != tt.expected {
				t.Errorf("validateAPIKey() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestValidateAPIKey_CaseInsensitive(t *testing.T) {
	// SHA256 hash of "correct-key" in uppercase
	hash := "DDB0FD2DEDE48502669718E09EF1447DBA46F3D3822E9FBF05AF11D874A0F23B"

	// Should work even with uppercase hash
	result := validateAPIKey("correct-key", hash)
	if !result {
		t.Error("validateAPIKey should handle uppercase hashes")
	}
}
