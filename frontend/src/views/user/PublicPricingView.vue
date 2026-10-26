<template>
  <AppLayout>
    <div class="flex min-h-0 flex-col gap-5">
      <div class="flex flex-col gap-3 border-b border-gray-200 pb-5 dark:border-dark-700 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('pricing.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('pricing.description') }}</p>
        </div>
        <div class="relative w-full sm:w-72">
          <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input v-model="search" type="search" class="input pl-10" :placeholder="t('pricing.search')" />
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <span class="mr-1 text-sm text-gray-500 dark:text-gray-400">{{ t('pricing.provider') }}</span>
        <button
          v-for="filter in providerFilters"
          :key="filter.name"
          type="button"
          class="rounded-full border px-3 py-1.5 text-xs transition-colors"
          :class="filter.name === selectedProvider ? 'border-primary-600 bg-primary-600 text-white' : 'border-gray-200 text-gray-600 hover:border-primary-400 dark:border-dark-600 dark:text-gray-300'"
          @click="selectedProvider = filter.name"
        >
          {{ filter.name }} <span class="ml-1 opacity-70">{{ filter.count }}</span>
        </button>
        <span v-if="snapshotFetchedAt" class="ml-auto text-xs text-gray-400 dark:text-gray-500">
          {{ t('pricing.updatedAt', { time: new Date(snapshotFetchedAt).toLocaleString() }) }}
        </span>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-20"><LoadingSpinner /></div>
      <div v-else-if="error" class="border-l-4 border-amber-500 bg-amber-50 px-4 py-4 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">{{ error }}</div>
      <div v-else class="min-h-0 overflow-auto rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800">
        <table class="w-full min-w-[860px] table-fixed">
          <thead class="sticky top-0 z-10 bg-gray-50 dark:bg-dark-800">
            <tr class="border-b border-gray-200 text-center text-xs font-medium text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <th class="w-56 px-4 py-3 text-left">{{ t('pricing.model') }}</th>
              <th class="w-28 px-3 py-3 text-left">{{ t('pricing.providerColumn') }}</th>
              <th v-for="field in priceFields" :key="field.key" class="px-3 py-3 text-right">{{ field.label }}</th>
              <th class="w-32 px-3 py-3">{{ t('pricing.ourMultiple') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="filteredModels.length === 0">
              <td colspan="7" class="px-4 py-16 text-center text-sm text-gray-500">{{ t('pricing.empty') }}</td>
            </tr>
            <tr v-for="row in filteredModels" :key="row.model.toLowerCase()" class="border-b border-gray-100 text-sm last:border-0 dark:border-dark-700/70">
              <td class="px-4 py-3">
                <div class="truncate font-medium text-gray-900 dark:text-white" :title="row.model">{{ row.model }}</div>
                <div class="mt-0.5 truncate text-xs text-gray-400">{{ row.provider_key }}</div>
              </td>
              <td class="truncate px-3 py-3 text-gray-600 dark:text-gray-300" :title="row.provider">{{ row.provider || '-' }}</td>
              <td v-for="field in priceFields" :key="`${row.model}-${field.key}`" class="px-3 py-2.5 text-right">
                <div class="flex items-baseline justify-end gap-2 text-xs text-gray-400 dark:text-gray-500">
                  <span class="whitespace-nowrap text-[10px]">{{ t('pricing.official') }}</span>
                  <span class="font-mono tabular-nums">{{ formatPrice(row.official[field.key]) }}</span>
                </div>
                <div class="mt-1 flex items-baseline justify-end gap-2 text-sm font-semibold text-primary-700 dark:text-primary-400">
                  <span class="whitespace-nowrap text-[10px]">{{ t('pricing.ourSelling') }}</span>
                  <span class="font-mono tabular-nums">{{ formatPrice(row.effective[field.key]) }}</span>
                </div>
              </td>
              <td class="border-l border-gray-100 px-3 py-3 text-center font-mono text-xs font-semibold text-primary-600 dark:border-dark-700/70 dark:text-primary-400">{{ formatMultiples(row.effective_multiple) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="text-xs text-gray-400 dark:text-gray-500">{{ t('pricing.unitHint') }}</div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getPublicPricing, type PublicPricingModel, type PublicPriceSet } from '@/api/pricing'
import { extractApiErrorMessage } from '@/utils/apiError'

type PriceKey = keyof PublicPriceSet
const { t } = useI18n()
const models = ref<PublicPricingModel[]>([])
const search = ref('')
const selectedProvider = ref('全部')
const snapshotFetchedAt = ref<string>()
const loading = ref(true)
const error = ref('')

const priceFields = [
  { key: 'input' as PriceKey, label: '输入' },
  { key: 'output' as PriceKey, label: '输出' },
  { key: 'cache_read' as PriceKey, label: '缓存读' },
  { key: 'cache_write' as PriceKey, label: '缓存写' },
]

const providerFilters = computed(() => {
  const counts = new Map<string, number>()
  for (const row of models.value) counts.set(row.provider, (counts.get(row.provider) || 0) + 1)
  return [{ name: '全部', count: models.value.length }, ...Array.from(counts.entries()).map(([name, count]) => ({ name, count }))]
})

const filteredModels = computed(() => {
  const query = search.value.trim().toLowerCase()
  return models.value.filter(row => {
    const matchesProvider = selectedProvider.value === '全部' || row.provider === selectedProvider.value
    const matchesSearch = !query || `${row.model} ${row.provider} ${row.provider_key}`.toLowerCase().includes(query)
    return matchesProvider && matchesSearch
  })
})

function formatPrice(value: number): string {
  return Number(value || 0).toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: 6 })
}

function formatMultiples(values: PublicPricingModel['effective_multiple']): string {
  const unique = Array.from(new Set(Object.values(values).filter((value): value is number => value != null).sort((a, b) => a - b).map(value => value.toFixed(2))))
  if (unique.length === 0) return '—'
  if (unique.length === 1) return `${unique[0]}×`
  return `${unique[0]}–${unique[unique.length - 1]}×`
}

onMounted(async () => {
  try {
    const view = await getPublicPricing()
    models.value = view.models
    snapshotFetchedAt.value = view.snapshot_fetched_at
  } catch (err) {
    error.value = extractApiErrorMessage(err, t('pricing.loadFailed'))
  } finally {
    loading.value = false
  }
})
</script>
