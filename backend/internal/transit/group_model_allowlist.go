// Derived from Wei-Shaw/sub2api v0.2.8; see docs/sub2api-compatibility.md.
// Only pure model-name matching is retained; no account or gateway dependencies.
package transit

import "strings"

type GroupModelAllowlist GroupModelsListConfig

func (a GroupModelAllowlist) Allows(model string) bool {
	if !a.Enabled {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	candidates := groupModelAllowlistCandidates(model)
	for _, entry := range a.Models {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		entry = strings.ToLower(entry)
		if strings.HasSuffix(entry, "*") {
			prefix := strings.TrimSuffix(entry, "*")
			for _, candidate := range candidates {
				if strings.HasPrefix(candidate, prefix) {
					return true
				}
			}
			continue
		}
		for _, candidate := range candidates {
			if candidate == entry {
				return true
			}
		}
	}
	return false
}

func groupModelAllowlistCandidates(model string) []string {
	candidates := make([]string, 0, 4)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	add(model)
	add(strings.TrimPrefix(model, "models/"))
	add(normalizeAllowlistClaudeModel(strings.TrimSuffix(model, "-thinking")))
	add(normalizeAllowlistOpenAIModel(model))
	return candidates
}

var allowlistClaudeModelIDs = map[string]string{
	"claude-sonnet-4-5": "claude-sonnet-4-5-20250929",
	"claude-opus-4-5":   "claude-opus-4-5-20251101",
	"claude-haiku-4-5":  "claude-haiku-4-5-20251001",
}

func normalizeAllowlistClaudeModel(id string) string {
	if id == "" {
		return id
	}
	if mapped, ok := allowlistClaudeModelIDs[id]; ok {
		return mapped
	}
	return id
}
