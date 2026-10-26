package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMergeManagedPricingPreservesUnmanagedModelsAndConfiguration(t *testing.T) {
	input := 1.25e-6
	existing := []service.ChannelModelPricing{
		{
			ID: 99, Platform: service.PlatformOpenAI,
			Models:     []string{"managed-old", "Keep-Manual"},
			InputPrice: &input,
			Intervals:  []service.PricingInterval{{MinTokens: 0, InputPrice: &input}},
		},
		{Platform: service.PlatformAnthropic, Models: []string{"claude-manual"}},
	}
	replacement := []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"managed-new"}}}

	merged := mergeManagedPricing(existing, []string{"MANAGED-OLD"}, []string{"managed-new"}, replacement)
	require.Len(t, merged, 3)
	require.Equal(t, []string{"Keep-Manual"}, merged[0].Models)
	require.Same(t, existing[0].InputPrice, merged[0].InputPrice)
	require.Equal(t, existing[0].Intervals, merged[0].Intervals)
	require.Equal(t, []string{"claude-manual"}, merged[1].Models)
	require.Equal(t, []string{"managed-new"}, merged[2].Models)
}
