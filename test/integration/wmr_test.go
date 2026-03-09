//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/api"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestWMRRankingAndTouchIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := testutil.MustLoadProviderConfig(t, "mock")
	dbPath := filepath.Join(t.TempDir(), "wmr.sqlite")
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:%s?cache=shared", dbPath)

	router, cleanup, err := api.NewRouter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	db, err := sqliterepo.Open(context.Background(), cfg.Database.SQLiteDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	postJSON := func(path string, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusCreated, postJSON("/v1/tenants", `{"id":"tenant_wmr","name":"Tenant WMR"}`).Code)

	store := func(content string) string {
		t.Helper()
		resp := postJSON(
			"/v1/memory",
			fmt.Sprintf(`{"tenant_id":"tenant_wmr","content":%q,"tier":"semantic"}`, content),
		)
		require.Equal(t, http.StatusCreated, resp.Code)
		var out struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
		require.NotEmpty(t, out.ID)
		return out.ID
	}

	oldID := store("Release codename is atlas.")
	newID := store("Release codename is atlas.")

	oldTS := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339Nano)
	newTS := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
	_, err = db.Exec(
		`UPDATE memories SET importance_score = ?, last_accessed_at = ?, updated_at = ? WHERE id = ?`,
		0.10, oldTS, oldTS, oldID,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		`UPDATE memories SET importance_score = ?, last_accessed_at = ?, updated_at = ? WHERE id = ?`,
		0.95, newTS, newTS, newID,
	)
	require.NoError(t, err)

	searchResp := postJSON("/v1/memory/search", `{"tenant_id":"tenant_wmr","query":"release codename atlas","top_k":2}`)
	require.Equal(t, http.StatusOK, searchResp.Code)
	var firstSearch struct {
		Items []struct {
			ID          string `json:"id"`
			RecallCount int    `json:"recall_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searchResp.Body.Bytes(), &firstSearch))
	require.Len(t, firstSearch.Items, 2)
	require.Equal(t, newID, firstSearch.Items[0].ID)
	require.Equal(t, oldID, firstSearch.Items[1].ID)

	secondResp := postJSON("/v1/memory/search", `{"tenant_id":"tenant_wmr","query":"release codename atlas","top_k":2}`)
	require.Equal(t, http.StatusOK, secondResp.Code)
	var secondSearch struct {
		Items []struct {
			ID          string `json:"id"`
			RecallCount int    `json:"recall_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(secondResp.Body.Bytes(), &secondSearch))
	require.Len(t, secondSearch.Items, 2)

	recalls := map[string]int{}
	for _, item := range secondSearch.Items {
		recalls[item.ID] = item.RecallCount
	}
	require.GreaterOrEqual(t, recalls[newID], 1)
	require.GreaterOrEqual(t, recalls[oldID], 1)
}
