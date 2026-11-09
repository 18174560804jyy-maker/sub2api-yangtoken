//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPlazaReferencePricesDoNotChangeBilling(t *testing.T) {
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{})
	billing := NewBillingService(&config.Config{}, catalog)
	svc := &ModelPlazaService{billingService: billing}
	for _, tc := range []struct {
		model               string
		input, output, read float64
	}{
		{"qwen3.8-max", 2, 6, .17},
		{"qwen3.7-max", 2.5, 7.5, .25},
		{"qwen3.8-flash", .15, .47, .016},
		{"mimo-v2.6-pro", .435, .87, .0036},
		{"mimo-v2.6-flash", .14, .28, .0028},
		{"kimi-k3.1-pro", 2, 10, .2},
		{"hy4-preview", .834, 2.501, .042},
		{"hy3", .132, .528, .033},
	} {
		t.Run(tc.model, func(t *testing.T) {
			before, beforeErr := billing.GetModelPricing(tc.model)
			p := svc.lookupOfficialPricing(context.Background(), tc.model, map[string]*PlazaOfficialPricing{})
			require.NotNil(t, p)
			require.InDelta(t, tc.input/1e6, *p.InputPrice, 1e-15)
			require.InDelta(t, tc.output/1e6, *p.OutputPrice, 1e-15)
			require.InDelta(t, tc.read/1e6, *p.CacheReadPrice, 1e-15)
			require.Contains(t, p.SourceURL, "https://")
			require.Contains(t, p.SourceNote, "2026-11-09")
			after, afterErr := billing.GetModelPricing(tc.model)
			require.Equal(t, before, after)
			require.Equal(t, beforeErr, afterErr)
		})
	}
	require.Nil(t, plazaOfficialReferenceFallback("unknown-model"))
	require.Equal(t, 0.0, *plazaOfficialReferenceFallback("mimo-v2.6-pro").CacheWritePrice)
	require.InDelta(t, 4e-6, *plazaOfficialReferenceFallback("kimi-k3.1-pro").CacheWrite1hPrice, 1e-15)
}

func TestPlazaReferencePricesPreferCatalog(t *testing.T) {
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"qwen3.8-max": {Mode: "chat", InputCostPerToken: 9e-6, OutputCostPerToken: 12e-6},
	})
	svc := &ModelPlazaService{billingService: NewBillingService(&config.Config{}, catalog)}
	p := svc.lookupOfficialPricing(context.Background(), "qwen3.8-max", map[string]*PlazaOfficialPricing{})
	require.InDelta(t, 9e-6, *p.InputPrice, 1e-15)
	require.Empty(t, p.SourceURL)
}
