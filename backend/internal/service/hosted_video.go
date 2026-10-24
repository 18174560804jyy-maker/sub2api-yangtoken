package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	HostedVideoCreate                   GrokMediaEndpoint        = "hosted_video_create"
	HostedVideoStatus                   GrokMediaEndpoint        = "hosted_video_status"
	HostedVideoContent                  GrokMediaEndpoint        = "hosted_video_content"
	OpenAIEndpointCapabilityHostedVideo OpenAIEndpointCapability = "hosted_video"
)

func (e GrokMediaEndpoint) IsHostedVideo() bool {
	return e == HostedVideoCreate || e == HostedVideoStatus || e == HostedVideoContent
}

func HostedVideoTaskKey(id string) string { return "hosted-video:" + strings.TrimSpace(id) }

// Forward exactly the same structured JSON that was validated and quoted.
func CanonicalHostedVideoBody(body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("request must be a JSON object")
	}
	allowed := map[string]bool{"model": true, "prompt": true, "resolution": true, "seconds": true, "duration": true, "aspect_ratio": true, "ratio": true, "images": true, "references": true, "face_direct": true, "n": true}
	for field := range fields {
		if !allowed[field] {
			return nil, fmt.Errorf("unsupported video parameter: %s", field)
		}
	}
	return json.Marshal(fields)
}

// Validate against the public API contract, never silently clamp a paid request.
func ParseHostedVideoRequest(body []byte) (GrokMediaRequestInfo, error) {
	info := GrokMediaRequestInfo{N: 1}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return info, fmt.Errorf("request must be a JSON object")
	}
	info.Model = gjson.GetBytes(body, "model").String()
	info.Prompt = gjson.GetBytes(body, "prompt").String()
	info.Resolution = gjson.GetBytes(body, "resolution").String()
	durationField, ratioField := "seconds", "aspect_ratio"
	if info.Model == "3.0" || info.Model == "3.5" {
		durationField, ratioField = "duration", "ratio"
	}
	d := gjson.GetBytes(body, durationField)
	info.DurationSeconds = int(d.Int())
	info.AspectRatio = gjson.GetBytes(body, ratioField).String()
	if strings.TrimSpace(info.Prompt) == "" || utf8.RuneCountInString(info.Prompt) > 4000 {
		return info, fmt.Errorf("prompt must contain 1-4000 characters")
	}
	if d.Type != gjson.Number || d.Float() != float64(info.DurationSeconds) {
		return info, fmt.Errorf("%s must be an integer", durationField)
	}
	n := gjson.GetBytes(body, "n")
	if n.Exists() && (n.Type != gjson.Number || n.Float() != 1) {
		return info, fmt.Errorf("only one video per request is supported")
	}
	ratioOK := false
	for _, r := range []string{"16:9", "9:16", "1:1", "4:3", "3:4", "21:9"} {
		ratioOK = ratioOK || info.AspectRatio == r
	}
	duration, resolution := info.DurationSeconds, info.Resolution
	valid := false
	switch info.Model {
	case "veo3.1-fast", "veo3.1-lite", "omni-flash":
		valid = (resolution == "720p" || resolution == "1080p" || resolution == "4k") && duration == 8
		ratioOK = info.AspectRatio == "16:9" || info.AspectRatio == "9:16"
	case "seedance2.0", "seedance2.5":
		maxDuration := 15
		if info.Model == "seedance2.5" {
			maxDuration = 30
		}
		valid = (resolution == "480p" || resolution == "720p" || resolution == "1080p") && duration >= 4 && duration <= maxDuration
		ratioOK = info.AspectRatio == "16:9" || info.AspectRatio == "9:16"
	case "minimax-h3":
		valid = (resolution == "768p" || resolution == "2k") && duration >= 4 && duration <= 15
		ratioOK = info.AspectRatio == "16:9" || info.AspectRatio == "9:16"
	case "3.0":
		valid = resolution == "720p" && (duration == 5 || duration == 10 || duration == 15)
	case "3.5":
		valid = resolution == "720p" && duration == 30
	case "sd-native-full-2.0":
		valid = (resolution == "480p" || resolution == "720p" || resolution == "1080p") && duration >= 4 && duration <= 15
	case "sd-native-full-2.5":
		valid = (resolution == "480p" || resolution == "720p") && duration >= 4 && duration <= 30
	case "minimax-h3-1080p":
		valid = resolution == "1080p" && duration >= 5 && duration <= 15 && (info.AspectRatio == "16:9" || info.AspectRatio == "9:16")
	case "wan-3.0-video-ref", "wan-3.0-no-video-ref":
		valid = resolution == "720p" && duration == 4
	default:
		return info, fmt.Errorf("unsupported public video model")
	}
	if !valid || !ratioOK {
		return info, fmt.Errorf("unsupported resolution, duration or aspect ratio for %s", info.Model)
	}
	if info.Model == "veo3.1-fast" || info.Model == "veo3.1-lite" || info.Model == "omni-flash" || info.Model == "seedance2.0" || info.Model == "seedance2.5" || info.Model == "minimax-h3" {
		for _, field := range []string{"images", "references", "face_direct"} {
			if gjson.GetBytes(body, field).Exists() {
				return info, fmt.Errorf("reference media for new video models requires a confirmed upstream public API contract")
			}
		}
	}
	// Local assets are not published automatically; the upstream validates its
	// model-specific media limits. Keep image inputs in the moderation request.
	for _, image := range gjson.GetBytes(body, "images").Array() {
		info.InputImageURLs = append(info.InputImageURLs, image.String())
	}
	for _, ref := range gjson.GetBytes(body, "references").Array() {
		if ref.Get("type").String() == "image" {
			info.InputImageURLs = append(info.InputImageURLs, ref.Get("source").String())
		}
	}
	return info, nil
}

