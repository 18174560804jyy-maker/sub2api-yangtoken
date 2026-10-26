package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type canvasUserRepo struct {
	UserRepository
	user *User
}

func (r *canvasUserRepo) GetByID(context.Context, int64) (*User, error) { return r.user, nil }

type canvasGroupRepo struct {
	GroupRepository
	group *Group
}

func (r *canvasGroupRepo) GetByID(context.Context, int64) (*Group, error) { return r.group, nil }

type canvasKeyRepo struct {
	APIKeyRepository
	mu      sync.Mutex
	keys    []APIKey
	created int
}

func (r *canvasKeyRepo) ListByUserID(_ context.Context, userID int64, p pagination.PaginationParams, f APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var keys []APIKey
	for _, k := range r.keys {
		if k.UserID == userID && k.GroupID != nil && *k.GroupID == *f.GroupID {
			keys = append(keys, k)
		}
	}
	return keys, &pagination.PaginationResult{Total: int64(len(keys))}, nil
}
func (r *canvasKeyRepo) Create(_ context.Context, key *APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created++
	key.ID = int64(100 + r.created)
	r.keys = append(r.keys, *key)
	return nil
}
func (r *canvasKeyRepo) CountByUserID(context.Context, int64) (int64, error) {
	return int64(len(r.keys)), nil
}
func canvasKeyFixture() (*APIKeyService, *canvasKeyRepo, *canvasUserRepo, *canvasGroupRepo) {
	r := &canvasKeyRepo{}
	u := &canvasUserRepo{user: &User{ID: 1, Status: StatusActive}}
	g := &canvasGroupRepo{group: &Group{ID: 27, Name: "GPT生图分组", Platform: PlatformOpenAI, Status: StatusActive, AllowImageGeneration: true}}
	return &APIKeyService{apiKeyRepo: r, userRepo: u, groupRepo: g, cfg: &config.Config{}}, r, u, g
}

func TestCanvasMediaKeyReuseDeleteAndConcurrency(t *testing.T) {
	s, r, _, g := canvasKeyFixture()
	first, err := s.EnsureCanvasMediaKey(context.Background(), 1, 27, 0, "127.0.0.1")
	require.NoError(t, err)
	require.Equal(t, "无限画布 · GPT生图分组", first.Name)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, e := s.EnsureCanvasMediaKey(context.Background(), 1, 27, 0, "127.0.0.1")
			if e != nil || key.ID != first.ID {
				t.Errorf("reuse: %v", e)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, r.created)
	r.keys = nil
	var ids [20]int64
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key, e := s.EnsureCanvasMediaKey(context.Background(), 1, 27, first.ID, "127.0.0.1")
			if e != nil {
				t.Error(e)
				return
			}
			ids[i] = key.ID
		}(i)
	}
	wg.Wait()
	require.Equal(t, 2, r.created)
	for _, id := range ids {
		require.Equal(t, int64(102), id)
	}
	manual := APIKey{ID: 999, UserID: 1, GroupID: &g.group.ID, Status: StatusActive, Key: "manual", Name: "Existing manual key"}
	r.keys = append(r.keys, manual)
	key, err := s.EnsureCanvasMediaKey(context.Background(), 1, 27, 999, "127.0.0.1")
	require.NoError(t, err)
	require.Equal(t, int64(999), key.ID)
	require.Equal(t, 2, r.created)
}

func TestCanvasMediaKeyRespectsPermissionsAndLimits(t *testing.T) {
	for _, kind := range []string{"unsupported", "inactive-user", "inactive-group", "no-images", "restricted", "wrong-platform", "count-limit"} {
		t.Run(kind, func(t *testing.T) {
			s, r, u, g := canvasKeyFixture()
			groupID := int64(27)
			switch kind {
			case "unsupported":
				groupID = 1
			case "inactive-user":
				u.user.Status = "inactive"
			case "inactive-group":
				g.group.Status = "inactive"
			case "no-images":
				g.group.AllowImageGeneration = false
			case "restricted":
				u.user.RestrictPublicGroups = true
			case "wrong-platform":
				g.group.Platform = PlatformGrok
			case "count-limit":
				s.cfg = &config.Config{}
				s.cfg.APIKeyCreate.MaxActivePerUser = 1
				r.keys = []APIKey{{ID: 9, UserID: 1}}
			}
			_, err := s.EnsureCanvasMediaKey(context.Background(), 1, groupID, 0, "127.0.0.1")
			require.Error(t, err)
			require.Zero(t, r.created)
		})
	}
}

func TestCanvasMediaKeySkipsUnusableKeysAndReusesAlternative(t *testing.T) {
	for _, kind := range []string{"other-user", "wrong-group", "disabled", "expired", "quota", "rate-limit", "ip"} {
		t.Run(kind, func(t *testing.T) {
			s, r, _, g := canvasKeyFixture()
			past := time.Now().Add(-time.Hour)
			now := time.Now()
			other := int64(26)
			bad := APIKey{ID: 9, UserID: 1, GroupID: &g.group.ID, Status: StatusActive, Key: "existing"}
			switch kind {
			case "other-user":
				bad.UserID = 2
			case "wrong-group":
				bad.GroupID = &other
			case "disabled":
				bad.Status = "inactive"
			case "expired":
				bad.ExpiresAt = &past
			case "quota":
				bad.Quota = 1
				bad.QuotaUsed = 1
			case "rate-limit":
				bad.RateLimit1d = 1
				bad.Usage1d = 1
				bad.Window1dStart = &now
			case "ip":
				bad.IPWhitelist = []string{"10.0.0.1"}
			}
			good := APIKey{ID: 10, UserID: 1, GroupID: &g.group.ID, Status: StatusActive, Key: fmt.Sprint("good-", kind)}
			r.keys = []APIKey{bad, good}
			key, err := s.EnsureCanvasMediaKey(context.Background(), 1, 27, 9, "127.0.0.1")
			require.NoError(t, err)
			require.Equal(t, int64(10), key.ID)
			require.Zero(t, r.created)
		})
	}
}
