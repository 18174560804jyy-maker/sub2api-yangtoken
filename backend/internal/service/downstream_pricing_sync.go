package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	DownstreamPricingSourceURL = "https://sub2.herohao.top/pricing/api/pricing"
	downstreamPricingBodyLimit = 2 << 20
	downstreamPricingTimeout   = 12 * time.Second
	maxDownstreamModels        = 2000
)

type DownstreamPriceSet struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type DownstreamMultipleSet struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type DownstreamUpstreamModel struct {
	Model       string                `json:"model"`
	Provider    string                `json:"provider"`
	ProviderKey string                `json:"provider_key"`
	Enabled     bool                  `json:"enabled"`
	Official    DownstreamPriceSet    `json:"official"`
	Selling     DownstreamPriceSet    `json:"selling"`
	Multiple    DownstreamMultipleSet `json:"multiple"`
}

type DownstreamModelOverride struct {
	Model   string             `json:"model"`
	Enabled bool               `json:"enabled"`
	Manual  bool               `json:"manual"`
	Prices  DownstreamPriceSet `json:"prices"`
}

type DownstreamPricingConfig struct {
	ChannelID         int64                     `json:"channel_id"`
	MarkupPercent     float64                   `json:"markup_percent"`
	Overrides         []DownstreamModelOverride `json:"overrides"`
	ManagedModels     []string                  `json:"managed_models"`
	Snapshot          []DownstreamUpstreamModel `json:"snapshot"`
	SnapshotFetchedAt *time.Time                `json:"snapshot_fetched_at,omitempty"`
}

type DownstreamPricingModel struct {
	Model       string                `json:"model"`
	Provider    string                `json:"provider"`
	ProviderKey string                `json:"provider_key"`
	Enabled     bool                  `json:"enabled"`
	Manual      bool                  `json:"manual"`
	Official    DownstreamPriceSet    `json:"official"`
	Selling     DownstreamPriceSet    `json:"selling"`
	Multiple    DownstreamMultipleSet `json:"multiple"`
	Effective   DownstreamPriceSet    `json:"effective"`
}

type DownstreamPricingView struct {
	ChannelID         int64                    `json:"channel_id"`
	SourceURL         string                   `json:"source_url"`
	MarkupPercent     float64                  `json:"markup_percent"`
	Models            []DownstreamPricingModel `json:"models"`
	SnapshotFetchedAt *time.Time               `json:"snapshot_fetched_at,omitempty"`
	Stale             bool                     `json:"stale"`
	Warning           string                   `json:"warning,omitempty"`
}

type DownstreamPublicPricingModel struct {
	Model             string                `json:"model"`
	Provider          string                `json:"provider"`
	ProviderKey       string                `json:"provider_key"`
	Official          DownstreamPriceSet    `json:"official"`
	Effective         DownstreamPriceSet    `json:"effective"`
	EffectiveMultiple DownstreamMultipleSet `json:"effective_multiple"`
}

type DownstreamPublicPricingView struct {
	Models            []DownstreamPublicPricingModel `json:"models"`
	SnapshotFetchedAt *time.Time                     `json:"snapshot_fetched_at,omitempty"`
}

type SaveDownstreamPricingInput struct {
	MarkupPercent float64                   `json:"markup_percent"`
	Models        []DownstreamModelOverride `json:"models"`
}

type DownstreamPricingSyncCommit struct {
	ChannelID     int64
	MarkupPercent float64
	Overrides     []DownstreamModelOverride
	ManagedModels []string
	Pricing       []ChannelModelPricing
	Snapshot      []DownstreamUpstreamModel
	FetchedAt     time.Time
}

type DownstreamPricingRepository interface {
	Get(ctx context.Context, channelID int64) (*DownstreamPricingConfig, error)
	GetLatest(ctx context.Context) (*DownstreamPricingConfig, error)
	SaveSnapshot(ctx context.Context, channelID int64, snapshot []DownstreamUpstreamModel, fetchedAt time.Time) error
	SyncChannel(ctx context.Context, commit DownstreamPricingSyncCommit) error
}

type downstreamPricingHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type DownstreamPricingSyncService struct {
	repo           DownstreamPricingRepository
	channelService *ChannelService
	gatewayService *GatewayService
	httpClient     downstreamPricingHTTPClient
}

func NewDownstreamPricingSyncService(repo DownstreamPricingRepository, channelService *ChannelService, gatewayService *GatewayService) *DownstreamPricingSyncService {
	return &DownstreamPricingSyncService{
		repo:           repo,
		channelService: channelService,
		gatewayService: gatewayService,
		httpClient: &http.Client{
			Timeout: downstreamPricingTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (s *DownstreamPricingSyncService) Get(ctx context.Context, channelID int64) (*DownstreamPricingView, error) {
	if channelID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CHANNEL_ID", "invalid channel ID")
	}
	if _, err := s.channelService.GetByID(ctx, channelID); err != nil {
		return nil, err
	}
	config, err := s.repo.Get(ctx, channelID)
	if err != nil {
		return nil, err
	}
	models, fetchedAt, fetchErr := s.fetch(ctx)
	stale := false
	warning := ""
	if fetchErr == nil {
		if err := s.repo.SaveSnapshot(ctx, channelID, models, fetchedAt); err != nil {
			return nil, fmt.Errorf("save downstream pricing snapshot: %w", err)
		}
		config.Snapshot = models
		config.SnapshotFetchedAt = &fetchedAt
	} else {
		if len(config.Snapshot) == 0 {
			return nil, infraerrors.New(http.StatusBadGateway, "DOWNSTREAM_PRICING_UNAVAILABLE", "pricing source is unavailable and no successful snapshot exists")
		}
		stale = true
		warning = "实时刷新失败，当前显示最近一次成功快照"
	}
	return buildDownstreamPricingView(config, stale, warning), nil
}

func (s *DownstreamPricingSyncService) GetPublic(ctx context.Context) (*DownstreamPublicPricingView, error) {
	config, err := s.repo.GetLatest(ctx)
	if err != nil {
		return nil, err
	}
	if config == nil || len(config.Snapshot) == 0 {
		return nil, infraerrors.New(http.StatusNotFound, "DOWNSTREAM_PRICING_NOT_CONFIGURED", "public pricing is not configured")
	}

	states := make(map[string]DownstreamModelOverride, len(config.Overrides))
	for _, item := range config.Overrides {
		states[strings.ToLower(strings.TrimSpace(item.Model))] = item
	}
	models := make([]DownstreamPublicPricingModel, 0, len(config.Snapshot))
	for _, item := range config.Snapshot {
		state, ok := states[strings.ToLower(item.Model)]
		if ok && !state.Enabled {
			continue
		}
		effective := markedUpPrices(item.Selling, config.MarkupPercent)
		if ok && state.Manual {
			effective = state.Prices
		}
		models = append(models, DownstreamPublicPricingModel{
			Model: item.Model, Provider: item.Provider, ProviderKey: item.ProviderKey,
			Official:  item.Official,
			Effective: effective, EffectiveMultiple: priceMultiple(effective, item.Official),
		})
	}
	return &DownstreamPublicPricingView{
		Models: models, SnapshotFetchedAt: config.SnapshotFetchedAt,
	}, nil
}

func (s *DownstreamPricingSyncService) Save(ctx context.Context, channelID int64, input SaveDownstreamPricingInput) (*DownstreamPricingView, error) {
	if channelID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CHANNEL_ID", "invalid channel ID")
	}
	if !finiteInRange(input.MarkupPercent, 0, 10000) {
		return nil, infraerrors.BadRequest("INVALID_MARKUP_PERCENT", "markup_percent must be between 0 and 10000")
	}
	channel, err := s.channelService.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	states, err := normalizeOverrides(input.Models)
	if err != nil {
		return nil, err
	}

	// Saving prices always requires a fresh production read. A stale snapshot is
	// suitable for display, but never for a write that changes billing.
	upstream, fetchedAt, err := s.fetch(ctx)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "DOWNSTREAM_PRICING_REFRESH_FAILED", "could not refresh production pricing; no changes were saved")
	}

	managedModels := make([]string, 0, len(upstream))
	overrides := make([]DownstreamModelOverride, 0, len(upstream))
	pricing := make([]ChannelModelPricing, 0, len(upstream))
	for _, item := range upstream {
		key := strings.ToLower(item.Model)
		state, ok := states[key]
		if !ok {
			state = DownstreamModelOverride{Model: item.Model, Enabled: item.Enabled}
		}
		state.Model = item.Model
		if state.Manual && !validPriceSet(state.Prices) {
			return nil, infraerrors.BadRequest("INVALID_MANUAL_PRICE", fmt.Sprintf("manual prices for %s must be finite and non-negative", item.Model))
		}
		managedModels = append(managedModels, item.Model)
		overrides = append(overrides, state)
		if !state.Enabled {
			continue
		}
		effective := markedUpPrices(item.Selling, input.MarkupPercent)
		if state.Manual {
			effective = state.Prices
		}
		pricing = append(pricing, priceSetToChannelPricing(item.Model, effective))
	}

	if err := s.repo.SyncChannel(ctx, DownstreamPricingSyncCommit{
		ChannelID: channelID, MarkupPercent: input.MarkupPercent, Overrides: overrides,
		ManagedModels: managedModels, Pricing: pricing, Snapshot: upstream, FetchedAt: fetchedAt,
	}); err != nil {
		return nil, fmt.Errorf("sync downstream pricing: %w", err)
	}

	s.channelService.InvalidateCache()
	for _, groupID := range channel.GroupIDs {
		gid := groupID
		s.gatewayService.InvalidateAvailableModelsCache(&gid, "")
	}
	config := &DownstreamPricingConfig{
		ChannelID: channelID, MarkupPercent: input.MarkupPercent, Overrides: overrides,
		ManagedModels: managedModels, Snapshot: upstream, SnapshotFetchedAt: &fetchedAt,
	}
	return buildDownstreamPricingView(config, false, ""), nil
}

