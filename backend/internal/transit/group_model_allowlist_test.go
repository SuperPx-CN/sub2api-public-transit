package transit

import (
	"reflect"
	"slices"
	"testing"
)

func TestGroupModelAllowlistMatching(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		entries     []string
		want        bool
	}{
		{"exact", "Text", []string{"Text"}, true},
		{"case and space", " Text ", []string{" teXT "}, true},
		{"prefix", "Grok-4.6", []string{"grok-*"}, true},
		{"prefix required", "grok", []string{"grok-*"}, false},
		{"not a glob", "gpt-5.5", []string{"gpt-*.5"}, false},
		{"bare wildcard", "anything", []string{"*"}, true},
		{"empty enabled", "Text", nil, false},
		{"blank entry", "Text", []string{" "}, false},
		{"gemini prefix", "models/gemini-2.5-pro", []string{"GEMINI-2.5-PRO"}, true},
		{"gemini denied", "models/gemini-2.5-flash", []string{"gemini-2.5-pro"}, false},
		{"upstream prefix is case sensitive", "MODELS/gemini-2.5-pro", []string{"gemini-2.5-pro"}, false},
		{"thinking", "claude-sonnet-4.5-thinking", []string{"claude-sonnet-4.5"}, true},
		{"dated claude alias", "claude-sonnet-4-5-thinking", []string{"claude-sonnet-4-5-20250929"}, true},
		{"opus alias", "claude-opus-4-5", []string{"claude-opus-4-5-20251101"}, true},
		{"haiku alias wildcard", "claude-haiku-4-5-thinking", []string{"claude-haiku-4-5-2025*"}, true},
		{"alias is directional", "claude-sonnet-4-5-20250929", []string{"claude-sonnet-4-5"}, false},
		{"openai reasoning", "gpt-5.5-codex-high", []string{"gpt-5.5"}, true},
		{"openai provider case and separators", "openai/GPT-5.5_CODEX_HIGH", []string{"gpt-5.5"}, true},
		{"openai mini", "gpt-5.4mini-xhigh", []string{"gpt-5.4-mini"}, true},
		{"openai spark", "gpt-5.3codexspark-high", []string{"gpt-5.3-codex-spark"}, true},
		{"openai sol", "gpt-6-sol-high", []string{"gpt-6-sol"}, true},
		{"openai unknown remains unknown", "gpt-unknown-high", []string{"gpt-5.4"}, false},
		{"not every suffix is stripped", "gpt-4.1-low", []string{"gpt-4.1"}, false},
		{"no reasoning suffix no codex alias", "gpt-5.5-codex", []string{"gpt-5.5"}, false},
		{"unknown reasoning level", "gpt-5.5-magic", []string{"gpt-5.5"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := GroupModelAllowlist{Enabled: true, Models: tc.entries}
			if got := cfg.Allows(tc.model); got != tc.want {
				t.Fatalf("Allows(%q, %v) = %v, want %v", tc.model, tc.entries, got, tc.want)
			}
			cfg.Enabled = false
			if !cfg.Allows(tc.model) {
				t.Fatal("disabled allowlist denied model")
			}
		})
	}
}

func TestAllowlistFiltersCatalogWithoutChangingModels(t *testing.T) {
	price := 0.000003
	g := Group{ID: 1, Name: "Public", Platform: "openai", RateMultiplier: 1.25}
	ch := Channel{ModelMapping: map[string]map[string]string{"openai": {"Alias": "Text"}}, ModelPricing: []ChannelModelPricing{
		{Platform: "openai", Models: []string{"Text", "models/gemini-2.5-pro", "claude-sonnet-4.5-thinking", "gpt-5.5-codex-high"}, InputPrice: &price, Intervals: []PricingInterval{{MinTokens: 100, InputPrice: &price}}},
	}}
	channels := []AvailableChannel{{Status: StatusActive, Groups: []AvailableGroupRef{{ID: g.ID, Name: g.Name, Platform: g.Platform}}, SupportedModels: ch.SupportedModels()}}
	baseline := buildPublicTransitGroups([]Group{g}, channels, nil, nil)[0]
	for _, tc := range []struct {
		name string
		cfg  GroupModelAllowlist
		want []string
	}{
		{"empty enabled", GroupModelAllowlist{Enabled: true}, nil},
		{"aliases and unconfirmed entries", GroupModelAllowlist{Enabled: true, Models: []string{"alias", "gemini-2.5-pro", "claude-sonnet-4.5", "gpt-5.5", "not-supported", "missing-*"}}, []string{"Alias", "claude-sonnet-4.5-thinking", "gpt-5.5-codex-high", "models/gemini-2.5-pro"}},
		{"target cannot allow client alias", GroupModelAllowlist{Enabled: true, Models: []string{"Text"}}, []string{"Text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g.ModelAllowlist = tc.cfg
			groups := buildPublicTransitGroups([]Group{g}, channels, nil, nil)
			if len(groups) != 1 || groups[0].Models == nil {
				t.Fatal(groups)
			}
			var names []string
			for _, m := range groups[0].Models {
				names = append(names, m.StandardModel)
				i := slices.IndexFunc(baseline.Models, func(v PublicTransitModel) bool { return v.StandardModel == m.StandardModel })
				if i < 0 || !reflect.DeepEqual(m, baseline.Models[i]) {
					t.Fatalf("price or provenance changed: %+v", m)
				}
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("got %v want %v", names, tc.want)
			}
		})
	}
	channels[0].SupportedModels = append(channels[0].SupportedModels, SupportedModel{Name: "", Platform: "openai"}, SupportedModel{Name: "gpt-*", Platform: "openai"})
	g.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"*"}}
	if got := buildPublicTransitGroups([]Group{g}, channels, nil, nil)[0]; !reflect.DeepEqual(got, baseline) {
		t.Fatal("bare wildcard added blank or pattern models")
	}
}
