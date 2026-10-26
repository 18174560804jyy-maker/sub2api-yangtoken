package handler

import (
	"context"
	"mime"
	"net/http"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) HostedVideoGeneration(c *gin.Context) {
	mediaType, _, mediaErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		h.errorResponse(c, http.StatusUnsupportedMediaType, "invalid_request_error", "Video API requires application/json")
		return
	}
	if _, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key")); err != nil || c.GetHeader("Idempotency-Key") == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Valid Idempotency-Key is required")
		return
	}
	h.handleGrokMedia(c, service.HostedVideoCreate, "")
}

func (h *OpenAIGatewayHandler) HostedVideoStatus(c *gin.Context) {
	h.handleGrokMedia(c, service.HostedVideoStatus, service.HostedVideoTaskKey(c.Param("request_id")))
}

func (h *OpenAIGatewayHandler) HostedVideoContent(c *gin.Context) {
	h.handleGrokMedia(c, service.HostedVideoContent, service.HostedVideoTaskKey(c.Param("request_id")))
}

// Quote is local-only: it cannot submit an upstream generation job.
func (h *OpenAIGatewayHandler) HostedVideoQuote(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key.Group == nil || key.Group.Platform != service.PlatformOpenAI || !service.GroupAllowsImageGeneration(key.Group) {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "Video generation is not enabled for this group")
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Cannot read quote request")
		return
	}
	body, err = service.CanonicalHostedVideoBody(body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	info, err := service.ParseHostedVideoRequest(body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	cost, err := h.gatewayService.QuoteHostedVideo(c.Request.Context(), key, info)
	if err != nil {
		h.errorResponse(c, http.StatusForbidden, "pricing_error", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"model": info.Model, "resolution": info.Resolution, "seconds": info.DurationSeconds, "amount": cost.ActualCost})
}

func prepareHostedVideoCompletionBilling(ctx context.Context, h *OpenAIGatewayHandler, key *service.APIKey, subject middleware.AuthSubject, taskID string, result *service.OpenAIForwardResult) *service.OpenAIForwardResult {
	if result == nil || result.VideoCount <= 0 {
		return nil
	}
	pending, err := h.gatewayService.LoadGrokVideoPendingBilling(ctx, taskID, subject.UserID, key.ID)
	if err != nil || pending == nil || pending.HostedVideoCost == nil {
		return nil
	}
	claimed, err := h.gatewayService.ClaimGrokVideoBilling(ctx, taskID, subject.UserID, key.ID)
	if err != nil || !claimed {
		return nil
	}
	merged := *result
	merged.Model, merged.BillingModel, merged.UpstreamModel = pending.Model, pending.BillingModel, pending.UpstreamModel
	merged.VideoResolution, merged.VideoDurationSeconds, merged.HostedVideoCost = pending.VideoResolution, pending.VideoDurationSeconds, pending.HostedVideoCost
	merged.RequestID, merged.ResponseID = service.StableGrokVideoBillingRequestID(taskID), taskID
	merged.Duration = service.GrokVideoE2EDuration(pending.CreatedAt, time.Now())
	return &merged
}
