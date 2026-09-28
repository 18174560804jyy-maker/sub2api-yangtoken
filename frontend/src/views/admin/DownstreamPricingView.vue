<template>
  <AppLayout>
    <div class="flex min-h-0 flex-col gap-4">
      <div class="flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 xl:flex-row xl:items-end xl:justify-between">
        <div class="flex flex-1 flex-wrap items-end gap-3">
          <div class="w-full sm:w-72">
            <label class="input-label">{{ t('admin.channels.downstreamPricing.channel') }}</label>
            <Select
              v-model="selectedChannelId"
              :options="channelOptions"
              :disabled="loadingChannels || saving"
              searchable
              @change="loadPricing"
            />
          </div>
          <div class="w-40">
            <label class="input-label">{{ t('admin.channels.downstreamPricing.markup') }}</label>
            <div class="relative">
              <input
                v-model.number="markupPercent"
                type="number"
                min="0"
                max="10000"
                step="0.1"
                class="input pr-8"
                :disabled="!selectedChannelId || saving"
                @input="recalculateAutomaticPrices"
              />
              <span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-400">%</span>
            </div>
          </div>
          <div class="relative w-full sm:w-64">
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="search" type="search" class="input pl-10" :placeholder="t('admin.channels.downstreamPricing.search')" />
          </div>
        </div>
        <div class="flex items-center justify-end gap-2">
          <button class="btn btn-secondary" :disabled="!selectedChannelId || loading || saving" :title="t('common.refresh')" @click="loadPricing">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button class="btn btn-primary" :disabled="!selectedChannelId || loading || saving || models.length === 0" @click="savePricing">
            <Icon name="check" size="md" class="mr-2" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500 dark:text-gray-400">
        <div class="flex min-w-0 items-center gap-2">
          <span class="h-2 w-2 flex-shrink-0 rounded-full" :class="stale ? 'bg-amber-500' : 'bg-emerald-500'" />
          <span>{{ statusText }}</span>
          <code class="hidden truncate rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700 lg:inline">{{ sourceUrl }}</code>
        </div>
        <span>{{ t('admin.channels.downstreamPricing.modelCount', { count: filteredModels.length }) }}</span>
      </div>

      <div v-if="warning" class="border-l-4 border-amber-500 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
        {{ warning }}
      </div>

      <div class="min-h-0 overflow-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <table class="w-full min-w-[2100px] table-fixed">
          <thead class="sticky top-0 z-10 bg-gray-50 dark:bg-dark-800">
            <tr class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <th class="w-64 px-4 py-3">{{ t('admin.channels.downstreamPricing.model') }}</th>
              <th class="w-28 px-3 py-3">{{ t('admin.channels.downstreamPricing.provider') }}</th>
              <th v-for="field in priceFields" :key="`official-${field.key}`" class="w-28 border-l border-gray-200 px-3 py-3 text-right dark:border-dark-700">{{ field.officialLabel }}</th>
              <th v-for="field in priceFields" :key="`upstream-${field.key}`" class="w-28 px-3 py-3 text-right">{{ field.label }}</th>
              <th class="w-28 border-l border-gray-200 px-3 py-3 text-center dark:border-dark-700">{{ t('admin.channels.downstreamPricing.herohaoMultiple') }}</th>
              <th class="w-32 px-3 py-3 text-center">{{ t('admin.channels.downstreamPricing.mode') }}</th>
              <th v-for="field in priceFields" :key="`effective-${field.key}`" class="w-32 px-3 py-3 text-right">{{ field.effectiveLabel }}</th>
              <th class="w-28 border-l border-gray-200 px-3 py-3 text-center dark:border-dark-700">{{ t('admin.channels.downstreamPricing.ourMultiple') }}</th>
              <th class="w-20 px-3 py-3 text-center">{{ t('admin.channels.downstreamPricing.enabled') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="18" class="px-4 py-16 text-center text-sm text-gray-500">{{ t('common.loading') }}</td>
            </tr>
            <tr v-else-if="filteredModels.length === 0">
              <td colspan="18" class="px-4 py-16 text-center text-sm text-gray-500">{{ t('admin.channels.downstreamPricing.empty') }}</td>
            </tr>
            <template v-else>
            <tr v-for="row in filteredModels" :key="row.model.toLowerCase()" class="border-b border-gray-100 text-sm last:border-0 dark:border-dark-700/70">
              <td class="px-4 py-3">
                <div class="truncate font-medium text-gray-900 dark:text-white" :title="row.model">{{ row.model }}</div>
                <div class="mt-0.5 truncate text-xs text-gray-400">{{ row.provider_key }}</div>
              </td>
              <td class="truncate px-3 py-3 text-gray-600 dark:text-gray-300" :title="row.provider">{{ row.provider || '-' }}</td>
              <td v-for="field in priceFields" :key="`official-${row.model}-${field.key}`" class="border-l border-gray-100 px-3 py-3 text-right font-mono text-xs text-gray-500 dark:border-dark-700/70 dark:text-gray-400">
                {{ formatPrice(row.official[field.key]) }}
              </td>
              <td v-for="field in priceFields" :key="`base-${row.model}-${field.key}`" class="px-3 py-3 text-right font-mono text-xs text-gray-600 dark:text-gray-300">
                {{ formatPrice(row.selling[field.key]) }}
              </td>
              <td class="border-l border-gray-100 px-3 py-3 text-center font-mono text-xs text-gray-600 dark:border-dark-700/70 dark:text-gray-300">{{ formatMultiples(row.multiple) }}</td>
              <td class="px-3 py-3">
                <div class="grid grid-cols-2 overflow-hidden rounded-md border border-gray-200 dark:border-dark-600">
                  <button type="button" class="h-8 text-xs" :class="!row.manual ? activeModeClass : inactiveModeClass" @click="setManual(row, false)">{{ t('admin.channels.downstreamPricing.auto') }}</button>
                  <button type="button" class="h-8 border-l border-gray-200 text-xs dark:border-dark-600" :class="row.manual ? activeModeClass : inactiveModeClass" @click="setManual(row, true)">{{ t('admin.channels.downstreamPricing.manual') }}</button>
                </div>
              </td>
              <td v-for="field in priceFields" :key="`effective-${row.model}-${field.key}`" class="px-2 py-2 text-right">
                <input
                  v-if="row.manual"
                  v-model.number="row.effective[field.key]"
                  type="number"
                  min="0"
                  step="0.0001"
                  class="input h-8 px-2 text-right font-mono text-xs"
                  :aria-label="`${row.model} ${field.effectiveLabel}`"
                />
                <span v-else class="font-mono text-xs font-medium text-gray-900 dark:text-white">{{ formatPrice(row.effective[field.key]) }}</span>
              </td>
              <td class="border-l border-gray-100 px-3 py-3 text-center font-mono text-xs font-medium text-primary-600 dark:border-dark-700/70 dark:text-primary-400">{{ formatMultiples(effectiveMultiples(row)) }}</td>
              <td class="px-3 py-3 text-center"><Toggle v-model="row.enabled" /></td>
            </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import adminAPI from '@/api/admin'
import type { Channel, DownstreamMultipleSet, DownstreamPriceSet, DownstreamPricingModel, DownstreamPricingView } from '@/api/admin/channels'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

type PriceKey = keyof DownstreamPriceSet

const { t } = useI18n()
const appStore = useAppStore()
const channels = ref<Channel[]>([])
const selectedChannelId = ref<number | null>(null)
const markupPercent = ref(0)
const models = ref<DownstreamPricingModel[]>([])
const sourceUrl = ref('https://sub2.herohao.top/pricing/api/pricing')
const snapshotFetchedAt = ref<string | undefined>()
const warning = ref('')
const stale = ref(false)
const search = ref('')
const loadingChannels = ref(false)
const loading = ref(false)
const saving = ref(false)

const activeModeClass = 'bg-primary-600 text-white'
const inactiveModeClass = 'bg-white text-gray-500 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-400 dark:hover:bg-dark-700'

const priceFields = computed<Array<{ key: PriceKey; label: string; officialLabel: string; effectiveLabel: string }>>(() => [
  { key: 'input', label: t('admin.channels.downstreamPricing.herohaoInput'), officialLabel: t('admin.channels.downstreamPricing.officialInput'), effectiveLabel: t('admin.channels.downstreamPricing.priceInput') },
  { key: 'output', label: t('admin.channels.downstreamPricing.herohaoOutput'), officialLabel: t('admin.channels.downstreamPricing.officialOutput'), effectiveLabel: t('admin.channels.downstreamPricing.priceOutput') },
  { key: 'cache_read', label: t('admin.channels.downstreamPricing.herohaoCacheRead'), officialLabel: t('admin.channels.downstreamPricing.officialCacheRead'), effectiveLabel: t('admin.channels.downstreamPricing.priceCacheRead') },
  { key: 'cache_write', label: t('admin.channels.downstreamPricing.herohaoCacheWrite'), officialLabel: t('admin.channels.downstreamPricing.officialCacheWrite'), effectiveLabel: t('admin.channels.downstreamPricing.priceCacheWrite') }
])

const channelOptions = computed(() => channels.value.map(channel => ({ value: channel.id, label: channel.name })))
const filteredModels = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return models.value
  return models.value.filter(row => `${row.model} ${row.provider} ${row.provider_key}`.toLowerCase().includes(query))
})
const statusText = computed(() => {
  if (!snapshotFetchedAt.value) return t('admin.channels.downstreamPricing.notRefreshed')
  return t(stale.value ? 'admin.channels.downstreamPricing.snapshotStatus' : 'admin.channels.downstreamPricing.liveStatus', {
    time: new Date(snapshotFetchedAt.value).toLocaleString()
  })
})

