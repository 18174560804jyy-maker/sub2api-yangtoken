package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// EnsureCanvasMediaKey reuses the user's own key, or creates one through the
// normal permission and creation-limit checks. Locks cover concurrent tabs on
// YangToken's single application instance without caching deleted credentials.
func (s *APIKeyService) EnsureCanvasMediaKey(ctx context.Context, userID, groupID, preferredID int64, clientIP string) (*APIKey, error) {
	platform := PlatformOpenAI
	switch groupID {
	case 19:
		platform = PlatformGrok
	case 26, 27:
	default:
		return nil, ErrGroupNotAllowed
	}
	lock := &s.canvasKeyLocks[(uint64(userID)*31+uint64(groupID))%uint64(len(s.canvasKeyLocks))]
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.ID != userID || user.Status != StatusActive {
		return nil, ErrUserNotActive
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil || group.ID != groupID || group.Status != StatusActive || group.Platform != platform || !group.AllowImageGeneration || !s.canUserBindGroup(ctx, user, group) {
		return nil, ErrGroupNotAllowed
	}
	var first *APIKey
	for page := 1; ; page++ {
		keys, result, err := s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{Page: page, PageSize: 100, SortBy: "created_at", SortOrder: "asc"}, APIKeyListFilters{GroupID: &groupID, Status: StatusActive})
		if err != nil {
			return nil, err
		}
		for i := range keys {
			key := &keys[i]
			if key.UserID != userID || key.GroupID == nil || *key.GroupID != groupID || !canvasMediaKeyUsable(key, clientIP) {
				continue
			}
			key.Group = group
			if key.ID == preferredID {
				return key, nil
			}
			if first == nil {
				first = key
			}
		}
		if result == nil || int64(page*100) >= result.Total {
			break
		}
	}
	if first != nil {
		return first, nil
	}
	key, err := s.Create(ctx, userID, CreateAPIKeyRequest{Name: "无限画布 · " + group.Name, GroupID: &groupID})
	if err != nil {
		return nil, err
	}
	key.Group = group
	return key, nil
}

func canvasMediaKeyUsable(key *APIKey, clientIP string) bool {
	if key.ID <= 0 || strings.TrimSpace(key.Key) == "" || !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() {
		return false
	}
	if (key.RateLimit5h > 0 && key.EffectiveUsage5h() >= key.RateLimit5h) ||
		(key.RateLimit1d > 0 && key.EffectiveUsage1d() >= key.RateLimit1d) ||
		(key.RateLimit7d > 0 && key.EffectiveUsage7d() >= key.RateLimit7d) {
		return false
	}
	allowed, _ := ip.CheckIPRestriction(clientIP, key.IPWhitelist, key.IPBlacklist)
	return allowed
}
