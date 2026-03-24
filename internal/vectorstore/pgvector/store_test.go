package pgvector

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestNewStoreRequiresDSN(t *testing.T) {
	store, err := NewStore(Options{})
	require.Nil(t, store)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dsn is required")
}

func TestStoreUpsertBootstrapsAndWrites(t *testing.T) {
	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(createExtensionSQL)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(store.query(createTableSQL))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(store.query(detectVectorDimSQL))).
		WillReturnRows(sqlmock.NewRows([]string{"vector_dims"}))
	mock.ExpectExec(regexp.QuoteMeta(store.query(upsertSQL))).
		WithArgs("tenant_1", "m1", "[1,0.5]", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.Upsert(context.Background(), "tenant_1", "m1", []float32{1, 0.5})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStoreUpsertRejectsDimensionMismatch(t *testing.T) {
	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(createExtensionSQL)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(store.query(createTableSQL))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(store.query(detectVectorDimSQL))).
		WillReturnRows(sqlmock.NewRows([]string{"vector_dims"}).AddRow(2))

	err := store.Upsert(context.Background(), "tenant_1", "m1", []float32{1, 0, 0})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dimension mismatch")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStoreSearchReturnsCandidates(t *testing.T) {
	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(createExtensionSQL)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(store.query(createTableSQL))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(store.query(detectVectorDimSQL))).
		WillReturnRows(sqlmock.NewRows([]string{"vector_dims"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta(store.query(searchSQL))).
		WithArgs("tenant_1", "[0.9,0.1]", 2).
		WillReturnRows(sqlmock.NewRows([]string{"memory_id", "similarity"}).
			AddRow("m1", 0.99).
			AddRow("m2", 0.12))

	candidates, err := store.Search(context.Background(), "tenant_1", []float32{0.9, 0.1}, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, []domain.VectorstoreCandidate{
		{MemoryID: "m1", Similarity: 0.99},
		{MemoryID: "m2", Similarity: 0.12},
	}, candidates)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStoreUpsertBatchUsesTransaction(t *testing.T) {
	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(createExtensionSQL)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(store.query(createTableSQL))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(store.query(detectVectorDimSQL))).
		WillReturnRows(sqlmock.NewRows([]string{"vector_dims"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(store.query(upsertSQL))).
		WithArgs("tenant_1", "m1", "[1,0]", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(store.query(upsertSQL))).
		WithArgs("tenant_1", "m2", "[0,1]", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := store.UpsertBatch(context.Background(), []domain.VectorUpsert{
		{TenantID: "tenant_1", MemoryID: "m1", Embedding: []float32{1, 0}},
		{TenantID: "tenant_1", MemoryID: "m2", Embedding: []float32{0, 1}},
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStoreDelete(t *testing.T) {
	store, mock, cleanup := newMockStore(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta(createExtensionSQL)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(store.query(createTableSQL))).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(store.query(detectVectorDimSQL))).
		WillReturnRows(sqlmock.NewRows([]string{"vector_dims"}))
	mock.ExpectExec(regexp.QuoteMeta(store.query(deleteSQL))).
		WithArgs("tenant_1", "m1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.Delete(context.Background(), "tenant_1", "m1")
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizeIdentifier(t *testing.T) {
	require.Equal(t, "pali_memories", normalizeIdentifier("pali_memories"))
	require.Equal(t, "", normalizeIdentifier("memory-embeddings"))
}

func TestVectorLiteral(t *testing.T) {
	require.Equal(t, "[1,0.25,-0.5]", vectorLiteral([]float32{1, 0.25, -0.5}))
}

func newMockStore(t *testing.T) (*Store, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	store := &Store{
		db:          db,
		table:       "pali_memories",
		autoMigrate: true,
	}

	cleanup := func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	}
	return store, mock, cleanup
}

func TestStoreCloseNilSafe(t *testing.T) {
	var store *Store
	require.NoError(t, store.Close())

	store = &Store{}
	require.NoError(t, store.Close())
}
