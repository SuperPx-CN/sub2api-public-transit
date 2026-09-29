// Derived from Wei-Shaw/sub2api v0.2.8; see docs/sub2api-compatibility.md.
// Only pure model-name matching is retained; no account or gateway dependencies.
// Spelling, suffix and path helpers are shared with pricing_lookup.go.
package transit

import "strings"

var codexModelMap = map[string]string{
	"gpt-6-sol":            "gpt-6-sol",
	"gpt-6-luna":           "gpt-6-luna",
	"gpt-6-astra":          "gpt-6-astra",
	"gpt-5.6-sol":          "gpt-5.6-sol",
	"gpt-5.6-terra":        "gpt-5.6-terra",
	"gpt-5.6-luna":         "gpt-5.6-luna",
	"gpt-5.5":              "gpt-5.5",
	"gpt-5.5-pro":          "gpt-5.5-pro",
	"codex-auto-review":    "codex-auto-review",
	"gpt-5.4":              "gpt-5.4",
	"gpt-5.4-mini":         "gpt-5.4-mini",
	"gpt-5.4-none":         "gpt-5.4",
	"gpt-5.4-low":          "gpt-5.4",
	"gpt-5.4-medium":       "gpt-5.4",
	"gpt-5.4-high":         "gpt-5.4",
	"gpt-5.4-xhigh":        "gpt-5.4",
	"gpt-5.4-chat-latest":  "gpt-5.4",
	"gpt-5.3":              "gpt-5.3-codex",
	"gpt-5.3-none":         "gpt-5.3-codex",
	"gpt-5.3-low":          "gpt-5.3-codex",
	"gpt-5.3-medium":       "gpt-5.3-codex",
	"gpt-5.3-high":         "gpt-5.3-codex",
	"gpt-5.3-xhigh":        "gpt-5.3-codex",
	"gpt-5.3-codex":        "gpt-5.3-codex",
	"gpt-5.3-codex-spark":  "gpt-5.3-codex-spark",
	"gpt-5.3-codex-low":    "gpt-5.3-codex",
	"gpt-5.3-codex-medium": "gpt-5.3-codex",
	"gpt-5.3-codex-high":   "gpt-5.3-codex",
	"gpt-5.3-codex-xhigh":  "gpt-5.3-codex",
	"gpt-5.2":              "gpt-5.2",
	"gpt-5.2-none":         "gpt-5.2",
	"gpt-5.2-low":          "gpt-5.2",
	"gpt-5.2-medium":       "gpt-5.2",
	"gpt-5.2-high":         "gpt-5.2",
	"gpt-5.2-xhigh":        "gpt-5.2",
	"gpt-5":                "gpt-5.4",
	"gpt-5-mini":           "gpt-5.4",
	"gpt-5-nano":           "gpt-5.4",
	"gpt-5.1":              "gpt-5.4",
	"gpt-5.1-codex":        "gpt-5.3-codex",
	"gpt-5.1-codex-max":    "gpt-5.3-codex",
	"gpt-5.1-codex-mini":   "gpt-5.3-codex",
	"gpt-5.2-codex":        "gpt-5.2",
	"codex-mini-latest":    "gpt-5.3-codex",
	"gpt-5-codex":          "gpt-5.3-codex",
}

var codexVersionModelPrefixes = []struct {
	prefix string
	target string
}{
	{prefix: "gpt-6-sol", target: "gpt-6-sol"},
	{prefix: "gpt-6-luna", target: "gpt-6-luna"},
	{prefix: "gpt-5.6-sol", target: "gpt-5.6-sol"},
	{prefix: "gpt-5.6-terra", target: "gpt-5.6-terra"},
	{prefix: "gpt-5.6-luna", target: "gpt-5.6-luna"},
	{prefix: "gpt-5.3-codex-spark", target: "gpt-5.3-codex-spark"},
	{prefix: "gpt-5.3-codex", target: "gpt-5.3-codex"},
	{prefix: "gpt-5.4-mini", target: "gpt-5.4-mini"},
	{prefix: "gpt-5.4-nano", target: "gpt-5.4-nano"},
	{prefix: "gpt-5.5-pro", target: "gpt-5.5-pro"},
	{prefix: "gpt-5.5", target: "gpt-5.5"},
	{prefix: "gpt-5.4", target: "gpt-5.4"},
	{prefix: "gpt-5.2", target: "gpt-5.2"},
}

func normalizeAllowlistOpenAIModel(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}

	normalized, _, ok := splitOpenAICompatReasoningModel(trimmed)
	if !ok || normalized == "" {
		return trimmed
	}
	return normalized
}

