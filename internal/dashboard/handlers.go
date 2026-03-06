package dashboard

import (
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/domain"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Handlers struct {
	memoryService *corememory.Service
	tenantService *coretenant.Service
}

type TenantView struct {
	ID          string
	Name        string
	CreatedAt   string
	MemoryCount int64
}

type MemoryView struct {
	ID         string
	TenantID   string
	Content    string
	Tier       string
	Tags       string
	Importance string
	UpdatedAt  string
	CreatedAt  string
	AccessedAt string
}

type MemoriesPageData struct {
	Page             string
	Error            string
	Info             string
	SelectedTenantID string
	Query            string
	Tenants          []TenantView
	Memories         []MemoryView
}

type TenantsPageData struct {
	Page    string
	Error   string
	Info    string
	Tenants []TenantView
}

type StatsPageData struct {
	Page         string
	Error        string
	Info         string
	TenantCount  int
	MemoryCount  int64
	TopTenantID  string
	TopTenantMem int64
}

func NewHandlers(memoryService *corememory.Service, tenantService *coretenant.Service) *Handlers {
	return &Handlers{
		memoryService: memoryService,
		tenantService: tenantService,
	}
}

func (h *Handlers) Index(c *gin.Context) {
	c.Redirect(http.StatusFound, "/dashboard/memories")
}

func (h *Handlers) Memories(c *gin.Context) {
	tenants, err := h.listTenantsWithCounts(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load tenants failed"})
		return
	}

	selectedTenantID := strings.TrimSpace(c.Query("tenant_id"))
	if selectedTenantID == "" && len(tenants) > 0 {
		selectedTenantID = tenants[0].ID
	}
	query := strings.TrimSpace(c.Query("q"))

	memories := []MemoryView{}
	var loadErr string
	if selectedTenantID != "" {
		var items []domain.Memory
		var err error
		if query != "" {
			items, err = h.memoryService.Search(c.Request.Context(), selectedTenantID, query, 50)
		} else {
			items, err = h.memoryService.List(c.Request.Context(), selectedTenantID, 50)
		}
		if err != nil {
			loadErr = err.Error()
		} else {
			memories = mapMemoryViews(items)
		}
	}

	h.render(c, "memories.html", MemoriesPageData{
		Page:             "memories",
		Error:            loadErr,
		Info:             c.Query("info"),
		SelectedTenantID: selectedTenantID,
		Query:            query,
		Tenants:          tenants,
		Memories:         memories,
	})
}

func (h *Handlers) CreateMemory(c *gin.Context) {
	tenantID := strings.TrimSpace(c.PostForm("tenant_id"))
	content := strings.TrimSpace(c.PostForm("content"))
	tier := strings.TrimSpace(c.PostForm("tier"))
	tags := parseCommaList(c.PostForm("tags"))

	memoryTier, err := parseTier(tier)
	if err != nil {
		h.redirectMemories(c, tenantID, "", "invalid tier")
		return
	}

	_, err = h.memoryService.Store(c.Request.Context(), corememory.StoreInput{
		TenantID: tenantID,
		Content:  content,
		Tier:     memoryTier,
		Tags:     tags,
	})
	if err != nil {
		h.redirectMemories(c, tenantID, "", err.Error())
		return
	}

	h.redirectMemories(c, tenantID, "memory stored", "")
}

