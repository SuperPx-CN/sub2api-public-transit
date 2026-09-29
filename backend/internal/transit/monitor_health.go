package transit

type ChannelMonitorV2Metric struct {
	SuccessRequests          int64                   `json:"success_requests"`
	ErrorRequests            int64                   `json:"error_requests"`
	RequestCount             int64                   `json:"request_count"`
	InputTokens              int64                   `json:"input_tokens"`
	OutputTokens             int64                   `json:"output_tokens"`
	CacheCreationTokens      int64                   `json:"cache_creation_tokens"`
	CacheReadTokens          int64                   `json:"cache_read_tokens"`
	TokenCount               int64                   `json:"token_count"`
	RPM                      float64                 `json:"rpm"`
	TPM                      float64                 `json:"tpm"`
	ErrorRate                float64                 `json:"error_rate"`
	SuccessRate              float64                 `json:"success_rate"`
	CacheRate                float64                 `json:"cache_rate"`
	CacheRateNumerator       int64                   `json:"cache_rate_numerator"`
	CacheRateDenominator     int64                   `json:"cache_rate_denominator"`
	TTFT                     ChannelMonitorV2Latency `json:"ttft"`
	Duration                 ChannelMonitorV2Latency `json:"duration"`
	UpstreamAffectedRequests *int64                  `json:"upstream_affected_requests,omitempty"`
	UpstreamAttemptCount     *int64                  `json:"upstream_attempt_count,omitempty"`
}

type ChannelMonitorV2Latency struct {
	SampleCount int64    `json:"sample_count"`
	P50Ms       *int64   `json:"p50_ms"`
	P90Ms       *int64   `json:"p90_ms"`
	P95Ms       *int64   `json:"p95_ms"`
	AvgMs       *float64 `json:"avg_ms"`
}

type ChannelMonitorV2Health struct {
	Overall   string `json:"overall"`
	ErrorRate string `json:"error_rate"`
	TTFT      string `json:"ttft"`
	Cache     string `json:"cache"`
	// Score is 0–100 when samples are sufficient; omitted/null when unknown.
	// Overall blends error-rate, TTFT p50, and cache rate (weights in Thresholds).
	Score          *float64                         `json:"score,omitempty"`
	ErrorRateScore *float64                         `json:"error_rate_score,omitempty"`
	TTFTScore      *float64                         `json:"ttft_score,omitempty"`
	CacheScore     *float64                         `json:"cache_score,omitempty"`
	MinimumSample  int64                            `json:"minimum_sample"`
	Thresholds     ChannelMonitorV2HealthThresholds `json:"thresholds"`
}

type ChannelMonitorV2HealthThresholds struct {
	// MinimumSample is required before scoring request/latency/cache signals.
	MinimumSample int64 `json:"minimum_sample"`
	// WarningErrorRate / CriticalErrorRate map to discrete bands for legacy UI.
	WarningErrorRate  float64 `json:"warning_error_rate"`
	CriticalErrorRate float64 `json:"critical_error_rate"`
	// TargetTTFTMs is the primary TTFT p50 budget (ms). At or below this is full TTFT score.
	TargetTTFTMs   int64 `json:"target_ttft_ms"`
	WarningTTFTMs  int64 `json:"warning_ttft_ms"`
	CriticalTTFTMs int64 `json:"critical_ttft_ms"`
	// WarningCacheRate / CriticalCacheRate: cache rate below these → warning/critical bands.
	// Higher cache rate is better; defaults 20% warning / 5% critical.
	WarningCacheRate  float64 `json:"warning_cache_rate"`
	CriticalCacheRate float64 `json:"critical_cache_rate"`
	// ErrorWeight + TTFTWeight + CacheWeight should sum to 1.0.
	ErrorWeight float64 `json:"error_weight"`
	TTFTWeight  float64 `json:"ttft_weight"`
	CacheWeight float64 `json:"cache_weight"`
}

