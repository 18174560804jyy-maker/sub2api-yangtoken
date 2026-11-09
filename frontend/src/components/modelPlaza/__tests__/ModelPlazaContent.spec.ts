import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaFilterBar from '../PlazaFilterBar.vue'
import PlazaGroupSection from '../PlazaGroupSection.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))

describe('Model plaza group order', () => {
  it('keeps pills and sections in platform/rate order through filtering', async () => {
    const groups = [
      { id: 1, platform: 'grok', rate_multiplier: .2 },
      { id: 2, platform: 'gemini', rate_multiplier: .25 },
      { id: 3, platform: 'anthropic', rate_multiplier: 1.2 },
      { id: 4, platform: 'openai', rate_multiplier: .22 },
      { id: 5, platform: 'anthropic', rate_multiplier: .5 },
      { id: 6, platform: 'openai', rate_multiplier: .3, user_rate_multiplier: .1 }
    ].map(g => ({ ...g, name: `group-${g.id}`, models: [{ name: 'shared-model' }] })) as ModelPlazaGroup[]
    const wrapper = mount(ModelPlazaContent, {
      props: { response: { groups, description: '' }, loading: false },
      global: { stubs: { Icon: true, PlazaFilterBar: true, PlazaGroupSection: true } }
    })
    const filters = wrapper.findComponent(PlazaFilterBar)
    const ids = () => wrapper.findAllComponents(PlazaGroupSection).map(section => section.props('group').id)
    expect(filters.props('platforms')).toEqual(['openai', 'anthropic', 'gemini', 'grok'])
    expect(filters.props('groups').map((g: { id: number }) => g.id)).toEqual([6, 4, 5, 3, 2, 1])
    expect(ids()).toEqual([6, 4, 5, 3, 2, 1])
    filters.vm.$emit('update:search', 'shared')
    await wrapper.vm.$nextTick()
    expect(ids()).toEqual([6, 4, 5, 3, 2, 1])
    filters.vm.$emit('update:platform', 'anthropic')
    await wrapper.vm.$nextTick()
    expect(ids()).toEqual([5, 3])
    expect(groups.map(g => g.id)).toEqual([1, 2, 3, 4, 5, 6])
  })
})
