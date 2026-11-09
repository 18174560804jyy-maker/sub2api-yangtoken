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
		model                      string
		input, output, read, write float64
	}{
		{"deepseek-v4-pro-0813", 4.5, 13.5, 0.15, 0},
		{"deepseek-v4.1-flash", 1, 4, 0.02, 0},
		{"glm-5.3", 8, 28, 2, 0},
		{"glm-5.3-flash", 1, 3.35, 0.23, 0},
		{"glm-5.3-flashx", 2, 7, 0.57, 0},
		{"hy3", 1, 4, 0.25, 0},
		{"hy4-preview", 6, 18, 0.3, 0},
		{"kimi-k3", 20, 100, 2, 0},
		{"kimi-k3.1-pro", 13, 68, 1.3, 0},
		{"mimo-v2.6-flash", 2, 4, 0.06, 0},
		{"mimo-v2.6-pro", 4.35, 8.7, 0.036, 0},
		{"minimax-m3", 2.1, 8.4, 0.42, 0},
		{"minimax-m3.1-flash", 1, 4, 0.12, 0},
		{"qwen3.7-max", 12, 36, 2.4, 15},
		{"qwen3.8-flash", 0.8, 2.7, 0.16, 1},
		{"qwen3.8-max", 12, 36, 2.4, 15},
	} {
		t.Run(tc.model, func(t *testing.T) {
			before, beforeErr := billing.GetModelPricing(tc.model)
			p := svc.lookupOfficialPricing(context.Background(), tc.model, map[string]*PlazaOfficialPricing{})
			require.NotNil(t, p)
			require.InDelta(t, tc.input/1e6, *p.InputPrice, 1e-15)
			require.InDelta(t, tc.output/1e6, *p.OutputPrice, 1e-15)
			require.InDelta(t, tc.read/1e6, *p.CacheReadPrice, 1e-15)
			require.Equal(t, "CNY", p.Currency)
			require.InDelta(t, tc.write/1e6, *p.CacheWritePrice, 1e-15)
			require.Nil(t, p.CacheWrite1hPrice)
			after, afterErr := billing.GetModelPricing(tc.model)
			require.Equal(t, before, after)
			require.Equal(t, beforeErr, afterErr)
		})
	}
	require.Nil(t, plazaOfficialReference("unknown-model"))
	require.Equal(t, 0.0, *plazaOfficialReference("mimo-v2.6-pro").CacheWritePrice)
	require.Nil(t, plazaOfficialReference("kimi-k3.1-pro").CacheWrite1hPrice)
}

func TestPlazaReferencePricesOverrideCatalogForDisplay(t *testing.T) {
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"qwen3.8-max": {Mode: "chat", InputCostPerToken: 9e-6, OutputCostPerToken: 12e-6},
	})
	svc := &ModelPlazaService{billingService: NewBillingService(&config.Config{}, catalog)}
	p := svc.lookupOfficialPricing(context.Background(), "qwen3.8-max", map[string]*PlazaOfficialPricing{})
	require.InDelta(t, 12e-6, *p.InputPrice, 1e-15)
	require.Equal(t, "CNY", p.Currency)
	billed, err := svc.billingService.GetModelPricing("qwen3.8-max")
	require.NoError(t, err)
	require.InDelta(t, 9e-6, billed.InputPricePerToken, 1e-15)
	require.Nil(t, plazaOfficialReference("gpt-6.1-sol"))
}
