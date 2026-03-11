package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	sqlitevec "github.com/pali-mem/pali/internal/vectorstore/sqlitevec"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestPostprocessWorkerVectorJobSucceeds(t *testing.T) {
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_worker_vec", Name: "Tenant Worker Vec"})
	require.NoError(t, err)

	stored, err := memoryRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_worker_vec",
		Content:  "User prefers tea over coffee.",
		Kind:     domain.MemoryKindObservation,
	})
	require.NoError(t, err)

	jobs, err := memoryRepo.EnqueuePostprocessJobs(ctx, []domain.MemoryPostprocessJobEnqueue{
		{
			IngestID:    "ing_vec",
			TenantID:    stored.TenantID,
			MemoryID:    stored.ID,
			JobType:     domain.PostprocessJobTypeVectorUpsert,
			MaxAttempts: 3,
		},
	}, 3)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	svc := NewService(
		memoryRepo,
		tenantRepo,
		sqlitevec.NewStore(db),
		embedderStub{},
		scorerStub{},
	)
	stop, err := svc.StartPostprocessWorkers(ctx, PostprocessWorkerOptions{
		Enabled:      true,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    4,
		WorkerCount:  1,
		Lease:        100 * time.Millisecond,
		MaxAttempts:  3,
		RetryBase:    10 * time.Millisecond,
		RetryMax:     100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer stop()

	waitForJobStatus(t, memoryRepo, jobs[0].ID, domain.PostprocessJobStatusSucceeded, 2*time.Second)

	var embeddingRows int
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(1) FROM memory_embeddings WHERE tenant_id = ? AND memory_id = ?`,
		stored.TenantID,
		stored.ID,
	).Scan(&embeddingRows)
	require.NoError(t, err)
	require.Equal(t, 1, embeddingRows)
}

func TestPostprocessWorkerParserJobCreatesDerivedMemories(t *testing.T) {
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_worker_parser", Name: "Tenant Worker Parser"})
	require.NoError(t, err)

	raw, err := memoryRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_worker_parser",
		Content:  "My name is Sam and I live in Austin.",
		Kind:     domain.MemoryKindRawTurn,
	})
	require.NoError(t, err)

	jobs, err := memoryRepo.EnqueuePostprocessJobs(ctx, []domain.MemoryPostprocessJobEnqueue{
		{
			IngestID:    "ing_parser",
			TenantID:    raw.TenantID,
			MemoryID:    raw.ID,
			JobType:     domain.PostprocessJobTypeParserExtract,
			MaxAttempts: 3,
		},
	}, 3)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	svc := NewService(
		memoryRepo,
		tenantRepo,
		sqlitevec.NewStore(db),
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			Provider:        "heuristic",
			Model:           "",
			StoreRawTurn:    true,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(NewHeuristicInfoParser()),
	)
	stop, err := svc.StartPostprocessWorkers(ctx, PostprocessWorkerOptions{
		Enabled:      true,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    4,
		WorkerCount:  1,
		Lease:        100 * time.Millisecond,
		MaxAttempts:  3,
		RetryBase:    10 * time.Millisecond,
		RetryMax:     100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer stop()

	waitForJobStatus(t, memoryRepo, jobs[0].ID, domain.PostprocessJobStatusSucceeded, 2*time.Second)

	require.Eventually(t, func() bool {
		memories, err := memoryRepo.Search(ctx, "tenant_worker_parser", "", 20)
		if err != nil {
			return false
		}
		return len(memories) >= 2
	}, 2*time.Second, 20*time.Millisecond)

	postJobs, err := memoryRepo.ListPostprocessJobs(ctx, domain.MemoryPostprocessJobFilter{
		TenantID: "tenant_worker_parser",
		Types:    []domain.PostprocessJobType{domain.PostprocessJobTypeVectorUpsert},
		Limit:    20,
	})
	require.NoError(t, err)
	require.NotEmpty(t, postJobs)
}

func TestPostprocessWorkerParserJobRetainsAnswerMetadataAndDropsScaffolds(t *testing.T) {
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_worker_metadata", Name: "Tenant Worker Metadata"})
	require.NoError(t, err)

	raw, err := memoryRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_worker_metadata",
		Content:  "On 8 May 2023, Melanie said she likes jazz.",
		Kind:     domain.MemoryKindRawTurn,
	})
	require.NoError(t, err)

	jobs, err := memoryRepo.EnqueuePostprocessJobs(ctx, []domain.MemoryPostprocessJobEnqueue{
		{
			IngestID:    "ing_parser_metadata",
			TenantID:    raw.TenantID,
			MemoryID:    raw.ID,
			JobType:     domain.PostprocessJobTypeParserExtract,
			MaxAttempts: 3,
		},
	}, 3)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	svc := NewService(
		memoryRepo,
		tenantRepo,
		sqlitevec.NewStore(db),
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:                    true,
			Provider:                   "stub",
			Model:                      "test",
			StoreRawTurn:               true,
			MaxFacts:                   4,
			DedupeThreshold:            0.88,
			UpdateThreshold:            0.94,
			AnswerSpanRetentionEnabled: true,
		},
		WithInfoParser(parserFuncStub(func(_ context.Context, _ string, _ int) ([]ParsedFact, error) {
			return []ParsedFact{
				{
					Content:  "dialogue D91 occurred on 8 May 2023 at 10:30 AM.",
					Kind:     domain.MemoryKindObservation,
					Entity:   "dialogue D91",
					Relation: "event",
					Value:    "10:30 AM",
				},
				{
					Content:  "Melanie likes jazz.",
					Kind:     domain.MemoryKindObservation,
					Entity:   "Melanie",
					Relation: "preference",
					Value:    "jazz",
				},
			}, nil
		})),
	)
	stop, err := svc.StartPostprocessWorkers(ctx, PostprocessWorkerOptions{
		Enabled:      true,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    4,
		WorkerCount:  1,
		Lease:        100 * time.Millisecond,
		MaxAttempts:  3,
		RetryBase:    10 * time.Millisecond,
		RetryMax:     100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer stop()

	waitForJobStatus(t, memoryRepo, jobs[0].ID, domain.PostprocessJobStatusSucceeded, 2*time.Second)

	var derived []domain.Memory
	require.Eventually(t, func() bool {
		memories, searchErr := memoryRepo.Search(ctx, "tenant_worker_metadata", "", 20)
		if searchErr != nil {
			return false
		}
		derived = derived[:0]
		for _, memory := range memories {
			if memory.Kind == domain.MemoryKindRawTurn {
				continue
			}
			derived = append(derived, memory)
		}
		return len(derived) == 1
	}, 2*time.Second, 20*time.Millisecond)

	require.Len(t, derived, 1)
	require.Equal(t, "Melanie likes jazz.", derived[0].Content)
	require.NotEmpty(t, derived[0].AnswerMetadata.AnswerKind)
	require.Equal(t, "On 8 May 2023, Melanie said she likes jazz", derived[0].AnswerMetadata.SourceSentence)
	require.NotEmpty(t, derived[0].AnswerMetadata.SurfaceSpan)
	require.NotEmpty(t, derived[0].AnswerMetadata.TemporalAnchor)
	require.NotContains(t, derived[0].QueryViewText, "what about")
}

func waitForJobStatus(
	t *testing.T,
	repo *sqliterepo.MemoryRepository,
	jobID string,
	target domain.PostprocessJobStatus,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := repo.GetPostprocessJob(context.Background(), jobID)
		if err == nil && job != nil && job.Status == target {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, err := repo.GetPostprocessJob(context.Background(), jobID)
	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, target, job.Status, fmt.Sprintf("job=%+v", *job))
}
