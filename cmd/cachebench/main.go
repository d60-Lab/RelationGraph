package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/d60-Lab/gin-template/internal/cacheperf"
	"github.com/d60-Lab/gin-template/internal/model"
)

type request struct {
	page int
	size int
}

func main() {
	ctx := context.Background()

	// Use PostgreSQL for realistic testing
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost user=postgres password=postgres dbname=postgres port=5434 sslmode=disable"
	}
	
	db := must(gorm.Open(postgres.Open(dsn), &gorm.Config{}))
	
	// Clean up existing test data
	mustDo(db.Exec("DROP TABLE IF EXISTS fans CASCADE").Error)
	mustDo(db.Exec("DROP TABLE IF EXISTS users CASCADE").Error)
	
	mustDo(db.AutoMigrate(&model.User{}, &model.Fan{}))

	const (
		userCount     = 20000  // 20k users in system
		ttlMinutes    = 10
		dbDelay       = 0 * time.Millisecond // No artificial delay with real DB
	)

	fmt.Println("Setting up test data...")
	
	// Create 3 test users to simulate different list scenarios
	user1 := model.User{ID: "user1", Username: "user1", Email: "user1@example.com", Password: "secret"}
	user2 := model.User{ID: "user2", Username: "user2", Email: "user2@example.com", Password: "secret"}
	user3 := model.User{ID: "user3", Username: "user3", Email: "user3@example.com", Password: "secret"}
	mustDo(db.Create(&user1).Error)
	mustDo(db.Create(&user2).Error)
	mustDo(db.Create(&user3).Error)

	// Create 20k users
	followers := make([]model.User, userCount)
	
	// User1's followers (10k)
	fanRows1 := make([]model.Fan, userCount/2)
	// User2's followers (10k, 50% overlap with user1)
	fanRows2 := make([]model.Fan, userCount/2)
	// User3's followers (10k, 50% overlap with user2)
	fanRows3 := make([]model.Fan, userCount/2)
	base := time.Now()
	for i := 0; i < userCount; i++ {
		id := uuid.NewString()
		followers[i] = model.User{
			ID:       id,
			Username: fmt.Sprintf("user_%d", i),
			Email:    fmt.Sprintf("user_%d@example.com", i),
			Password: "secret",
			Age:      18 + (i % 20),
		}
	}
	
	// Setup fan relationships with overlapping users
	for i := 0; i < userCount/2; i++ {
		// User1's followers: user 0-9999
		fanRows1[i] = model.Fan{
			ID:        uuid.NewString(),
			UserID:    user1.ID,
			FanID:     followers[i].ID,
			CreatedAt: base.Add(-time.Duration(i) * time.Second),
		}
		
		// User2's followers: user 5000-14999 (50% overlap with user1)
		fanRows2[i] = model.Fan{
			ID:        uuid.NewString(),
			UserID:    user2.ID,
			FanID:     followers[i+userCount/4].ID,
			CreatedAt: base.Add(-time.Duration(i) * time.Second),
		}
		
		// User3's followers: user 7500-17499 (overlap with user2)
		fanRows3[i] = model.Fan{
			ID:        uuid.NewString(),
			UserID:    user3.ID,
			FanID:     followers[(i+userCount*3/8)%userCount].ID,
			CreatedAt: base.Add(-time.Duration(i) * time.Second),
		}
	}
	
	mustDo(db.CreateInBatches(&followers, 1000).Error)
	mustDo(db.CreateInBatches(&fanRows1, 1000).Error)
	mustDo(db.CreateInBatches(&fanRows2, 1000).Error)
	mustDo(db.CreateInBatches(&fanRows3, 1000).Error)
	fmt.Println("Test data ready: 3 users with overlapping followers")

	// Use real Redis
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6380"
	}
	
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	
	// Test Redis connection
	if err := client.Ping(ctx).Err(); err != nil {
		panic(fmt.Sprintf("Failed to connect to Redis at %s: %v", redisAddr, err))
	}

	svc := cacheperf.NewFollowerService(db, client, ttlMinutes*time.Minute, dbDelay)

	// Generate requests for all 3 users (simulate multiple list scenarios)
	reqs1 := makeRequests(3000)
	reqs2 := makeRequests(3000)
	reqs3 := makeRequests(3000)
	
	// Mix requests from 3 different users
	allReqs := make([]struct{userID string; req request}, 0, 9000)
	for _, r := range reqs1 {
		allReqs = append(allReqs, struct{userID string; req request}{user1.ID, r})
	}
	for _, r := range reqs2 {
		allReqs = append(allReqs, struct{userID string; req request}{user2.ID, r})
	}
	for _, r := range reqs3 {
		allReqs = append(allReqs, struct{userID string; req request}{user3.ID, r})
	}

	noCache := runMultiUserScenario(ctx, svc, allReqs, false, func(ctx context.Context, userID string, r request) ([]cacheperf.FollowerSnapshot, error) {
		return svc.FetchFollowersNoCache(ctx, userID, r.page, r.size)
	}, client)

	naive := runMultiUserScenario(ctx, svc, allReqs, true, func(ctx context.Context, userID string, r request) ([]cacheperf.FollowerSnapshot, error) {
		return svc.FetchFollowersNaiveCache(ctx, userID, r.page, r.size)
	}, client)

	optimized := runMultiUserScenario(ctx, svc, allReqs, true, func(ctx context.Context, userID string, r request) ([]cacheperf.FollowerSnapshot, error) {
		return svc.FetchFollowersOptimized(ctx, userID, r.page, r.size)
	}, client)

	fmt.Println("\nFollower list latency (9k req across 3 users, 20k users, PostgreSQL + Redis)")
	fmt.Printf("%-18s avg=%v p95=%v p99=%v db_page=%d db_index=%d db_user_bulk=%d cache_keys=%d mem=%s\n",
		"No cache", avg(noCache.durations), pct(noCache.durations, 0.95), pct(noCache.durations, 0.99),
		noCache.counters.PageQueries, noCache.counters.IndexLoads, noCache.counters.UserBulkLoad,
		noCache.cacheKeys, formatBytes(noCache.memoryBytes),
	)
	fmt.Printf("%-18s avg=%v p95=%v p99=%v db_page=%d db_index=%d db_user_bulk=%d cache_keys=%d mem=%s\n",
		"Naive list cache", avg(naive.durations), pct(naive.durations, 0.95), pct(naive.durations, 0.99),
		naive.counters.PageQueries, naive.counters.IndexLoads, naive.counters.UserBulkLoad,
		naive.cacheKeys, formatBytes(naive.memoryBytes),
	)
	fmt.Printf("%-18s avg=%v p95=%v p99=%v db_page=%d db_index=%d db_user_bulk=%d cache_keys=%d mem=%s\n",
		"Optimized cache", avg(optimized.durations), pct(optimized.durations, 0.95), pct(optimized.durations, 0.99),
		optimized.counters.PageQueries, optimized.counters.IndexLoads, optimized.counters.UserBulkLoad,
		optimized.cacheKeys, formatBytes(optimized.memoryBytes),
	)
}