function formatPrice(value: number): string {
  return Number(value || 0).toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: 6 })
}

function formatMultiples(values: DownstreamMultipleSet): string {
  const unique = Array.from(new Set(Object.values(values).filter((value): value is number => value != null).map(value => value.toFixed(2))))
  if (unique.length === 0) return '—'
  if (unique.length === 1) return `${unique[0]}×`
  return `${unique[0]}–${unique[unique.length - 1]}×`
}

function effectiveMultiples(row: DownstreamPricingModel): DownstreamMultipleSet {
  const ratio = (value: number, official: number) => official > 0 ? value / official : null
  return {
    input: ratio(row.effective.input, row.official.input),
    output: ratio(row.effective.output, row.official.output),
    cache_read: ratio(row.effective.cache_read, row.official.cache_read),
    cache_write: ratio(row.effective.cache_write, row.official.cache_write),
  }
}

function applyView(view: DownstreamPricingView) {
  markupPercent.value = view.markup_percent
  models.value = view.models
  sourceUrl.value = view.source_url
  snapshotFetchedAt.value = view.snapshot_fetched_at
  stale.value = view.stale
  warning.value = view.warning || ''
}

function automaticPrices(selling: DownstreamPriceSet): DownstreamPriceSet {
  const factor = 1 + Number(markupPercent.value || 0) / 100
  return {
    input: selling.input * factor,
    output: selling.output * factor,
    cache_read: selling.cache_read * factor,
    cache_write: selling.cache_write * factor
  }
}

