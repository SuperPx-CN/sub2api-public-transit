import axios from 'axios'
import type { GroupPlatform } from '@/types'

function buildRootUrl(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  if (typeof window === 'undefined') return normalized
  return `${window.location.origin}${normalized}`
}

export interface PublicTransitDiscovery {
  schema_version: string
  system: string
  snapshot_url: string
  homepage_url?: string
  generated_at: string
}

export interface PublicTransitSnapshot {
  schema_version: string
  system: string
  generated_at: string
  station: PublicTransitStation
  billing: PublicTransitBilling
  groups: PublicTransitGroup[]
  monitoring: PublicTransitMonitor[]
  cache: PublicTransitCacheDisclosure
  disclosure: PublicTransitSourceDisclosure
  limits: PublicTransitLimits
  completeness: PublicTransitCompleteness
  endpoints: PublicTransitEndpoints
}

export interface PublicTransitStation {
  name: string
  homepage_url: string
  price_url?: string
  monitor_url?: string
  support_url?: string
  system_type: string
}

export interface PublicTransitBilling {
  currency: string
  credit_currency: string
  recharge_ratio: string
  recharge_multiplier: number
  recharge_multiplier_unit: string
  minimum_top_up: number
  model_basis_price: string
  model_price_unit: string
  standardized_price_version: string
}

export interface PublicTransitGroup {
  name: string
  platform: GroupPlatform
  subscription_type?: string
  rate_multiplier: number
  is_exclusive: boolean
  cache_usage: PublicTransitCacheUsage
  models: PublicTransitModel[]
}

export interface PublicTransitCacheUsage {
  last_24h: PublicTransitCacheUsageWindow
  last_7d: PublicTransitCacheUsageWindow
  total?: PublicTransitCacheUsageWindow
}

export interface PublicTransitCacheUsageWindow {
  period: string
  input_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  cache_hit_rate: number
}

export interface PublicTransitModel {
  standard_model: string
  raw_model: string
  platform: GroupPlatform
  billing_mode: string
  price_source?: string
  catalog_source?: string
  price?: PublicTransitModelPrice
  source: PublicTransitModelSource
  supported_protocols: string[]
  intervals?: PublicTransitPriceInterval[]
}

export interface PublicTransitModelPrice {
  input_usd_per_token?: number
  output_usd_per_token?: number
  cache_write_usd_per_token?: number
  cache_read_usd_per_token?: number
  image_output_usd_per_token?: number
  per_request_usd?: number
  image_size_prices?: Record<string, number | null | undefined>
}

export interface PublicTransitPriceInterval {
  min_tokens: number
  max_tokens?: number
  tier_label?: string
  input_usd_per_token?: number
  output_usd_per_token?: number
  cache_write_usd_per_token?: number
  cache_read_usd_per_token?: number
  per_request_usd?: number
}

export interface PublicTransitModelSource {
  upstream_type: string
  account_pool_type: string
  disclosure: string
}

export interface PublicTransitMonitor {
  name: string
  provider: string
  group_name?: string
  primary_model: string
  primary_status: string
  availability_7d: number
  availability_15d: number
  availability_30d: number
  avg_latency_7d_ms?: number
  latest_latency_ms?: number
  latest_ping_latency_ms?: number
  last_checked_at?: string
  extra_models: PublicTransitExtraModelStatus[]
  models: PublicTransitMonitorModel[]
  timeline: PublicTransitMonitorTimeline[]
}

export interface PublicTransitExtraModelStatus {
  model: string
  status: string
  latency_ms?: number
}

export interface PublicTransitMonitorModel {
  model: string
  latest_status: string
  latest_latency_ms?: number
  availability_7d: number
  availability_15d: number
  availability_30d: number
  avg_latency_7d_ms?: number
}

export interface PublicTransitMonitorTimeline {
  status: string
  latency_ms?: number
  ping_latency_ms?: number
  checked_at: string
}

export interface PublicTransitCacheDisclosure {
  supported: boolean
  write_unit?: string
  read_unit?: string
  hit_rate?: number
  hit_rate_period?: string
}

export interface PublicTransitSourceDisclosure {
  upstream_type: string
  account_pool_type: string
  is_mixed: boolean
  is_reverse: boolean
  note: string
}

export interface PublicTransitLimits {
  concurrency?: string
  rpm?: string
  tpm?: string
  daily_quota?: string
  over_limit_behavior?: string
  dynamic_rate_limit?: string
}

export interface PublicTransitCompleteness {
  has_recharge_ratio: boolean
  has_group_multipliers: boolean
  has_model_pricing: boolean
  has_monitoring: boolean
  has_source_disclosure: boolean
  warnings: string[]
}

export interface PublicTransitEndpoints {
  discovery_url: string
  snapshot_url: string
  public_page_url?: string
}

export async function getPublicTransitSnapshot(options?: {
  signal?: AbortSignal
}): Promise<PublicTransitSnapshot> {
  const { data } = await axios.get<PublicTransitSnapshot>(
    buildRootUrl('/api/public/transit/v1/snapshot'),
    { signal: options?.signal },
  )
  return data
}

export async function getPublicTransitDiscovery(options?: {
  signal?: AbortSignal
}): Promise<PublicTransitDiscovery> {
  const { data } = await axios.get<PublicTransitDiscovery>(
    buildRootUrl('/.well-known/ai-transit.json'),
    { signal: options?.signal },
  )
  return data
}

export const publicTransitAPI = {
  getPublicTransitDiscovery,
  getPublicTransitSnapshot,
}

export default publicTransitAPI
