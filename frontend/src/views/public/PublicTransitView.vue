<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 bg-white/95 dark:border-dark-800 dark:bg-dark-900/95">
      <div class="mx-auto flex max-w-7xl flex-col gap-4 px-4 py-5 sm:px-6 lg:flex-row lg:items-center lg:justify-between lg:px-8">
        <div>
          <router-link to="/home" class="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-primary-600 dark:text-dark-300 dark:hover:text-primary-400">
            <Icon name="arrowLeft" size="sm" />
            {{ t('publicTransit.backHome') }}
          </router-link>
          <h1 class="mt-3 text-2xl font-semibold tracking-normal text-gray-950 dark:text-white">
            {{ snapshot?.station.name || t('publicTransit.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
            {{ t('publicTransit.subtitle') }}
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" type="button" @click="copy(discoveryUrl)">
            <Icon name="copy" size="sm" />
            {{ t('publicTransit.copyDiscovery') }}
          </button>
          <button class="btn btn-secondary" type="button" @click="copy(snapshotUrl)">
            <Icon name="copy" size="sm" />
            {{ t('publicTransit.copySnapshot') }}
          </button>
          <a class="btn btn-primary" :href="snapshotUrl" target="_blank" rel="noopener noreferrer">
            <Icon name="externalLink" size="sm" />
            JSON
          </a>
        </div>
      </div>
    </header>

    <main class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
      <div v-if="loading" class="grid gap-4 md:grid-cols-3">
        <Skeleton v-for="i in 6" :key="i" class="h-28" />
      </div>

      <div v-else-if="error" class="rounded-lg border border-red-200 bg-red-50 p-6 text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-200">
        <div class="flex items-start gap-3">
          <Icon name="exclamationCircle" size="lg" class="mt-0.5" />
          <div>
            <h2 class="text-base font-semibold">{{ t('publicTransit.disabledTitle') }}</h2>
            <p class="mt-1 text-sm">{{ t('publicTransit.disabledDesc') }}</p>
          </div>
        </div>
      </div>

      <template v-else-if="snapshot">
        <section class="grid gap-4 lg:grid-cols-4">
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('publicTransit.rechargeRatio') }}</p>
            <p class="mt-2 text-lg font-semibold">{{ snapshot.billing.recharge_ratio }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('publicTransit.groups') }}</p>
            <p class="mt-2 text-lg font-semibold">{{ snapshot.groups.length }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('publicTransit.models') }}</p>
            <p class="mt-2 text-lg font-semibold">{{ modelCount }}</p>
          </div>
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-800 dark:bg-dark-900">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('publicTransit.generatedAt') }}</p>
            <p class="mt-2 text-sm font-medium">{{ formatDate(snapshot.generated_at) }}</p>
          </div>
        </section>

        <section class="mt-5 rounded-lg border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900">
          <div class="flex flex-col gap-3 border-b border-gray-200 p-4 dark:border-dark-800 md:flex-row md:items-center md:justify-between">
            <div>
              <h2 class="text-base font-semibold">{{ t('publicTransit.modelPricing') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('publicTransit.modelPricingHint') }}</p>
            </div>
            <div class="grid w-full gap-2 sm:w-auto sm:grid-cols-[minmax(220px,320px)_160px]">
              <input
                v-model.trim="search"
                type="search"
                class="input h-9 w-full min-w-0"
                :placeholder="t('publicTransit.searchPlaceholder')"
              />
              <select v-model="platformFilter" class="input h-9 w-full min-w-0 pr-8">
                <option value="">{{ t('publicTransit.allPlatforms') }}</option>
                <option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option>
              </select>
            </div>
          </div>
          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-800">
              <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-950 dark:text-dark-400">
                <tr>
                  <th class="px-4 py-3 text-left font-medium">{{ t('publicTransit.group') }}</th>
                  <th class="px-4 py-3 text-left font-medium">{{ t('publicTransit.multiplier') }}</th>
                  <th class="px-4 py-3 text-left font-medium">{{ t('publicTransit.models') }}</th>
                  <th class="px-4 py-3 text-left font-medium">{{ t('publicTransit.availability') }}</th>
                  <th class="px-4 py-3 text-left font-medium">{{ t('publicTransit.totalCacheHitRate') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <template v-for="row in filteredGroups" :key="row.key">
                  <tr
                    class="cursor-pointer select-none transition-colors hover:bg-gray-50 focus-within:bg-gray-50 dark:hover:bg-dark-800/60 dark:focus-within:bg-dark-800/60"
                    tabindex="0"
                    @click="toggleGroup(row.key)"
                    @keydown.enter.prevent="toggleGroup(row.key)"
                    @keydown.space.prevent="toggleGroup(row.key)"
                  >
                    <td class="px-4 py-3">
                      <div class="flex min-w-56 items-center gap-3 text-left">
                        <Icon
                          :name="expandedGroups.has(row.key) ? 'chevronDown' : 'chevronRight'"
                          size="sm"
                          class="flex-shrink-0 text-gray-400 transition-transform"
                        />
                        <span
                          class="inline-flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg"
                          :class="platformIconShellClass(row.group.platform)"
                        >
                          <PlatformIcon :platform="row.group.platform" size="md" />
                        </span>
                        <span class="min-w-0">
                          <span class="block truncate font-medium">{{ row.group.name }}</span>
                          <span class="block truncate text-xs" :class="platformTextClass(row.group.platform)">
                            {{ platformLabel(row.group.platform) }}
                          </span>
                        </span>
                      </div>
                    </td>
                    <td class="px-4 py-3">
                      <span
                        class="inline-flex min-w-[58px] items-center justify-center rounded-full px-2.5 py-1 text-xs font-semibold tabular-nums ring-1"
                        :class="multiplierBadgeClass(row.group.rate_multiplier)"
                      >
                        {{ formatMultiplier(row.group.rate_multiplier) }}
                      </span>
                    </td>
                    <td class="px-4 py-3">
                      <span class="font-medium">{{ row.models.length }}</span>
                    </td>
                    <td class="px-4 py-3 text-xs">
                      <div v-if="row.monitorSummary" class="space-y-1">
                        <span
                          class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px]"
                          :class="monitorFormat.statusBadgeClass(row.monitorSummary.status)"
                        >
                          {{ monitorFormat.statusLabel(row.monitorSummary.status) }}
                        </span>
                        <div class="text-gray-500 dark:text-dark-400">
                          {{ row.monitorSummary.model }}
                        </div>
                      </div>
                      <span v-else class="text-gray-400">-</span>
                    </td>
                    <td class="px-4 py-3 text-xs">
                      <div
                        class="inline-flex min-w-[190px] items-center justify-between gap-3 rounded-lg px-3 py-2 ring-1"
                        :class="cacheUsageBadgeClass(row.group)"
                      >
                        <div class="min-w-0">
                          <div class="font-medium">{{ t('publicTransit.total') }}</div>
                          <div class="mt-0.5 whitespace-nowrap text-[11px] opacity-80">
                            {{ t('publicTransit.cacheHit') }} {{ totalCacheUsageRow(row.group).read }} /
                            {{ t('publicTransit.cacheCreate') }} {{ totalCacheUsageRow(row.group).create }}
                          </div>
                        </div>
                        <span class="shrink-0 text-sm font-semibold tabular-nums">
                          {{ totalCacheUsageRow(row.group).rate }}
                        </span>
                      </div>
                    </td>
                  </tr>
                  <tr v-if="expandedGroups.has(row.key)" class="bg-gray-50/70 dark:bg-dark-950/40">
                    <td colspan="5" class="px-4 py-4">
                      <div v-if="row.models.length > 0" class="overflow-x-auto rounded-lg border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900">
                        <table class="min-w-[1120px] text-xs">
                          <thead class="bg-gray-50 text-gray-500 dark:bg-dark-950 dark:text-dark-400">
                            <tr>
                              <th class="min-w-[220px] whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.model') }}</th>
                              <th class="whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.billingMode') }}</th>
                              <th class="whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.priceSource') }}</th>
                              <th class="whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.inputPrice') }}</th>
                              <th class="whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.outputPrice') }}</th>
                              <th class="whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.cacheInputPrice') }}</th>
                              <th class="whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.cacheCreatePrice') }}</th>
                              <th class="min-w-[180px] whitespace-nowrap px-3 py-2 text-left font-medium">{{ t('publicTransit.perRequestPrice') }}</th>
                            </tr>
                          </thead>
                          <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                            <tr v-for="model in row.models" :key="`${row.key}:${model.standard_model}`">
                              <td class="px-3 py-2 font-mono">
                                <span class="block truncate" :title="model.standard_model">{{ model.standard_model }}</span>
                              </td>
                              <td class="px-3 py-2 whitespace-nowrap">{{ formatBillingMode(model.billing_mode) }}</td>
                              <td class="whitespace-nowrap px-3 py-2">{{ formatPriceSource(model.price_source) }}</td>
                              <td class="whitespace-nowrap px-3 py-2 font-mono tabular-nums">{{ formatModelTokenPrice(model, model.price?.input_usd_per_token) }}</td>
                              <td class="whitespace-nowrap px-3 py-2 font-mono tabular-nums">{{ formatModelTokenPrice(model, model.price?.output_usd_per_token) }}</td>
                              <td class="whitespace-nowrap px-3 py-2 font-mono tabular-nums">{{ formatModelTokenPrice(model, model.price?.cache_read_usd_per_token) }}</td>
                              <td class="whitespace-nowrap px-3 py-2 font-mono tabular-nums">{{ formatModelTokenPrice(model, model.price?.cache_write_usd_per_token) }}</td>
                              <td class="px-3 py-2">
                                <div v-if="requestPriceParts(model).length > 0" class="flex flex-wrap gap-1 font-mono tabular-nums">
                                  <div
                                    v-for="part in requestPriceParts(model)"
                                    :key="part.label"
                                    class="inline-flex min-w-0 items-center gap-1 rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-800"
                                  >
                                    <span class="shrink-0 text-gray-500 dark:text-dark-400">{{ part.label }}</span>
                                    <span class="min-w-0 truncate" :title="part.value">{{ part.value }}</span>
                                  </div>
                                </div>
                                <span v-else class="font-mono text-gray-500 dark:text-dark-400">-</span>
                              </td>
                            </tr>
                          </tbody>
                        </table>
                      </div>
                      <div v-else class="rounded-lg border border-dashed border-gray-200 bg-white p-4 text-sm text-gray-500 dark:border-dark-800 dark:bg-dark-900 dark:text-dark-400">
                        {{ t('publicTransit.emptyGroupModels') }}
                      </div>
                    </td>
                  </tr>
                </template>
                <tr v-if="filteredGroups.length === 0">
                  <td colspan="5" class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">
                    {{ t('publicTransit.emptyModels') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="mt-5 rounded-lg border border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900">
          <div class="flex flex-col gap-3 border-b border-gray-200 p-4 dark:border-dark-800 md:flex-row md:items-center md:justify-between">
            <div>
              <h2 class="text-base font-semibold">{{ t('publicTransit.monitoring') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('publicTransit.monitoringHint') }}</p>
            </div>
            <div class="inline-flex w-fit rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-700 dark:bg-dark-950">
              <button
                v-for="win in monitorWindows"
                :key="win"
                type="button"
                :class="[
                  'rounded-md px-3 py-1.5 text-xs font-medium transition-colors',
                  monitorWindow === win
                    ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white'
                    : 'text-gray-500 hover:text-gray-800 dark:text-dark-300 dark:hover:text-white',
                ]"
                @click="monitorWindow = win"
              >
                {{ t(`channelStatus.windowTab.${win}`) }}
              </button>
            </div>
          </div>
          <div class="p-4">
            <div class="public-monitor-grid">
              <MonitorCardGrid
                :items="publicMonitorViews"
                :window="monitorWindow"
                :countdown-seconds="null"
                :loading="false"
                :detail-cache="publicMonitorDetailCache"
                @card-click="openMonitorDetail"
              />
            </div>
          </div>
        </section>

        <p class="mt-4 text-xs leading-relaxed text-gray-500 dark:text-dark-400">
          {{ t('publicTransit.publicNote', {
            upstream: snapshot.disclosure.upstream_type,
            accountPool: snapshot.disclosure.account_pool_type,
          }) }}
        </p>
      </template>
    </main>

    <BaseDialog
      :show="showMonitorDetail"
      :title="selectedMonitorDetail?.name || t('channelStatus.detailTitle')"
      width="wide"
      @close="showMonitorDetail = false"
    >
      <div v-if="selectedMonitorDetail" class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead class="border-b border-gray-200 dark:border-dark-700">
            <tr class="text-xs uppercase tracking-wider text-gray-500 dark:text-gray-400">
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.model') }}</th>
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestStatus') }}</th>
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestLatency') }}</th>
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability7d') }}</th>
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability15d') }}</th>
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability30d') }}</th>
              <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.avgLatency7d') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="m in selectedMonitorDetail.models"
              :key="m.model"
              class="border-b border-gray-100 dark:border-dark-800"
            >
              <td class="py-2 pr-3 font-medium text-gray-900 dark:text-gray-100">{{ m.model }}</td>
              <td class="py-2 pr-3">
                <span
                  class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px]"
                  :class="monitorFormat.statusBadgeClass(m.latest_status)"
                >
                  {{ monitorFormat.statusLabel(m.latest_status) }}
                </span>
              </td>
              <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ monitorFormat.formatLatency(m.latest_latency_ms) }}</td>
              <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ monitorFormat.formatPercent(m.availability_7d) }}</td>
              <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ monitorFormat.formatPercent(m.availability_15d) }}</td>
              <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ monitorFormat.formatPercent(m.availability_30d) }}</td>
              <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ monitorFormat.formatLatency(m.avg_latency_7d_ms) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <template #footer>
        <div class="flex justify-end">
          <button type="button" class="btn btn-secondary" @click="showMonitorDetail = false">
            {{ t('channelStatus.closeDetail') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import type { MonitorStatus, Provider, UserMonitorDetail, UserMonitorView } from '@/api/channelMonitor'
import {
  getPublicTransitSnapshot,
  type PublicTransitCacheUsageWindow,
  type PublicTransitGroup,
  type PublicTransitModel,
  type PublicTransitMonitor,
  type PublicTransitSnapshot,
} from '@/api/publicTransit'

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const monitorFormat = useChannelMonitorFormat()

const snapshot = ref<PublicTransitSnapshot | null>(null)
const loading = ref(true)
const error = ref('')
const search = ref('')
const platformFilter = ref('')
const expandedGroups = ref<Set<string>>(new Set())
const monitorWindow = ref<'7d' | '15d' | '30d'>('7d')
const monitorWindows = ['7d', '15d', '30d'] as const
const showMonitorDetail = ref(false)
const selectedMonitorId = ref<number | null>(null)

const discoveryUrl = computed(() => snapshot.value?.endpoints.discovery_url || `${window.location.origin}/.well-known/ai-transit.json`)
const snapshotUrl = computed(() => snapshot.value?.endpoints.snapshot_url || `${window.location.origin}/api/public/transit/v1/snapshot`)

const modelCount = computed(() => {
  return (snapshot.value?.groups || []).reduce((total, group) => total + (group.models?.length || 0), 0)
})
const platforms = computed(() => Array.from(new Set((snapshot.value?.groups || []).map((group) => group.platform))).sort())
const monitorByGroup = computed(() => {
  const out = new Map<string, PublicTransitMonitor>()
  for (const item of snapshot.value?.monitoring || []) {
    if (!item.group_name) continue
    out.set(groupKey({ name: item.group_name, platform: item.provider } as PublicTransitGroup), item)
    out.set(item.group_name.toLowerCase(), item)
  }
  return out
})
const filteredGroups = computed(() => {
  const q = search.value.toLowerCase()
  return (snapshot.value?.groups || []).flatMap((group) => {
    const models = group.models || []
    const matchedModels = !q
      ? models
      : models.filter((model) => model.standard_model.toLowerCase().includes(q))
    const matchesSearch =
      !q ||
      group.name.toLowerCase().includes(q) ||
      group.platform.toLowerCase().includes(q) ||
      matchedModels.length > 0
    if ((!platformFilter.value || group.platform === platformFilter.value) && matchesSearch) {
      const key = groupKey(group)
      return [{
        key,
        group,
        models: q && !group.name.toLowerCase().includes(q) && !group.platform.toLowerCase().includes(q)
          ? matchedModels
          : models,
        monitorSummary: monitorSummaryForGroup(group),
      }]
    }
    return []
  })
})

const publicMonitorViews = computed<UserMonitorView[]>(() => {
  return (snapshot.value?.monitoring || []).map((item, index) => publicMonitorToUserView(item, index + 1))
})

const publicMonitorDetailCache = computed<Record<number, UserMonitorDetail>>(() => {
  const out: Record<number, UserMonitorDetail> = {}
  for (const [index, item] of (snapshot.value?.monitoring || []).entries()) {
    const id = index + 1
    out[id] = publicMonitorToUserDetail(item, id)
  }
  return out
})

const selectedMonitorDetail = computed(() => {
  if (selectedMonitorId.value == null) return null
  return publicMonitorDetailCache.value[selectedMonitorId.value] || null
})

function formatPrice(value?: number) {
  if (value == null) return '-'
  if (!Number.isFinite(value)) return '-'
  const abs = Math.abs(value)
  const fractionDigits = abs > 0 && abs < 0.000001 ? 10 : 8
  const normalized = value.toFixed(fractionDigits).replace(/\.?0+$/, '')
  return `$${normalized}`
}

function formatTokenPrice(value?: number) {
  if (value == null || !Number.isFinite(value)) return '-'
  return formatCompactUsd(value * 1_000_000)
}

function formatModelTokenPrice(model: PublicTransitModel, value?: number) {
  if (model.billing_mode === 'per_request') return '-'
  return formatTokenPrice(value)
}

function formatCompactUsd(value: number) {
  const abs = Math.abs(value)
  const fractionDigits = abs >= 100 ? 2 : abs >= 1 ? 4 : 6
  const normalized = value.toFixed(fractionDigits).replace(/\.?0+$/, '')
  return `$${normalized}`
}

function requestPriceParts(model: PublicTransitModel) {
  const parts: Array<{ label: string; value: string }> = []
  if (model.price?.per_request_usd != null) {
    parts.push({ label: t('publicTransit.billingPerRequest'), value: formatCompactUsd(model.price.per_request_usd) })
  }
  const imagePrices = model.price?.image_size_prices || {}
  for (const size of ['1k', '2k', '4k']) {
    const value = imagePrices[size]
    if (value != null) {
      parts.push({ label: size.toUpperCase(), value: formatPrice(value) })
    }
  }
  return parts
}

function formatBillingMode(value?: string) {
  if (value === 'per_request') return t('publicTransit.billingPerRequest')
  if (value === 'token') return t('publicTransit.billingToken')
  return value || '-'
}

function formatPriceSource(value?: string) {
  if (value === 'custom') return t('publicTransit.priceSourceCustom')
  if (value === 'standard') return t('publicTransit.priceSourceStandard')
  if (value === 'unknown') return t('publicTransit.priceSourceUnknown')
  return value || '-'
}

function platformLabel(platform: string) {
  switch (platform) {
    case 'anthropic':
      return 'Anthropic'
    case 'openai':
      return 'OpenAI'
    case 'gemini':
      return 'Gemini'
    case 'antigravity':
      return 'Antigravity'
    case 'grok':
      return 'Grok'
    default:
      return platform
  }
}

function platformIconShellClass(platform: string) {
  switch (platform) {
    case 'anthropic':
      return 'bg-orange-50 text-orange-600 ring-1 ring-orange-100 dark:bg-orange-950/30 dark:text-orange-300 dark:ring-orange-900/50'
    case 'openai':
      return 'bg-emerald-50 text-emerald-600 ring-1 ring-emerald-100 dark:bg-emerald-950/30 dark:text-emerald-300 dark:ring-emerald-900/50'
    case 'gemini':
      return 'bg-sky-50 text-sky-600 ring-1 ring-sky-100 dark:bg-sky-950/30 dark:text-sky-300 dark:ring-sky-900/50'
    case 'antigravity':
      return 'bg-purple-50 text-purple-600 ring-1 ring-purple-100 dark:bg-purple-950/30 dark:text-purple-300 dark:ring-purple-900/50'
    case 'grok':
      return 'bg-slate-100 text-slate-700 ring-1 ring-slate-200 dark:bg-slate-800 dark:text-slate-200 dark:ring-slate-700'
    default:
      return 'bg-gray-100 text-gray-600 ring-1 ring-gray-200 dark:bg-dark-800 dark:text-dark-300 dark:ring-dark-700'
  }
}

function platformTextClass(platform: string) {
  switch (platform) {
    case 'anthropic':
      return 'text-orange-600 dark:text-orange-300'
    case 'openai':
      return 'text-emerald-600 dark:text-emerald-300'
    case 'gemini':
      return 'text-sky-600 dark:text-sky-300'
    case 'antigravity':
      return 'text-purple-600 dark:text-purple-300'
    case 'grok':
      return 'text-slate-600 dark:text-slate-300'
    default:
      return 'text-gray-500 dark:text-dark-400'
  }
}

function formatPercent(value: number) {
  return `${value.toFixed(1)}%`
}

function formatMultiplier(value: number) {
  if (!Number.isFinite(value)) return '-'
  const normalized = Number.isInteger(value) ? value.toFixed(0) : value.toFixed(3).replace(/\.?0+$/, '')
  return `${normalized}x`
}

function multiplierBadgeClass(value: number) {
  if (!Number.isFinite(value)) {
    return 'bg-gray-100 text-gray-500 ring-gray-200 dark:bg-dark-800 dark:text-dark-300 dark:ring-dark-700'
  }
  if (value <= 0.25) {
    return 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300 dark:ring-emerald-900/60'
  }
  if (value < 1) {
    return 'bg-teal-50 text-teal-700 ring-teal-200 dark:bg-teal-950/40 dark:text-teal-300 dark:ring-teal-900/60'
  }
  if (value === 1) {
    return 'bg-slate-50 text-slate-700 ring-slate-200 dark:bg-dark-800 dark:text-dark-200 dark:ring-dark-700'
  }
  if (value <= 2) {
    return 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950/40 dark:text-amber-300 dark:ring-amber-900/60'
  }
  return 'bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950/40 dark:text-rose-300 dark:ring-rose-900/60'
}

function formatTokenCompact(value?: number) {
  const safe = value != null && Number.isFinite(value) ? value : 0
  if (safe >= 1_000_000_000) return `${(safe / 1_000_000_000).toFixed(1)}B`
  if (safe >= 1_000_000) return `${(safe / 1_000_000).toFixed(1)}M`
  if (safe >= 1_000) return `${(safe / 1_000).toFixed(1)}K`
  return String(safe)
}

function totalCacheUsageRow(group: PublicTransitGroup) {
  return cacheUsageRow(group.cache_usage?.total)
}

function cacheUsageBadgeClass(group: PublicTransitGroup) {
  const usage = group.cache_usage?.total
  const rate = usage?.cache_hit_rate ?? 0
  const hasCacheTokens = (usage?.cache_read_tokens ?? 0) > 0 || (usage?.cache_creation_tokens ?? 0) > 0
  if (!hasCacheTokens && rate <= 0) {
    return 'bg-gray-50 text-gray-500 ring-gray-100 dark:bg-dark-900 dark:text-dark-300 dark:ring-dark-800'
  }
  if (rate >= 80) {
    return 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950/35 dark:text-emerald-300 dark:ring-emerald-900/60'
  }
  if (rate >= 50) {
    return 'bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950/35 dark:text-sky-300 dark:ring-sky-900/60'
  }
  if (rate > 0) {
    return 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950/35 dark:text-amber-300 dark:ring-amber-900/60'
  }
  return 'bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950/35 dark:text-rose-300 dark:ring-rose-900/60'
}

function cacheUsageRow(usage?: PublicTransitCacheUsageWindow) {
  return {
    rate: formatPercent(usage?.cache_hit_rate ?? 0),
    read: formatTokenCompact(usage?.cache_read_tokens),
    create: formatTokenCompact(usage?.cache_creation_tokens),
  }
}

function formatDate(value: string) {
  if (!value) return '-'
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

function normalizeStatus(status: string): MonitorStatus {
  if (status === 'operational' || status === 'degraded' || status === 'failed' || status === 'error') {
    return status
  }
  return 'error'
}

function normalizeProvider(provider: string): Provider {
  if (provider === 'anthropic' || provider === 'gemini') return provider
  return 'openai'
}

function groupKey(group: Pick<PublicTransitGroup, 'name' | 'platform'>) {
  return `${group.platform}:${group.name}`
}

function toggleGroup(key: string) {
  const next = new Set(expandedGroups.value)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  expandedGroups.value = next
}

function monitorSummaryForGroup(group: PublicTransitGroup) {
  const item = monitorByGroup.value.get(groupKey(group)) || monitorByGroup.value.get(group.name.toLowerCase())
  if (!item) return null
  return {
    status: normalizeStatus(item.primary_status),
    model: item.primary_model,
  }
}

function publicMonitorToUserView(item: PublicTransitMonitor, id: number): UserMonitorView {
  return {
    id,
    name: item.name,
    provider: normalizeProvider(item.provider),
    group_name: item.group_name || '',
    primary_model: item.primary_model,
    primary_status: normalizeStatus(item.primary_status),
    primary_latency_ms: item.latest_latency_ms ?? null,
    primary_ping_latency_ms: item.latest_ping_latency_ms ?? null,
    availability_7d: item.availability_7d,
    extra_models: (item.extra_models || []).map((m) => ({
      model: m.model,
      status: normalizeStatus(m.status),
      latency_ms: m.latency_ms ?? null,
    })),
    timeline: (item.timeline || []).map((point) => ({
      status: normalizeStatus(point.status),
      latency_ms: point.latency_ms ?? null,
      ping_latency_ms: point.ping_latency_ms ?? null,
      checked_at: point.checked_at,
    })),
  }
}

function publicMonitorToUserDetail(item: PublicTransitMonitor, id: number): UserMonitorDetail {
  const models = item.models?.length
    ? item.models
    : [{
        model: item.primary_model,
        latest_status: item.primary_status,
        latest_latency_ms: item.latest_latency_ms,
        availability_7d: item.availability_7d,
        availability_15d: item.availability_15d,
        availability_30d: item.availability_30d,
        avg_latency_7d_ms: item.avg_latency_7d_ms,
      }]
  return {
    id,
    name: item.name,
    provider: normalizeProvider(item.provider),
    group_name: item.group_name || '',
    models: models.map((m) => ({
      model: m.model,
      latest_status: normalizeStatus(m.latest_status),
      latest_latency_ms: m.latest_latency_ms ?? null,
      availability_7d: m.availability_7d,
      availability_15d: m.availability_15d,
      availability_30d: m.availability_30d,
      avg_latency_7d_ms: m.avg_latency_7d_ms ?? null,
    })),
  }
}

function openMonitorDetail(item: UserMonitorView) {
  selectedMonitorId.value = item.id
  showMonitorDetail.value = true
}

async function copy(value: string) {
  await copyToClipboard(value)
}

onMounted(async () => {
  loading.value = true
  error.value = ''
  try {
    snapshot.value = await getPublicTransitSnapshot()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.public-monitor-grid :deep(> div > .grid) {
  grid-template-columns: repeat(auto-fit, minmax(320px, 380px));
}

@media (max-width: 640px) {
  .public-monitor-grid :deep(> div > .grid) {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
