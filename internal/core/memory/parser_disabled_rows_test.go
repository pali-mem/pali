package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

type storedRowSnapshot struct {
	content, kind, tier, tags, queryView string
}

// disabledParserInputs is a representative set of inputs for the parser-disabled
// stored-row regression test.
func disabledParserInputs() []StoreInput {
	return []StoreInput{
		{Content: "met teammate for coffee today"},
		{Content: "I prefer dark mode in every editor", Tags: []string{"Preference"}},
		{Content: "[TIME: 8 May, 2023] Melanie: I went to the Bali Arts Festival last weekend. It was amazing!"},
		{Content: "Caroline works as a nurse and lives in Boston", Tier: domain.MemoryTierSemantic},
		{Content: "Team offsite planned for next Friday", Kind: domain.MemoryKindEvent, Tags: []string{"work", "event"}},
		{Content: "Always use tabs for Go files", CreatedBy: domain.MemoryCreatedByUser},
		{Content: "ok"},
	}
}

// expectedDisabledParserRows was captured from the base commit (df914c6) with the
// parser disabled (default options). Rows are listed per input, in input order.
var expectedDisabledParserRows = []storedRowSnapshot{
	{"met teammate for coffee today", "raw_turn", "episodic", "[]", ""},
	{"I prefer dark mode in every editor", "raw_turn", "semantic", "[Preference]", ""},
	{"[TIME: 8 May, 2023] Melanie: I went to the Bali Arts Festival last weekend. It was amazing!", "raw_turn", "episodic", "[]", ""},
	{"Caroline works as a nurse and lives in Boston", "raw_turn", "semantic", "[]", ""},
	{"Team offsite planned for next Friday", "event", "episodic", "[work event]", ""},
	{"Always use tabs for Go files", "raw_turn", "semantic", "[]", ""},
	{"ok", "raw_turn", "episodic", "[]", ""},
}

func snapshotRows(rows []domain.Memory) []storedRowSnapshot {
	out := make([]storedRowSnapshot, 0, len(rows))
	for _, m := range rows {
		out = append(out, storedRowSnapshot{m.Content, string(m.Kind), string(m.Tier), fmt.Sprint(m.Tags), m.QueryViewText})
	}
	return out
}

func TestParserDisabledStoredRowsUnchanged(t *testing.T) {
	newSvc := func() (*Service, *structuredRepoStub) {
		repo := &structuredRepoStub{}
		svc := NewService(repo, tenantRepoStub{existsByID: map[string]bool{"t": true}}, &structuredVectorStub{}, embedderStub{}, scorerStub{})
		return svc, repo
	}
	inputs := func() []StoreInput {
		in := disabledParserInputs()
		for i := range in {
			in[i].TenantID = "t"
		}
		return in
	}

	t.Run("store", func(t *testing.T) {
		svc, repo := newSvc()
		for _, in := range inputs() {
			_, err := svc.Store(context.Background(), in)
			require.NoError(t, err)
		}
		require.Equal(t, expectedDisabledParserRows, snapshotRows(repo.stored))
	})
	t.Run("store batch", func(t *testing.T) {
		svc, repo := newSvc()
		_, err := svc.StoreBatch(context.Background(), inputs())
		require.NoError(t, err)
		require.Equal(t, expectedDisabledParserRows, snapshotRows(repo.stored))
	})
}
