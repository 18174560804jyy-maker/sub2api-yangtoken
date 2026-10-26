import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import ProfileBalanceNotifyCard from '../ProfileBalanceNotifyCard.vue'

vi.mock('@/api', () => ({
  userAPI: { updateProfile: vi.fn() }
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: null }) }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() })
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)

describe('ProfileBalanceNotifyCard', () => {
  it('uses the registration email as the only notification recipient', () => {
    const wrapper = mount(ProfileBalanceNotifyCard, {
      props: {
        enabled: true,
        threshold: null,
        systemDefaultThreshold: 5,
        userEmail: 'registered@example.com'
      }
    })

    expect(wrapper.text()).toContain('registered@example.com')
    expect(wrapper.text()).toContain('profile.balanceNotify.registrationEmailHint')
    expect(wrapper.find('input[type="email"]').exists()).toBe(false)
  })
})
