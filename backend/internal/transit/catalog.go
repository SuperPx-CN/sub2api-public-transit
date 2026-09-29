package transit

import (
	"sort"
	"strings"
	"time"
)

type BillingMode string

const (
	BillingModeToken      BillingMode = "token"       // 按 token 区间计费
	BillingModePerRequest BillingMode = "per_request" // 按次计费（支持上下文窗口分层）
	BillingModeImage      BillingMode = "image"       // 图片计费（当前按次，预留 token 计费）
	BillingModeVideo      BillingMode = "video"       // 视频生成计费（按视频生成次数）
)

type ChannelModelPricing struct {
	ID               int64
	ChannelID        int64
	Platform         string            // 所属平台（anthropic/openai/gemini/...）
	Models           []string          // 绑定的模型列表
	BillingMode      BillingMode       // 计费模式
	InputPrice       *float64          // 每 token 输入价格（USD）— 向后兼容 flat 定价
	OutputPrice      *float64          // 每 token 输出价格（USD）
	CacheWritePrice  *float64          // 缓存写入价格
	CacheReadPrice   *float64          // 缓存读取价格
	ImageInputPrice  *float64          // 图片输入 token 价格（如 gpt-image-2 图片编辑）；未配置时回退文本输入价
	ImageOutputPrice *float64          // 图片输出价格（向后兼容）
	PerRequestPrice  *float64          // 默认按次计费价格（USD）
	Intervals        []PricingInterval // 区间定价列表
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PricingInterval 定价区间（token 区间 / 按次分层 / 图片分辨率分层）
type PricingInterval struct {
	ID              int64
	PricingID       int64
	MinTokens       int      // 区间下界（含）
	MaxTokens       *int     // 区间上界（不含），nil = 无上限
	TierLabel       string   // 层级标签（按次/图片模式：1K, 2K, 4K, HD 等）
	InputPrice      *float64 // token 模式：每 token 输入价
	OutputPrice     *float64 // token 模式：每 token 输出价
	CacheWritePrice *float64 // token 模式：缓存写入价
	CacheReadPrice  *float64 // token 模式：缓存读取价
	PerRequestPrice *float64 // 按次/图片模式：每次请求价格
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SupportedModel struct {
	Name          string               // 用户侧模型名
	Platform      string               // 所属平台
	Pricing       *ChannelModelPricing // 定价详情（nil 表示未配置定价）
	PricingSource string               // custom / standard / unknown
	CatalogSource string               // channel / group_models_list / monitoring
}

// wildcardSuffix 是模型模式中的通配符后缀标记（仅支持尾部匹配）。
const wildcardSuffix = "*"

const (
	ModelPriceSourceCustom   = "custom"
	ModelPriceSourceStandard = "standard"
	ModelPriceSourceUnknown  = "unknown"

	ModelCatalogSourceChannel         = "channel"
	ModelCatalogSourceGroupModelsList = "group_models_list"
	ModelCatalogSourceMonitoring      = "monitoring"
)

// splitWildcardSuffix 将模型模式拆分为 (prefix, isWildcard)。
//
//	"claude-opus-*"  → ("claude-opus-", true)
//	"claude-opus-4"  → ("claude-opus-4", false)
//	"*"              → ("", true)
//
// 注意：返回的 prefix 保持原始大小写，由调用方按需 ToLower。
func splitWildcardSuffix(pattern string) (prefix string, isWildcard bool) {
	if strings.HasSuffix(pattern, wildcardSuffix) {
		return strings.TrimSuffix(pattern, wildcardSuffix), true
	}
	return pattern, false
}

// GetModelPricingByPlatform 在指定平台下查找精确模型的定价，未找到返回 nil。
// 与 GetModelPricing 的区别：按 Platform 隔离，避免跨平台同名模型误匹配。
func (c *Channel) GetModelPricingByPlatform(platform, model string) *ChannelModelPricing {
	if c == nil {
		return nil
	}
	modelLower := strings.ToLower(model)
	for i := range c.ModelPricing {
		if c.ModelPricing[i].Platform != platform {
			continue
		}
		for _, m := range c.ModelPricing[i].Models {
			if strings.ToLower(m) == modelLower {
				cp := c.ModelPricing[i].Clone()
				return &cp
			}
		}
	}
	return nil
}

// platformPricingIndex 是单个平台下定价信息的复合索引。
// 一次扫描即可同时支持精确查找（exact 分支）与有序遍历（wildcard 分支），
// 避免 SupportedModels 对每个平台重复扫描定价列表。
//
// byLower 与 names/originalCase 共享同一套去重规则：以 lower-case 模型名为 key，
// 首个命中保留其原始大小写。names 维持按定价行扫描顺序的稳定迭代。
type platformPricingIndex struct {
	byLower      map[string]*ChannelModelPricing // lowercased model name → pricing (Clone'd)
	originalCase map[string]string               // lowercased model name → original-case model name
	names        []string                        // priced model names in their ORIGINAL case, insertion-ordered, deduped case-insensitively (first wins)
}

// buildPricingIndex 对渠道的定价列表做一次扫描，按 platform 聚合为查找索引。
// 索引值是定价条目的 Clone 指针，调用方可安全按需返回副本而不污染缓存。
// 通配符后缀条目（如 "claude-*"）不被索引（它们是模式，不是具体模型名）。
// 同一平台中以大小写不敏感方式去重，先出现者保留原始大小写。
func buildPricingIndex(pricings []ChannelModelPricing) map[string]*platformPricingIndex {
	idx := make(map[string]*platformPricingIndex)
	for i := range pricings {
		p := pricings[i]
		pidx, ok := idx[p.Platform]
		if !ok {
			pidx = &platformPricingIndex{
				byLower:      make(map[string]*ChannelModelPricing),
				originalCase: make(map[string]string),
				names:        make([]string, 0),
			}
			idx[p.Platform] = pidx
		}
		for _, m := range p.Models {
			if _, wild := splitWildcardSuffix(m); wild {
				continue
			}
			lower := strings.ToLower(m)
			if _, exists := pidx.byLower[lower]; exists {
				continue // 首个命中胜出（case-insensitive 去重后第一个定价 / 第一个原始大小写）
			}
			cp := pricings[i].Clone()
			pidx.byLower[lower] = &cp
			pidx.originalCase[lower] = m
			pidx.names = append(pidx.names, m)
		}
	}
	return idx
}

// SupportedModels 计算渠道的支持模型列表，结果保证不含通配符。
//
// 算法（mapping ∪ pricing 并联）：
//
//   - Pass A（mapping）：遍历 ModelMapping
//   - 精确 src → target：显示名 = src（用户视角），定价用 target 在同 platform 定价里查
//     （mapping 改写后实际计费的是 target；这是用户感知的"实际花费"）。
//     target 为空或为通配符时退化为按 src 自查。
//   - 通配符 src（如 "claude-3-*"）：用同 platform 定价里前缀匹配的模型作为候选展开，
//     每个候选用自身定价（通配符场景一般是 passthrough，target 通常也是通配符）。
//   - "*" 单独 mapping key 走通配符分支（前缀为空 → 全展开）。
//   - Pass B（pricing-only）：遍历 ModelPricing 中所有非通配符模型，对未在 Pass A 添加过的
//     补齐——显示名 = 定价模型名，定价 = 自身（这是关键修复：定价存在即代表渠道支持该模型，
//     即使没配映射）。
//
// 显示名命中定价时使用**定价的原始大小写**（定价是模型身份的事实来源）。
// 按 (Platform, Name) 稳定排序，按 (Platform, lowercase(Name)) 去重，先到者胜出。
//
// 注意：定价仅在 channel.ModelPricing 内查找——全局 LiteLLM 回落由调用方
// （`ChannelService.ListAvailable`）在合成展示数据时叠加。
func (c *Channel) SupportedModels() []SupportedModel {
	if c == nil {
		return nil
	}
	if len(c.ModelMapping) == 0 && len(c.ModelPricing) == 0 {
		return nil
	}

	idx := buildPricingIndex(c.ModelPricing)

	type dedupKey struct {
		platform string
		name     string
	}
	seen := make(map[dedupKey]struct{})
	result := make([]SupportedModel, 0)

	// lookup 在 platform pricing index 中按精确名查定价，命中时返回定价大小写。
	lookup := func(pidx *platformPricingIndex, name string) (display string, pricing *ChannelModelPricing) {
		if pidx == nil || name == "" {
			return name, nil
		}
		lower := strings.ToLower(name)
		if p, ok := pidx.byLower[lower]; ok {
			return pidx.originalCase[lower], p
		}
		return name, nil
	}

	add := func(platform, displayName string, pricing *ChannelModelPricing) {
		key := dedupKey{platform: platform, name: strings.ToLower(displayName)}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, SupportedModel{
			Name:          displayName,
			Platform:      platform,
			Pricing:       pricing,
			CatalogSource: ModelCatalogSourceChannel,
		})
	}

	// Pass A：从 mapping 展开
	for platform, mapping := range c.ModelMapping {
		if len(mapping) == 0 {
			continue
		}
		pidx := idx[platform]
		for src, target := range mapping {
			prefix, isWild := splitWildcardSuffix(src)
			if isWild {
				if pidx == nil {
					continue
				}
				prefixLower := strings.ToLower(prefix)
				for _, candidate := range pidx.names {
					if strings.HasPrefix(strings.ToLower(candidate), prefixLower) {
						display, pricing := lookup(pidx, candidate)
						add(platform, display, pricing)
					}
				}
				continue
			}
			// 精确 mapping：定价按 target 查；target 缺失/通配则退化按 src 查
			pricingKey := target
			if pricingKey == "" {
				pricingKey = src
			}
			if _, targetWild := splitWildcardSuffix(pricingKey); targetWild {
				pricingKey = src
			}
			_, pricing := lookup(pidx, pricingKey)
			// 显示名优先用 src 在定价里的原始大小写（若 src 本身是个定价模型名）
			displayName, _ := lookup(pidx, src)
			add(platform, displayName, pricing)
		}
	}

	// Pass B：从 pricing 补齐 mapping 未覆盖的具体模型（修复"定价存在但没配映射 → 不显示"）
	for platform, pidx := range idx {
		for _, name := range pidx.names {
			display, pricing := lookup(pidx, name)
			add(platform, display, pricing)
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Platform != result[j].Platform {
			return result[i].Platform < result[j].Platform
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func (p ChannelModelPricing) Clone() ChannelModelPricing {
	cp := p
	if p.Models != nil {
		cp.Models = make([]string, len(p.Models))
		copy(cp.Models, p.Models)
	}
	if p.Intervals != nil {
		cp.Intervals = make([]PricingInterval, len(p.Intervals))
		copy(cp.Intervals, p.Intervals)
	}
	return cp
}

// Clone 返回 Channel 的深拷贝
type Channel struct {
	ModelPricing []ChannelModelPricing
	ModelMapping map[string]map[string]string
}
