package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyQuerySimpleConjunctionIsNotMultiHop(t *testing.T) {
	profile := classifyQuery("who likes tea and coffee?")
	require.False(t, profile.MultiHop)
}

func TestClassifyQueryBeforeClauseIsMultiHop(t *testing.T) {
	profile := classifyQuery("who met Jordan before moving to Austin?")
	require.True(t, profile.MultiHop)
}

func TestClassifyQueryTwoClauseAndIsMultiHop(t *testing.T) {
	profile := classifyQuery("who met Jordan and moved to Austin?")
	require.True(t, profile.MultiHop)
}

func TestBuildQueryPlanMultiHopCapturesNamedEntities(t *testing.T) {
	profile := classifyQuery("who did Melanie meet and where did she move?")
	plan := buildQueryPlan("who did Melanie meet and where did she move?", profile)
	require.Equal(t, "graph_entity_expansion", plan.Intent)
	require.Equal(t, "melanie", plan.primaryEntity())
}

func TestClassifyEntityHintQueryForSingleHopReturnsName(t *testing.T) {
	profile := classifyQuery("What is Melanie's reason for getting into running?")
	entity, ok := classifyEntityHintQuery("What is Melanie's reason for getting into running?", profile)
	require.True(t, ok)
	require.Equal(t, "Melanie", entity)
}

func TestBuildQueryPlanAggregationRoute(t *testing.T) {
	profile := classifyQuery("what activities does melanie do?")
	plan := buildQueryPlan("what activities does melanie do?", profile)
	require.Equal(t, "aggregation_lookup", plan.Intent)
	require.NotEmpty(t, plan.Entities)
	require.Equal(t, "melanie", plan.primaryEntity())
	require.Equal(t, "activity", plan.primaryRelation())
	require.NotEmpty(t, plan.FallbackPath)
}

func TestBuildQueryPlanTemporalRoute(t *testing.T) {
	profile := classifyQuery("when did Alex move to Austin?")
	plan := buildQueryPlan("when did Alex move to Austin?", profile)
	require.Equal(t, "temporal_lookup", plan.Intent)
	require.Equal(t, "time_anchored_fact", plan.RequiredEvidence)
}

func TestClassifyAggregationQuery_DoesNotHijackFactualQuestion(t *testing.T) {
	_, ok := classifyAggregationQuery("What did Caroline research?")
	require.False(t, ok)
}

func TestClassifyAggregationQuery_RequiresExplicitSetIntent(t *testing.T) {
	_, ok := classifyAggregationQuery("When did Caroline attend a support group?")
	require.False(t, ok)
}
