package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/api/dto"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	"github.com/pali-mem/pali/internal/domain"
)

type MemoryHandler struct {
	service               *corememory.Service
	maxPostprocessAttempt int
}

func NewMemoryHandler(service *corememory.Service, maxPostprocessAttempts ...int) *MemoryHandler {
	maxAttempts := 5
	if len(maxPostprocessAttempts) > 0 && maxPostprocessAttempts[0] > 0 {
		maxAttempts = maxPostprocessAttempts[0]
	}
	return &MemoryHandler{
		service:               service,
		maxPostprocessAttempt: maxAttempts,
	}
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

func (h *MemoryHandler) Ingest(c *gin.Context) {
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

	receipt, err := h.service.IngestAsync(c.Request.Context(), storeInput, h.maxPostprocessAttempt)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, dto.IngestMemoryResponse{
		IngestID:   receipt.IngestID,
		MemoryIDs:  append([]string{}, receipt.MemoryIDs...),
		JobIDs:     append([]string{}, receipt.JobIDs...),
		AcceptedAt: receipt.AcceptedAt,
	})
}

func (h *MemoryHandler) IngestBatch(c *gin.Context) {
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

	receipt, err := h.service.IngestBatchAsync(c.Request.Context(), inputs, h.maxPostprocessAttempt)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, dto.IngestMemoryResponse{
		IngestID:   receipt.IngestID,
		MemoryIDs:  append([]string{}, receipt.MemoryIDs...),
		JobIDs:     append([]string{}, receipt.JobIDs...),
		AcceptedAt: receipt.AcceptedAt,
	})
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

func (h *MemoryHandler) GetPostprocessJob(c *gin.Context) {
	jobID := c.Param("id")
	job, err := h.service.GetPostprocessJob(c.Request.Context(), jobID)
	if err != nil {
		writeError(c, err)
		return
	}
	if err := enforceTenantAccess(c, job.TenantID); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toPostprocessJobResponse(*job))
}

func (h *MemoryHandler) ListPostprocessJobs(c *gin.Context) {
	tenantID := strings.TrimSpace(c.Query("tenant_id"))
	if err := enforceTenantAccess(c, tenantID); err != nil {
		writeError(c, err)
		return
	}

	filter := domain.MemoryPostprocessJobFilter{
		TenantID: tenantID,
		Limit:    50,
	}
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		filter.Limit = limit
	}

	var err error
	filter.Statuses, err = parsePostprocessStatuses(c.QueryArray("status"), c.Query("status"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	filter.Types, err = parsePostprocessTypes(c.QueryArray("type"), c.Query("type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jobs, err := h.service.ListPostprocessJobs(c.Request.Context(), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	out := make([]dto.PostprocessJobResponse, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, toPostprocessJobResponse(job))
	}
	c.JSON(http.StatusOK, dto.ListPostprocessJobsResponse{Items: out})
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

func parsePostprocessStatuses(values []string, csv string) ([]domain.PostprocessJobStatus, error) {
	raw := flattenCSVQuery(values, csv)
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[domain.PostprocessJobStatus]struct{}, len(raw))
	out := make([]domain.PostprocessJobStatus, 0, len(raw))
	for _, value := range raw {
		var status domain.PostprocessJobStatus
		switch strings.ToLower(strings.TrimSpace(value)) {
		case string(domain.PostprocessJobStatusQueued):
			status = domain.PostprocessJobStatusQueued
		case string(domain.PostprocessJobStatusRunning):
			status = domain.PostprocessJobStatusRunning
		case string(domain.PostprocessJobStatusSucceeded):
			status = domain.PostprocessJobStatusSucceeded
		case string(domain.PostprocessJobStatusFailed):
			status = domain.PostprocessJobStatusFailed
		case string(domain.PostprocessJobStatusDeadLetter):
			status = domain.PostprocessJobStatusDeadLetter
		default:
			return nil, domain.ErrInvalidInput
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out, nil
}

func parsePostprocessTypes(values []string, csv string) ([]domain.PostprocessJobType, error) {
	raw := flattenCSVQuery(values, csv)
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[domain.PostprocessJobType]struct{}, len(raw))
	out := make([]domain.PostprocessJobType, 0, len(raw))
	for _, value := range raw {
		var jobType domain.PostprocessJobType
		switch strings.ToLower(strings.TrimSpace(value)) {
		case string(domain.PostprocessJobTypeParserExtract):
			jobType = domain.PostprocessJobTypeParserExtract
		case string(domain.PostprocessJobTypeVectorUpsert):
			jobType = domain.PostprocessJobTypeVectorUpsert
		default:
			return nil, domain.ErrInvalidInput
		}
		if _, ok := seen[jobType]; ok {
			continue
		}
		seen[jobType] = struct{}{}
		out = append(out, jobType)
	}
	return out, nil
}

func flattenCSVQuery(values []string, csv string) []string {
	out := make([]string, 0, len(values)+2)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			out = append(out, part)
		}
	}
	if strings.TrimSpace(csv) == "" {
		return out
	}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func toPostprocessJobResponse(job domain.MemoryPostprocessJob) dto.PostprocessJobResponse {
	return dto.PostprocessJobResponse{
		ID:          job.ID,
		IngestID:    job.IngestID,
		TenantID:    job.TenantID,
		MemoryID:    job.MemoryID,
		Type:        string(job.JobType),
		Status:      string(job.Status),
		Attempts:    job.Attempts,
		MaxAttempts: job.MaxAttempts,
		AvailableAt: job.AvailableAt,
		LeaseOwner:  job.LeaseOwner,
		LeasedUntil: job.LeasedUntil,
		LastError:   job.LastError,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}
}
