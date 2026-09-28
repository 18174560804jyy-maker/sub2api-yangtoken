package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	openAIAccountCapacityCooldown             = 30 * time.Minute
	openAIAccountCapacityCooldownReasonPrefix = "openai upstream account capacity cooldown: "
)

func isOpenAIAccountCapacityError(payload []byte, message string) bool {
	if openAIStreamFailedEventErrorCode(payload) == "server_is_overloaded" {
		return true
	}
	combined := strings.ToLower(strings.TrimSpace(message + " " + extractOpenAISSEErrorMessage(payload)))
	return strings.Contains(combined, "server_is_overloaded") ||
		strings.Contains(combined, "our servers are currently overloaded") ||
		strings.Contains(combined, "selected model is at capacity")
}

func normalizeOpenAIAccountCapacityMessage(payload []byte, message string) string {
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if !isOpenAIAccountCapacityError(payload, message) {
		return message
	}
	if extracted := extractOpenAISSEErrorMessage(payload); extracted != "" {
		message = extracted
	}
	switch strings.ToLower(strings.TrimSpace(message)) {
	case "", "unknown error", "openai response failed", "server_is_overloaded":
		return "Our servers are currently overloaded. Please try again later."
	default:
		return message
	}
}

func isOpenAIAccountCapacityCooldownActive(account *Account, now time.Time) bool {
	return account != nil &&
		account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) &&
		strings.HasPrefix(account.TempUnschedulableReason, openAIAccountCapacityCooldownReasonPrefix)
}

func openAIAccountCapacityCooldownState(account *Account, payload []byte, message string, now time.Time) (time.Time, string, bool) {
	if account == nil || account.Platform != PlatformOpenAI || !isOpenAIAccountCapacityError(payload, message) {
		return time.Time{}, "", false
	}
	message = normalizeOpenAIAccountCapacityMessage(payload, message)
	if message == "" {
		message = "upstream account capacity exhausted"
	}
	return now.Add(openAIAccountCapacityCooldown), openAIAccountCapacityCooldownReasonPrefix + message, true
}

// observeOpenAIAccountCapacity records the upstream signal without changing
// account scheduling. The current gateway request may still fail over, but a
// single user request must not make the account unavailable to later traffic.
func (s *OpenAIGatewayService) observeOpenAIAccountCapacity(account *Account, payload []byte, message string) {
	if s == nil || account == nil || !isOpenAIAccountCapacityError(payload, message) {
		return
	}
	logger.L().With(zap.String("component", "service.openai_gateway")).Warn(
		"openai.account_capacity_observed",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.String("message", normalizeOpenAIAccountCapacityMessage(payload, message)),
		zap.Bool("scheduling_changed", false),
	)
}

func (s *AccountTestService) tempUnscheduleOpenAIAccountCapacity(ctx context.Context, account *Account, payload []byte, message string) {
	if s == nil || s.accountRepo == nil {
		return
	}
	until, reason, ok := openAIAccountCapacityCooldownState(account, payload, message, time.Now())
	if !ok {
		return
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if err := s.accountRepo.SetTempUnschedulable(stateCtx, account.ID, until, reason); err != nil {
		logger.L().With(zap.String("component", "service.account_test")).Warn(
			"openai.account_test_capacity_cooldown_failed",
			zap.Int64("account_id", account.ID),
			zap.Error(err),
		)
		return
	}
	logger.L().With(zap.String("component", "service.account_test")).Warn(
		"openai.account_test_capacity_cooldown",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.Time("until", until),
		zap.String("reason", reason),
	)
}

func (s *AccountTestService) tempUnscheduleOpenAIAccountCapacityByID(ctx context.Context, accountID int64, message string) {
	if s == nil || s.accountRepo == nil {
		return
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		logger.L().With(zap.String("component", "service.account_test")).Warn(
			"openai.account_test_capacity_account_load_failed",
			zap.Int64("account_id", accountID),
			zap.Error(err),
		)
		return
	}
	s.tempUnscheduleOpenAIAccountCapacity(ctx, account, nil, message)
}