func (s *OpenAIGatewayService) QuoteHostedVideo(ctx context.Context, key *APIKey, info GrokMediaRequestInfo) (*CostBreakdown, error) {
	if key == nil || key.Group == nil || key.User == nil {
		return nil, fmt.Errorf("video group is required")
	}
	resolved := s.resolveOpenAIChannelPricing(ctx, info.Model, key)
	if resolved == nil || resolved.Source != PricingSourceChannel || (resolved.Mode != BillingModePerRequest && resolved.Mode != BillingModeVideo) {
		return nil, fmt.Errorf("video model has no explicit channel price")
	}
	tierFound := false
	for _, tier := range resolved.RequestTiers {
		tierFound = tierFound || (tier.TierLabel == info.Resolution && tier.PerRequestPrice != nil && *tier.PerRequestPrice > 0)
	}
	if !tierFound {
		return nil, fmt.Errorf("video resolution has no explicit price")
	}
	units := 1.0
	if resolved.Mode == BillingModeVideo {
		units = float64(info.DurationSeconds)
	}
	rate := s.ResolveUserGroupRateMultiplier(ctx, key.User.ID, key.Group.ID, key.Group.RateMultiplier)
	cost, err := s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: info.Model, GroupID: key.GroupID, Group: key.Group, RequestCount: 1, UsageUnits: units, SizeTier: info.Resolution, RateMultiplier: resolveVideoRateMultiplier(key, rate), Resolver: s.resolver, Resolved: resolved})
	if err != nil {
		return nil, err
	}
	if cost == nil || cost.TotalCost <= 0 || math.IsNaN(cost.ActualCost) || math.IsInf(cost.ActualCost, 0) {
		return nil, fmt.Errorf("invalid video price")
	}
	cost.BillingMode = string(BillingModeVideo)
	return cost, nil
}

type hostedVideoReceipt struct {
	Fingerprint string
	Status      int
	Body        json.RawMessage
	Result      OpenAIForwardResult
}

