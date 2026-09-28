import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PublicPricingView from '../PublicPricingView.vue'

vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/pricing', () => ({
  getPublicPricing: async () => ({
    models: [{
      model: 'deepseek-v4-flash-0731', provider: 'DeepSeek', provider_key: 'deepseek',
      official: { input: 1, output: 4, cache_read: .02, cache_write: 0 },
      effective: { input: .16, output: .64, cache_read: .0032, cache_write: 0 },
      effective_multiple: { input: .16, output: .16, cache_read: .16, cache_write: null },
    }, {
      model: 'range-model', provider: 'Other', provider_key: 'other',
      official: { input: 1, output: 4, cache_read: .02, cache_write: 0 },
      effective: { input: .3, output: .8, cache_read: .005, cache_write: 0 },
      effective_multiple: { input: .3, output: .2, cache_read: .25, cache_write: null },
    }],
  }),
}))

describe('customer pricing', () => {
  it('shows official and customer prices only, with sorted multiples and working search', async () => {
    const wrapper = mount(PublicPricingView, { global: { stubs: { Icon: true, LoadingSpinner: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toMatch(/herohao|pricing.herohao/i)
    expect(wrapper.find('iframe').exists()).toBe(false)
    const rows = wrapper.findAll('tbody tr')
    expect(rows[0].findAll('td')).toHaveLength(7)
    expect(rows[0].text()).toContain('1.0000')
    expect(rows[0].text()).toContain('0.1600')
    expect(rows[0].text()).toContain('0.16×')
    expect(rows[1].text()).toContain('0.20–0.30×')
    await wrapper.get('input').setValue('deepseek')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    wrapper.unmount()
  })
})
