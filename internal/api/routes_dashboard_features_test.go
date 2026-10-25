package api

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func createDashboardTenant(t *testing.T, r *gin.Engine, id string) {
	t.Helper()
	form := url.Values{}
	form.Set("id", id)
	form.Set("name", "Name "+id)
	require.Equal(t, http.StatusSeeOther, postDashboardForm(r, "/dashboard/tenants", form).Code)
}

// createDashboardMemory stores a memory through the dashboard and returns its ID.
func createDashboardMemory(t *testing.T, r *gin.Engine, tenantID, content, tier, tags string) string {
	t.Helper()
	form := url.Values{}
	form.Set("tenant_id", tenantID)
	form.Set("content", content)
	form.Set("tier", tier)
	form.Set("tags", tags)
	w := postDashboardForm(r, "/dashboard/memories", form)
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return strings.TrimPrefix(loc.Path, "/dashboard/memories/view/")
}

var dashboardRowPattern = regexp.MustCompile(`class="memory-title-link">(pagemem-\d+)<`)

func dashboardRowContents(body string) []string {
	var out []string
	for _, m := range dashboardRowPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestDashboardMemoriesPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	createDashboardTenant(t, r, "tenant_page")
	for i := 0; i < 60; i++ {
		tier := "semantic"
		if i%2 == 1 {
			tier = "episodic"
		}
		createDashboardMemory(t, r, "tenant_page", fmt.Sprintf("pagemem-%03d", i), tier, "")
	}

	w := performRequest(r, http.MethodGet, "/dashboard/memories?tenant_id=tenant_page&tier=semantic", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, dashboardRowContents(w.Body.String()), 30)
	require.NotContains(t, w.Body.String(), "rel=\"next\"")

	w = performRequest(r, http.MethodGet, "/dashboard/memories?tenant_id=tenant_page", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	page1 := dashboardRowContents(w.Body.String())
	require.Len(t, page1, 50)
	body := w.Body.String()
	require.Contains(t, body, "Showing 1-50")
	require.NotContains(t, body, "rel=\"prev\"")
	require.Contains(t, body, "rel=\"next\"")
	require.Contains(t, body, "tenant_id=tenant_page&amp;offset=50")

	w = performRequest(r, http.MethodGet, "/dashboard/memories?tenant_id=tenant_page&offset=50", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	page2 := dashboardRowContents(w.Body.String())
	require.Len(t, page2, 10)
	body = w.Body.String()
	require.Contains(t, body, "Showing 51-60")
	require.NotContains(t, body, "rel=\"next\"")
	require.Contains(t, body, "rel=\"prev\"")

	seen := map[string]bool{}
	for _, c := range append(page1, page2...) {
		require.False(t, seen[c], "duplicate across pages: %s", c)
		seen[c] = true
	}
	require.Len(t, seen, 60)

	// Filters are preserved in pagination links.
	for i := 0; i < 60; i++ {
		createDashboardMemory(t, r, "tenant_page", fmt.Sprintf("pagemem-%03d", 100+i), "semantic", "")
	}
	w = performRequest(r, http.MethodGet, "/dashboard/memories?tenant_id=tenant_page&tier=semantic", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, dashboardRowContents(w.Body.String()), 50)
	require.Contains(t, w.Body.String(), "tier=semantic&amp;offset=50")
}

func TestDashboardEditMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	createDashboardTenant(t, r, "tenant_edit")
	createDashboardTenant(t, r, "tenant_other")
	id := createDashboardMemory(t, r, "tenant_edit", "original editable content", "semantic", "a,b")

	editPath := "/dashboard/memories/" + id + "/edit"
	form := url.Values{}
	form.Set("tenant_id", "tenant_edit")
	form.Set("content", "rewritten <b>content</b>")
	form.Set("tags", "x, y")
	form.Set("tier", "working")
	form.Set("importance", "0.70")
	form.Set("filter_q", "rewritten")
	w := postDashboardForm(r, editPath, form)
	require.Equal(t, http.StatusSeeOther, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "/dashboard/memories/view/"+id)
	require.Contains(t, loc, "info=memory+updated")
	require.Contains(t, loc, "q=rewritten")

	w = performRequest(r, http.MethodGet, loc, "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "memory updated")
	require.Contains(t, body, "rewritten &lt;b&gt;content&lt;/b&gt;")
	require.NotContains(t, body, "rewritten <b>content</b>")
	require.Contains(t, body, "Importance 0.70")
	require.Contains(t, body, `value="x, y"`)
	require.Contains(t, body, `<option value="working" selected>`)

	// Validation failures re-render the form with an inline error.
	cases := map[string]func(url.Values){
		"empty content":  func(v url.Values) { v.Set("content", "   ") },
		"bad importance": func(v url.Values) { v.Set("importance", "1.5") },
		"non-numeric":    func(v url.Values) { v.Set("importance", "high") },
		"bad tier":       func(v url.Values) { v.Set("tier", "bogus") },
	}
	for name, mutate := range cases {
		bad := url.Values{}
		for k, v := range form {
			bad[k] = v
		}
		mutate(bad)
		w = postDashboardForm(r, editPath, bad)
		require.Equal(t, http.StatusUnprocessableEntity, w.Code, name)
		require.Contains(t, w.Body.String(), `role="alert"`, name)
		require.Contains(t, w.Body.String(), "Edit memory", name)
	}
	w = performRequest(r, http.MethodGet, "/dashboard/memories/view/"+id+"?tenant_id=tenant_edit", "", nil)
	require.Contains(t, w.Body.String(), "rewritten &lt;b&gt;content&lt;/b&gt;")

	// A different tenant cannot edit the memory.
	other := url.Values{}
	other.Set("tenant_id", "tenant_other")
	other.Set("content", "hijacked")
	other.Set("tags", "")
	other.Set("tier", "semantic")
	other.Set("importance", "0.50")
	w = postDashboardForm(r, editPath, other)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Contains(t, w.Header().Get("Location"), "error=")
	w = performRequest(r, http.MethodGet, "/dashboard/memories/view/"+id+"?tenant_id=tenant_edit", "", nil)
	require.NotContains(t, w.Body.String(), "hijacked")

	// Resubmitting unchanged values is a no-op, not an error.
	w = postDashboardForm(r, editPath, form)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Contains(t, w.Header().Get("Location"), "info=no+changes")
}

func TestDashboardMemoryDetailRecallFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	createDashboardTenant(t, r, "tenant_recall")
	id := createDashboardMemory(t, r, "tenant_recall", "recall field memory", "semantic", "")

	w := performRequest(r, http.MethodGet, "/dashboard/memories/view/"+id+"?tenant_id=tenant_recall", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "Recall count")
	require.Contains(t, body, "Last recalled")
	require.Contains(t, body, "Last accessed")
	// Never recalled yet; last accessed is stamped at creation.
	require.Regexp(t, `(?s)Recall count</dt>\s*<dd>0</dd>`, body)
	require.Regexp(t, `(?s)Last recalled</dt>\s*<dd>Never</dd>`, body)
	require.Regexp(t, `(?s)Last accessed</dt>\s*<dd>\d{4}-\d{2}-\d{2} \d{2}:\d{2}</dd>`, body)

	searchBody := `{"tenant_id":"tenant_recall","query":"recall field memory","top_k":5}`
	w = performRequest(r, http.MethodPost, "/v1/memory/search", searchBody, map[string]string{"Content-Type": "application/json"})
	require.Equal(t, http.StatusOK, w.Code)

	w = performRequest(r, http.MethodGet, "/dashboard/memories/view/"+id+"?tenant_id=tenant_recall", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body = w.Body.String()
	require.Regexp(t, `(?s)Recall count</dt>\s*<dd>[1-9]\d*</dd>`, body)
	require.Regexp(t, `(?s)Last recalled</dt>\s*<dd>\d{4}-\d{2}-\d{2} \d{2}:\d{2}</dd>`, body)
}

func TestDashboardStatsBreakdown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	createDashboardTenant(t, r, "tenant_stats")
	createDashboardMemory(t, r, "tenant_stats", "stats memory one", "semantic", "")
	createDashboardMemory(t, r, "tenant_stats", "stats memory two", "semantic", "")
	createDashboardMemory(t, r, "tenant_stats", "stats memory three", "episodic", "")

	w := performRequest(r, http.MethodGet, "/dashboard/stats", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "Per-tenant breakdown")
	require.Contains(t, body, "<code>tenant_stats</code>")
	require.Contains(t, body, "semantic 2")
	require.Contains(t, body, "episodic 1")
	require.Contains(t, body, "Total recalls")
	require.Contains(t, body, "Newest memory")
	require.NotContains(t, body, "None</td>")
}
