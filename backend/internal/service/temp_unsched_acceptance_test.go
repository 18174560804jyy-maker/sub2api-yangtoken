//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type tempUnschedAcceptanceRepo struct {
	schedulerTestOpenAIAccountRepo
	modelRateLimitCalls []modelNotFoundRateLimitCall
	accountTempCalls    int
}

func (r *tempUnschedAcceptanceRepo) SetModelRateLimit(_ context.Context, id int64, model string, resetAt time.Time, reason ...string) error {
	call := modelNotFoundRateLimitCall{accountID: id, scope: model, resetAt: resetAt}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, call)
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			setAccountModelRateLimitSnapshot(&r.accounts[i], model, resetAt, call.reason, time.Now())
			break
		}
	}
	return nil
}

func (r *tempUnschedAcceptanceRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.accountTempCalls++
	return nil
}

func acceptanceTempRule(code int, keyword string) map[string]any {
	return map[string]any{
		"error_code":       float64(code),
		"keywords":         []any{keyword},
		"duration_minutes": float64(10),
		"scope":            "model",
	}
}

func TestConfiguredOpenAICapacityErrorAcceptance_ModelFailoverIsolationAndExpiry(t *testing.T) {
	ctx := context.Background()
	primary := Account{
		ID: 32111, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Priority: 0,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules":   []any{acceptanceTempRule(http.StatusServiceUnavailable, "overloaded")},
		},
	}
	secondary := Account{ID: 32112, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 5}
	repo := &tempUnschedAcceptanceRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{primary, secondary}}}
	rateLimits := &RateLimitService{accountRepo: repo}
	gateway := &OpenAIGatewayService{accountRepo: repo, rateLimitService: rateLimits}
	body := []byte(`{"error":{"type":"server_error","message":"Our servers are currently overloaded. Please try again later."}}`)

	stopped := gateway.handleOpenAIAccountUpstreamError(ctx, &repo.accounts[0], http.StatusServiceUnavailable, http.Header{}, body, "gpt-5.5")
	require.True(t, stopped, "matched upstream failure should stop retrying this account for this request")
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, int64(32111), repo.modelRateLimitCalls[0].accountID)
	require.Equal(t, "gpt-5.5", repo.modelRateLimitCalls[0].scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
	require.Zero(t, repo.accountTempCalls, "model-scoped rules must not pause the whole account")

	selected, err := gateway.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.5", nil)
	require.NoError(t, err)
	require.Equal(t, int64(32112), selected.ID, "failed model should switch to the next account")
	selected, err = gateway.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.4", nil)
	require.NoError(t, err)
	require.Equal(t, int64(32111), selected.ID, "other models on the original account should remain available")

	// Advance only the stored scheduler deadline to test the eligibility boundary
	// without sleeping through ten minutes or changing a production account.
	setAccountModelRateLimitSnapshot(&repo.accounts[0], "gpt-5.5", time.Now().Add(-time.Second), "expired acceptance fixture", time.Now())
	selected, err = gateway.SelectAccountForModelWithExclusions(ctx, nil, "", "gpt-5.5", nil)
	require.NoError(t, err)
	require.Equal(t, int64(32111), selected.ID, "the original model should be eligible again after expiry")
}

func TestConfiguredGrokTempRulePrecedesLegacyAccountCooldown(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	account := &Account{
		ID: 5098, Platform: PlatformGrok, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules":   []any{acceptanceTempRule(http.StatusBadGateway, "upstream failed")},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:      repo,
		rateLimitService: &RateLimitService{accountRepo: repo},
	}

	svc.handleGrokAccountUpstreamError(
		withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), account,
		http.StatusBadGateway, http.Header{}, []byte(`{"error":"upstream failed"}`),
	)

	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "grok-4.5", repo.modelRateLimitCalls[0].scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
	require.Zero(t, repo.tempCalls, "configured model cooldown must take precedence over the legacy account cooldown")
}

func TestConfiguredOpenAIRulesOnlyMatchObservedUpstreamParameterFault(t *testing.T) {
	account := &Account{Credentials: map[string]any{
		"temp_unschedulable_enabled": true,
		"temp_unschedulable_rules": []any{
			acceptanceTempRule(http.StatusBadRequest, "模型请求失败，请稍后重试或更换模型"),
		},
	}}
	require.Len(t, matchTempUnschedulableRules(account, http.StatusBadRequest,
		[]byte(`{"error":{"message":"模型请求失败，请稍后重试或更换模型。","type":"upstream_error"}}`)), 1)
	require.Empty(t, matchTempUnschedulableRules(account, http.StatusBadRequest,
		[]byte(`{"error":{"message":"Invalid request: malformed input","type":"invalid_request_error"}}`)))
}

func TestConfiguredOpenAIRulePrecedesBuiltinModelNotFoundCooldown(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := openAIModelNotFoundTempAccount()
	account.Credentials["temp_unschedulable_rules"] = []any{
		acceptanceTempRule(http.StatusNotFound, "model not found"),
	}

	handled := svc.handleOpenAIAccountUpstreamError(
		context.Background(), account, http.StatusNotFound, http.Header{},
		[]byte(`{"error":{"code":"model_not_found","message":"model not found"}}`), "gpt-5.5",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.5", repo.modelRateLimitCalls[0].scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
}

func TestConfiguredModelNotFoundRulePrecedesBuiltinCooldownInCommonHandler(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	account := newTempUnschedScopeAccount("model", nil)
	account.Credentials["temp_unschedulable_rules"] = []any{
		acceptanceTempRule(http.StatusNotFound, "model_not_found"),
	}
	svc := &RateLimitService{accountRepo: repo}

	handled := svc.HandleUpstreamError(
		context.Background(), account, http.StatusNotFound, http.Header{},
		[]byte(`{"error":{"code":"model_not_found","message":"model not found"}}`), "gpt-5.5",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.5", repo.modelRateLimitCalls[0].scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
}

func TestConfiguredOpenAIRulePrecedesAccessBlockWhenModelKnown(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := openAIModelNotFoundTempAccount()
	account.Credentials["temp_unschedulable_rules"] = []any{
		acceptanceTempRule(http.StatusForbidden, "upstream access forbidden"),
	}

	handled := svc.handleOpenAIAccountUpstreamError(
		context.Background(), account, http.StatusForbidden, http.Header{},
		[]byte(`{"error":{"message":"Upstream access forbidden, please contact administrator"}}`), "gpt-5.5",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.5", repo.modelRateLimitCalls[0].scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
}

func TestConfiguredGenericAuthRejectionIsModelScoped(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := openAIModelNotFoundTempAccount()
	account.Credentials["temp_unschedulable_rules"] = []any{
		acceptanceTempRule(http.StatusUnauthorized, "authentication failed"),
	}
	body := []byte(`{"error":{"message":"authentication failed: upstream rejected credential for this model"}}`)

	handled := svc.handleOpenAIAccountUpstreamError(
		context.Background(), account, http.StatusUnauthorized, http.Header{}, body, "gpt-5.5",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.5", repo.modelRateLimitCalls[0].scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.modelRateLimitCalls[0].resetAt, 5*time.Second)
}
