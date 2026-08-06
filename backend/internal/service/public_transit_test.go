//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestBuildPublicTransitGroups_FiltersExclusiveGroupsAndExportsPricing(t *testing.T) {
	configuredGroups := []Group{
		{
			ID:               10,
			Name:             "public-pro",
			Platform:         "anthropic",
			SubscriptionType: "standard",
			RateMultiplier:   1.25,
			Status:           StatusActive,
		},
		{
			ID:             11,
			Name:           "private-vip",
			Platform:       "anthropic",
			RateMultiplier: 0.8,
			Status:         StatusActive,
			IsExclusive:    true,
		},
	}
	channels := []AvailableChannel{{
		ID:     1,
		Name:   "primary",
		Status: StatusActive,
		Groups: []AvailableGroupRef{
			{
				ID:               10,
				Name:             "public-pro",
				Platform:         "anthropic",
				SubscriptionType: "standard",
				RateMultiplier:   1.25,
			},
			{
				ID:             11,
				Name:           "private-vip",
				Platform:       "anthropic",
				RateMultiplier: 0.8,
				IsExclusive:    true,
			},
		},
		SupportedModels: []SupportedModel{{
			Name:          "claude-sonnet-4",
			Platform:      "anthropic",
			PricingSource: ModelPriceSourceCustom,
			CatalogSource: ModelCatalogSourceChannel,
			Pricing: &ChannelModelPricing{
				Platform:        "anthropic",
				Models:          []string{"claude-sonnet-4"},
				BillingMode:     BillingModeToken,
				InputPrice:      testPtrFloat64(3e-6),
				OutputPrice:     testPtrFloat64(1.5e-5),
				CacheWritePrice: testPtrFloat64(3.75e-6),
				CacheReadPrice:  testPtrFloat64(3e-7),
			},
		}},
	}}

	cacheUsage := map[int64]PublicTransitCacheUsage{
		10: publicCacheUsageFromSummary(usagestats.GroupCacheUsageSummary{
			GroupID: 10,
			Last24h: usagestats.GroupCacheUsageWindow{
				InputTokens:         100,
				CacheCreationTokens: 20,
				CacheReadTokens:     30,
				CacheHitRate:        20,
			},
			Last7d: usagestats.GroupCacheUsageWindow{
				InputTokens:         400,
				CacheCreationTokens: 80,
				CacheReadTokens:     120,
				CacheHitRate:        20,
			},
			Total: usagestats.GroupCacheUsageWindow{
				InputTokens:         900,
				CacheCreationTokens: 100,
				CacheReadTokens:     1000,
				CacheHitRate:        50,
			},
		}),
	}
	groups := buildPublicTransitGroups(configuredGroups, channels, cacheUsage, nil)

	require.Len(t, groups, 1)
	require.Equal(t, "public-pro", groups[0].Name)
	require.False(t, groups[0].IsExclusive)
	require.InDelta(t, 1.25, groups[0].RateMultiplier, 1e-12)
	require.Equal(t, int64(30), groups[0].CacheUsage.Last24h.CacheReadTokens)
	require.InDelta(t, 20, groups[0].CacheUsage.Last24h.CacheHitRate, 1e-12)
	require.Equal(t, int64(120), groups[0].CacheUsage.Last7d.CacheReadTokens)
	require.InDelta(t, 50, groups[0].CacheUsage.Total.CacheHitRate, 1e-12)
	require.Len(t, groups[0].Models, 1)

	model := groups[0].Models[0]
	require.Equal(t, "claude-sonnet-4", model.StandardModel)
	require.Equal(t, "anthropic", model.Platform)
	require.Equal(t, string(BillingModeToken), model.BillingMode)
	require.Equal(t, ModelPriceSourceCustom, model.PriceSource)
	require.Equal(t, ModelCatalogSourceChannel, model.CatalogSource)
	require.NotNil(t, model.Price)
	require.InDelta(t, 3e-6, *model.Price.InputUSDPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, *model.Price.OutputUSDPerToken, 1e-12)
	require.True(t, hasCachePricing(groups))
}

func TestBuildPublicTransitGroups_ExportsConfiguredGroupsWithoutAvailableChannels(t *testing.T) {
	configuredGroups := []Group{
		{
			ID:               1,
			Name:             "gpt pro号池",
			Platform:         "openai",
			SubscriptionType: "standard",
			RateMultiplier:   0.2,
			Status:           StatusActive,
		},
		{
			ID:             2,
			Name:           "disabled",
			Platform:       "openai",
			RateMultiplier: 9,
			Status:         StatusDisabled,
		},
		{
			ID:             3,
			Name:           "exclusive",
			Platform:       "openai",
			RateMultiplier: 0.1,
			Status:         StatusActive,
			IsExclusive:    true,
		},
	}

	groups := buildPublicTransitGroups(configuredGroups, nil, nil, nil)

	require.Len(t, groups, 1)
	require.Equal(t, "gpt pro号池", groups[0].Name)
	require.Equal(t, "openai", groups[0].Platform)
	require.InDelta(t, 0.2, groups[0].RateMultiplier, 1e-12)
	require.Equal(t, "last_24h", groups[0].CacheUsage.Last24h.Period)
	require.Zero(t, groups[0].CacheUsage.Last24h.CacheHitRate)
	require.Empty(t, groups[0].Models)
}

func TestBuildPublicTransitGroups_ExportsEnabledGroupModelsList(t *testing.T) {
	groups := buildPublicTransitGroups([]Group{{
		ID:             1,
		Name:           "gpt free号池",
		Platform:       "openai",
		RateMultiplier: 0.1,
		Status:         StatusActive,
		ModelsListConfig: GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"gpt-5.5", "gpt-image-2"},
		},
	}}, nil, nil, nil)

	require.Len(t, groups, 1)
	require.Len(t, groups[0].Models, 2)
	require.Equal(t, "gpt-5.5", groups[0].Models[0].StandardModel)
	require.Equal(t, ModelCatalogSourceGroupModelsList, groups[0].Models[0].CatalogSource)
	require.Equal(t, ModelPriceSourceUnknown, groups[0].Models[0].PriceSource)
}

func TestToPublicTransitModel_NormalizesImageModeToPerRequest(t *testing.T) {
	price := 0.134
	group := Group{
		Platform:     "openai",
		ImagePrice1K: &price,
		ImagePrice2K: testPtrFloat64(0.201),
		ImagePrice4K: testPtrFloat64(0.268),
	}
	model := toPublicTransitModel(SupportedModel{
		Name:          "gpt-image-2",
		Platform:      "openai",
		PricingSource: ModelPriceSourceCustom,
		CatalogSource: ModelCatalogSourceChannel,
		Pricing: &ChannelModelPricing{
			BillingMode:     BillingModeImage,
			PerRequestPrice: &price,
		},
	}, group)

	require.Equal(t, string(BillingModePerRequest), model.BillingMode)
	require.NotNil(t, model.Price)
	require.Equal(t, &price, model.Price.PerRequestUSD)
	require.InDelta(t, 0.134, *model.Price.ImageSizePrices["1k"], 1e-12)
	require.InDelta(t, 0.201, *model.Price.ImageSizePrices["2k"], 1e-12)
	require.InDelta(t, 0.268, *model.Price.ImageSizePrices["4k"], 1e-12)
}
