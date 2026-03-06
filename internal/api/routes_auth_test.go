package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/auth"
	"github.com/vein05/pali/internal/config"
)

func TestAuthMiddlewareAndTenantEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Defaults()
	cfg.Embedding.Provider = "mock"
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:api_auth_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "secret"
	cfg.Auth.Issuer = "pali"

	r, cleanup, err := NewRouter(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, cleanup()) }()

	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(`{"id":"tenant_1","name":"Tenant One"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	tokenTenant1 := mustSignTenantToken(t, "secret", "pali", "tenant_1")

	req = httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(`{"id":"tenant_2","name":"Tenant Two"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenTenant1)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(`{"id":"tenant_1","name":"Tenant One"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenTenant1)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(`{"tenant_id":"tenant_1","content":"secure memory","tier":"semantic"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenTenant1)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/v1/memory/batch", bytes.NewBufferString(`{"items":[{"tenant_id":"tenant_1","content":"secure batch memory","tier":"semantic"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenTenant1)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
}

func mustSignTenantToken(t *testing.T, secret, issuer, tenantID string) string {
	t.Helper()
	claims := auth.TenantClaims{
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}
