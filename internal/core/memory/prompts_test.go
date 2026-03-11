package memory

import (
	"strings"
	"testing"

	coreprompts "github.com/pali-mem/pali/internal/core/prompts"
	"github.com/stretchr/testify/require"
)

func TestBuildParserPrompt_ContainsMaxFactsAndSchema(t *testing.T) {
	prompt := coreprompts.Parser("Alice likes coffee", 3)

	require.Contains(t, prompt, "Output at most 3 facts.")
	require.Contains(t, prompt, "\"facts\":[{\"content\":\"...\"")
	require.Contains(t, prompt, "Turn:\nAlice likes coffee")
}

func TestBuildParserPrompt_TrimsContentIntoTemplate(t *testing.T) {
	prompt := coreprompts.Parser("  Alice   likes   coffee  ", 1)

	require.Contains(t, prompt, "  Alice   likes   coffee  ")
	require.Contains(t, prompt, "Output at most 1 facts.")
	require.True(t, strings.Contains(prompt, "Turn:\n  Alice   likes   coffee  "))
}
