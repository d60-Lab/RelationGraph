package main

import (
    "context"
    "fmt"
    "math"
    "os"
    "sort"
    "strconv"
    "time"

    "github.com/google/uuid"

    "github.com/d60-Lab/gin-template/config"
    "github.com/d60-Lab/gin-template/internal/model"
    "github.com/d60-Lab/gin-template/internal/repository"
    "github.com/d60-Lab/gin-template/internal/service"
    "github.com/d60-Lab/gin-template/pkg/database"
)

func must[T any](v T, err error) T { if err != nil { panic(err) }; return v }

func pct(vs []time.Duration, p float64) time.Duration {
    if len(vs) == 0 { return 0 }
    xs := append([]time.Duration(nil), vs...)
    sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
    k := int(math.Ceil(p*float64(len(xs)))) - 1
    if k < 0 { k = 0 }
    if k >= len(xs) { k = len(xs)-1 }
    return xs[k]
}

func main() {
    cfg := must(config.Load())
    db := must(database.InitDB(cfg))

    followRepo := repository.NewFollowRepository(db)
    fanRepo := repository.NewFanRepository(db)
    publisher := service.NewPublisher(db)

    // params
    N := 20000                // number of fans for the author
    POSTS := 100              // posts to publish
    WORKERS := 8              // fanout workers
    BATCH := 1000             // inbox upsert batch
    CLAIM := 64               // claim per tick
    if s := os.Getenv("N"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { N = v } }
    if s := os.Getenv("POSTS"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { POSTS = v } }
    if s := os.Getenv("WORKERS"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { WORKERS = v } }
    if s := os.Getenv("BATCH"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { BATCH = v } }
    if s := os.Getenv("CLAIM"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { CLAIM = v } }

    // clean tables for a reproducible run (ok for local bench)
    _ = db.Exec("TRUNCATE TABLE inbox, outbox, posts, fans, follows, users RESTART IDENTITY CASCADE").Error

    // fix composite unique index for inbox (user_id, post_id)
    _ = db.Exec("DROP INDEX IF EXISTS ux_inbox_user_post").Error
    _ = db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS ux_inbox_user_post ON inbox (user_id, post_id)").Error

    // seed one author and N fans
    author := model.User{ID: "author0", Username: "author0", Email: "author0@example.com", Password: "p"}
    _ = db.Where("id = ?", author.ID).FirstOrCreate(&author).Error
    users := make([]model.User, N)
    for i := 0; i < N; i++ {
        id := uuid.New().String()
        users[i] = model.User{ID: id, Username: "u"+id[:8], Email: id[:8]+"@example.com", Password: "p"}
    }
    _ = db.CreateInBatches(&users, 1000).Error
    // follow + fan tables
    for i := 0; i < N; i++ { _ = followRepo.Create(context.Background(), users[i].ID, author.ID) }
    for i := 0; i < N; i++ { _ = fanRepo.Create(context.Background(), author.ID, users[i].ID) }

    // start fanout workers
    worker := service.NewFanoutWorker(db, fanRepo, WORKERS, BATCH, CLAIM, 20*time.Millisecond)
    stop := worker.Start()
    defer stop(context.Background())

    // publish POSTS
    pubDurations := make([]time.Duration, 0, POSTS)
    for i := 0; i < POSTS; i++ {
        st := time.Now()
        _, err := publisher.Publish(context.Background(), author.ID, fmt.Sprintf("hello %d", i))
        if err != nil { panic(err) }
        pubDurations = append(pubDurations, time.Since(st))
    }

    // collect landing metrics
    land := make([]time.Duration, 0, POSTS)
    timeout := time.After(2 * time.Minute)
    for len(land) < POSTS {
        select {
        case d := <-worker.Metrics():
            land = append(land, d)
        case <-timeout:
            fmt.Printf("timeout while waiting for fanout metrics: got=%d want=%d\n", len(land), POSTS)
            goto PRINT
        }
    }

PRINT:
    // output
    var pubSum time.Duration
    for _, d := range pubDurations { pubSum += d }
    fmt.Printf("N=%d POSTS=%d WORKERS=%d BATCH=%d CLAIM=%d\n", N, POSTS, WORKERS, BATCH, CLAIM)
    fmt.Printf("Publish tx latency: avg=%v p95=%v p99=%v\n", pubSum/time.Duration(len(pubDurations)), pct(pubDurations, 0.95), pct(pubDurations, 0.99))
    var landSum time.Duration
    for _, d := range land { landSum += d }
    fmt.Printf("Fanout landing (outbox->done): samples=%d avg=%v p95=%v p99=%v\n", len(land), landSum/time.Duration(len(land)), pct(land, 0.95), pct(land, 0.99))

    // measure one user's timeline read (seek first page)
    if len(users) > 0 {
        st := time.Now()
        var rows []model.Inbox
        _ = db.Where("user_id = ?", users[0].ID).Order("score DESC, id DESC").Limit(50).Find(&rows).Error
        fmt.Printf("Timeline read (user0, limit=50): %v, rows=%d\n", time.Since(st), len(rows))
    }
}
