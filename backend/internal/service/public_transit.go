package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const (
	PublicTransitSchemaVersion = "ai-transit.v1"
	PublicTransitSystem        = "sub2api"

	PublicTransitWellKnownPath = "/.well-known/ai-transit.json"
	PublicTransitSnapshotPath  = "/api/public/transit/v1/snapshot"
	PublicTransitPagePath      = "/public/transit"
)

// PublicTransitService assembles a read-only, whitelisted public snapshot for
// crawlers and public pages. It deliberately reuses existing channel, pricing,
// payment and monitor services instead of exposing authenticated endpoints.
type PublicTransitService struct {
	channelService *ChannelService
	monitorService *ChannelMonitorService
	settingService *SettingService
	paymentConfig  *PaymentConfigService
	groupRepo      GroupRepository
	usageRepo      UsageLogRepository
}

func NewPublicTransitService(
	channelService *ChannelService,
	monitorService *ChannelMonitorService,
	settingService *SettingService,
	paymentConfig *PaymentConfigService,
	groupRepo GroupRepository,
	usageRepo UsageLogRepository,
) *PublicTransitService {
	return &PublicTransitService{
		channelService: channelService,
		monitorService: monitorService,
		settingService: settingService,
		paymentConfig:  paymentConfig,
		groupRepo:      groupRepo,
		usageRepo:      usageRepo,
	}
}

type PublicTransitRuntime struct {
	APIEnabled  bool
	PageEnabled bool
}

func (s *SettingService) GetPublicTransitRuntime(ctx context.Context) PublicTransitRuntime {
	vals, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyPublicTransitEnabled, SettingKeyPublicTransitPageEnabled})
	if err != nil {
		return PublicTransitRuntime{APIEnabled: false, PageEnabled: false}
	}
	apiEnabled := !isFalseSettingValue(vals[SettingKeyPublicTransitEnabled])
	pageValue, hasPageValue := vals[SettingKeyPublicTransitPageEnabled]
	pageEnabled := false
	if hasPageValue {
		pageEnabled = pageValue == "true"
	} else {
		// Backward compatibility: old deployments had one switch for both API
		// and page. If that switch was explicitly enabled, keep the page visible
		// until the admin saves the new page switch.
		pageEnabled = vals[SettingKeyPublicTransitEnabled] == "true"
	}
	if !apiEnabled {
		pageEnabled = false
	}
	return PublicTransitRuntime{APIEnabled: apiEnabled, PageEnabled: pageEnabled}
}

func (s *PublicTransitService) Enabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.GetPublicTransitRuntime(ctx).APIEnabled
}

func (s *PublicTransitService) PageEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.GetPublicTransitRuntime(ctx).PageEnabled
}

type PublicTransitDiscovery struct {
	SchemaVersion string `json:"schema_version"`
	System        string `json:"system"`
	SnapshotURL   string `json:"snapshot_url"`
	HomepageURL   string `json:"homepage_url,omitempty"`
	GeneratedAt   string `json:"generated_at"`
}

type PublicTransitSnapshot struct {
	SchemaVersion string                        `json:"schema_version"`
	System        string                        `json:"system"`
	GeneratedAt   string                        `json:"generated_at"`
	Station       PublicTransitStation          `json:"station"`
	Billing       PublicTransitBilling          `json:"billing"`
	Groups        []PublicTransitGroup          `json:"groups"`
	Monitoring    []PublicTransitMonitor        `json:"monitoring"`
	Cache         PublicTransitCacheDisclosure  `json:"cache"`
	Disclosure    PublicTransitSourceDisclosure `json:"disclosure"`
	Limits        PublicTransitLimits           `json:"limits"`
	Completeness  PublicTransitCompleteness     `json:"completeness"`
	Endpoints     PublicTransitEndpoints        `json:"endpoints"`
}

type PublicTransitStation struct {
	Name        string `json:"name"`
	HomepageURL string `json:"homepage_url"`
	PriceURL    string `json:"price_url"`
	MonitorURL  string `json:"monitor_url"`
	SupportURL  string `json:"support_url,omitempty"`
	SystemType  string `json:"system_type"`
}

