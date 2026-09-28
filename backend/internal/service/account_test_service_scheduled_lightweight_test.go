package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestCreateScheduledClaudeTestPayloadIsLightweight(t *testing.T) {
	payload := createScheduledClaudeTestPayload("claude-sonnet-5")

	require.Equal(t, "claude-sonnet-5", payload["model"])
	require.Equal(t, scheduledAnthropicMaxOutputTokens, payload["max_tokens"])
	require.Equal(t, true, payload["stream"])
	require.NotContains(t, payload, "system")
	require.NotContains(t, payload, "metadata")
	require.NotContains(t, payload, "temperature")

	messages := payload["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	require.Equal(t, scheduledAccountTestPrompt, content[0]["text"])
}

func TestCreateScheduledOpenAITestPayloadIsLightweight(t *testing.T) {
	payload := createScheduledOpenAITestPayload("gpt-5.6-sol", false)

	require.Equal(t, "gpt-5.6-sol", payload["model"])
	require.Equal(t, scheduledOpenAIMaxOutputTokens, payload["max_output_tokens"])
	require.Equal(t, true, payload["stream"])
	require.NotContains(t, payload, "instructions")
	require.NotContains(t, payload, "store")

	input := payload["input"].([]map[string]any)
	content := input[0]["content"].([]map[string]any)
	require.Equal(t, scheduledAccountTestPrompt, content[0]["text"])
}

func TestCreateOpenAITestPayloadKeepsManualCompatibility(t *testing.T) {
	payload := createOpenAITestPayload("gpt-5.6-sol", false)

	require.Equal(t, openai.DefaultInstructions, payload["instructions"])
	require.NotContains(t, payload, "max_output_tokens")
}

func TestCreateScheduledOpenAIChatCompletionsPayloadIsLightweight(t *testing.T) {
	payload := createScheduledOpenAIChatCompletionsTestPayload("gpt-5.6-sol")

	require.Equal(t, scheduledOpenAIMaxOutputTokens, payload["max_tokens"])
	messages := payload["messages"].([]map[string]any)
	require.Equal(t, scheduledAccountTestPrompt, messages[0]["content"])
}