// The create claim is never released after an ambiguous failure. Only a stored
// receipt may be replayed; no automatic resubmission to a different account.
func (s *OpenAIGatewayService) ForwardHostedVideo(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, taskID string, body []byte, key *APIKey, userID int64) (*OpenAIForwardResult, error) {
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityHostedVideo) || s.cache == nil {
		return nil, fmt.Errorf("hosted video account/cache is unavailable")
	}
	base, err := s.validateUpstreamBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return nil, err
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	target, method := base+"/videos", http.MethodGet
	result := &OpenAIForwardResult{ResponseID: taskID}
	receiptKey, fingerprint := "", ""
	if endpoint == HostedVideoCreate {
		body, err = CanonicalHostedVideoBody(body)
		if err != nil {
			return nil, err
		}
		info, parseErr := ParseHostedVideoRequest(body)
		if parseErr != nil {
			return nil, parseErr
		}
		cost, quoteErr := s.QuoteHostedVideo(ctx, key, info)
		if quoteErr != nil {
			return nil, quoteErr
		}
		if quoted := c.GetHeader("X-YangToken-Video-Quote"); quoted != "" {
			amount, parseErr := strconv.ParseFloat(quoted, 64)
			if parseErr != nil || math.IsNaN(amount) || math.Abs(amount-cost.ActualCost) > 0.0000001 {
				c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "Video price changed; request a new quote"}})
				return nil, fmt.Errorf("video quote changed")
			}
		}
		if key.Group.SubscriptionType != SubscriptionTypeSubscription && key.User.Balance < cost.ActualCost {
			return nil, fmt.Errorf("insufficient balance for quoted video price")
		}
		idem, idemErr := NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
		if idemErr != nil || idem == "" {
			return nil, fmt.Errorf("valid Idempotency-Key is required")
		}
		receiptKey = grokVideoPendingBillingKey("hosted-create:"+HashIdempotencyKey(idem), userID, key.ID)
		fingerprint = HashUsageRequestPayload(body)
		raw, getErr := s.cache.GetGrokVideoPendingBilling(ctx, receiptKey)
		if getErr != nil {
			return nil, getErr
		}
		if len(raw) > 0 {
			var receipt hostedVideoReceipt
			if json.Unmarshal(raw, &receipt) != nil || receipt.Fingerprint != fingerprint {
				c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "Idempotency-Key request conflict"}})
				return nil, fmt.Errorf("video create conflict")
			}
			c.Data(receipt.Status, "application/json", receipt.Body)
			return &receipt.Result, nil
		}
		claimed, claimErr := s.ClaimGrokVideoBilling(ctx, "hosted-create:"+HashIdempotencyKey(idem), userID, key.ID)
		if claimErr != nil {
			return nil, claimErr
		}
		if !claimed {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "Original video operation is processing or uncertain; do not resubmit"}})
			return nil, fmt.Errorf("video create already attempted")
		}
		result.Model, result.BillingModel, result.UpstreamModel = info.Model, info.Model, account.GetMappedModel(info.Model)
		result.VideoResolution, result.VideoDurationSeconds, result.HostedVideoCost = info.Resolution, info.DurationSeconds, cost
		body, err = sjson.SetBytes(body, "model", result.UpstreamModel)
		if err != nil {
			return nil, err
		}
		method = http.MethodPost
	} else {
		id := strings.TrimPrefix(taskID, "hosted-video:")
		if id == "" || validateUpstreamPathSegment("video task ID", id) != nil {
			return nil, fmt.Errorf("invalid video task ID")
		}
		target += "/" + id
		if endpoint == HostedVideoContent {
			target += "/content?download=1"
		}
	}
	token := strings.TrimSpace(account.GetCredential("api_key"))
	if token == "" {
		return nil, fmt.Errorf("video account missing API key")
	}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	if endpoint == HostedVideoCreate {
		req.Header.Set("Idempotency-Key", c.GetHeader("Idempotency-Key"))
	}
	if endpoint == HostedVideoContent {
		req.Header.Set("Accept", "video/*,application/octet-stream")
		if value := c.GetHeader("Range"); value != "" {
			req.Header.Set("Range", value)
		}
	}
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	result.Duration, result.ResponseHeaders = time.Since(started), resp.Header.Clone()
	if endpoint == HostedVideoContent && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent) {
		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if !strings.HasPrefix(contentType, "video/") && !strings.HasPrefix(contentType, "application/octet-stream") {
			return nil, fmt.Errorf("video content endpoint did not return video bytes")
		}
		writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		for _, name := range []string{"Content-Type", "Content-Range", "Accept-Ranges", "Content-Length"} {
			if value := resp.Header.Get(name); value != "" {
				c.Header(name, value)
			}
		}
		c.Status(resp.StatusCode)
		_, err = io.Copy(c.Writer, resp.Body)
		if err == nil {
			result.VideoCount = 1
		}
		return result, err
	}
	responseBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
		return nil, fmt.Errorf("video upstream status %d", resp.StatusCode)
	}
	if endpoint == HostedVideoCreate {
		id := ""
		for _, path := range []string{"id", "task_id", "request_id", "data.id", "data.task_id"} {
			if id = strings.TrimSpace(gjson.GetBytes(responseBody, path).String()); id != "" {
				break
			}
		}
		if id == "" || validateUpstreamPathSegment("video task ID", id) != nil {
			return nil, fmt.Errorf("video receipt missing valid task ID; result uncertain")
		}
		result.ResponseID = HostedVideoTaskKey(id)
		if err = s.BindGrokMediaVideoRequestAccount(ctx, key.GroupID, result.ResponseID, userID, key.ID, account.ID); err != nil {
			return nil, err
		}
		pending := GrokVideoPendingBilling{Model: result.Model, BillingModel: result.BillingModel, UpstreamModel: result.UpstreamModel, VideoResolution: result.VideoResolution, VideoDurationSeconds: result.VideoDurationSeconds, HostedVideoCost: result.HostedVideoCost}
		if err = s.StoreGrokVideoPendingBilling(ctx, result.ResponseID, userID, key.ID, pending); err != nil {
			return nil, err
		}
		raw, marshalErr := json.Marshal(hostedVideoReceipt{Fingerprint: fingerprint, Status: resp.StatusCode, Body: responseBody, Result: *result})
		if marshalErr != nil {
			return nil, marshalErr
		}
		if err = s.cache.SetGrokVideoPendingBilling(ctx, receiptKey, raw, grokVideoPendingBillingTTL(s.cfg)); err != nil {
			return nil, err
		}
	} else if endpoint == HostedVideoStatus {
		status := gjson.GetBytes(responseBody, "status").String()
		if status == "" {
			status = gjson.GetBytes(responseBody, "data.status").String()
		}
		if status == "completed" || status == "succeeded" || status == "done" {
			result.VideoCount = 1
		}
	}
	writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
	return result, nil
}
