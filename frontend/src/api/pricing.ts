import { apiClient } from './client'

export interface PublicPriceSet {
  input: number
  output: number
  cache_read: number
  cache_write: number
}

export interface PublicMultipleSet {
  input: number | null
  output: number | null
  cache_read: number | null
  cache_write: number | null
}

export interface PublicPricingModel {
  model: string
  provider: string
  provider_key: string
  official: PublicPriceSet
  effective: PublicPriceSet
  effective_multiple: PublicMultipleSet
}

export interface PublicPricingView {
  models: PublicPricingModel[]
  snapshot_fetched_at?: string
}

export async function getPublicPricing(): Promise<PublicPricingView> {
  const { data } = await apiClient.get<PublicPricingView>('/pricing')
  return data
}
