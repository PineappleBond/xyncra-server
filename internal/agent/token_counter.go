package agent

// HeuristicTokenCounter uses len(text)/4 as a rough token estimate.
// This is the default when no external tokenizer is configured.
// It satisfies the D-001 zero-dependency principle.
type HeuristicTokenCounter struct{}

// CountTokens returns a rough estimate of the number of tokens in text.
// The heuristic divides byte length by 4, which approximates English
// token counts. For precise counting, use a tiktoken-based implementation.
func (h *HeuristicTokenCounter) CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return len(text) / 4
}
