package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type downstreamPricingHTTPClientFunc func(req *http.Request) (*http.Response, error)

type publicPricingTestRepository struct {
	DownstreamPricingRepository
	config *DownstreamPricingConfig
}

func (r publicPricingTestRepository) GetLatest(context.Context) (*DownstreamPricingConfig, error) {
	return r.config, nil
}

func TestPublicPricingHidesSupplierPricesAndComputesCustomerMultiples(t *testing.T) {
	svc := &DownstreamPricingSyncService{repo: publicPricingTestRepository{
		config: &DownstreamPricingConfig{
			MarkupPercent: 100,
			Snapshot: []DownstreamUpstreamModel{
				{Model: "automatic", Official: DownstreamPriceSet{Input: 1, Output: 4, CacheRead: .02}, Selling: DownstreamPriceSet{Input: .08, Output: .32, CacheRead: .0016}},
				{Model: "manual", Official: DownstreamPriceSet{Input: 2}, Selling: DownstreamPriceSet{Input: .1}},
				{Model: "disabled"},
			},
			Overrides: []DownstreamModelOverride{
				{Model: "manual", Enabled: true, Manual: true, Prices: DownstreamPriceSet{Input: .5}},
				{Model: "disabled", Enabled: false},
			},
		},
	}}
	view, err := svc.GetPublic(context.Background())
	require.NoError(t, err)
	require.Len(t, view.Models, 2)
	require.Equal(t, 1.0, view.Models[0].Official.Input)
	require.Equal(t, .16, view.Models[0].Effective.Input)
	require.InDelta(t, .16, *view.Models[0].EffectiveMultiple.Input, 1e-10)
	require.InDelta(t, .16, *view.Models[0].EffectiveMultiple.CacheRead, 1e-10)
	require.Nil(t, view.Models[0].EffectiveMultiple.CacheWrite)
	require.Equal(t, .5, view.Models[1].Effective.Input)
	require.Equal(t, .25, *view.Models[1].EffectiveMultiple.Input)
	body, err := json.Marshal(view)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.NotContains(t, payload, "source_url")
	models, ok := payload["models"].([]any)
	require.True(t, ok)
	for _, model := range models {
		row, ok := model.(map[string]any)
		require.True(t, ok)
		require.NotContains(t, row, "selling")
		require.NotContains(t, row, "multiple")
	}
}

func (f downstreamPricingHTTPClientFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestParseDownstreamPricingSellingAndCaseInsensitiveDedup(t *testing.T) {
	body := []byte(`{
		"token":{"models":[
			{"model":"Model-X","provider":"Vendor","providerKey":"vendor","enabled":true,"prices":{
				"input":{"official":"1.0000","selling":"0.0800","multiple":0.08},"output":{"official":"4.0000","selling":0.32,"multiple":0.08},"cacheRead":{"official":"0.0200","selling":"0.0016","multiple":0.08},"cacheWrite":{"official":"0.0000","selling":"0.0000","multiple":null}
			}},
			{"model":"model-x","provider":"duplicate","providerKey":"duplicate","enabled":false,"prices":{
				"input":{"selling":"1"},"output":{"selling":"2"},"cacheRead":{"selling":"3"},"cacheWrite":{"selling":"4"}
			}}
		]}
	}`)

	models, err := parseDownstreamPricing(body)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "Model-X", models[0].Model)
	require.Equal(t, 0.08, models[0].Selling.Input)
	require.Equal(t, 0.32, models[0].Selling.Output)
	require.Equal(t, 0.0016, models[0].Selling.CacheRead)
	require.Zero(t, models[0].Selling.CacheWrite)
	require.Equal(t, 1.0, models[0].Official.Input)
	require.NotNil(t, models[0].Multiple.Input)
	require.Equal(t, 0.08, *models[0].Multiple.Input)
	require.Nil(t, models[0].Multiple.CacheWrite)
}

func TestParseDownstreamPricingRejectsMissingRequiredSelling(t *testing.T) {
	body := []byte(`{"token":{"models":[{"model":"model-x","enabled":true,"prices":{"input":{},"output":{"selling":"1"},"cacheRead":{"selling":"0"},"cacheWrite":{"selling":"0"}}}]}}`)
	_, err := parseDownstreamPricing(body)
	require.ErrorContains(t, err, "missing selling field")
}

func TestDownstreamPricingMarkupAndPerTokenConversion(t *testing.T) {
	prices := markedUpPrices(DownstreamPriceSet{Input: 1, Output: 2, CacheRead: 0.25, CacheWrite: 0}, 25)
	require.Equal(t, DownstreamPriceSet{Input: 1.25, Output: 2.5, CacheRead: 0.3125, CacheWrite: 0}, prices)

	channelPricing := priceSetToChannelPricing("model-x", prices)
	require.Equal(t, PlatformOpenAI, channelPricing.Platform)
	require.InDelta(t, 1.25e-6, *channelPricing.InputPrice, 1e-15)
	require.InDelta(t, 2.5e-6, *channelPricing.OutputPrice, 1e-15)
	require.InDelta(t, 0.3125e-6, *channelPricing.CacheReadPrice, 1e-15)
	require.Zero(t, *channelPricing.CacheWritePrice)
}

func TestNormalizeDownstreamOverridesRejectsCaseInsensitiveDuplicate(t *testing.T) {
	_, err := normalizeOverrides([]DownstreamModelOverride{{Model: "Model-X"}, {Model: "model-x"}})
	require.Error(t, err)
}

func TestDownstreamPricingFetchUsesOnlyFixedNoStoreURL(t *testing.T) {
	svc := &DownstreamPricingSyncService{
		httpClient: downstreamPricingHTTPClientFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodGet, req.Method)
			require.Equal(t, DownstreamPricingSourceURL, req.URL.String())
			require.Equal(t, "no-store", req.Header.Get("Cache-Control"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
				Body: io.NopCloser(strings.NewReader(
					`{"token":{"models":[{"model":"model-x","provider":"Vendor","providerKey":"vendor","enabled":true,"prices":{"input":{"selling":"1"},"output":{"selling":"2"},"cacheRead":{"selling":"0.1"},"cacheWrite":{"selling":"0"}}}]}}`,
				)),
			}, nil
		}),
	}

	models, _, err := svc.fetch(context.Background())
	require.NoError(t, err)
	require.Equal(t, "model-x", models[0].Model)
}