func (s *DownstreamPricingSyncService) fetch(ctx context.Context) ([]DownstreamUpstreamModel, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DownstreamPricingSourceURL, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("fetch pricing source: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("pricing source returned HTTP %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		return nil, time.Time{}, fmt.Errorf("pricing source returned unexpected content type %q", contentType)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, downstreamPricingBodyLimit+1))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read pricing source: %w", err)
	}
	if len(body) > downstreamPricingBodyLimit {
		return nil, time.Time{}, fmt.Errorf("pricing source response exceeds %d bytes", downstreamPricingBodyLimit)
	}
	models, err := parseDownstreamPricing(body)
	if err != nil {
		return nil, time.Time{}, err
	}
	return models, time.Now().UTC(), nil
}

func parseDownstreamPricing(body []byte) ([]DownstreamUpstreamModel, error) {
	type sellingValue struct {
		Official json.RawMessage `json:"official"`
		Selling  json.RawMessage `json:"selling"`
		Multiple json.RawMessage `json:"multiple"`
	}
	type sourceModel struct {
		Model       string                                                      `json:"model"`
		Provider    string                                                      `json:"provider"`
		ProviderKey string                                                      `json:"providerKey"`
		Enabled     bool                                                        `json:"enabled"`
		Prices      struct{ Input, Output, CacheRead, CacheWrite sellingValue } `json:"prices"`
	}
	var payload struct {
		Token struct {
			Models []sourceModel `json:"models"`
		} `json:"token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode pricing source: %w", err)
	}
	if len(payload.Token.Models) == 0 || len(payload.Token.Models) > maxDownstreamModels {
		return nil, fmt.Errorf("pricing source contains invalid model count %d", len(payload.Token.Models))
	}
	seen := make(map[string]struct{}, len(payload.Token.Models))
	result := make([]DownstreamUpstreamModel, 0, len(payload.Token.Models))
	for _, raw := range payload.Token.Models {
		name := strings.TrimSpace(raw.Model)
		if !validDownstreamModelName(name) {
			return nil, fmt.Errorf("pricing source contains invalid model name %q", name)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		selling, err := parseSellingRawPrices(raw.Prices.Input.Selling, raw.Prices.Output.Selling, raw.Prices.CacheRead.Selling, raw.Prices.CacheWrite.Selling)
		if err != nil {
			return nil, fmt.Errorf("invalid selling price for %s: %w", name, err)
		}
		official, err := parseOptionalRawPrices(raw.Prices.Input.Official, raw.Prices.Output.Official, raw.Prices.CacheRead.Official, raw.Prices.CacheWrite.Official)
		if err != nil {
			return nil, fmt.Errorf("invalid official price for %s: %w", name, err)
		}
		multiple, err := parseMultipleRawPrices(raw.Prices.Input.Multiple, raw.Prices.Output.Multiple, raw.Prices.CacheRead.Multiple, raw.Prices.CacheWrite.Multiple)
		if err != nil {
			return nil, fmt.Errorf("invalid price multiple for %s: %w", name, err)
		}
		result = append(result, DownstreamUpstreamModel{Model: name, Provider: strings.TrimSpace(raw.Provider), ProviderKey: strings.TrimSpace(raw.ProviderKey), Enabled: raw.Enabled, Official: official, Selling: selling, Multiple: multiple})
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Model) < strings.ToLower(result[j].Model) })
	return result, nil
}

func parseOptionalRawPrices(values ...json.RawMessage) (DownstreamPriceSet, error) {
	parsed := make([]float64, len(values))
	for i, value := range values {
		trimmed := strings.TrimSpace(string(value))
		if trimmed == "" || trimmed == "null" || trimmed == `""` {
			continue
		}
		if strings.HasPrefix(trimmed, `"`) {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return DownstreamPriceSet{}, fmt.Errorf("invalid official string")
			}
			trimmed = text
		}
		v, err := json.Number(trimmed).Float64()
		if err != nil || !finiteInRange(v, 0, 1e9) {
			return DownstreamPriceSet{}, fmt.Errorf("invalid numeric official value")
		}
		parsed[i] = v
	}
	return DownstreamPriceSet{Input: parsed[0], Output: parsed[1], CacheRead: parsed[2], CacheWrite: parsed[3]}, nil
}

