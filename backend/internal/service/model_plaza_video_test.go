//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPlazaGrokVideoMappingOnlyUsesBillingRates(t *testing.T) {
	model := "grok-imagine-video-1.5"
	channel := Channel{
		ID: 1, Name: "video", Status: StatusActive, GroupIDs: []int64{19},
		ModelMapping: map[string]map[string]string{"grok": {model: model}},
	}
	group := Group{
		ID: 19, Platform: "grok", RateMultiplier: .2,
		VideoRateIndependent: true, VideoRateMultiplier: 1,
		VideoPrice480P: testPtrFloat64(.4), VideoPrice720P: testPtrFloat64(.4),
		VideoPrice1080P:  testPtrFloat64(.4),
		VideoModelPrices: map[string]map[string]float64{model: {"480p": .15, "720p": .15, "1080p": .15}},
	}
	svc := newPlazaService([]Channel{channel}, []Group{group}, nil)
	svc.billingService = NewBillingService(&config.Config{}, newStubPricingServiceFromMap(nil))
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Models, 1)
	p := groups[0].Models[0].Pricing
	require.Equal(t, BillingModeVideo, p.BillingMode)
	require.Len(t, p.Intervals, 3)
	for i, tier := range []string{"480p", "720p", "1080p"} {
		require.Equal(t, tier, p.Intervals[i].TierLabel)
		billed := svc.billingService.CalculateVideoCost(model, tier, 1, 1, group.VideoPriceConfig(), 1)
		require.InDelta(t, .15, *p.Intervals[i].PerRequestPrice, 1e-12)
		require.InDelta(t, billed.TotalCost, *p.Intervals[i].PerRequestPrice, 1e-12)
	}
	require.Empty(t, channel.ModelPricing)
	require.InDelta(t, .4, *group.VideoPrice720P, 1e-12)
	require.True(t, groups[0].VideoRateIndependent)
	require.Equal(t, 1.0, groups[0].VideoRateMultiplier)
}

func TestPlazaGrokVideoFallbackAndExplicitPricing(t *testing.T) {
	svc := &ModelPlazaService{billingService: NewBillingService(&config.Config{}, newStubPricingServiceFromMap(nil))}
	model := "grok-imagine-video-1.5"
	group := &Group{VideoPrice720P: testPtrFloat64(0)}
	m := &PlazaModel{Name: model, Platform: "grok", Pricing: &ChannelModelPricing{BillingMode: BillingModeToken}}
	original := m.Pricing
	svc.fillDisplayPricing(context.Background(), m, group)
	require.Equal(t, BillingModeToken, original.BillingMode)
	require.Equal(t, BillingModeVideo, m.Pricing.BillingMode)
	for _, tier := range m.Pricing.Intervals {
		cost := svc.billingService.CalculateVideoCost(model, tier.TierLabel, 1, 1, group.VideoPriceConfig(), 1)
		require.Equal(t, cost.TotalCost, *tier.PerRequestPrice)
	}
	require.Equal(t, 0.0, *m.Pricing.Intervals[1].PerRequestPrice)

	for _, mode := range []BillingMode{BillingModeVideo, BillingModePerRequest, BillingModeImage} {
		raw := &ChannelModelPricing{BillingMode: mode, PerRequestPrice: testPtrFloat64(.9)}
		m := &PlazaModel{Name: model, Pricing: raw}
		svc.fillDisplayPricing(context.Background(), m, &Group{})
		require.Same(t, raw, m.Pricing)
	}
	group.ModelPricing = []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(.7)}}
	m = &PlazaModel{Name: model}
	svc.fillDisplayPricing(context.Background(), m, group)
	require.Equal(t, .7, *m.Pricing.PerRequestPrice)
	require.Empty(t, m.Pricing.Intervals)
	require.Nil(t, svc.plazaGrokVideoDisplayPricing(&PlazaModel{Name: "grok-4.7"}, group))
	require.Nil(t, svc.plazaGrokVideoDisplayPricing(&PlazaModel{Name: "grok-imagine-video-unknown"}, group))
}