function recalculateAutomaticPrices() {
  for (const row of models.value) {
    if (!row.manual) row.effective = automaticPrices(row.selling)
  }
}

function setManual(row: DownstreamPricingModel, manual: boolean) {
  row.manual = manual
  if (!manual) row.effective = automaticPrices(row.selling)
}

async function loadChannels() {
  loadingChannels.value = true
  try {
    const result = await adminAPI.channels.list(1, 1000, { sort_by: 'name', sort_order: 'asc' })
    channels.value = result.items
    if (!selectedChannelId.value && channels.value.length > 0) selectedChannelId.value = channels.value[0].id
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channels.downstreamPricing.loadChannelsFailed')))
  } finally {
    loadingChannels.value = false
  }
}

async function loadPricing() {
  if (!selectedChannelId.value || loading.value) return
  loading.value = true
  try {
    applyView(await adminAPI.channels.getDownstreamPricing(selectedChannelId.value))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channels.downstreamPricing.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function savePricing() {
  if (!selectedChannelId.value || saving.value) return
  saving.value = true
  try {
    const view = await adminAPI.channels.saveDownstreamPricing(selectedChannelId.value, {
      markup_percent: Number(markupPercent.value),
      models: models.value.map(row => ({ model: row.model, enabled: row.enabled, manual: row.manual, prices: { ...row.effective } }))
    })
    applyView(view)
    appStore.showSuccess(t('admin.channels.downstreamPricing.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channels.downstreamPricing.saveFailed')))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  await loadChannels()
  await loadPricing()
})
</script>
