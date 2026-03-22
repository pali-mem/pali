package dashboard

import (
	"embed"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/config"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	"github.com/pali-mem/pali/internal/domain"
	"github.com/pali-mem/pali/internal/telemetry"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templatesFS embed.FS

const dashboardSearchMinScore = 0.25

type Handlers struct {
	memoryService *corememory.Service
	tenantService *coretenant.Service
	telemetry     *telemetry.Service
	configPage    ConfigPageData
}

type TenantView struct {
	ID          string
	Name        string
	CreatedAt   string
	MemoryCount int64
}

type MemoryView struct {
	ID              string
	TenantID        string
	Content         string
	Tier            string
	Kind            string
	Tags            []string
	Source          string
	CreatedBy       string
	CanonicalKey    string
	SourceTurnHash  string
	Extractor       string
	ExtractorVer    string
	Importance      string
	RecallCount     int
	UpdatedAt       string
	CreatedAt       string
	AccessedAt      string
	LastRecalledAt  string
	HasDetailFields bool
}

type MemoryFilterState struct {
	SelectedTenantID string
	Query            string
	SelectedTier     string
	SelectedKind     string
}

type MemoriesPageData struct {
	Page         string
	Error        string
	Info         string
	Filters      MemoryFilterState
	Tenants      []TenantView
	Memories     []MemoryView
	ResultCount  int
	ComposerOpen bool
}

type MemoryDetailPageData struct {
	Page    string
	Error   string
	Info    string
	Filters MemoryFilterState
	BackURL string
	Memory  MemoryView
}

type TenantFilterState struct {
	Query string
}

type TenantsPageData struct {
	Page          string
	Error         string
	Info          string
	Filters       TenantFilterState
	Tenants       []TenantView
	ResultCount   int
	TotalMemories int64
	ComposerOpen  bool
}

type StatsPageData struct {
	Page           string
	Error          string
	Info           string
	TenantCount    int
	MemoryCount    int64
	TopTenantID    string
	TopTenantMem   int64
	Tenants        []TenantView
	HasMoreTenants bool
	ConfigPath     string
}

type ConfigPageData struct {
	Page          string
	Error         string
	Info          string
	ConfigPath    string
	ConfigSource  string
	DocsURL       string
	DocsBlurb     string
	DocsLinkLabel string
}

type AnalyticsPageData struct {
	Page  string
	Error string
	Info  string
}

func NewHandlers(memoryService *corememory.Service, tenantService *coretenant.Service, telemetryService *telemetry.Service, cfg config.Config, configPath string) *Handlers {
	return &Handlers{
		memoryService: memoryService,
		tenantService: tenantService,
		telemetry:     telemetryService,
		configPage:    buildConfigPageData(cfg, configPath),
	}
}

func (h *Handlers) Index(c *gin.Context) {
	c.Redirect(http.StatusFound, "/dashboard/stats")
}

func (h *Handlers) Memories(c *gin.Context) {
	tenants, err := h.listTenantsWithCounts(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load tenants failed"})
		return
	}

	filters, filterErr := readMemoryFilters(c)
	if filters.SelectedTenantID == "" && len(tenants) > 0 {
		filters.SelectedTenantID = tenants[0].ID
	}

	memories := []MemoryView{}
	loadErr := filterErr
	if filters.SelectedTenantID != "" && loadErr == "" {
		var (
			items []domain.Memory
			err   error
		)
		if filters.Query != "" {
			searchOptions := corememory.SearchOptions{
				MinScore: dashboardSearchMinScore,
				Tiers:    filterTierSelection(filters.SelectedTier),
				Kinds:    filterKindSelection(filters.SelectedKind),
			}
			items, err = h.memoryService.SearchWithFilters(c.Request.Context(), filters.SelectedTenantID, filters.Query, 50, searchOptions)
			if err == nil && len(items) == 0 {
				// Fallback: preserve exact/literal query expectations when semantic score
				// is below threshold (for example, short keyword lookups).
				fallbackOptions := searchOptions
				fallbackOptions.MinScore = 0
				fallbackItems, fallbackErr := h.memoryService.SearchWithFilters(c.Request.Context(), filters.SelectedTenantID, filters.Query, 50, fallbackOptions)
				if fallbackErr != nil {
					err = fallbackErr
				} else {
					items = filterMemoriesByLiteralQuery(fallbackItems, filters.Query)
				}
			}
		} else {
			items, err = h.memoryService.List(c.Request.Context(), filters.SelectedTenantID, 200)
			if err == nil {
				items = filterMemoryItems(items, filters.SelectedTier, filters.SelectedKind)
			}
		}
		if err != nil {
			loadErr = err.Error()
		} else {
			memories = mapMemoryViews(items)
		}
	}

	h.render(c, "memories.html", MemoriesPageData{
		Page:         "memories",
		Error:        firstNonEmpty(c.Query("error"), loadErr),
		Info:         c.Query("info"),
		Filters:      filters,
		Tenants:      tenants,
		Memories:     memories,
		ResultCount:  len(memories),
		ComposerOpen: c.Query("compose") == "1",
	})
}

func (h *Handlers) CreateMemory(c *gin.Context) {
	filters, _ := readMemoryFiltersFromValues(c.PostForm)
	tenantID := strings.TrimSpace(c.PostForm("tenant_id"))
	content := strings.TrimSpace(c.PostForm("content"))
	tier := strings.TrimSpace(c.PostForm("tier"))
	tags := parseCommaList(c.PostForm("tags"))
	filters.SelectedTenantID = tenantID

	memoryTier, err := parseTier(tier)
	if err != nil {
		h.redirectMemories(c, filters, "", "invalid tier", true)
		return
	}

	_, err = h.memoryService.Store(c.Request.Context(), corememory.StoreInput{
		TenantID: tenantID,
		Content:  content,
		Tier:     memoryTier,
		Tags:     tags,
	})
	if err != nil {
		h.redirectMemories(c, filters, "", err.Error(), true)
		return
	}

	h.redirectMemories(c, filters, "memory stored", "", false)
}

func (h *Handlers) DeleteMemory(c *gin.Context) {
	filters, _ := readMemoryFiltersFromValues(c.PostForm)
	memoryID := strings.TrimSpace(c.Param("id"))
	tenantID := strings.TrimSpace(c.PostForm("tenant_id"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(c.Query("tenant_id"))
	}
	filters.SelectedTenantID = tenantID

	if err := h.memoryService.Delete(c.Request.Context(), tenantID, memoryID); err != nil {
		h.redirectMemories(c, filters, "", err.Error(), false)
		return
	}

	h.redirectMemories(c, filters, "memory deleted", "", false)
}

func (h *Handlers) ViewMemory(c *gin.Context) {
	filters, filterErr := readMemoryFilters(c)
	memoryID := strings.TrimSpace(c.Param("id"))
	if filters.SelectedTenantID == "" {
		h.redirectMemories(c, filters, "", "tenant_id is required to view memory", false)
		return
	}

	item, err := h.memoryService.Get(c.Request.Context(), filters.SelectedTenantID, memoryID)
	if err != nil {
		h.redirectMemories(c, filters, "", err.Error(), false)
		return
	}
	if item == nil {
		h.redirectMemories(c, filters, "", "memory not found", false)
		return
	}

	h.render(c, "memory_detail.html", MemoryDetailPageData{
		Page:    "memories",
		Error:   filterErr,
		Info:    c.Query("info"),
		Filters: filters,
		BackURL: buildMemoriesURL(filters),
		Memory:  mapMemoryView(*item),
	})
}

func (h *Handlers) Tenants(c *gin.Context) {
	tenants, err := h.listTenantsWithCounts(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load tenants failed"})
		return
	}

	filters := TenantFilterState{Query: strings.TrimSpace(c.Query("q"))}
	tenants = filterTenants(tenants, filters.Query)
	var totalMemories int64
	for _, tenant := range tenants {
		totalMemories += tenant.MemoryCount
	}

	h.render(c, "tenants.html", TenantsPageData{
		Page:          "tenants",
		Error:         c.Query("error"),
		Info:          c.Query("info"),
		Filters:       filters,
		Tenants:       tenants,
		ResultCount:   len(tenants),
		TotalMemories: totalMemories,
		ComposerOpen:  c.Query("compose") == "1",
	})
}

func (h *Handlers) CreateTenant(c *gin.Context) {
	filters := TenantFilterState{Query: strings.TrimSpace(c.PostForm("q"))}
	id := strings.TrimSpace(c.PostForm("id"))
	name := strings.TrimSpace(c.PostForm("name"))
	_, err := h.tenantService.Create(c.Request.Context(), domain.Tenant{
		ID:   id,
		Name: name,
	})
	if err != nil {
		h.redirectTenants(c, filters, "", err.Error(), true)
		return
	}
	h.redirectTenants(c, filters, "tenant created", "", false)
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
	topTenants := topTenantsByMemory(tenants, 10)

	h.render(c, "stats.html", StatsPageData{
		Page:           "stats",
		Error:          "",
		Info:           "",
		TenantCount:    len(tenants),
		MemoryCount:    totalMemories,
		TopTenantID:    topID,
		TopTenantMem:   topCount,
		Tenants:        topTenants,
		HasMoreTenants: len(tenants) > len(topTenants),
		ConfigPath:     h.configPage.ConfigPath,
	})
}

func (h *Handlers) Config(c *gin.Context) {
	data := h.configPage
	data.Page = "config"
	h.render(c, "config.html", data)
}

func (h *Handlers) Analytics(c *gin.Context) {
	h.render(c, "analytics.html", AnalyticsPageData{
		Page:  "analytics",
		Error: "",
		Info:  "",
	})
}

func (h *Handlers) AnalyticsData(c *gin.Context) {
	snapshot := h.telemetry.Snapshot(telemetry.SnapshotOptions{
		Events:     20,
		TopTenants: 5,
	})
	c.JSON(http.StatusOK, snapshot)
}

func (h *Handlers) listTenantsWithCounts(c *gin.Context) ([]TenantView, error) {
	tenants, err := h.tenantService.ListWithStats(c.Request.Context(), 200)
	if err != nil {
		return nil, err
	}

	out := make([]TenantView, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, TenantView{
			ID:          t.Tenant.ID,
			Name:        t.Tenant.Name,
			CreatedAt:   t.Tenant.CreatedAt.Format("2006-01-02 15:04"),
			MemoryCount: t.Stats.MemoryCount,
		})
	}
	return out, nil
}

func mapMemoryViews(items []domain.Memory) []MemoryView {
	out := make([]MemoryView, 0, len(items))
	for _, m := range items {
		out = append(out, mapMemoryView(m))
	}
	return out
}

func mapMemoryView(m domain.Memory) MemoryView {
	return MemoryView{
		ID:              m.ID,
		TenantID:        m.TenantID,
		Content:         m.Content,
		Tier:            string(m.Tier),
		Kind:            string(m.Kind),
		Tags:            append([]string{}, m.Tags...),
		Source:          strings.TrimSpace(m.Source),
		CreatedBy:       string(m.CreatedBy),
		CanonicalKey:    strings.TrimSpace(m.CanonicalKey),
		SourceTurnHash:  strings.TrimSpace(m.SourceTurnHash),
		Extractor:       strings.TrimSpace(m.Extractor),
		ExtractorVer:    strings.TrimSpace(m.ExtractorVersion),
		Importance:      strconv.FormatFloat(m.Importance, 'f', 2, 64),
		RecallCount:     m.RecallCount,
		UpdatedAt:       formatDashboardTime(m.UpdatedAt),
		CreatedAt:       formatDashboardTime(m.CreatedAt),
		AccessedAt:      formatOptionalDashboardTime(m.LastAccessedAt),
		LastRecalledAt:  formatOptionalDashboardTime(m.LastRecalledAt),
		HasDetailFields: strings.TrimSpace(m.CanonicalKey) != "" || strings.TrimSpace(m.SourceTurnHash) != "" || strings.TrimSpace(m.Extractor) != "" || strings.TrimSpace(m.Source) != "",
	}
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

func parseTierFilter(tier string) (string, error) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" || tier == "all" {
		return "", nil
	}
	switch domain.MemoryTier(tier) {
	case domain.MemoryTierWorking, domain.MemoryTierEpisodic, domain.MemoryTierSemantic, domain.MemoryTierAuto:
		return tier, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func parseKindFilter(kind string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || kind == "all" {
		return "", nil
	}
	switch domain.MemoryKind(kind) {
	case domain.MemoryKindRawTurn, domain.MemoryKindObservation, domain.MemoryKindSummary, domain.MemoryKindEvent:
		return kind, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func readMemoryFilters(c *gin.Context) (MemoryFilterState, string) {
	return readMemoryFiltersFromValues(c.Query)
}

func readMemoryFiltersFromValues(get func(string) string) (MemoryFilterState, string) {
	filters := MemoryFilterState{
		SelectedTenantID: strings.TrimSpace(get("tenant_id")),
		Query:            strings.TrimSpace(get("q")),
	}

	var errs []string
	if tier, err := parseTierFilter(get("tier")); err == nil {
		filters.SelectedTier = tier
	} else {
		errs = append(errs, "invalid tier filter")
	}
	if kind, err := parseKindFilter(get("kind")); err == nil {
		filters.SelectedKind = kind
	} else {
		errs = append(errs, "invalid kind filter")
	}

	return filters, strings.Join(errs, "; ")
}

func filterTierSelection(selected string) []domain.MemoryTier {
	if selected == "" {
		return nil
	}
	return []domain.MemoryTier{domain.MemoryTier(selected)}
}

func filterKindSelection(selected string) []domain.MemoryKind {
	if selected == "" {
		return nil
	}
	return []domain.MemoryKind{domain.MemoryKind(selected)}
}

func filterMemoryItems(items []domain.Memory, tier, kind string) []domain.Memory {
	if tier == "" && kind == "" {
		return items
	}
	out := make([]domain.Memory, 0, len(items))
	for _, item := range items {
		if tier != "" && string(item.Tier) != tier {
			continue
		}
		if kind != "" && string(item.Kind) != kind {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterMemoriesByLiteralQuery(items []domain.Memory, query string) []domain.Memory {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return items
	}
	queryTokens := tokenizeLiteralQuery(query)

	out := make([]domain.Memory, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(strings.Join([]string{
			item.Content,
			item.QueryViewText,
			item.Source,
			strings.Join(item.Tags, " "),
		}, " "))

		if strings.Contains(haystack, query) {
			out = append(out, item)
			continue
		}
		if len(queryTokens) == 0 {
			continue
		}

		allTokensFound := true
		for _, token := range queryTokens {
			if !strings.Contains(haystack, token) {
				allTokensFound = false
				break
			}
		}
		if allTokensFound {
			out = append(out, item)
		}
	}
	return out
}

func tokenizeLiteralQuery(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func filterTenants(items []TenantView, query string) []TenantView {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	out := make([]TenantView, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.ID), query) || strings.Contains(strings.ToLower(item.Name), query) {
			out = append(out, item)
		}
	}
	return out
}

func topTenantsByMemory(items []TenantView, limit int) []TenantView {
	if limit <= 0 || len(items) == 0 {
		return []TenantView{}
	}
	if len(items) <= limit {
		out := make([]TenantView, len(items))
		copy(out, items)
		sort.Slice(out, func(i, j int) bool {
			if out[i].MemoryCount != out[j].MemoryCount {
				return out[i].MemoryCount > out[j].MemoryCount
			}
			return out[i].ID < out[j].ID
		})
		return out
	}

	out := make([]TenantView, len(items))
	copy(out, items)
	sort.Slice(out, func(i, j int) bool {
		if out[i].MemoryCount != out[j].MemoryCount {
			return out[i].MemoryCount > out[j].MemoryCount
		}
		return out[i].ID < out[j].ID
	})
	return out[:limit]
}

func buildMemoriesURL(filters MemoryFilterState) string {
	values := url.Values{}
	if filters.SelectedTenantID != "" {
		values.Set("tenant_id", filters.SelectedTenantID)
	}
	if filters.Query != "" {
		values.Set("q", filters.Query)
	}
	if filters.SelectedTier != "" {
		values.Set("tier", filters.SelectedTier)
	}
	if filters.SelectedKind != "" {
		values.Set("kind", filters.SelectedKind)
	}
	location := "/dashboard/memories"
	if len(values) > 0 {
		location += "?" + values.Encode()
	}
	return location
}

func formatDashboardTime(ts time.Time) string {
	if ts.IsZero() {
		return "Unavailable"
	}
	return ts.Format("2006-01-02 15:04")
}

func formatOptionalDashboardTime(ts time.Time) string {
	if ts.IsZero() {
		return "Never"
	}
	return ts.Format("2006-01-02 15:04")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func buildConfigPageData(cfg config.Config, configPath string) ConfigPageData {
	const docsURL = "https://pali-mem.github.io/pali/"
	displayPath := strings.TrimSpace(configPath)
	raw := ""
	if displayPath != "" {
		b, err := os.ReadFile(displayPath)
		if err == nil {
			raw = string(b)
		}
	}
	if raw == "" {
		b, err := yaml.Marshal(cfg)
		if err == nil {
			raw = string(b)
		}
		if displayPath == "" {
			displayPath = "Runtime config (defaults and environment)"
		}
	}
	return ConfigPageData{
		Page:          "config",
		ConfigPath:    displayPath,
		ConfigSource:  raw,
		DocsURL:       docsURL,
		DocsBlurb:     "Want to use Qdrant, Ollama, or any other of our extensions? Please go to our docs.",
		DocsLinkLabel: "Read the docs",
	}
}

func (h *Handlers) redirectTenants(c *gin.Context, filters TenantFilterState, info, errMsg string, composeOpen bool) {
	values := url.Values{}
	if filters.Query != "" {
		values.Set("q", filters.Query)
	}
	if info != "" {
		values.Set("info", info)
	}
	if errMsg != "" {
		values.Set("error", errMsg)
	}
	if composeOpen {
		values.Set("compose", "1")
	}
	location := "/dashboard/tenants"
	if len(values) > 0 {
		location += "?" + values.Encode()
	}
	c.Redirect(http.StatusSeeOther, location)
}

func (h *Handlers) redirectMemories(c *gin.Context, filters MemoryFilterState, info, errMsg string, composeOpen bool) {
	values := url.Values{}
	if filters.SelectedTenantID != "" {
		values.Set("tenant_id", filters.SelectedTenantID)
	}
	if filters.Query != "" {
		values.Set("q", filters.Query)
	}
	if filters.SelectedTier != "" {
		values.Set("tier", filters.SelectedTier)
	}
	if filters.SelectedKind != "" {
		values.Set("kind", filters.SelectedKind)
	}
	if info != "" {
		values.Set("info", info)
	}
	if errMsg != "" {
		values.Set("error", errMsg)
	}
	if composeOpen {
		values.Set("compose", "1")
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
