package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyGrokImagineImageGeometryMapsOpenAISize(t *testing.T) {
	t.Parallel()

	out, err := applyGrokImagineImageGeometry([]byte(`{"model":"grok-imagine-image-2.0","prompt":"hi","size":"1152x1536"}`))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "size").Exists())
	require.Equal(t, "2k", gjson.GetBytes(out, "resolution").String())
	require.Equal(t, "3:4", gjson.GetBytes(out, "aspect_ratio").String())
}

func TestApplyGrokImagineImageGeometryKeepsClientGeometry(t *testing.T) {
	t.Parallel()

	out, err := applyGrokImagineImageGeometry([]byte(`{"size":"1024x1024","resolution":"2K","aspect_ratio":"16:9"}`))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "size").Exists())
	require.Equal(t, "2k", gjson.GetBytes(out, "resolution").String())
	require.Equal(t, "16:9", gjson.GetBytes(out, "aspect_ratio").String())
}

func TestSanitizeGrokMediaForwardBodyConvertsImageSize(t *testing.T) {
	t.Parallel()

	out, contentType, err := sanitizeGrokMediaForwardBody(
		GrokMediaEndpointImagesGenerations,
		[]byte(`{"model":"grok-imagine-image","prompt":"hi","size":"1024x1024"}`),
		"application/json",
	)
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.False(t, gjson.GetBytes(out, "size").Exists())
	require.Equal(t, "1k", gjson.GetBytes(out, "resolution").String())
	require.Equal(t, "1:1", gjson.GetBytes(out, "aspect_ratio").String())
}

func TestParseGrokMediaRequestKeepsImageResolutionOutOfVideoNormalize(t *testing.T) {
	t.Parallel()

	info := ParseGrokMediaRequest("application/json", []byte(`{"model":"grok-imagine-image-2.0","resolution":"2K","aspect_ratio":"16:9"}`))
	require.Equal(t, "2k", info.ImageResolution)
	require.Equal(t, "16:9", info.AspectRatio)
	require.Equal(t, VideoBillingResolution480P, info.Resolution)
}

func TestGrokImagineAspectRatioFromSize(t *testing.T) {
	t.Parallel()
	require.Equal(t, "1:1", grokImagineAspectRatioFromSize("1024x1024"))
	require.Equal(t, "3:4", grokImagineAspectRatioFromSize("1152x1536"))
	require.Equal(t, "4:3", grokImagineAspectRatioFromSize("1536x1152"))
	require.Equal(t, "16:9", grokImagineAspectRatioFromSize("1792x1024"))
}

func TestGrokImageResolutionBilling(t *testing.T) {
	for _, tc := range []struct {
		name, request, input, tier string
	}{
		{"canvas_1k", `{"model":"grok-imagine-image","resolution":"1k","aspect_ratio":"1:1"}`, "1k", ImageBillingSize1K},
		{"canvas_2k", `{"model":"grok-imagine-image","resolution":"2K","aspect_ratio":"16:9"}`, "2k", ImageBillingSize2K},
		{"explicit_1k_over_size", `{"model":"grok-imagine-image","resolution":"1k","size":"2048x2048"}`, "1k", ImageBillingSize1K},
		{"explicit_2k_over_size", `{"model":"grok-imagine-image","resolution":"2k","size":"1024x1024"}`, "2k", ImageBillingSize2K},
		{"legacy_size", `{"model":"grok-imagine-image","size":"1024x1024"}`, "1024x1024", ImageBillingSize1K},
		{"missing_geometry", `{"model":"grok-imagine-image"}`, "", ImageBillingSize2K},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := ParseGrokMediaRequest("application/json", []byte(tc.request))
			require.Equal(t, tc.tier, info.SizeTier)
			for _, endpoint := range []GrokMediaEndpoint{GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits} {
				meta := grokMediaUsageFromResponse(endpoint, info, []byte(`{"data":[{"url":"https://example.com/image.png"}]}`))
				require.Equal(t, tc.input, meta.ImageInputSize)
				result := &OpenAIForwardResult{ImageCount: meta.ImageCount, ImageSize: meta.ImageSize, ImageInputSize: meta.ImageInputSize, ImageOutputSizes: meta.ImageOutputSizes}
				ApplyOpenAIImageBillingResolution(result)
				require.Equal(t, tc.tier, result.ImageSize)
			}
		})
	}
}