type PublicTransitBilling struct {
	Currency                 string  `json:"currency"`
	CreditCurrency           string  `json:"credit_currency"`
	RechargeRatio            string  `json:"recharge_ratio"`
	RechargeMultiplier       float64 `json:"recharge_multiplier"`
	RechargeMultiplierUnit   string  `json:"recharge_multiplier_unit"`
	MinimumTopUp             float64 `json:"minimum_top_up"`
	ModelBasisPrice          string  `json:"model_basis_price"`
	ModelPriceUnit           string  `json:"model_price_unit"`
	StandardizedPriceVersion string  `json:"standardized_price_version"`
}

type PublicTransitGroup struct {
	Name             string                  `json:"name"`
	Platform         string                  `json:"platform"`
	SubscriptionType string                  `json:"subscription_type,omitempty"`
	RateMultiplier   float64                 `json:"rate_multiplier"`
	IsExclusive      bool                    `json:"is_exclusive"`
	CacheUsage       PublicTransitCacheUsage `json:"cache_usage"`
	Models           []PublicTransitModel    `json:"models"`
}

type PublicTransitCacheUsage struct {
	Last24h PublicTransitCacheUsageWindow `json:"last_24h"`
	Last7d  PublicTransitCacheUsageWindow `json:"last_7d"`
	Total   PublicTransitCacheUsageWindow `json:"total"`
}