func parseMultipleRawPrices(values ...json.RawMessage) (DownstreamMultipleSet, error) {
	parsed := make([]*float64, len(values))
	for i, value := range values {
		trimmed := strings.TrimSpace(string(value))
		if trimmed == "" || trimmed == "null" {
			continue
		}
		v, err := json.Number(trimmed).Float64()
		if err != nil || !finiteInRange(v, 0, 1e9) {
			return DownstreamMultipleSet{}, fmt.Errorf("invalid numeric multiple value")
		}
		parsed[i] = &v
	}
	return DownstreamMultipleSet{Input: parsed[0], Output: parsed[1], CacheRead: parsed[2], CacheWrite: parsed[3]}, nil
}

func parseSellingRawPrices(values ...json.RawMessage) (DownstreamPriceSet, error) {
	parsed := make([]float64, len(values))
	for i, value := range values {
		trimmed := strings.TrimSpace(string(value))
		if trimmed == "" || trimmed == "null" {
			if i == 3 { // Missing cache-write pricing is explicitly treated as zero.
				parsed[i] = 0
				continue
			}
			return DownstreamPriceSet{}, fmt.Errorf("missing selling field")
		}
		if strings.HasPrefix(trimmed, "\"") {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return DownstreamPriceSet{}, fmt.Errorf("invalid selling string")
			}
			trimmed = text
		}
		v, err := json.Number(trimmed).Float64()
		if err != nil || !finiteInRange(v, 0, 1e9) {
			return DownstreamPriceSet{}, fmt.Errorf("invalid numeric selling value")
		}
		parsed[i] = v
	}
	return DownstreamPriceSet{Input: parsed[0], Output: parsed[1], CacheRead: parsed[2], CacheWrite: parsed[3]}, nil
}

