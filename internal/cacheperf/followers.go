package cacheperf

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/d60-Lab/gin-template/internal/model"
)

// FollowerSnapshot contains minimal user info required by timeline/follower pages.
type FollowerSnapshot struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Age      int    `json:"age"`
}

// FollowerService demonstrates different caching strategies for follower list reads.
type FollowerService struct {
	db      *gorm.DB
	cache   *redis.Client
	ttl     time.Duration
	dbDelay time.Duration

	pageQueries  atomic.Int64
	indexLoads   atomic.Int64
	userBulkLoad atomic.Int64
}

// NewFollowerService builds a demo service using the provided DB + Redis client.
// dbDelay simulates the round-trip cost of hitting the primary store.
func NewFollowerService(db *gorm.DB, cache *redis.Client, ttl, dbDelay time.Duration) *FollowerService {
	return &FollowerService{db: db, cache: cache, ttl: ttl, dbDelay: dbDelay}
}

func (s *FollowerService) FetchFollowersNoCache(ctx context.Context, userID string, page, size int) ([]FollowerSnapshot, error) {
	return s.queryFollowers(ctx, userID, page, size)
}

func (s *FollowerService) FetchFollowersNaiveCache(ctx context.Context, userID string, page, size int) ([]FollowerSnapshot, error) {
	key := fmt.Sprintf("followers:%s:%d:%d", userID, page, size)
	if data, err := s.cache.Get(ctx, key).Bytes(); err == nil {
		var out []FollowerSnapshot
		if uErr := json.Unmarshal(data, &out); uErr == nil {
			return out, nil
		}
	}

	rows, err := s.queryFollowers(ctx, userID, page, size)
	if err != nil {
		return nil, err
	}
	if payload, err := json.Marshal(rows); err == nil {
		_ = s.cache.Set(ctx, key, payload, s.ttl).Err()
	}
	return rows, nil
}

func (s *FollowerService) FetchFollowersOptimized(ctx context.Context, userID string, page, size int) ([]FollowerSnapshot, error) {
	key := fmt.Sprintf("followers:index:%s", userID)
	
	// Calculate range
	start := (page - 1) * size
	end := start + size - 1
	
	// Try to get IDs directly from Redis List with range query
	exists, _ := s.cache.Exists(ctx, key).Result()
	var ids []string
	
	if exists > 0 {
		// Use LRANGE to get only the needed IDs (efficient!)
		ids, _ = s.cache.LRange(ctx, key, int64(start), int64(end)).Result()
	}
	
	// If cache miss, load all IDs and cache them
	if len(ids) == 0 {
		allIDs, err := s.loadFollowerIDsAndCache(ctx, userID)
		if err != nil {
			return nil, err
		}
		
		if start >= len(allIDs) {
			return []FollowerSnapshot{}, nil
		}
		endIdx := start + size
		if endIdx > len(allIDs) {
			endIdx = len(allIDs)
		}
		ids = allIDs[start:endIdx]
	}

	snapshots, err := s.loadUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (s *FollowerService) loadFollowerIDsAndCache(ctx context.Context, userID string) ([]string, error) {
	time.Sleep(s.dbDelay)
	s.indexLoads.Add(1)

	var ids []string
	if err := s.db.WithContext(ctx).
		Table("fans").
		Select("fan_id").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Scan(&ids).Error; err != nil {
		return nil, err
	}

	// Store as Redis List
	key := fmt.Sprintf("followers:index:%s", userID)
	if len(ids) > 0 {
		pipe := s.cache.Pipeline()
		pipe.Del(ctx, key)
		pipe.RPush(ctx, key, interfaceSlice(ids)...)
		pipe.Expire(ctx, key, s.ttl)
		pipe.Exec(ctx)
	}
	
	return ids, nil
}

func (s *FollowerService) queryFollowers(ctx context.Context, userID string, page, size int) ([]FollowerSnapshot, error) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	time.Sleep(s.dbDelay)

	s.pageQueries.Add(1)

	var rows []FollowerSnapshot
	err := s.db.WithContext(ctx).
		Table("fans").
		Select("users.id", "users.username", "users.email", "users.age").
		Joins("JOIN users ON fans.fan_id = users.id").
		Where("fans.user_id = ?", userID).
		Order("fans.created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}



func interfaceSlice(strs []string) []interface{} {
	result := make([]interface{}, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}

func (s *FollowerService) loadUsers(ctx context.Context, ids []string) ([]FollowerSnapshot, error) {
	if len(ids) == 0 {
		return []FollowerSnapshot{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = fmt.Sprintf("user:%s", id)
	}

	cached := make(map[string]FollowerSnapshot, len(ids))
	if vals, err := s.cache.MGet(ctx, keys...).Result(); err == nil {
		for i, v := range vals {
			if v == nil {
				continue
			}
			if str, ok := v.(string); ok {
				var snap FollowerSnapshot
				if uErr := json.Unmarshal([]byte(str), &snap); uErr == nil {
					cached[ids[i]] = snap
				}
			}
		}
	}

	missing := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := cached[id]; !ok {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		s.userBulkLoad.Add(1)

		time.Sleep(s.dbDelay)

		var users []model.User
		if err := s.db.WithContext(ctx).Where("id IN ?", missing).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, u := range users {
			snap := FollowerSnapshot{
				ID:       u.ID,
				Username: u.Username,
				Email:    u.Email,
				Age:      u.Age,
			}
			cached[u.ID] = snap
			if payload, err := json.Marshal(snap); err == nil {
				_ = s.cache.Set(ctx, fmt.Sprintf("user:%s", u.ID), payload, s.ttl).Err()
			}
		}
	}

	result := make([]FollowerSnapshot, 0, len(ids))
	for _, id := range ids {
		if snap, ok := cached[id]; ok {
			result = append(result, snap)
		}
	}
	return result, nil
}

// ResetCounters clears recorded db call counters.
func (s *FollowerService) ResetCounters() {
	s.pageQueries.Store(0)
	s.indexLoads.Store(0)
	s.userBulkLoad.Store(0)
}

// Counters reports how many underlying DB loads were executed.
func (s *FollowerService) Counters() FollowerDBCounters {
	return FollowerDBCounters{
		PageQueries:  s.pageQueries.Load(),
		IndexLoads:   s.indexLoads.Load(),
		UserBulkLoad: s.userBulkLoad.Load(),
	}
}

// FollowerDBCounters summarises DB hits during a run.
type FollowerDBCounters struct {
	PageQueries  int64
	IndexLoads   int64
	UserBulkLoad int64
}
