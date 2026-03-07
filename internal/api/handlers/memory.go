package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/vein05/pali/internal/api/dto"
	corememory "github.com/vein05/pali/internal/core/memory"
	"github.com/vein05/pali/internal/domain"
)

type MemoryHandler struct {
	service *corememory.Service
}

func NewMemoryHandler(service *corememory.Service) *MemoryHandler {
	return &MemoryHandler{service: service}
}

func (h *MemoryHandler) Store(c *gin.Context) {
	var req dto.StoreMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if err := enforceTenantAccess(c, req.TenantID); err != nil {
		writeError(c, err)
		return
	}
	storeInput, err := parseStoreInput(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stored, err := h.service.Store(c.Request.Context(), storeInput)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.StoreMemoryResponse{
		ID:        stored.ID,
		CreatedAt: stored.CreatedAt,
	})
}

func (h *MemoryHandler) StoreBatch(c *gin.Context) {
	var req dto.StoreMemoryBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items must not be empty"})
		return
	}

	inputs := make([]corememory.StoreInput, 0, len(req.Items))
	for _, item := range req.Items {
		if err := enforceTenantAccess(c, item.TenantID); err != nil {
			writeError(c, err)
			return
		}
		storeInput, err := parseStoreInput(item)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		inputs = append(inputs, storeInput)
	}

	stored, err := h.service.StoreBatch(c.Request.Context(), inputs)
	if err != nil {
		writeError(c, err)
		return
	}
	out := make([]dto.StoreMemoryResponse, 0, len(stored))
	for _, memory := range stored {
		out = append(out, dto.StoreMemoryResponse{
			ID:        memory.ID,
			CreatedAt: memory.CreatedAt,
		})
	}
	c.JSON(http.StatusCreated, dto.StoreMemoryBatchResponse{Items: out})
}

func (h *MemoryHandler) Search(c *gin.Context) {
	var req dto.SearchMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if req.MinScore < 0 || req.MinScore > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid min_score"})
		return
	}
	searchTiers, err := parseSearchTiers(req.Tiers)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	searchKinds, err := parseSearchKinds(req.Kinds)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := enforceTenantAccess(c, req.TenantID); err != nil {
		writeError(c, err)
		return
	}

	searchOpts := corememory.SearchOptions{
		MinScore:     req.MinScore,
		Tiers:        searchTiers,
		Kinds:        searchKinds,
		DisableTouch: req.DisableTouch,
	}
	var (
		items []domain.Memory
		debug *corememory.SearchDebugInfo
	)
	if req.Debug {
		items, debug, err = h.service.SearchWithFiltersDebug(c.Request.Context(), req.TenantID, req.Query, req.TopK, searchOpts)
	} else {
		items, err = h.service.SearchWithFilters(c.Request.Context(), req.TenantID, req.Query, req.TopK, searchOpts)
	}
	if err != nil {
		writeError(c, err)
		return
	}

	out := make([]dto.MemoryResponse, 0, len(items))
	for _, m := range items {
		out = append(out, dto.MemoryResponse{
			ID:             m.ID,
			TenantID:       m.TenantID,
			Content:        m.Content,
			Tier:           string(m.Tier),
			Tags:           m.Tags,
			Source:         m.Source,
			CreatedBy:      string(m.CreatedBy),
			Kind:           string(m.Kind),
			RecallCount:    m.RecallCount,
			CreatedAt:      m.CreatedAt,
			UpdatedAt:      m.UpdatedAt,
			LastAccessedAt: m.LastAccessedAt,
			LastRecalledAt: m.LastRecalledAt,
		})
	}

	response := dto.SearchMemoryResponse{Items: out}
	if debug != nil {
		response.Debug = &dto.SearchMemoryDebugDTO{
			Plan: dto.SearchPlanDebugDTO{
				Intent:           debug.Plan.Intent,
				Confidence:       debug.Plan.Confidence,
				Entities:         append([]string{}, debug.Plan.Entities...),
				Relations:        append([]string{}, debug.Plan.Relations...),
				TimeConstraints:  append([]string{}, debug.Plan.TimeConstraints...),
				RequiredEvidence: debug.Plan.RequiredEvidence,
				FallbackPath:     append([]string{}, debug.Plan.FallbackPath...),
			},
		}
		for _, factor := range debug.Ranking {
			response.Debug.Ranking = append(response.Debug.Ranking, dto.SearchRankingDebugDTO{
				Rank:         factor.Rank,
				MemoryID:     factor.MemoryID,
				Kind:         factor.Kind,
				Tier:         factor.Tier,
				LexicalScore: factor.LexicalScore,
				QueryOverlap: factor.QueryOverlap,
				RouteFit:     factor.RouteFit,
			})
		}
	}

	c.JSON(http.StatusOK, response)
}

func (h *MemoryHandler) Delete(c *gin.Context) {
	memoryID := c.Param("id")
	tenantID := c.Query("tenant_id")
	if err := enforceTenantAccess(c, tenantID); err != nil {
		writeError(c, err)
		return
	}

	if err := h.service.Delete(c.Request.Context(), tenantID, memoryID); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func parseTier(t string) (domain.MemoryTier, error) {
	switch strings.ToLower(strings.TrimSpace(t)) {
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

func parseSearchTiers(raw []string) ([]domain.MemoryTier, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[domain.MemoryTier]struct{}, len(raw))
	out := make([]domain.MemoryTier, 0, len(raw))
	for _, tierRaw := range raw {
		tier, err := parseTier(tierRaw)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if tier == domain.MemoryTierAuto {
			return nil, domain.ErrInvalidInput
		}
		if _, ok := seen[tier]; ok {
			continue
		}
		seen[tier] = struct{}{}
		out = append(out, tier)
	}
	return out, nil
}

func parseSearchKinds(raw []string) ([]domain.MemoryKind, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[domain.MemoryKind]struct{}, len(raw))
	out := make([]domain.MemoryKind, 0, len(raw))
	for _, kindRaw := range raw {
		kind, err := parseKind(kindRaw)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	return out, nil
}

func parseCreatedBy(raw string) (domain.MemoryCreatedBy, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(domain.MemoryCreatedByAuto):
		return domain.MemoryCreatedByAuto, nil
	case string(domain.MemoryCreatedByUser):
		return domain.MemoryCreatedByUser, nil
	case string(domain.MemoryCreatedBySystem):
		return domain.MemoryCreatedBySystem, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func parseKind(raw string) (domain.MemoryKind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(domain.MemoryKindRawTurn):
		return domain.MemoryKindRawTurn, nil
	case string(domain.MemoryKindObservation):
		return domain.MemoryKindObservation, nil
	case string(domain.MemoryKindSummary):
		return domain.MemoryKindSummary, nil
	case string(domain.MemoryKindEvent):
		return domain.MemoryKindEvent, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func parseStoreInput(req dto.StoreMemoryRequest) (corememory.StoreInput, error) {
	tier, err := parseTier(req.Tier)
	if err != nil {
		return corememory.StoreInput{}, err
	}
	createdBy, err := parseCreatedBy(req.CreatedBy)
	if err != nil {
		return corememory.StoreInput{}, err
	}
	kind, err := parseKind(req.Kind)
	if err != nil {
		return corememory.StoreInput{}, err
	}
	return corememory.StoreInput{
		TenantID:  req.TenantID,
		Content:   req.Content,
		Tags:      req.Tags,
		Tier:      tier,
		Kind:      kind,
		Source:    req.Source,
		CreatedBy: createdBy,
	}, nil
}