func buildDownstreamPricingView(config *DownstreamPricingConfig, stale bool, warning string) *DownstreamPricingView {
	states := make(map[string]DownstreamModelOverride, len(config.Overrides))
	for _, item := range config.Overrides {
		states[strings.ToLower(strings.TrimSpace(item.Model))] = item
	}
	models := make([]DownstreamPricingModel, 0, len(config.Snapshot))
	for _, item := range config.Snapshot {
		state, ok := states[strings.ToLower(item.Model)]
		if !ok {
			state = DownstreamModelOverride{Enabled: item.Enabled}
		}
		effective := markedUpPrices(item.Selling, config.MarkupPercent)
		if state.Manual {
			effective = state.Prices
		}
		models = append(models, DownstreamPricingModel{Model: item.Model, Provider: item.Provider, ProviderKey: item.ProviderKey, Enabled: state.Enabled, Manual: state.Manual, Official: item.Official, Selling: item.Selling, Multiple: item.Multiple, Effective: effective})
	}
	return &DownstreamPricingView{ChannelID: config.ChannelID, SourceURL: DownstreamPricingSourceURL, MarkupPercent: config.MarkupPercent, Models: models, SnapshotFetchedAt: config.SnapshotFetchedAt, Stale: stale, Warning: warning}
}

func priceMultiple(numerator, denominator DownstreamPriceSet) DownstreamMultipleSet {
	return DownstreamMultipleSet{
		Input:      ratio(numerator.Input, denominator.Input),
		Output:     ratio(numerator.Output, denominator.Output),
		CacheRead:  ratio(numerator.CacheRead, denominator.CacheRead),
		CacheWrite: ratio(numerator.CacheWrite, denominator.CacheWrite),
	}
}

func ratio(numerator, denominator float64) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := numerator / denominator
	return &value
}

func normalizeOverrides(items []DownstreamModelOverride) (map[string]DownstreamModelOverride, error) {
	result := make(map[string]DownstreamModelOverride, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Model)
		if !validDownstreamModelName(name) {
			return nil, infraerrors.BadRequest("INVALID_MODEL", "invalid model name")
		}
		key := strings.ToLower(name)
		if _, exists := result[key]; exists {
			return nil, infraerrors.BadRequest("DUPLICATE_MODEL", fmt.Sprintf("duplicate model %s", name))
		}
		item.Model = name
		result[key] = item
	}
	return result, nil
}

func validDownstreamModelName(value string) bool {
	if value == "" || len(value) > 200 {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validPriceSet(p DownstreamPriceSet) bool {
	return finiteInRange(p.Input, 0, 1e9) && finiteInRange(p.Output, 0, 1e9) && finiteInRange(p.CacheRead, 0, 1e9) && finiteInRange(p.CacheWrite, 0, 1e9)
}

func finiteInRange(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}

func markedUpPrices(p DownstreamPriceSet, percent float64) DownstreamPriceSet {
	factor := 1 + percent/100
	return DownstreamPriceSet{Input: p.Input * factor, Output: p.Output * factor, CacheRead: p.CacheRead * factor, CacheWrite: p.CacheWrite * factor}
}

func priceSetToChannelPricing(model string, p DownstreamPriceSet) ChannelModelPricing {
	perToken := func(value float64) *float64 { converted := value / 1_000_000; return &converted }
	return ChannelModelPricing{Platform: PlatformOpenAI, Models: []string{model}, BillingMode: BillingModeToken, InputPrice: perToken(p.Input), OutputPrice: perToken(p.Output), CacheReadPrice: perToken(p.CacheRead), CacheWritePrice: perToken(p.CacheWrite)}
}
