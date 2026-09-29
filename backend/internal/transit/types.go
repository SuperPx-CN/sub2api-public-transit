package transit

const (
	PublicTransitSchemaVersion = "ai-transit.v1"
	PublicTransitSystem        = "sub2api"
	PublicTransitWellKnownPath = "/.well-known/ai-transit.json"
	PublicTransitSnapshotPath  = "/api/public/transit/v1/snapshot"
	StatusActive               = "active"
)

type GroupModelsListConfig struct {
	Enabled bool
	Models  []string
}
type Group struct {
	ID                                       int64
	Name, Platform, SubscriptionType, Status string
	RateMultiplier                           float64
	IsExclusive                              bool
	ImagePrice1K, ImagePrice2K, ImagePrice4K *float64
	ModelsListConfig                         GroupModelsListConfig
}

func (g Group) CustomModelsListEnabled() bool {
	return g.ModelsListConfig.Enabled && len(g.ModelsListConfig.Models) > 0
}