func DefaultChannelMonitorV2HealthThresholds() ChannelMonitorV2HealthThresholds {
	return ChannelMonitorV2HealthThresholds{
		MinimumSample:     50,
		WarningErrorRate:  0.05,
		CriticalErrorRate: 0.20,
		TargetTTFTMs:      3000,
		WarningTTFTMs:     3000,
		CriticalTTFTMs:    10000,
		// A zero/zero cache threshold means cache misses do not affect health
		// until an operator explicitly configures cache scoring.
		WarningCacheRate:  0,
		CriticalCacheRate: 0,
		ErrorWeight:       0.60,
		TTFTWeight:        0.20,
		CacheWeight:       0.20,
	}
}

func NormalizeChannelMonitorV2HealthThresholds(in ChannelMonitorV2HealthThresholds) ChannelMonitorV2HealthThresholds {
	def := DefaultChannelMonitorV2HealthThresholds()
	if in.MinimumSample <= 0 {
		in.MinimumSample = def.MinimumSample
	}
	if in.MinimumSample < 1 {
		in.MinimumSample = 1
	}
	if in.MinimumSample > 10000 {
		in.MinimumSample = 10000
	}
	if in.WarningErrorRate <= 0 {
		in.WarningErrorRate = def.WarningErrorRate
	}
	if in.CriticalErrorRate <= 0 {
		in.CriticalErrorRate = def.CriticalErrorRate
	}
	if in.CriticalErrorRate < in.WarningErrorRate {
		in.CriticalErrorRate = in.WarningErrorRate
	}
	if in.TargetTTFTMs <= 0 {
		in.TargetTTFTMs = def.TargetTTFTMs
	}
	if in.WarningTTFTMs <= 0 {
		in.WarningTTFTMs = def.WarningTTFTMs
	}
	if in.WarningTTFTMs < in.TargetTTFTMs {
		in.WarningTTFTMs = in.TargetTTFTMs + 1
	}
	if in.CriticalTTFTMs <= 0 {
		in.CriticalTTFTMs = def.CriticalTTFTMs
	}
	if in.CriticalTTFTMs < in.WarningTTFTMs {
		in.CriticalTTFTMs = in.WarningTTFTMs
	}
	if in.WarningCacheRate < 0 {
		in.WarningCacheRate = 0
	}
	if in.CriticalCacheRate < 0 {
		in.CriticalCacheRate = 0
	}
	if in.WarningCacheRate > 1 {
		in.WarningCacheRate = 1
	}
	if in.CriticalCacheRate > 1 {
		in.CriticalCacheRate = 1
	}
	if in.CriticalCacheRate > in.WarningCacheRate {
		in.CriticalCacheRate = in.WarningCacheRate
	}
	if in.ErrorWeight <= 0 && in.TTFTWeight <= 0 && in.CacheWeight <= 0 {
		in.ErrorWeight, in.TTFTWeight, in.CacheWeight = def.ErrorWeight, def.TTFTWeight, def.CacheWeight
	}
	return in
}

func ChannelMonitorV2HealthFor(metrics ChannelMonitorV2Metric) ChannelMonitorV2Health {
	return ChannelMonitorV2HealthForWithThresholds(metrics, DefaultChannelMonitorV2HealthThresholds())
}

