//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pali-mem/pali/internal/api"
	apiauth "github.com/pali-mem/pali/internal/auth"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestMultiTenantRESTIsolationStrictJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, cleanup := newAuthenticatedIntegrationRouter(t)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	tokenA := mustGenerateTenantJWT(t, "tenant_a", time.Hour)
	tokenB := mustGenerateTenantJWT(t, "tenant_b", time.Hour)

	require.Equal(t, http.StatusCreated, postJSONAuth(t, router, "/v1/tenants", tokenA, `{"id":"tenant_a","name":"Tenant A"}`).Code)
	require.Equal(t, http.StatusCreated, postJSONAuth(t, router, "/v1/tenants", tokenB, `{"id":"tenant_b","name":"Tenant B"}`).Code)

	require.Equal(t, http.StatusForbidden, postJSONAuth(t, router, "/v1/tenants", tokenA, `{"id":"tenant_b","name":"Wrong Tenant"}`).Code)

	storeA := postJSONAuth(t, router, "/v1/memory", tokenA, `{"tenant_id":"tenant_a","content":"tenant a private note","tier":"semantic"}`)
	require.Equal(t, http.StatusCreated, storeA.Code)
	var createdA struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(storeA.Body.Bytes(), &createdA))
	require.NotEmpty(t, createdA.ID)

	storeB := postJSONAuth(t, router, "/v1/memory", tokenB, `{"tenant_id":"tenant_b","content":"tenant b private note","tier":"semantic"}`)
	require.Equal(t, http.StatusCreated, storeB.Code)
	var createdB struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(storeB.Body.Bytes(), &createdB))
	require.NotEmpty(t, createdB.ID)

	require.Equal(t, http.StatusForbidden, postJSONAuth(t, router, "/v1/memory", tokenA, `{"tenant_id":"tenant_b","content":"cross-tenant write","tier":"semantic"}`).Code)
	require.Equal(t, http.StatusForbidden, postJSONAuth(t, router, "/v1/memory/search", tokenA, `{"tenant_id":"tenant_b","query":"private","top_k":5}`).Code)
	require.Equal(t, http.StatusForbidden, postJSONAuth(t, router, "/v1/memory/batch", tokenA, `{"items":[{"tenant_id":"tenant_a","content":"ok","tier":"semantic"},{"tenant_id":"tenant_b","content":"not ok","tier":"semantic"}]}`).Code)

	searchA := postJSONAuth(t, router, "/v1/memory/search", tokenA, `{"tenant_id":"tenant_a","query":"private","top_k":10}`)
	require.Equal(t, http.StatusOK, searchA.Code)
	var searchAResp struct {
		Items []struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
			Content  string `json:"content"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searchA.Body.Bytes(), &searchAResp))
	require.NotEmpty(t, searchAResp.Items)
	for _, item := range searchAResp.Items {
		require.Equal(t, "tenant_a", item.TenantID)
		require.NotEqual(t, createdB.ID, item.ID)
	}

	require.Equal(t, http.StatusForbidden, getAuth(t, router, "/v1/tenants/tenant_b/stats", tokenA).Code)
	require.Equal(t, http.StatusOK, getAuth(t, router, "/v1/tenants/tenant_a/stats", tokenA).Code)

	ingestA := postJSONAuth(t, router, "/v1/memory/ingest", tokenA, `{"tenant_id":"tenant_a","content":"queued tenant a ingest","tier":"auto"}`)
	require.Equal(t, http.StatusAccepted, ingestA.Code)
	var ingestResp struct {
		JobIDs []string `json:"job_ids"`
	}
	require.NoError(t, json.Unmarshal(ingestA.Body.Bytes(), &ingestResp))
	require.NotEmpty(t, ingestResp.JobIDs)

	require.Equal(t, http.StatusForbidden, getAuth(t, router, "/v1/memory/jobs?tenant_id=tenant_b", tokenA).Code)
	require.Equal(t, http.StatusForbidden, getAuth(t, router, "/v1/memory/jobs/"+ingestResp.JobIDs[0], tokenB).Code)
	require.Equal(t, http.StatusOK, getAuth(t, router, "/v1/memory/jobs?tenant_id=tenant_a", tokenA).Code)
	require.Equal(t, http.StatusOK, getAuth(t, router, "/v1/memory/jobs/"+ingestResp.JobIDs[0], tokenA).Code)

	require.Equal(t, http.StatusForbidden, deleteAuth(t, router, "/v1/memory/"+createdB.ID+"?tenant_id=tenant_b", tokenA).Code)
	require.Equal(t, http.StatusNotFound, deleteAuth(t, router, "/v1/memory/"+createdA.ID+"?tenant_id=tenant_b", tokenB).Code)
	require.Equal(t, http.StatusNoContent, deleteAuth(t, router, "/v1/memory/"+createdA.ID+"?tenant_id=tenant_a", tokenA).Code)
}

func TestMultiTenantConcurrentIsolationInvariants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, cleanup := newAuthenticatedIntegrationRouter(t)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	tenantIDs := []string{"tenant_c1", "tenant_c2", "tenant_c3", "tenant_c4"}
	tokens := make(map[string]string, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		tokens[tenantID] = mustGenerateTenantJWT(t, tenantID, time.Hour)
		resp := postJSONAuth(t, router, "/v1/tenants", tokens[tenantID], fmt.Sprintf(`{"id":%q,"name":%q}`, tenantID, tenantID))
		require.Equal(t, http.StatusCreated, resp.Code)
	}

	var wg sync.WaitGroup
	for _, tenantID := range tenantIDs {
		tenantID := tenantID
		token := tokens[tenantID]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				content := fmt.Sprintf("%s event %02d", tenantID, i)
				store := postJSONAuth(t, router, "/v1/memory", token, fmt.Sprintf(`{"tenant_id":%q,"content":%q,"tier":"semantic"}`, tenantID, content))
				require.Equal(t, http.StatusCreated, store.Code)

				search := postJSONAuth(t, router, "/v1/memory/search", token, fmt.Sprintf(`{"tenant_id":%q,"query":"%s","top_k":20}`, tenantID, tenantID))
				require.Equal(t, http.StatusOK, search.Code)

				var result struct {
					Items []struct {
						TenantID string `json:"tenant_id"`
						Content  string `json:"content"`
					} `json:"items"`
				}
				require.NoError(t, json.Unmarshal(search.Body.Bytes(), &result))
				for _, item := range result.Items {
					require.Equal(t, tenantID, item.TenantID)
				}
			}
		}()
	}
	wg.Wait()

	for _, tenantID := range tenantIDs {
		search := postJSONAuth(t, router, "/v1/memory/search", tokens[tenantID], fmt.Sprintf(`{"tenant_id":%q,"query":"tenant_","top_k":100}`, tenantID))
		require.Equal(t, http.StatusOK, search.Code)
		var result struct {
			Items []struct {
				TenantID string `json:"tenant_id"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(search.Body.Bytes(), &result))
		require.NotEmpty(t, result.Items)
		for _, item := range result.Items {
			require.Equal(t, tenantID, item.TenantID)
		}
	}
}

func TestMultiTenantAuthBoundaryFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, cleanup := newAuthenticatedIntegrationRouter(t)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	validToken := mustGenerateTenantJWT(t, "tenant_auth", time.Hour)
	require.Equal(t, http.StatusCreated, postJSONAuth(t, router, "/v1/tenants", validToken, `{"id":"tenant_auth","name":"Tenant Auth"}`).Code)

	require.Equal(t, http.StatusUnauthorized, postJSONNoAuth(t, router, "/v1/memory", `{"tenant_id":"tenant_auth","content":"missing auth","tier":"semantic"}`).Code)

	wrongSecret := mustGenerateTenantJWTWithClaims(t, []byte("wrong-secret"), apiauth.TenantClaims{
		TenantID: "tenant_auth",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "pali",
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	})
	require.Equal(t, http.StatusUnauthorized, postJSONAuth(t, router, "/v1/memory", wrongSecret, `{"tenant_id":"tenant_auth","content":"wrong secret","tier":"semantic"}`).Code)

	wrongIssuer := mustGenerateTenantJWTWithClaims(t, []byte(testJWTSecret), apiauth.TenantClaims{
		TenantID: "tenant_auth",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "not-pali",
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	})
	require.Equal(t, http.StatusUnauthorized, postJSONAuth(t, router, "/v1/memory", wrongIssuer, `{"tenant_id":"tenant_auth","content":"wrong issuer","tier":"semantic"}`).Code)

	expired := mustGenerateTenantJWTWithClaims(t, []byte(testJWTSecret), apiauth.TenantClaims{
		TenantID: "tenant_auth",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testJWTIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(-time.Hour)),
		},
	})
	require.Equal(t, http.StatusUnauthorized, postJSONAuth(t, router, "/v1/memory", expired, `{"tenant_id":"tenant_auth","content":"expired","tier":"semantic"}`).Code)

	missingTenantClaim := mustGenerateTenantJWTWithClaims(t, []byte(testJWTSecret), apiauth.TenantClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testJWTIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	})
	require.Equal(t, http.StatusUnauthorized, postJSONAuth(t, router, "/v1/memory", missingTenantClaim, `{"tenant_id":"tenant_auth","content":"missing claim","tier":"semantic"}`).Code)

	malformed := httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(`{"tenant_id":"tenant_auth","content":"bad scheme","tier":"semantic"}`))
	malformed.Header.Set("Authorization", "Token "+validToken)
	malformed.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, malformed)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

const (
	testJWTSecret = "integration-secret"
	testJWTIssuer = "pali"
)

func newAuthenticatedIntegrationRouter(t *testing.T) (*gin.Engine, func() error) {
	t.Helper()

	cfg := testutil.MustLoadProviderConfig(t, "mock")
	dbPath := filepath.Join(t.TempDir(), "multitenancy.sqlite")
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:%s?cache=shared", dbPath)
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = testJWTSecret
	cfg.Auth.Issuer = testJWTIssuer

	router, cleanup, err := api.NewRouter(cfg)
	require.NoError(t, err)
	return router, cleanup
}

func mustGenerateTenantJWT(t *testing.T, tenantID string, ttl time.Duration) string {
	t.Helper()
	token, err := apiauth.GenerateTenantToken(testJWTSecret, testJWTIssuer, tenantID, ttl)
	require.NoError(t, err)
	return token
}

func mustGenerateTenantJWTWithClaims(t *testing.T, secret []byte, claims apiauth.TenantClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	require.NoError(t, err)
	return signed
}

func postJSONNoAuth(t *testing.T, router *gin.Engine, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func postJSONAuth(t *testing.T, router *gin.Engine, path string, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func getAuth(t *testing.T, router *gin.Engine, path string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func deleteAuth(t *testing.T, router *gin.Engine, path string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