type PublicTransitCacheUsageWindow struct {
	Period              string  `json:"period"`
	InputTokens         int64   `json:"input_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheHitRate        float64 `json:"cache_hit_rate"`
}

type PublicTransitModel struct {
	StandardModel     string                       `json:"standard_model"`
	RawModel          string                       `json:"raw_model"`
	Platform          string                       `json:"platform"`
	BillingMode       string                       `json:"billing_mode"`
	PriceSource       string                       `json:"price_source"`
	CatalogSource     string                       `json:"catalog_source"`
	Price             *PublicTransitModelPrice     `json:"price,omitempty"`
	Source            PublicTransitModelSource     `json:"source"`
	SupportedProtocol []string                     `json:"supported_protocols"`
	Intervals         []PublicTransitPriceInterval `json:"intervals,omitempty"`
}

type PublicTransitModelPrice struct {
	InputUSDPerToken       *float64            `json:"input_usd_per_token,omitempty"`
	OutputUSDPerToken      *float64            `json:"output_usd_per_token,omitempty"`
	CacheWriteUSDPerToken  *float64            `json:"cache_write_usd_per_token,omitempty"`
	CacheReadUSDPerToken   *float64            `json:"cache_read_usd_per_token,omitempty"`
	ImageOutputUSDPerToken *float64            `json:"image_output_usd_per_token,omitempty"`
	PerRequestUSD          *float64            `json:"per_request_usd,omitempty"`
	ImageSizePrices        map[string]*float64 `json:"image_size_prices,omitempty"`
}

type PublicTransitPriceInterval struct {
	MinTokens             int      `json:"min_tokens"`
	MaxTokens             *int     `json:"max_tokens,omitempty"`
	TierLabel             string   `json:"tier_label,omitempty"`
	InputUSDPerToken      *float64 `json:"input_usd_per_token,omitempty"`
	OutputUSDPerToken     *float64 `json:"output_usd_per_token,omitempty"`
	CacheWriteUSDPerToken *float64 `json:"cache_write_usd_per_token,omitempty"`
	CacheReadUSDPerToken  *float64 `json:"cache_read_usd_per_token,omitempty"`
	PerRequestUSD         *float64 `json:"per_request_usd,omitempty"`
}

type PublicTransitModelSource struct {
	UpstreamType    string `json:"upstream_type"`
	AccountPoolType string `json:"account_pool_type"`
	Disclosure      string `json:"disclosure"`
}

type PublicTransitMonitor struct {
	Name                string                          `json:"name"`
	Provider            string                          `json:"provider"`
	GroupName           string                          `json:"group_name,omitempty"`
	PrimaryModel        string                          `json:"primary_model"`
	PrimaryStatus       string                          `json:"primary_status"`
	Availability7d      float64                         `json:"availability_7d"`
	Availability15d     float64                         `json:"availability_15d"`
	Availability30d     float64                         `json:"availability_30d"`
	AvgLatency7dMs      *int                            `json:"avg_latency_7d_ms,omitempty"`
	LatestLatencyMs     *int                            `json:"latest_latency_ms,omitempty"`
	LatestPingLatencyMs *int                            `json:"latest_ping_latency_ms,omitempty"`
	LastCheckedAt       string                          `json:"last_checked_at,omitempty"`
	ExtraModels         []PublicTransitExtraModelStatus `json:"extra_models"`
	Models              []PublicTransitMonitorModel     `json:"models"`
	Timeline            []PublicTransitMonitorTimeline  `json:"timeline"`
}

type PublicTransitExtraModelStatus struct {
	Model     string `json:"model"`
	Status    string `json:"status"`
	LatencyMs *int   `json:"latency_ms,omitempty"`
}

type PublicTransitMonitorModel struct {
	Model           string  `json:"model"`
	LatestStatus    string  `json:"latest_status"`
	LatestLatencyMs *int    `json:"latest_latency_ms,omitempty"`
	Availability7d  float64 `json:"availability_7d"`
	Availability15d float64 `json:"availability_15d"`
	Availability30d float64 `json:"availability_30d"`
	AvgLatency7dMs  *int    `json:"avg_latency_7d_ms,omitempty"`
}

type PublicTransitMonitorTimeline struct {
	Status        string `json:"status"`
	LatencyMs     *int   `json:"latency_ms,omitempty"`
	PingLatencyMs *int   `json:"ping_latency_ms,omitempty"`
	CheckedAt     string `json:"checked_at"`
}

type PublicTransitCacheDisclosure struct {
	Supported     bool     `json:"supported"`
	WriteUnit     string   `json:"write_unit,omitempty"`
	ReadUnit      string   `json:"read_unit,omitempty"`
	HitRate       *float64 `json:"hit_rate,omitempty"`
	HitRatePeriod string   `json:"hit_rate_period,omitempty"`
}

type PublicTransitSourceDisclosure struct {
	UpstreamType    string `json:"upstream_type"`
	AccountPoolType string `json:"account_pool_type"`
	IsMixed         bool   `json:"is_mixed"`
	IsReverse       bool   `json:"is_reverse"`
	Note            string `json:"note"`
}

type PublicTransitLimits struct {
	Concurrency       string `json:"concurrency,omitempty"`
	RPM               string `json:"rpm,omitempty"`
	TPM               string `json:"tpm,omitempty"`
	DailyQuota        string `json:"daily_quota,omitempty"`
	OverLimitBehavior string `json:"over_limit_behavior,omitempty"`
	DynamicRateLimit  string `json:"dynamic_rate_limit,omitempty"`
}

type PublicTransitCompleteness struct {
	HasRechargeRatio    bool     `json:"has_recharge_ratio"`
	HasGroupMultipliers bool     `json:"has_group_multipliers"`
	HasModelPricing     bool     `json:"has_model_pricing"`
	HasMonitoring       bool     `json:"has_monitoring"`
	HasSourceDisclosure bool     `json:"has_source_disclosure"`
	Warnings            []string `json:"warnings"`
}

type PublicTransitEndpoints struct {
	DiscoveryURL  string `json:"discovery_url"`
	SnapshotURL   string `json:"snapshot_url"`
	PublicPageURL string `json:"public_page_url,omitempty"`
}

func (s *PublicTransitService) Discovery(ctx context.Context, baseURL string) (*PublicTransitDiscovery, error) {
	if !s.Enabled(ctx) {
		return nil, infraerrors.NotFound("PUBLIC_TRANSIT_DISABLED", "public transit snapshot is disabled")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	runtime := s.settingService.GetPublicTransitRuntime(ctx)
	payload := &PublicTransitDiscovery{
		SchemaVersion: PublicTransitSchemaVersion,
		System:        PublicTransitSystem,
		SnapshotURL:   absoluteURL(baseURL, PublicTransitSnapshotPath),
		GeneratedAt:   now,
	}
	if runtime.PageEnabled {
		payload.HomepageURL = absoluteURL(baseURL, PublicTransitPagePath)
	}
	return payload, nil
}

func (s *PublicTransitService) Snapshot(ctx context.Context, baseURL string) (*PublicTransitSnapshot, error) {
	if !s.Enabled(ctx) {
		return nil, infraerrors.NotFound("PUBLIC_TRANSIT_DISABLED", "public transit snapshot is disabled")
	}

	publicSettings, err := s.settingService.GetPublicSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load public settings: %w", err)
	}

	paymentCfg, err := s.paymentConfig.GetPaymentConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load payment config: %w", err)
	}

	channels, err := s.channelService.ListAvailable(ctx)
	if err != nil {
		return nil, fmt.Errorf("list public channels: %w", err)
	}

	configuredGroups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list public groups: %w", err)
	}

	var monitorItems []PublicTransitMonitor
	if s.settingService.GetChannelMonitorRuntime(ctx).Enabled {
		monitors, err := s.monitorService.ListUserView(ctx)
		if err != nil {
			return nil, fmt.Errorf("list public monitors: %w", err)
		}
		monitorDetails, err := s.publicMonitorDetails(ctx, monitors)
		if err != nil {
			return nil, fmt.Errorf("load public monitor details: %w", err)
		}
		monitorItems = buildPublicTransitMonitors(monitors, monitorDetails)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	runtime := s.settingService.GetPublicTransitRuntime(ctx)
	cacheUsageByGroupID, err := s.publicGroupCacheUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("load public group cache usage: %w", err)
	}

	groups := buildPublicTransitGroups(configuredGroups, channels, cacheUsageByGroupID, publicPricingService(s.channelService))
	completeness := buildPublicTransitCompleteness(groups, monitorItems)

	station := PublicTransitStation{
		Name:        firstNonEmpty(publicSettings.SiteName, "Sub2API"),
		HomepageURL: absoluteURL(baseURL, "/home"),
		SupportURL:  publicSettings.ContactInfo,
		SystemType:  PublicTransitSystem,
	}
	endpoints := PublicTransitEndpoints{
		DiscoveryURL: absoluteURL(baseURL, PublicTransitWellKnownPath),
		SnapshotURL:  absoluteURL(baseURL, PublicTransitSnapshotPath),
	}
	if runtime.PageEnabled {
		station.PriceURL = absoluteURL(baseURL, PublicTransitPagePath)
		station.MonitorURL = absoluteURL(baseURL, PublicTransitPagePath+"?view=monitoring")
		endpoints.PublicPageURL = absoluteURL(baseURL, PublicTransitPagePath)
	}

	return &PublicTransitSnapshot{
		SchemaVersion: PublicTransitSchemaVersion,
		System:        PublicTransitSystem,
		GeneratedAt:   now,
		Station:       station,
		Billing: PublicTransitBilling{
			Currency:                 "CNY",
			CreditCurrency:           "USD",
			RechargeRatio:            fmt.Sprintf("1 CNY = %.8g USD balance", paymentCfg.BalanceRechargeMultiplier),
			RechargeMultiplier:       paymentCfg.BalanceRechargeMultiplier,
			RechargeMultiplierUnit:   "USD balance per 1 CNY",
			MinimumTopUp:             paymentCfg.MinAmount,
			ModelBasisPrice:          "USD per token/request from Sub2API channel pricing with LiteLLM fallback where configured",
			ModelPriceUnit:           "USD",
			StandardizedPriceVersion: PublicTransitSchemaVersion,
		},
		Groups:     groups,
		Monitoring: monitorItems,
		Cache: PublicTransitCacheDisclosure{
			Supported: hasCachePricing(groups),
			WriteUnit: "USD per token",
			ReadUnit:  "USD per token",
		},
		Disclosure: PublicTransitSourceDisclosure{
			UpstreamType:    "mixed",
			AccountPoolType: "mixed",
			IsMixed:         true,
			IsReverse:       true,
			Note:            "Sub2API public snapshot discloses normalized pricing and availability only. Exact upstream accounts, cookies, keys and internal channel IDs are intentionally omitted.",
		},
		Limits: PublicTransitLimits{
			DynamicRateLimit: "see group RPM and station policy",
		},
		Completeness: completeness,
		Endpoints:    endpoints,
	}, nil
}

func buildPublicTransitGroups(configuredGroups []Group, channels []AvailableChannel, cacheUsageByGroupID map[int64]PublicTransitCacheUsage, pricingService *PricingService) []PublicTransitGroup {
	type groupKey struct {
		id       int64
		name     string
		platform string
	}
	byKey := make(map[groupKey]*PublicTransitGroup)
	modelSeen := make(map[groupKey]map[string]struct{})
	groupByKey := make(map[groupKey]Group)

	for _, g := range configuredGroups {
		if g.Status != "" && g.Status != StatusActive {
			continue
		}
		if g.IsExclusive {
			continue
		}
		key := groupKey{id: g.ID, name: g.Name, platform: g.Platform}
		groupByKey[key] = g
		byKey[key] = &PublicTransitGroup{
			Name:             g.Name,
			Platform:         g.Platform,
			SubscriptionType: g.SubscriptionType,
			RateMultiplier:   g.RateMultiplier,
			IsExclusive:      false,
			CacheUsage:       publicCacheUsageForGroup(cacheUsageByGroupID, g.ID),
			Models:           []PublicTransitModel{},
		}
		modelSeen[key] = make(map[string]struct{})
	}

	for _, ch := range channels {
		if ch.Status != StatusActive {
			continue
		}
		for _, g := range ch.Groups {
			if g.IsExclusive {
				continue
			}
			key := groupKey{id: g.ID, name: g.Name, platform: g.Platform}
			out, ok := byKey[key]
			if !ok {
				groupByKey[key] = Group{
					ID:               g.ID,
					Name:             g.Name,
					Platform:         g.Platform,
					SubscriptionType: g.SubscriptionType,
					RateMultiplier:   g.RateMultiplier,
					IsExclusive:      g.IsExclusive,
					Status:           StatusActive,
				}
				byKey[key] = &PublicTransitGroup{
					Name:             g.Name,
					Platform:         g.Platform,
					SubscriptionType: g.SubscriptionType,
					RateMultiplier:   g.RateMultiplier,
					IsExclusive:      false,
					CacheUsage:       publicCacheUsageForGroup(cacheUsageByGroupID, g.ID),
					Models:           []PublicTransitModel{},
				}
				modelSeen[key] = make(map[string]struct{})
				out = byKey[key]
			}
			for _, m := range ch.SupportedModels {
				if m.Platform != g.Platform {
					continue
				}
				modelKey := strings.ToLower(m.Platform + "\x00" + m.Name)
				if _, exists := modelSeen[key][modelKey]; exists {
					continue
				}
				modelSeen[key][modelKey] = struct{}{}
				group := groupByKey[key]
				out.Models = append(out.Models, toPublicTransitModel(m, group))
			}
		}
	}

	for _, g := range configuredGroups {
		if g.Status != "" && g.Status != StatusActive {
			continue
		}
		if g.IsExclusive || !g.CustomModelsListEnabled() {
			continue
		}
		key := groupKey{id: g.ID, name: g.Name, platform: g.Platform}
		out, ok := byKey[key]
		if !ok {
			continue
		}
		for _, modelName := range g.ModelsListConfig.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				continue
			}
			modelKey := strings.ToLower(g.Platform + "\x00" + modelName)
			if _, exists := modelSeen[key][modelKey]; exists {
				continue
			}
			modelSeen[key][modelKey] = struct{}{}
			out.Models = append(out.Models, toPublicTransitModel(
				supportedModelFromPublicCatalog(g.Platform, modelName, pricingService),
				g,
			))
		}
	}

	groups := make([]PublicTransitGroup, 0, len(byKey))
	for _, g := range byKey {
		sort.SliceStable(g.Models, func(i, j int) bool {
			return strings.ToLower(g.Models[i].StandardModel) < strings.ToLower(g.Models[j].StandardModel)
		})
		groups = append(groups, *g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Platform == groups[j].Platform {
			return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name)
		}
		return groups[i].Platform < groups[j].Platform
	})
	return groups
}

func publicPricingService(channelService *ChannelService) *PricingService {
	if channelService == nil {
		return nil
	}
	return channelService.pricingService
}

func supportedModelFromPublicCatalog(platform, name string, pricingService *PricingService) SupportedModel {
	out := SupportedModel{
		Name:          name,
		Platform:      platform,
		PricingSource: ModelPriceSourceUnknown,
		CatalogSource: ModelCatalogSourceGroupModelsList,
	}
	if pricingService == nil {
		return out
	}
	if lp := pricingService.GetModelPricing(name); lp != nil {
		out.Pricing = synthesizePricingFromLiteLLM(lp, nil)
		out.PricingSource = ModelPriceSourceStandard
	}
	return out
}

func toPublicTransitModel(m SupportedModel, group Group) PublicTransitModel {
	billingMode := publicBillingMode(m.Pricing)
	priceSource := m.PricingSource
	if priceSource == "" {
		if m.Pricing != nil {
			priceSource = ModelPriceSourceCustom
		} else {
			priceSource = ModelPriceSourceUnknown
		}
	}
	catalogSource := m.CatalogSource
	if catalogSource == "" {
		catalogSource = ModelCatalogSourceChannel
	}
	out := PublicTransitModel{
		StandardModel:     m.Name,
		RawModel:          m.Name,
		Platform:          m.Platform,
		BillingMode:       billingMode,
		PriceSource:       priceSource,
		CatalogSource:     catalogSource,
		Price:             toPublicTransitPrice(m.Pricing, group),
		Source:            defaultPublicTransitModelSource(),
		SupportedProtocol: protocolsForPlatform(m.Platform),
	}
	if m.Pricing != nil {
		out.Intervals = toPublicTransitIntervals(m.Pricing.Intervals)
	}
	return out
}

func publicBillingMode(p *ChannelModelPricing) string {
	if p == nil || p.BillingMode == "" {
		return string(BillingModeToken)
	}
	if p.BillingMode == BillingModePerRequest || p.BillingMode == BillingModeImage {
		return string(BillingModePerRequest)
	}
	return string(BillingModeToken)
}

func toPublicTransitPrice(p *ChannelModelPricing, group Group) *PublicTransitModelPrice {
	if p == nil {
		return nil
	}
	return &PublicTransitModelPrice{
		InputUSDPerToken:       p.InputPrice,
		OutputUSDPerToken:      p.OutputPrice,
		CacheWriteUSDPerToken:  p.CacheWritePrice,
		CacheReadUSDPerToken:   p.CacheReadPrice,
		ImageOutputUSDPerToken: p.ImageOutputPrice,
		PerRequestUSD:          p.PerRequestPrice,
		ImageSizePrices:        publicImageSizePrices(p, group),
	}
}

func publicImageSizePrices(p *ChannelModelPricing, group Group) map[string]*float64 {
	if p == nil || (p.BillingMode != BillingModeImage && p.BillingMode != BillingModePerRequest) {
		return nil
	}
	prices := map[string]*float64{}
	if group.ImagePrice1K != nil {
		prices["1k"] = group.ImagePrice1K
	}
	if group.ImagePrice2K != nil {
		prices["2k"] = group.ImagePrice2K
	}
	if group.ImagePrice4K != nil {
		prices["4k"] = group.ImagePrice4K
	}
	if len(prices) == 0 {
		return nil
	}
	return prices
}

func toPublicTransitIntervals(src []PricingInterval) []PublicTransitPriceInterval {
	out := make([]PublicTransitPriceInterval, 0, len(src))
	for _, iv := range src {
		out = append(out, PublicTransitPriceInterval{
			MinTokens:             iv.MinTokens,
			MaxTokens:             iv.MaxTokens,
			TierLabel:             iv.TierLabel,
			InputUSDPerToken:      iv.InputPrice,
			OutputUSDPerToken:     iv.OutputPrice,
			CacheWriteUSDPerToken: iv.CacheWritePrice,
			CacheReadUSDPerToken:  iv.CacheReadPrice,
			PerRequestUSD:         iv.PerRequestPrice,
		})
	}
	return out
}

func defaultPublicTransitModelSource() PublicTransitModelSource {
	return PublicTransitModelSource{
		UpstreamType:    "mixed",
		AccountPoolType: "mixed",
		Disclosure:      "source disclosed at station level; account internals are not public",
	}
}

func protocolsForPlatform(platform string) []string {
	switch strings.ToLower(platform) {
	case "anthropic":
		return []string{"anthropic_messages", "openai_compatible"}
	case "openai":
		return []string{"openai_chat_completions", "openai_responses"}
	case "gemini":
		return []string{"gemini", "openai_compatible"}
	default:
		return []string{"openai_compatible"}
	}
}

func (s *PublicTransitService) publicGroupCacheUsage(ctx context.Context) (map[int64]PublicTransitCacheUsage, error) {
	if s == nil || s.usageRepo == nil {
		return map[int64]PublicTransitCacheUsage{}, nil
	}
	now := time.Now().UTC()
	rows, err := s.usageRepo.GetGroupCacheUsageSummary(ctx, now.Add(-24*time.Hour), now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}
	out := make(map[int64]PublicTransitCacheUsage, len(rows))
	for _, row := range rows {
		out[row.GroupID] = publicCacheUsageFromSummary(row)
	}
	return out, nil
}

func publicCacheUsageForGroup(cacheUsageByGroupID map[int64]PublicTransitCacheUsage, groupID int64) PublicTransitCacheUsage {
	if usage, ok := cacheUsageByGroupID[groupID]; ok {
		return usage
	}
	return PublicTransitCacheUsage{
		Last24h: PublicTransitCacheUsageWindow{Period: "last_24h"},
		Last7d:  PublicTransitCacheUsageWindow{Period: "last_7d"},
		Total:   PublicTransitCacheUsageWindow{Period: "total"},
	}
}

func publicCacheUsageFromSummary(row usagestats.GroupCacheUsageSummary) PublicTransitCacheUsage {
	return PublicTransitCacheUsage{
		Last24h: publicCacheUsageWindow("last_24h", row.Last24h),
		Last7d:  publicCacheUsageWindow("last_7d", row.Last7d),
		Total:   publicCacheUsageWindow("total", row.Total),
	}
}

func publicCacheUsageWindow(period string, src usagestats.GroupCacheUsageWindow) PublicTransitCacheUsageWindow {
	return PublicTransitCacheUsageWindow{
		Period:              period,
		InputTokens:         src.InputTokens,
		CacheCreationTokens: src.CacheCreationTokens,
		CacheReadTokens:     src.CacheReadTokens,
		CacheHitRate:        src.CacheHitRate,
	}
}

func (s *PublicTransitService) publicMonitorDetails(ctx context.Context, views []*UserMonitorView) (map[int64]*UserMonitorDetail, error) {
	out := make(map[int64]*UserMonitorDetail, len(views))
	for _, v := range views {
		if v == nil || v.ID <= 0 {
			continue
		}
		detail, err := s.monitorService.GetUserDetail(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		out[v.ID] = detail
	}
	return out, nil
}

func buildPublicTransitMonitors(views []*UserMonitorView, details map[int64]*UserMonitorDetail) []PublicTransitMonitor {
	out := make([]PublicTransitMonitor, 0, len(views))
	for _, v := range views {
		if v == nil {
			continue
		}
		extras := make([]PublicTransitExtraModelStatus, 0, len(v.ExtraModels))
		for _, e := range v.ExtraModels {
			extras = append(extras, PublicTransitExtraModelStatus{
				Model:     e.Model,
				Status:    e.Status,
				LatencyMs: e.LatencyMs,
			})
		}
		models := buildPublicTransitMonitorModels(details[v.ID])
		timeline := buildPublicTransitMonitorTimeline(v.Timeline)
		item := PublicTransitMonitor{
			Name:                v.Name,
			Provider:            v.Provider,
			GroupName:           v.GroupName,
			PrimaryModel:        v.PrimaryModel,
			PrimaryStatus:       v.PrimaryStatus,
			Availability7d:      v.Availability7d,
			LatestLatencyMs:     v.PrimaryLatencyMs,
			LatestPingLatencyMs: v.PrimaryPingLatencyMs,
			ExtraModels:         extras,
			Models:              models,
			Timeline:            timeline,
		}
		for _, m := range models {
			if m.Model != v.PrimaryModel {
				continue
			}
			item.Availability7d = m.Availability7d
			item.Availability15d = m.Availability15d
			item.Availability30d = m.Availability30d
			item.AvgLatency7dMs = m.AvgLatency7dMs
			break
		}
		if len(v.Timeline) > 0 {
			item.LastCheckedAt = v.Timeline[0].CheckedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return strings.ToLower(out[i].PrimaryModel) < strings.ToLower(out[j].PrimaryModel)
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func buildPublicTransitMonitorModels(detail *UserMonitorDetail) []PublicTransitMonitorModel {
	if detail == nil {
		return []PublicTransitMonitorModel{}
	}
	out := make([]PublicTransitMonitorModel, 0, len(detail.Models))
	for _, m := range detail.Models {
		out = append(out, PublicTransitMonitorModel{
			Model:           m.Model,
			LatestStatus:    m.LatestStatus,
			LatestLatencyMs: m.LatestLatencyMs,
			Availability7d:  m.Availability7d,
			Availability15d: m.Availability15d,
			Availability30d: m.Availability30d,
			AvgLatency7dMs:  m.AvgLatency7dMs,
		})
	}
	return out
}

func buildPublicTransitMonitorTimeline(src []UserMonitorTimelinePoint) []PublicTransitMonitorTimeline {
	out := make([]PublicTransitMonitorTimeline, 0, len(src))
	for _, point := range src {
		out = append(out, PublicTransitMonitorTimeline{
			Status:        point.Status,
			LatencyMs:     point.LatencyMs,
			PingLatencyMs: point.PingLatencyMs,
			CheckedAt:     point.CheckedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func buildPublicTransitCompleteness(groups []PublicTransitGroup, monitors []PublicTransitMonitor) PublicTransitCompleteness {
	c := PublicTransitCompleteness{
		HasRechargeRatio:    true,
		HasSourceDisclosure: true,
		HasMonitoring:       len(monitors) > 0,
	}
	for _, g := range groups {
		if g.RateMultiplier > 0 {
			c.HasGroupMultipliers = true
		}
		for _, m := range g.Models {
			if m.Price != nil {
				c.HasModelPricing = true
				break
			}
		}
	}
	if !c.HasGroupMultipliers {
		c.Warnings = append(c.Warnings, "no public non-exclusive group multiplier found")
	}
	if !c.HasModelPricing {
		c.Warnings = append(c.Warnings, "no public model pricing found")
	}
	if !c.HasMonitoring {
		c.Warnings = append(c.Warnings, "no enabled channel monitor found")
	}
	return c
}

func hasCachePricing(groups []PublicTransitGroup) bool {
	for _, g := range groups {
		for _, m := range g.Models {
			if m.Price != nil && (m.Price.CacheReadUSDPerToken != nil || m.Price.CacheWriteUSDPerToken != nil) {
				return true
			}
			for _, iv := range m.Intervals {
				if iv.CacheReadUSDPerToken != nil || iv.CacheWriteUSDPerToken != nil {
					return true
				}
			}
		}
	}
	return false
}

func absoluteURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}
