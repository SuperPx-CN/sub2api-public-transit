package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/lib/pq"
	"sort"
	"strings"
	"time"
)

func loadMonitors(ctx context.Context, tx *sql.Tx, settings map[string]string, now time.Time) ([]PublicTransitMonitor, []string, error) {
	empty := []PublicTransitMonitor{}
	if isFalse(settings["channel_monitor_enabled"]) {
		return empty, nil, nil
	}
	tables := []string{"channel_monitors", "channel_monitor_histories"}
	v2 := strings.EqualFold(strings.TrimSpace(settings["channel_monitor_mode"]), "v2")
	if v2 {
		tables = []string{"channel_monitor_v2_config", "channel_monitor_v2_metrics_rollup", "channel_monitor_v2_latency_histograms_rollup", "channel_monitor_v2_error_metrics_rollup", "channel_monitor_v2_watermarks"}
	}
	warnings := []string{}
	for _, table := range tables {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists); err != nil {
			return nil, nil, err
		}
		if !exists {
			warnings = append(warnings, "optional monitoring table missing: "+table)
		}
	}
	if len(warnings) > 0 {
		return empty, warnings, nil
	}
	var result []PublicTransitMonitor
	var err error
	if v2 {
		result, err = loadMonitorV2(ctx, tx, now)
	} else {
		result, err = loadMonitorV1(ctx, tx, now)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Provider == result[j].Provider {
			return strings.ToLower(result[i].PrimaryModel) < strings.ToLower(result[j].PrimaryModel)
		}
		return result[i].Provider < result[j].Provider
	})
	return result, nil, err
}
func loadMonitorV1(ctx context.Context, tx *sql.Tx, now time.Time) ([]PublicTransitMonitor, error) {
	type item struct {
		id     int64
		m      PublicTransitMonitor
		extras []string
	}
	items := []item{}
	err := readRows(ctx, tx, "SELECT id,name,provider,group_name,primary_model,extra_models FROM channel_monitors WHERE enabled ORDER BY id", nil, func(r *sql.Rows) error {
		v := item{}
		var raw []byte
		if e := r.Scan(&v.id, &v.m.Name, &v.m.Provider, &v.m.GroupName, &v.m.PrimaryModel, &raw); e != nil {
			return e
		}
		if e := json.Unmarshal(raw, &v.extras); e != nil {
			return e
		}
		items = append(items, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := []PublicTransitMonitor{}
	for _, v := range items {
		m := v.m
		m.ExtraModels = []PublicTransitExtraModelStatus{}
		m.Models = []PublicTransitMonitorModel{}
		m.Timeline = []PublicTransitMonitorTimeline{}
		latest := map[string]PublicTransitMonitorModel{}
		pings := map[string]*int{}
		err = readRows(ctx, tx, "SELECT DISTINCT ON (model) model,status,latency_ms,ping_latency_ms FROM channel_monitor_histories WHERE monitor_id=$1 ORDER BY model,checked_at DESC,id DESC", []any{v.id}, func(r *sql.Rows) error {
			var row PublicTransitMonitorModel
			var ping *int
			if e := r.Scan(&row.Model, &row.LatestStatus, &row.LatestLatencyMs, &ping); e != nil {
				return e
			}
			latest[row.Model] = row
			pings[row.Model] = ping
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(latest) == 0 {
			continue
		}
		query := "SELECT model,"
		for i, days := range []int{7, 15, 30} {
			_ = days
			cut := []string{"$2", "$3", "$4"}[i]
			query += "COALESCE(100.0*COUNT(*) FILTER (WHERE status IN ('operational','degraded') AND checked_at >= " + cut + ")/NULLIF(COUNT(*) FILTER (WHERE checked_at >= " + cut + "),0),0),"
		}
		query += "AVG(latency_ms) FILTER (WHERE checked_at >= $2) FROM channel_monitor_histories WHERE monitor_id=$1 AND checked_at >= $4 GROUP BY model"
		err = readRows(ctx, tx, query, []any{v.id, now.Add(-7 * 24 * time.Hour), now.Add(-15 * 24 * time.Hour), now.Add(-30 * 24 * time.Hour)}, func(r *sql.Rows) error {
			var name string
			var a, b, c float64
			var avg *float64
			if e := r.Scan(&name, &a, &b, &c, &avg); e != nil {
				return e
			}
			row := latest[name]
			row.Availability7d = a
			row.Availability15d = b
			row.Availability30d = c
			if avg != nil {
				n := int(*avg)
				row.AvgLatency7dMs = &n
			}
			latest[name] = row
			return nil
		})
		if err != nil {
			return nil, err
		}
		names := append([]string{m.PrimaryModel}, v.extras...)
		for _, name := range names {
			row := latest[name]
			row.Model = name
			m.Models = append(m.Models, row)
		}
		primary := latest[m.PrimaryModel]
		m.PrimaryStatus = primary.LatestStatus
		m.LatestLatencyMs = primary.LatestLatencyMs
		m.LatestPingLatencyMs = pings[m.PrimaryModel]
		m.Availability7d = primary.Availability7d
		m.Availability15d = primary.Availability15d
		m.Availability30d = primary.Availability30d
		m.AvgLatency7dMs = primary.AvgLatency7dMs
		for _, name := range v.extras {
			row := latest[name]
			m.ExtraModels = append(m.ExtraModels, PublicTransitExtraModelStatus{Model: name, Status: row.LatestStatus, LatencyMs: row.LatestLatencyMs})
		}
		err = readRows(ctx, tx, "SELECT status,latency_ms,ping_latency_ms,checked_at FROM channel_monitor_histories WHERE monitor_id=$1 AND model=$2 ORDER BY checked_at DESC,id DESC LIMIT 60", []any{v.id, m.PrimaryModel}, func(r *sql.Rows) error {
			var point PublicTransitMonitorTimeline
			var checked time.Time
			if e := r.Scan(&point.Status, &point.LatencyMs, &point.PingLatencyMs, &checked); e != nil {
				return e
			}
			point.CheckedAt = checked.UTC().Format(time.RFC3339)
			m.Timeline = append(m.Timeline, point)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(m.Timeline) > 0 {
			m.LastCheckedAt = m.Timeline[0].CheckedAt
		}
		out = append(out, m)
	}
	return out, nil
}

type monitorPlatform struct {
	Platform string
	Enabled  bool
	Models   []string
}

func loadMonitorV2(ctx context.Context, tx *sql.Tx, now time.Time) ([]PublicTransitMonitor, error) {
	out := []PublicTransitMonitor{}
	var enabled bool
	var raw, thresholdJSON []byte
	var groupIDs pq.Int64Array
	var ignored pq.StringArray
	err := tx.QueryRowContext(ctx, "SELECT enabled,platforms,group_ids,health_thresholds,COALESCE(ignored_error_categories,'{}') FROM channel_monitor_v2_config WHERE id=1").Scan(&enabled, &raw, &groupIDs, &thresholdJSON, &ignored)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if !enabled {
		return out, nil
	}
	var platforms []monitorPlatform
	if err = json.Unmarshal(raw, &platforms); err != nil {
		return nil, err
	}
	thresholds := DefaultChannelMonitorV2HealthThresholds()
	if len(thresholdJSON) > 0 {
		if err = json.Unmarshal(thresholdJSON, &thresholds); err != nil {
			return nil, err
		}
	}
	// Match the existing seven-day view's twelve-hour buckets and shared coverage boundary.
	end := now.UTC().Truncate(12 * time.Hour).Add(12 * time.Hour)
	start := end.Add(-7 * 24 * time.Hour)
	var usageStart, errorStart, through, computed *time.Time
	err = tx.QueryRowContext(ctx, "SELECT usage_coverage_start,error_coverage_start,data_through,last_successful_at FROM channel_monitor_v2_watermarks WHERE id=1").Scan(&usageStart, &errorStart, &through, &computed)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if through == nil || computed == nil {
		return out, nil
	}
	for _, t := range []*time.Time{usageStart, errorStart} {
		if t != nil && t.After(start) {
			start = *t
		}
	}
	if through.Before(end) {
		end = *through
	}
	where := " WHERE bucket_seconds=43200 AND bucket_start >= $1 AND bucket_start < $2"
	args := []any{start, end}
	if len(groupIDs) > 0 {
		where += " AND group_id=ANY($3)"
		args = append(args, groupIDs)
	}
	selected := map[string]monitorPlatform{}
	for _, p := range platforms {
		if p.Enabled {
			selected[p.Platform] = p
		}
	}
	display := func(platform, model string) (string, bool) {
		p, ok := selected[platform]
		if !ok {
			return "", false
		}
		model = strings.TrimSpace(model)
		if model == "" {
			return "__other__", true
		}
		if len(p.Models) == 0 {
			return model, true
		}
		for _, m := range p.Models {
			if m == model {
				return model, true
			}
		}
		return "__other__", true
	}
	type acc struct {
		metric  ChannelMonitorV2Metric
		sum     int64
		hist    map[int64]int64
		ignored int64
	}
	accs := map[string]*acc{}
	err = readRows(ctx, tx, "SELECT platform,model,SUM(success_requests),SUM(error_requests),SUM(input_tokens+cache_creation_tokens+cache_read_tokens),SUM(cache_read_tokens),SUM(ttft_sum_ms),SUM(ttft_count) FROM channel_monitor_v2_metrics_rollup"+where+" GROUP BY platform,model", args, func(r *sql.Rows) error {
		var platform, model string
		var success, fail, denom, read, sum, count int64
		if e := r.Scan(&platform, &model, &success, &fail, &denom, &read, &sum, &count); e != nil {
			return e
		}
		name, ok := display(platform, model)
		if !ok {
			return nil
		}
		key := platform + "\x00" + name
		a := accs[key]
		if a == nil {
			a = &acc{hist: map[int64]int64{}}
			accs[key] = a
		}
		a.metric.SuccessRequests += success
		a.metric.ErrorRequests += fail
		a.metric.CacheRateDenominator += denom
		a.metric.CacheRateNumerator += read
		a.sum += sum
		a.metric.TTFT.SampleCount += count
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = readRows(ctx, tx, "SELECT platform,model,upper_bound_ms,SUM(sample_count) FROM channel_monitor_v2_latency_histograms_rollup"+where+" AND user_id=0 AND metric='ttft' GROUP BY platform,model,upper_bound_ms", args, func(r *sql.Rows) error {
		var platform, model string
		var bound, count int64
		if e := r.Scan(&platform, &model, &bound, &count); e != nil {
			return e
		}
		name, ok := display(platform, model)
		if ok {
			if a := accs[platform+"\x00"+name]; a != nil {
				a.hist[bound] += count
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = readRows(ctx, tx, "SELECT platform,model,error_category,SUM(error_requests) FROM channel_monitor_v2_error_metrics_rollup"+where+" GROUP BY platform,model,error_category", args, func(r *sql.Rows) error {
		var platform, model, category string
		var n int64
		if e := r.Scan(&platform, &model, &category, &n); e != nil {
			return e
		}
		name, ok := display(platform, model)
		if ok {
			for _, c := range ignored {
				if c == category {
					if a := accs[platform+"\x00"+name]; a != nil {
						a.ignored += n
					}
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for key, a := range accs {
		m := a.metric
		m.RequestCount = m.SuccessRequests + m.ErrorRequests
		if m.RequestCount <= 0 {
			continue
		}
		m.SuccessRate = float64(m.SuccessRequests) / float64(m.RequestCount)
		counted := m.ErrorRequests - a.ignored
		if counted < 0 {
			counted = 0
		}
		m.ErrorRate = float64(counted) / float64(m.RequestCount)
		if m.CacheRateDenominator > 0 {
			m.CacheRate = float64(m.CacheRateNumerator) / float64(m.CacheRateDenominator)
		}
		var avg *int
		if m.TTFT.SampleCount > 0 {
			n := int(float64(a.sum)/float64(m.TTFT.SampleCount) + 0.5)
			avg = &n
		}
		m.TTFT.P50Ms = histPercentile(a.hist, 0.5)
		m.TTFT.P95Ms = histPercentile(a.hist, 0.95)
		health := ChannelMonitorV2HealthForWithThresholds(m, thresholds)
		parts := strings.SplitN(key, "\x00", 2)
		out = append(out, PublicTransitMonitor{Name: strings.TrimSpace(parts[0] + " " + parts[1]), Provider: parts[0], PrimaryModel: parts[1], PrimaryStatus: health.Overall, Availability7d: m.SuccessRate, AvgLatency7dMs: avg, Models: []PublicTransitMonitorModel{{Model: parts[1], LatestStatus: health.Overall, Availability7d: m.SuccessRate, AvgLatency7dMs: avg}}, Timeline: []PublicTransitMonitorTimeline{}})
	}
	return out, nil
}

func histPercentile(hist map[int64]int64, p float64) *int64 {
	var total int64
	keys := make([]int64, 0, len(hist))
	for bound, count := range hist {
		keys = append(keys, bound)
		total += count
	}
	if total == 0 {
		return nil
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	target := int64(float64(total)*p + 0.999999)
	var cumulative int64
	for _, bound := range keys {
		cumulative += hist[bound]
		if cumulative >= target {
			v := bound
			return &v
		}
	}
	v := keys[len(keys)-1]
	return &v
}