func splitOpenAICompatReasoningModel(model string) (normalizedModel string, reasoningEffort string, ok bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", "", false
	}

	modelID := trimmed
	if strings.Contains(modelID, "/") {
		parts := strings.Split(modelID, "/")
		modelID = parts[len(parts)-1]
	}
	modelID = strings.TrimSpace(modelID)
	if !strings.HasPrefix(strings.ToLower(modelID), "gpt-") {
		return trimmed, "", false
	}

	parts := strings.FieldsFunc(strings.ToLower(modelID), func(r rune) bool {
		switch r {
		case '-', '_', ' ':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		return trimmed, "", false
	}

	last := strings.NewReplacer("-", "", "_", "", " ", "").Replace(parts[len(parts)-1])
	switch last {
	case "none", "minimal":
	case "low", "medium", "high":
		reasoningEffort = last
	case "xhigh", "extrahigh":
		reasoningEffort = "xhigh"
	default:
		return trimmed, "", false
	}

	return normalizeCodexModel(modelID), reasoningEffort, true
}

func normalizeKnownOpenAICodexModel(model string) string {
	normalized := canonicalizeOpenAIModelAliasSpelling(model)
	if normalized == "" {
		return ""
	}

	if mapped := getNormalizedCodexModel(normalized); mapped != "" {
		return mapped
	}
	if strings.HasSuffix(normalized, "-openai-compact") {
		if mapped := getNormalizedCodexModel(strings.TrimSuffix(normalized, "-openai-compact")); mapped != "" {
			return mapped
		}
	}

	if isAllowlistGPT6SolOrLunaModelSpelling(normalized) {
		if strings.HasPrefix(normalized, "gpt-6-sol") {
			return "gpt-6-sol"
		}
		return "gpt-6-luna"
	}

	switch {
	case normalized == "gpt-6" || normalized == "gpt-6-astra":
		return "gpt-6-astra"
	case strings.Contains(normalized, "gpt-5.6-sol"):
		return "gpt-5.6-sol"
	case strings.Contains(normalized, "gpt-5.6-terra"):
		return "gpt-5.6-terra"
	case strings.Contains(normalized, "gpt-5.6-luna"):
		return "gpt-5.6-luna"
	case normalized == "gpt-5.6":
		return "gpt-5.6-sol"
	case strings.HasPrefix(normalized, "gpt-5.6-"):
		suffix := strings.TrimPrefix(normalized, "gpt-5.6-")
		if suffix == "max" || isKnownCodexModelSuffix(suffix) {
			return "gpt-5.6-sol"
		}
		return ""
	case strings.Contains(normalized, "gpt-5.5-pro"):
		return "gpt-5.5-pro"
	case strings.Contains(normalized, "gpt-5.5"):
		return "gpt-5.5"
	case strings.Contains(normalized, "gpt-5.4-mini"):
		return "gpt-5.4-mini"
	case strings.Contains(normalized, "gpt-5.4-nano"):
		return "gpt-5.4-nano"
	case strings.Contains(normalized, "gpt-5.4"):
		return "gpt-5.4"
	case strings.Contains(normalized, "gpt-5.2"):
		return "gpt-5.2"
	case strings.Contains(normalized, "gpt-5.3-codex-spark"):
		return "gpt-5.3-codex-spark"
	case strings.Contains(normalized, "gpt-5.3-codex"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "gpt-5.3"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "codex"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "gpt-5"):
		return "gpt-5.4"
	default:
		return ""
	}
}

func isAllowlistGPT6SolOrLunaModelSpelling(model string) bool {
	canonical := canonicalizeOpenAIModelAliasSpelling(model)
	for _, base := range []string{"gpt-6-sol", "gpt-6-luna"} {
		if canonical == base {
			return true
		}
		suffix, ok := strings.CutPrefix(canonical, base+"-")
		if ok {
			switch suffix {
			case "none", "low", "medium", "high", "xhigh", "max", "openai-compact":
				return true
			}
		}
	}
	return false
}

func normalizeCodexModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "gpt-5.4"
	}
	if mapped, ok := normalizeKnownCodexModel(model); ok {
		return mapped
	}
	return model
}

func normalizeKnownCodexModel(model string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	if isOpenAIImageGenerationModel(model) {
		return model, true
	}

	modelID := lastOpenAIModelSegment(model)

	if normalized := canonicalizeOpenAIModelAliasSpelling(modelID); normalized != "" {
		modelID = normalized
	}
	if mapped := normalizeKnownOpenAICodexModel(modelID); mapped != "" {
		return mapped, true
	}
	key := codexModelLookupKey(modelID)
	if key == "" {
		return "", false
	}
	if mapped := getNormalizedCodexModel(key); mapped != "" {
		return mapped, true
	}
	for _, item := range codexVersionModelPrefixes {
		if key == item.prefix {
			return item.target, true
		}
		suffix, ok := strings.CutPrefix(key, item.prefix+"-")
		if ok && isKnownCodexModelSuffix(suffix) {
			return item.target, true
		}
	}
	return "", false
}

func codexModelLookupKey(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if strings.Contains(modelID, "/") {
		parts := strings.Split(modelID, "/")
		modelID = parts[len(parts)-1]
	}
	return strings.ToLower(strings.Join(strings.Fields(modelID), "-"))
}

func getNormalizedCodexModel(modelID string) string {
	key := codexModelLookupKey(modelID)
	if key == "" {
		return ""
	}
	if mapped, ok := codexModelMap[key]; ok {
		return mapped
	}
	return ""
}
