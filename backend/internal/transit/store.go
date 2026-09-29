package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var ErrDisabled = errors.New("public transit disabled")

type Store struct {
	DB      *sql.DB
	Config  Config
	Pricing *PricingService
}

func Open(c Config) (*Store, error) {
	p, err := LoadPricing(c.PricingFile)
	if err != nil {
		return nil, fmt.Errorf("load local pricing: %w", err)
	}
	db, err := sql.Open("postgres", c.DSN)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	return &Store{DB: db, Config: c, Pricing: p}, nil
}
func readRows(ctx context.Context, tx *sql.Tx, q string, args []any, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
func settings(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	out := map[string]string{}
	err := readRows(ctx, tx, "SELECT key,value FROM settings WHERE key IN ('public_transit_enabled','site_name','contact_info','MIN_RECHARGE_AMOUNT','BALANCE_RECHARGE_MULTIPLIER','channel_monitor_enabled','channel_monitor_mode')", nil, func(r *sql.Rows) error {
		var k, v string
		if e := r.Scan(&k, &v); e != nil {
			return e
		}
		out[k] = v
		return nil
	})
	return out, err
}
func (s *Store) Enabled(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	vals, err := settings(ctx, tx)
	if err != nil {
		return err
	}
	if isFalse(vals["public_transit_enabled"]) {
		return ErrDisabled
	}
	// Validate the core schema and SELECT privileges even on discovery/cache hits.
	rows, err := tx.QueryContext(ctx, coreSchemaQuery)
	if err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Snapshot(ctx context.Context) (*PublicTransitSnapshot, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	vals, err := settings(ctx, tx)
	if err != nil {
		return nil, err
	}
	if isFalse(vals["public_transit_enabled"]) {
		return nil, ErrDisabled
	}
	groups, err := loadGroups(ctx, tx)
	if err != nil {
		return nil, err
	}
	channels, err := s.loadChannels(ctx, tx, groups)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	usage, err := loadUsage(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	monitors, warnings, err := loadMonitors(ctx, tx, vals, now)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return assemble(s.Config, vals, buildPublicTransitGroups(groups, channels, usage, s.Pricing), monitors, warnings, now), nil
}
func isFalse(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "disabled":
		return true
	}
	return false
}
func positiveSetting(v string, def float64) float64 {
	n, e := strconv.ParseFloat(v, 64)
	if e != nil || n <= 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return def
	}
	return n
}
func assemble(c Config, vals map[string]string, groups []PublicTransitGroup, monitors []PublicTransitMonitor, warnings []string, now time.Time) *PublicTransitSnapshot {
	name := strings.TrimSpace(vals["site_name"])
	if name == "" {
		name = "Sub2API"
	}
	mult := positiveSetting(vals["BALANCE_RECHARGE_MULTIPLIER"], 1)
	completeness := buildPublicTransitCompleteness(groups, monitors)
	completeness.Warnings = append(completeness.Warnings, warnings...)
	return &PublicTransitSnapshot{
		SchemaVersion: PublicTransitSchemaVersion, System: PublicTransitSystem, GeneratedAt: now.Format(time.RFC3339),
		Station: PublicTransitStation{Name: name, HomepageURL: c.HomeURL, PriceURL: c.PriceURL, MonitorURL: c.MonitorURL, SupportURL: vals["contact_info"], SystemType: PublicTransitSystem},
		Billing: PublicTransitBilling{Currency: "CNY", CreditCurrency: "USD", RechargeRatio: fmt.Sprintf("1 CNY = %.8g USD balance", mult), RechargeMultiplier: mult, RechargeMultiplierUnit: "USD balance per 1 CNY", MinimumTopUp: numericSetting(vals["MIN_RECHARGE_AMOUNT"], 1), ModelBasisPrice: "USD per token/request from Sub2API channel pricing with LiteLLM fallback where configured", ModelPriceUnit: "USD", StandardizedPriceVersion: PublicTransitSchemaVersion},
		Groups:  groups, Monitoring: monitors, Cache: PublicTransitCacheDisclosure{Supported: hasCachePricing(groups), WriteUnit: "USD per token", ReadUnit: "USD per token"},
		Disclosure: PublicTransitSourceDisclosure{UpstreamType: "mixed", AccountPoolType: "mixed", IsMixed: true, IsReverse: true, Note: "Sub2API public snapshot discloses normalized pricing and availability only. Exact upstream accounts, cookies, keys and internal channel IDs are intentionally omitted."},
		Limits:     PublicTransitLimits{DynamicRateLimit: "see group RPM and station policy"}, Completeness: completeness,
		Endpoints: PublicTransitEndpoints{DiscoveryURL: absoluteURL(c.PublicBaseURL, PublicTransitWellKnownPath), SnapshotURL: absoluteURL(c.PublicBaseURL, PublicTransitSnapshotPath)},
	}
}
func loadGroups(ctx context.Context, tx *sql.Tx) ([]Group, error) {
	out := []Group{}
	err := readRows(ctx, tx, "SELECT id,name,platform,subscription_type,rate_multiplier,image_price_1k,image_price_2k,image_price_4k,models_list_config FROM groups WHERE status='active' AND NOT is_exclusive AND deleted_at IS NULL ORDER BY id", nil, func(r *sql.Rows) error {
		var g Group
		var raw []byte
		if err := r.Scan(&g.ID, &g.Name, &g.Platform, &g.SubscriptionType, &g.RateMultiplier, &g.ImagePrice1K, &g.ImagePrice2K, &g.ImagePrice4K, &raw); err != nil {
			return err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &g.ModelsListConfig); err != nil {
				return err
			}
		}
		g.Status = StatusActive
		out = append(out, g)
		return nil
	})
	return out, err
}
func (s *Store) loadChannels(ctx context.Context, tx *sql.Tx, groups []Group) ([]AvailableChannel, error) {
	type entry struct {
		id     int64
		ch     Channel
		groups []AvailableGroupRef
	}
	entries := []*entry{}
	byID := map[int64]*entry{}
	gm := map[int64]Group{}
	for _, g := range groups {
		gm[g.ID] = g
	}
	err := readRows(ctx, tx, "SELECT id,model_mapping FROM channels WHERE status='active' ORDER BY lower(name),id", nil, func(r *sql.Rows) error {
		e := &entry{}
		var raw []byte
		if err := r.Scan(&e.id, &raw); err != nil {
			return err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &e.ch.ModelMapping); err != nil {
				return err
			}
		}
		entries = append(entries, e)
		byID[e.id] = e
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = readRows(ctx, tx, "SELECT cg.channel_id,cg.group_id FROM channel_groups cg JOIN channels c ON c.id=cg.channel_id JOIN groups g ON g.id=cg.group_id WHERE c.status='active' AND g.status='active' AND NOT g.is_exclusive AND g.deleted_at IS NULL ORDER BY cg.id", nil, func(r *sql.Rows) error {
		var cid, gid int64
		if e := r.Scan(&cid, &gid); e != nil {
			return e
		}
		if ch := byID[cid]; ch != nil {
			g := gm[gid]
			ch.groups = append(ch.groups, AvailableGroupRef{ID: g.ID, Name: g.Name, Platform: g.Platform, SubscriptionType: g.SubscriptionType, RateMultiplier: g.RateMultiplier})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	pricing := map[int64]*ChannelModelPricing{}
	err = readRows(ctx, tx, "SELECT p.id,p.channel_id,p.platform,p.models,p.billing_mode,p.input_price,p.output_price,p.cache_write_price,p.cache_read_price,p.image_output_price,p.per_request_price FROM channel_model_pricing p JOIN channels c ON c.id=p.channel_id WHERE c.status='active' ORDER BY p.id", nil, func(r *sql.Rows) error {
		var p ChannelModelPricing
		var raw []byte
		if e := r.Scan(&p.ID, &p.ChannelID, &p.Platform, &raw, &p.BillingMode, &p.InputPrice, &p.OutputPrice, &p.CacheWritePrice, &p.CacheReadPrice, &p.ImageOutputPrice, &p.PerRequestPrice); e != nil {
			return e
		}
		if e := json.Unmarshal(raw, &p.Models); e != nil {
			return e
		}
		pricing[p.ID] = &p
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = readRows(ctx, tx, "SELECT i.pricing_id,i.min_tokens,i.max_tokens,COALESCE(i.tier_label,''),i.input_price,i.output_price,i.cache_write_price,i.cache_read_price,i.per_request_price FROM channel_pricing_intervals i JOIN channel_model_pricing p ON p.id=i.pricing_id JOIN channels c ON c.id=p.channel_id WHERE c.status='active' ORDER BY i.sort_order,i.id", nil, func(r *sql.Rows) error {
		var id int64
		var v PricingInterval
		if e := r.Scan(&id, &v.MinTokens, &v.MaxTokens, &v.TierLabel, &v.InputPrice, &v.OutputPrice, &v.CacheWritePrice, &v.CacheReadPrice, &v.PerRequestPrice); e != nil {
			return e
		}
		if p := pricing[id]; p != nil {
			p.Intervals = append(p.Intervals, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Stable pricing order is essential to first-wins deduplication.
	ids := make([]int64, 0, len(pricing))
	for id := range pricing {
		ids = append(ids, id)
	}
	sortIDs(ids)
	for _, id := range ids {
		p := pricing[id]
		if e := byID[p.ChannelID]; e != nil {
			e.ch.ModelPricing = append(e.ch.ModelPricing, *p)
		}
	}
	out := []AvailableChannel{}
	for _, e := range entries {
		models := e.ch.SupportedModels()
		for i := range models {
			m := &models[i]
			m.PricingSource = ModelPriceSourceCustom
			if pricingNeedsFallback(m.Pricing) {
				m.PricingSource = ModelPriceSourceUnknown
				if lp := s.Pricing.GetModelPricing(m.Name); lp != nil {
					m.Pricing = synthesizePricingFromLiteLLM(lp, m.Pricing)
					m.PricingSource = ModelPriceSourceStandard
				}
			}
		}
		out = append(out, AvailableChannel{Status: StatusActive, Groups: e.groups, SupportedModels: models})
	}
	return out, nil
}
func loadUsage(ctx context.Context, tx *sql.Tx, now time.Time) (map[int64]PublicTransitCacheUsage, error) {
	out := map[int64]PublicTransitCacheUsage{}
	query := "SELECT g.id,"
	for _, cut := range []string{"$1", "$2", "NULL"} {
		for j, col := range []string{"input_tokens", "cache_creation_tokens", "cache_read_tokens"} {
			if cut == "NULL" {
				query += "COALESCE(SUM(u." + col + "),0)"
			} else {
				query += "COALESCE(SUM(u." + col + ") FILTER (WHERE u.created_at >= " + cut + "),0)"
			}
			if cut != "NULL" || j != 2 {
				query += ","
			}
		}
	}
	query += " FROM groups g LEFT JOIN usage_logs u ON u.group_id=g.id WHERE g.status='active' AND NOT g.is_exclusive AND g.deleted_at IS NULL GROUP BY g.id"
	err := readRows(ctx, tx, query, []any{now.Add(-24 * time.Hour), now.Add(-7 * 24 * time.Hour)}, func(r *sql.Rows) error {
		var id int64
		u := publicCacheUsageForGroup(nil, 0)
		if e := r.Scan(&id, &u.Last24h.InputTokens, &u.Last24h.CacheCreationTokens, &u.Last24h.CacheReadTokens, &u.Last7d.InputTokens, &u.Last7d.CacheCreationTokens, &u.Last7d.CacheReadTokens, &u.Total.InputTokens, &u.Total.CacheCreationTokens, &u.Total.CacheReadTokens); e != nil {
			return e
		}
		for _, w := range []*PublicTransitCacheUsageWindow{&u.Last24h, &u.Last7d, &u.Total} {
			total := w.InputTokens + w.CacheCreationTokens + w.CacheReadTokens
			if total > 0 {
				w.CacheHitRate = float64(w.CacheReadTokens) / float64(total) * 100
			}
		}
		out[id] = u
		return nil
	})
	return out, err
}
func sortIDs(ids []int64) { sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] }) }

const coreSchemaQuery = "SELECT g.id,g.name,g.platform,g.subscription_type,g.rate_multiplier,g.image_price_1k,g.image_price_2k,g.image_price_4k,g.models_list_config,g.status,g.is_exclusive,g.deleted_at,c.id,c.name,c.status,c.model_mapping,cg.id,cg.channel_id,cg.group_id,p.id,p.channel_id,p.platform,p.models,p.billing_mode,p.input_price,p.output_price,p.cache_write_price,p.cache_read_price,p.image_output_price,p.per_request_price,i.id,i.pricing_id,i.min_tokens,i.max_tokens,i.tier_label,i.input_price,i.output_price,i.cache_write_price,i.cache_read_price,i.per_request_price,i.sort_order,u.group_id,u.created_at,u.input_tokens,u.cache_creation_tokens,u.cache_read_tokens FROM groups g,channels c,channel_groups cg,channel_model_pricing p,channel_pricing_intervals i,usage_logs u WHERE false"

func numericSetting(value string, fallback float64) float64 {
	n, e := strconv.ParseFloat(value, 64)
	if e != nil {
		return fallback
	}
	return n
}
