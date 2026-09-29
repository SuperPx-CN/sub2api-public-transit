package transit

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type LiteLLMModelPricing struct {
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	InputCostPerTokenPriority           float64 `json:"input_cost_per_token_priority"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	OutputCostPerTokenPriority          float64 `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority float64 `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority     float64 `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold      int     `json:"long_context_input_token_threshold,omitempty"`
	LongContextInputCostMultiplier      float64 `json:"long_context_input_cost_multiplier,omitempty"`
	LongContextOutputCostMultiplier     float64 `json:"long_context_output_cost_multiplier,omitempty"`
	SupportsServiceTier                 bool    `json:"supports_service_tier"`
	LiteLLMProvider                     string  `json:"litellm_provider"`
	Mode                                string  `json:"mode"`
	SupportsPromptCaching               bool    `json:"supports_prompt_caching"`
	OutputCostPerImage                  float64 `json:"output_cost_per_image"`       // 图片生成模型每张图片价格
	OutputCostPerImageToken             float64 `json:"output_cost_per_image_token"` // 图片输出 token 价格
	InputCostPerImageToken              float64 `json:"input_cost_per_image_token"`  // 图片输入 token 价格（如 gpt-image-2 图片编辑）

	// TokenPricingAbsent 表示源数据中 input/output token 价格均缺失（仅有图片价）。
	// 此类条目只可用于图片计费，token 计费必须回退到 fallback 或 fail-closed，
	// 否则 token 流量会被按 $0 计费。零值（false）表示条目具备 token 价格。
	TokenPricingAbsent bool `json:"-"`
}

//go:embed prices.json
var bundledPrices []byte

type PricingService struct {
	prices map[string]*LiteLLMModelPricing
}

func LoadPricing(path string) (*PricingService, error) {
	data := bundledPrices
	var err error
	if path != "" {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}
	var entries map[string]json.RawMessage
	if err = json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	p := &PricingService{prices: map[string]*LiteLLMModelPricing{}}
	for name, raw := range entries {
		if name == "sample_spec" {
			continue
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		valid := false
		for _, key := range []string{"input_cost_per_token", "output_cost_per_token", "output_cost_per_image", "output_cost_per_image_token", "input_cost_per_image_token"} {
			if v, ok := fields[key]; ok && string(v) != "null" {
				valid = true
			}
		}
		if !valid {
			continue
		}
		var v LiteLLMModelPricing
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		p.prices[strings.ToLower(name)] = &v
	}
	if len(p.prices) == 0 {
		return nil, fmt.Errorf("no valid pricing entries")
	}
	return p, nil
}