type scenarioResult struct {
	durations   []time.Duration
	counters    cacheperf.FollowerDBCounters
	cacheKeys   int
	memoryBytes int64
}

func runMultiUserScenario(ctx context.Context, svc *cacheperf.FollowerService, reqs []struct{userID string; req request}, warm bool, call func(context.Context, string, request) ([]cacheperf.FollowerSnapshot, error), client *redis.Client) scenarioResult {
	// Clear Redis
	client.FlushAll(ctx)
	svc.ResetCounters()

	if warm {
		fmt.Print("  Warming cache...")
		for _, r := range reqs {
			if _, err := call(ctx, r.userID, r.req); err != nil {
				panic(err)
			}
		}
		fmt.Println(" done")
	}

	fmt.Print("  Running benchmark...")
	out := make([]time.Duration, 0, len(reqs))
	for _, r := range reqs {
		start := time.Now()
		if _, err := call(ctx, r.userID, r.req); err != nil {
			panic(err)
		}
		out = append(out, time.Since(start))
	}
	fmt.Println(" done")
	
	// Measure Redis memory usage
	keys, _ := client.Keys(ctx, "*").Result()
	keyCount := len(keys)
	
	// Get real Redis memory stats
	info, err := client.Info(ctx, "memory").Result()
	var memBytes int64
	if err == nil {
		memBytes = parseRedisMemory(info)
	}
	
	return scenarioResult{
		durations:   out,
		counters:    svc.Counters(),
		cacheKeys:   keyCount,
		memoryBytes: memBytes,
	}
}

func runScenario(ctx context.Context, svc *cacheperf.FollowerService, reqs []request, warm bool, call func(context.Context, request) ([]cacheperf.FollowerSnapshot, error), client *redis.Client) scenarioResult {
	// Clear Redis
	client.FlushAll(ctx)
	svc.ResetCounters()

	if warm {
		fmt.Print("  Warming cache...")
		for _, r := range reqs {
			if _, err := call(ctx, r); err != nil {
				panic(err)
			}
		}
		fmt.Println(" done")
	}

	fmt.Print("  Running benchmark...")
	out := make([]time.Duration, 0, len(reqs))
	for _, r := range reqs {
		start := time.Now()
		if _, err := call(ctx, r); err != nil {
			panic(err)
		}
		out = append(out, time.Since(start))
	}
	fmt.Println(" done")
	
	// Measure Redis memory usage
	keys, _ := client.Keys(ctx, "*").Result()
	keyCount := len(keys)
	
	// Get real Redis memory stats
	info, err := client.Info(ctx, "memory").Result()
	var memBytes int64
	if err == nil {
		memBytes = parseRedisMemory(info)
	}
	
	return scenarioResult{
		durations:   out,
		counters:    svc.Counters(),
		cacheKeys:   keyCount,
		memoryBytes: memBytes,
	}
}

// parseRedisMemory extracts used_memory from Redis INFO
func parseRedisMemory(info string) int64 {
	lines := []rune(info)
	var result int64
	
	// Look for "used_memory:" line
	for i := 0; i < len(lines); {
		if i+12 < len(lines) && string(lines[i:i+12]) == "used_memory:" {
			// Parse the number
			i += 12
			var num int64
			for i < len(lines) && lines[i] >= '0' && lines[i] <= '9' {
				num = num*10 + int64(lines[i]-'0')
				i++
			}
			result = num
			break
		}
		i++
	}
	return result
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func makeRequests(n int) []request {
	sizes := []int{20, 40, 60}
	out := make([]request, n)
	rnd := rand.New(rand.NewSource(42))
	for i := 0; i < n; i++ {
		size := sizes[rnd.Intn(len(sizes))]
		page := 1
		if rnd.Float64() > 0.72 {
			// simulate deep pagination or different views
			page = 2 + rnd.Intn(120)
		}
		out[i] = request{page: page, size: size}
	}
	return out
}

func avg(vs []time.Duration) time.Duration {
	if len(vs) == 0 {
		return 0
	}
	var sum time.Duration
	for _, v := range vs {
		sum += v
	}
	return sum / time.Duration(len(vs))
}

func pct(vs []time.Duration, p float64) time.Duration {
	if len(vs) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), vs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func mustDo(err error) {
	if err != nil {
		panic(err)
	}
}