func ChannelMonitorV2HealthForWithThresholds(metrics ChannelMonitorV2Metric, thresholds ChannelMonitorV2HealthThresholds) ChannelMonitorV2Health {
	thresholds = NormalizeChannelMonitorV2HealthThresholds(thresholds)
	result := ChannelMonitorV2Health{
		Overall: "unknown", ErrorRate: "unknown", TTFT: "unknown", Cache: "unknown",
		MinimumSample: thresholds.MinimumSample, Thresholds: thresholds,
	}

	type scored struct {
		score  float64
		weight float64
		band   string
	}
	parts := make([]scored, 0, 3)

	if metrics.RequestCount >= result.MinimumSample {
		s := errorRateScore(metrics.ErrorRate, thresholds.CriticalErrorRate)
		result.ErrorRateScore = &s
		result.ErrorRate = healthBand(metrics.ErrorRate, thresholds.WarningErrorRate, thresholds.CriticalErrorRate)
		parts = append(parts, scored{score: s, weight: thresholds.ErrorWeight, band: result.ErrorRate})
	}
	// Prefer p50 for TTFT scoring; fall back to p95 only if p50 is missing.
	if metrics.TTFT.SampleCount >= result.MinimumSample {
		var ttftMs *int64
		if metrics.TTFT.P50Ms != nil {
			ttftMs = metrics.TTFT.P50Ms
		} else if metrics.TTFT.P95Ms != nil {
			ttftMs = metrics.TTFT.P95Ms
		}
		if ttftMs != nil {
			s := ttftP50Score(float64(*ttftMs), float64(thresholds.TargetTTFTMs), float64(thresholds.CriticalTTFTMs))
			result.TTFTScore = &s
			result.TTFT = healthBand(float64(*ttftMs), float64(thresholds.WarningTTFTMs), float64(thresholds.CriticalTTFTMs))
			parts = append(parts, scored{score: s, weight: thresholds.TTFTWeight, band: result.TTFT})
		}
	}
	// Cache: need a meaningful denominator; higher rate is better.
	if metrics.CacheRateDenominator >= result.MinimumSample {
		s := cacheRateScore(metrics.CacheRate)
		if thresholds.WarningCacheRate <= 0 && thresholds.CriticalCacheRate <= 0 {
			// A zero/zero cache threshold means "do not penalize cache misses".
			s = 100
		}
		result.CacheScore = &s
		// Invert for healthBand (lower is worse): use (1 - rate) against warning/critical floors.
		result.Cache = cacheRateBand(metrics.CacheRate, thresholds.WarningCacheRate, thresholds.CriticalCacheRate)
		parts = append(parts, scored{score: s, weight: thresholds.CacheWeight, band: result.Cache})
	}

	if len(parts) == 0 {
		return result
	}
	var weightSum, scoreSum float64
	for _, p := range parts {
		weightSum += p.weight
		scoreSum += p.weight * p.score
	}
	if weightSum <= 0 {
		return result
	}
	overall := scoreSum / weightSum
	result.Score = &overall
	result.Overall = scoreBand(overall)
	return result
}

// errorRateScore maps error rate to 0–100. 0% → 100; at/above critical → 0 (linear).
func errorRateScore(errorRate, critical float64) float64 {
	if critical <= 0 {
		critical = 0.05
	}
	if errorRate <= 0 {
		return 100
	}
	if errorRate >= critical {
		return 0
	}
	return 100 * (1 - errorRate/critical)
}

// ttftP50Score maps TTFT p50 ms to 0–100.
// At/below target → 100; at/above critical → 0; linear in between.
func ttftP50Score(p50Ms, targetMs, criticalMs float64) float64 {
	if targetMs <= 0 {
		targetMs = 2500
	}
	if criticalMs <= targetMs {
		criticalMs = targetMs * 2.4
	}
	if p50Ms <= targetMs {
		return 100
	}
	if p50Ms >= criticalMs {
		return 0
	}
	return 100 * (1 - (p50Ms-targetMs)/(criticalMs-targetMs))
}

// cacheRateScore maps cache hit rate to 0–100 (higher is better, linear).
func cacheRateScore(cacheRate float64) float64 {
	if cacheRate <= 0 {
		return 0
	}
	if cacheRate >= 1 {
		return 100
	}
	return 100 * cacheRate
}

// cacheRateBand: below critical → critical; below warning → warning; else healthy.
func cacheRateBand(cacheRate, warning, critical float64) string {
	if cacheRate < critical {
		return "critical"
	}
	if cacheRate < warning {
		return "warning"
	}
	return "healthy"
}

// scoreBand maps continuous 0–100 scores to coarse labels for legacy consumers.
func scoreBand(score float64) string {
	switch {
	case score >= 80:
		return "healthy"
	case score >= 50:
		return "warning"
	default:
		return "critical"
	}
}

func healthBand(value, warning, critical float64) string {
	if value >= critical {
		return "critical"
	}
	if value >= warning {
		return "warning"
	}
	return "healthy"
}
