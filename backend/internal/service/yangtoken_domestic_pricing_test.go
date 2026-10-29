//go:build unit

package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestYangTokenDomesticScreenshotPricing(t *testing.T) {
	data, err := os.ReadFile("testdata/yangtoken_domestic_prices_20261028.json")
	require.NoError(t, err)
	var fixture struct {
		LedgerUnitsPerCNY float64 `json:"ledger_units_per_cny"`
		GroupMultiplier   float64 `json:"group_multiplier"`
		Models            []struct {
			Models     []string `json:"models"`
			Input      float64  `json:"input"`
			Output     float64  `json:"output"`
			CacheRead  float64  `json:"cache_read"`
			CacheWrite float64  `json:"cache_write"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.Len(t, fixture.Models, 25)
	require.Equal(t, 1.0, fixture.LedgerUnitsPerCNY)
	require.Equal(t, 0.3, fixture.GroupMultiplier)
	perToken := func(price float64) *float64 {
		value := price * fixture.LedgerUnitsPerCNY / 1_000_000
		return &value
	}
	var cards []ChannelModelPricing
	seen := make(map[string]bool)
	for _, row := range fixture.Models {
		for _, model := range row.Models {
			require.False(t, seen[model], "duplicate price: %s", model)
			seen[model] = true
		}
		cards = append(cards, ChannelModelPricing{
			Platform: PlatformOpenAI, Models: row.Models, BillingMode: BillingModeToken,
			InputPrice: perToken(row.Input), OutputPrice: perToken(row.Output),
			CacheReadPrice: perToken(row.CacheRead), CacheWritePrice: perToken(row.CacheWrite),
			CacheWrite1hPrice: perToken(row.CacheWrite),
		})
	}
	require.Len(t, seen, 27)
	cards, err = normalizeGroupModelPricing(PlatformOpenAI, cards)
	require.NoError(t, err)
	group := &Group{ID: 25, Platform: PlatformOpenAI, ModelPricing: cards, LongContextPricingEnabled: true}
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)
	for _, row := range fixture.Models {
		for _, model := range row.Models {
			t.Run(model, func(t *testing.T) {
				resolved := resolver.Resolve(context.Background(), PricingInput{Model: model, Group: group})
				require.Equal(t, PricingSourceGroup, resolved.Source)
				for _, component := range []struct {
					name   string
					tokens UsageTokens
					price  float64
				}{
					{"input", UsageTokens{InputTokens: 1_000_000}, row.Input},
					{"output", UsageTokens{OutputTokens: 1_000_000}, row.Output},
					{"cache_read", UsageTokens{CacheReadTokens: 1_000_000}, row.CacheRead},
					{"cache_write", UsageTokens{CacheCreationTokens: 1_000_000}, row.CacheWrite},
				} {
					t.Run(component.name, func(t *testing.T) {
						for _, multiplier := range []float64{fixture.GroupMultiplier, 1, 0.5, 0} {
							for _, hour := range []int{2, 12} {
								cost, err := bs.CalculateCostUnified(CostInput{
									Ctx: context.Background(), Model: model, Group: group, Resolver: resolver,
									Tokens: component.tokens, RateMultiplier: multiplier,
									PricingAt: time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC),
								})
								require.NoError(t, err)
								require.InDelta(t, component.price, cost.TotalCost, 1e-10)
								require.InDelta(t, component.price*multiplier, cost.ActualCost, 1e-10)
							}
						}
					})
				}
			})
		}
	}
}