func (h *Handlers) DeleteMemory(c *gin.Context) {
	memoryID := strings.TrimSpace(c.Param("id"))
	tenantID := strings.TrimSpace(c.PostForm("tenant_id"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(c.Query("tenant_id"))
	}

	if err := h.memoryService.Delete(c.Request.Context(), tenantID, memoryID); err != nil {
		h.redirectMemories(c, tenantID, "", err.Error())
		return
	}

	h.redirectMemories(c, tenantID, "memory deleted", "")
}

func (h *Handlers) Tenants(c *gin.Context) {
	tenants, err := h.listTenantsWithCounts(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load tenants failed"})
		return
	}

	h.render(c, "tenants.html", TenantsPageData{
		Page:    "tenants",
		Error:   c.Query("error"),
		Info:    c.Query("info"),
		Tenants: tenants,
	})
}

func (h *Handlers) CreateTenant(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	name := strings.TrimSpace(c.PostForm("name"))
	_, err := h.tenantService.Create(c.Request.Context(), domain.Tenant{
		ID:   id,
		Name: name,
	})
	if err != nil {
		h.redirectTenants(c, "", err.Error())
		return
	}
	h.redirectTenants(c, "tenant created", "")
}

func (h *Handlers) Stats(c *gin.Context) {
	tenants, err := h.listTenantsWithCounts(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load stats failed"})
		return
	}

	var totalMemories int64
	var topID string
	var topCount int64
	for _, t := range tenants {
		totalMemories += t.MemoryCount
		if t.MemoryCount > topCount {
			topCount = t.MemoryCount
			topID = t.ID
		}
	}

	h.render(c, "stats.html", StatsPageData{
		Page:         "stats",
		Error:        "",
		Info:         "",
		TenantCount:  len(tenants),
		MemoryCount:  totalMemories,
		TopTenantID:  topID,
		TopTenantMem: topCount,
	})
}

func (h *Handlers) listTenantsWithCounts(c *gin.Context) ([]TenantView, error) {
	tenants, err := h.tenantService.List(c.Request.Context(), 200)
	if err != nil {
		return nil, err
	}

	out := make([]TenantView, 0, len(tenants))
	for _, t := range tenants {
		stats, err := h.tenantService.Stats(c.Request.Context(), t.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TenantView{
			ID:          t.ID,
			Name:        t.Name,
			CreatedAt:   t.CreatedAt.Format("2006-01-02 15:04"),
			MemoryCount: stats.MemoryCount,
		})
	}
	return out, nil
}

func mapMemoryViews(items []domain.Memory) []MemoryView {
	out := make([]MemoryView, 0, len(items))
	for _, m := range items {
		out = append(out, MemoryView{
			ID:         m.ID,
			TenantID:   m.TenantID,
			Content:    m.Content,
			Tier:       string(m.Tier),
			Tags:       strings.Join(m.Tags, ", "),
			Importance: strconv.FormatFloat(m.Importance, 'f', 2, 64),
			UpdatedAt:  m.UpdatedAt.Format("2006-01-02 15:04"),
			CreatedAt:  m.CreatedAt.Format("2006-01-02 15:04"),
			AccessedAt: m.LastAccessedAt.Format("2006-01-02 15:04"),
		})
	}
	return out
}

func parseCommaList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseTier(tier string) (domain.MemoryTier, error) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "", string(domain.MemoryTierAuto):
		return domain.MemoryTierAuto, nil
	case string(domain.MemoryTierWorking):
		return domain.MemoryTierWorking, nil
	case string(domain.MemoryTierEpisodic):
		return domain.MemoryTierEpisodic, nil
	case string(domain.MemoryTierSemantic):
		return domain.MemoryTierSemantic, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func (h *Handlers) redirectTenants(c *gin.Context, info, errMsg string) {
	values := url.Values{}
	if info != "" {
		values.Set("info", info)
	}
	if errMsg != "" {
		values.Set("error", errMsg)
	}
	location := "/dashboard/tenants"
	if len(values) > 0 {
		location += "?" + values.Encode()
	}
	c.Redirect(http.StatusSeeOther, location)
}

func (h *Handlers) redirectMemories(c *gin.Context, tenantID, info, errMsg string) {
	values := url.Values{}
	if tenantID != "" {
		values.Set("tenant_id", tenantID)
	}
	if info != "" {
		values.Set("info", info)
	}
	if errMsg != "" {
		values.Set("error", errMsg)
	}
	location := "/dashboard/memories"
	if len(values) > 0 {
		location += "?" + values.Encode()
	}
	c.Redirect(http.StatusSeeOther, location)
}

func (h *Handlers) render(c *gin.Context, page string, data any) {
	t, err := template.ParseFS(templatesFS, "templates/layout.html", "templates/"+page)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "dashboard template parse error"})
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, "layout.html", data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "dashboard render error"})
		return
	}
}
