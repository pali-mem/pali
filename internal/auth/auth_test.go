package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestJWTAuthenticator_Authenticate(t *testing.T) {
	authenticator, err := NewJWTAuthenticator("secret", "pali")
	require.NoError(t, err)

	signed, err := GenerateTenantToken("secret", "pali", "tenant_1", 5*time.Minute)
	require.NoError(t, err)

	tenantID, err := authenticator.Authenticate(context.Background(), signed)
	require.NoError(t, err)
	require.Equal(t, "tenant_1", tenantID)
}

func TestMiddleware_UnauthorizedAndTenantContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticator, err := NewJWTAuthenticator("secret", "pali")
	require.NoError(t, err)

	r := gin.New()
	r.Use(Middleware(authenticator))
	r.GET("/protected", func(c *gin.Context) {
		tenantID, ok := TenantIDFromGin(c)
		require.True(t, ok)
		require.Equal(t, "tenant_2", tenantID)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	signed, err := GenerateTenantToken("secret", "pali", "tenant_2", 5*time.Minute)
	require.NoError(t, err)

	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestGenerateTenantToken_DefaultTTL(t *testing.T) {
	token, err := GenerateTenantToken("secret", "pali", "tenant_3", 0)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	authenticator, err := NewJWTAuthenticator("secret", "pali")
	require.NoError(t, err)
	tenantID, err := authenticator.Authenticate(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "tenant_3", tenantID)
}

func TestGenerateTenantToken_RejectsInvalidInput(t *testing.T) {
	_, err := GenerateTenantToken("", "pali", "tenant_1", time.Hour)
	require.Error(t, err)

	_, err = GenerateTenantToken("secret", "pali", "", time.Hour)
	require.Error(t, err)
}

func TestJWTAuthenticator_AuthenticateRejectsWrongSecret(t *testing.T) {
	token, err := GenerateTenantToken("secret", "pali", "tenant_1", time.Minute)
	require.NoError(t, err)

	wrongAuth, err := NewJWTAuthenticator("other-secret", "pali")
	require.NoError(t, err)

	_, err = wrongAuth.Authenticate(context.Background(), token)
	require.Error(t, err)
}

func TestMiddleware_RejectsWrongAuthScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticator, err := NewJWTAuthenticator("secret", "pali")
	require.NoError(t, err)

	r := gin.New()
	r.Use(Middleware(authenticator))
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Token nope")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_TrimmedBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authenticator, err := NewJWTAuthenticator("secret", "pali")
	require.NoError(t, err)

	r := gin.New()
	r.Use(Middleware(authenticator))
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	token, err := GenerateTenantToken("secret", "pali", "tenant_1", time.Minute)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer   "+token+"   ")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
