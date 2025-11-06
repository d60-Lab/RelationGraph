package main

import (
    "context"
    "fmt"
    "math"
    "os"
    "strconv"
    "sort"
    "time"

    "github.com/google/uuid"

    "github.com/d60-Lab/gin-template/config"
    "github.com/d60-Lab/gin-template/internal/model"
    "github.com/d60-Lab/gin-template/internal/repository"
    "github.com/d60-Lab/gin-template/internal/service"
    "github.com/d60-Lab/gin-template/pkg/database"
)

func must[T any](v T, err error) T { if err != nil { panic(err) }; return v }

func main() {
    cfg := must(config.Load())
    db := must(database.InitDB(cfg))

    // repositories & services
    followRepo := repository.NewFollowRepository(db)
    fanRepo := repository.NewFanRepository(db)
    replicator := service.NewFanReplicator(fanRepo, 100000)
    stop := replicator.Start(8)
    relSvc := service.NewRelationshipService(followRepo, fanRepo, replicator)

    ctx := context.Background()

    N := 10000
    if s := os.Getenv("N"); s != "" {
        if n, err := strconv.Atoi(s); err == nil && n > 0 { N = n }
    }
    CONC := 1
    if s := os.Getenv("CONC"); s != "" {
        if c, err := strconv.Atoi(s); err == nil && c > 0 { CONC = c }
    }
    PAGE := 50
    if s := os.Getenv("PAGE"); s != "" {
        if p, err := strconv.Atoi(s); err == nil && p > 0 { PAGE = p }
    }

    // seed users: u0 is celebrity; others follow u0
    celeb := model.User{ID: "u0", Username: "u0", Email: "u0@example.com", Password: "p"}
    _ = db.Where("id = ?", celeb.ID).FirstOrCreate(&celeb).Error
    users := make([]model.User, N)
    batch := 1000
    for i := 0; i < N; i++ {
        id := uuid.New().String()
        users[i] = model.User{ID: id, Username: "u" + id[:8], Email: id[:8] + "@example.com", Password: "p"}
        if (i+1)%batch == 0 {
            sub := users[i+1-batch : i+1]
            _ = db.Create(&sub).Error
        }
    }
    if N%batch != 0 {
        sub := users[N-N%batch:]
        _ = db.Create(&sub).Error
    }

    // measure async path (service async redundancy) with concurrency
    type rec struct{ d time.Duration }
    asyncRecs := make([]time.Duration, 0, N)
    asyncCh := make(chan time.Duration, N)
    // metrics for replication landing
    repMetrics := replicator.Metrics()
    repRecs := make([]time.Duration, 0, N)
    doneRep := make(chan struct{})
    go func() {
        timeout := time.NewTimer(5 * time.Minute)
        defer timeout.Stop()
        for {
            select {
            case d := <-repMetrics:
                repRecs = append(repRecs, d)
            case <-doneRep:
                return
            case <-timeout.C:
                return
            }
        }
    }()

    maxQ := 0
    quitSample := make(chan struct{})
    go func() {
        ticker := time.NewTicker(50 * time.Millisecond)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                if q := replicator.QueueLen(); q > maxQ { maxQ = q }
            case <-quitSample:
                return
            }
        }
    }()

    t0 := time.Now()
    // dispatch N operations with CONC workers
    var workers int = CONC
    if workers > N { workers = N }
    errCh := make(chan error, workers)
    // simpler deterministic feed
    feed := make(chan int, N)
    for i := 0; i < N; i++ { feed <- i }
    close(feed)
    for w := 0; w < workers; w++ {
        go func() {
            for i := range feed {
                st := time.Now()
                _ = relSvc.Follow(ctx, users[i].ID, celeb.ID)
                asyncCh <- time.Since(st)
            }
            errCh <- nil
        }()
    }
    for w := 0; w < workers; w++ { <-errCh }
    close(asyncCh)
    for d := range asyncCh { asyncRecs = append(asyncRecs, d) }
    asyncDur := time.Since(t0)
    close(quitSample)

    // wait a bit for replication to catch up
    drainStart := time.Now()
    time.Sleep(500 * time.Millisecond)

    // measure sync path (2 writes)
    t1 := time.Now()
    for i := 0; i < N; i++ {
        _ = followRepo.Create(ctx, celeb.ID, users[i].ID)
        _ = fanRepo.Create(ctx, users[i].ID, celeb.ID)
    }
    syncDur := time.Since(t1)

    // queries
    q0 := time.Now()
    _, _ = fanRepo.ListFans(ctx, celeb.ID, 0, PAGE)
    fansDur := time.Since(q0)

    q1 := time.Now()
    _, _ = followRepo.ListFollowings(ctx, celeb.ID, 0, PAGE)
    follDur := time.Since(q1)

    // stop replicator (will wait queue to drain internally)
    _ = stop(context.Background())
    drainDur := time.Since(drainStart)
    close(doneRep)

    // Percentiles helper
    pct := func(vs []time.Duration, p float64) time.Duration {
        if len(vs) == 0 { return 0 }
        xs := append([]time.Duration(nil), vs...)
        // partial sort
        sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
        k := int(math.Ceil(p*float64(len(xs)))) - 1
        if k < 0 { k = 0 }
        if k >= len(xs) { k = len(xs)-1 }
        return xs[k]
    }

    // print
    fmt.Printf("N=%d, CONC=%d, PAGE=%d\n", N, CONC, PAGE)
    fmt.Printf("Async follow latency total: %v, per op: %v, p50: %v, p95: %v, p99: %v\n",
        asyncDur, asyncDur/time.Duration(N), pct(asyncRecs, 0.50), pct(asyncRecs, 0.95), pct(asyncRecs, 0.99))
    fmt.Printf("Sync (2 writes) total: %v, per op: %v\n", syncDur, syncDur/time.Duration(N))
    fmt.Printf("Query fans(%d) latency: %v\n", PAGE, fansDur)
    fmt.Printf("Query following(%d) latency: %v\n", PAGE, follDur)
    // replication metrics
    if len(repRecs) > 0 {
        fmt.Printf("Replication landing: samples=%d, p50=%v, p95=%v, p99=%v, maxQueue=%d, drain=%v\n",
            len(repRecs), pct(repRecs, 0.50), pct(repRecs, 0.95), pct(repRecs, 0.99), maxQ, drainDur)
    }
}
