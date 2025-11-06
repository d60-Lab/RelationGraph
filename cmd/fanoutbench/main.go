package main

import (
    "context"
    "fmt"
    "os"
    "sort"
    "strconv"
    "sync"
    "time"

    "github.com/d60-Lab/gin-template/config"
    "github.com/d60-Lab/gin-template/pkg/database"
)

func main() {
    cfg, err := config.Load()
    if err != nil { panic(err) }
    db, err := database.InitDB(cfg)
    if err != nil { panic(err) }

    // prepare a small table to simulate shards
    type ShardItem struct {
        ID    int    `gorm:"primaryKey;autoIncrement"`
        Shard int    `gorm:"index"`
        Val   string `gorm:"type:varchar(32)"`
    }
    db.AutoMigrate(&ShardItem{})
    var cnt int64
    db.Model(&ShardItem{}).Count(&cnt)
    SHARDS := 64
    if s := os.Getenv("SHARDS"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { SHARDS = v } }
    if cnt < int64(SHARDS) {
        items := make([]ShardItem, SHARDS)
        for i := 0; i < SHARDS; i++ { items[i] = ShardItem{Shard: i, Val: fmt.Sprintf("v%02d", i)} }
        db.Create(&items)
    }

    REPEAT := 50
    if s := os.Getenv("REPEAT"); s != "" { if v, e := strconv.Atoi(s); e == nil && v > 0 { REPEAT = v } }

    single := func(ctx context.Context, shard int) time.Duration {
        st := time.Now()
        var it ShardItem
        _ = db.WithContext(ctx).Where("shard = ?", shard).Limit(1).First(&it).Error
        return time.Since(st)
    }

    fanout := func(ctx context.Context, shards int) time.Duration {
        st := time.Now()
        var wg sync.WaitGroup
        wg.Add(shards)
        for i := 0; i < shards; i++ {
            go func(s int) { defer wg.Done(); var it ShardItem; _ = db.WithContext(ctx).Where("shard = ?", s).Limit(1).First(&it).Error }(i)
        }
        wg.Wait()
        return time.Since(st)
    }

    // run
    ctx := context.Background()
    singles := make([]time.Duration, 0, REPEAT)
    fanouts := make([]time.Duration, 0, REPEAT)
    for i := 0; i < REPEAT; i++ { singles = append(singles, single(ctx, 0)) }
    for i := 0; i < REPEAT; i++ { fanouts = append(fanouts, fanout(ctx, SHARDS)) }

    pct := func(vs []time.Duration, p float64) time.Duration {
        xs := append([]time.Duration(nil), vs...)
        sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
        k := int(float64(len(xs))*p)
        if k < 0 { k = 0 }
        if k >= len(xs) { k = len(xs)-1 }
        return xs[k]
    }

    var sum1, sum2 time.Duration
    for _, d := range singles { sum1 += d }
    for _, d := range fanouts { sum2 += d }
    fmt.Printf("SHARDS=%d REPEAT=%d\n", SHARDS, REPEAT)
    fmt.Printf("Single-shard query: avg=%v p95=%v p99=%v\n", sum1/time.Duration(len(singles)), pct(singles, 0.95), pct(singles, 0.99))
    fmt.Printf("Fan-out %d-shard queries: avg=%v p95=%v p99=%v\n", SHARDS, sum2/time.Duration(len(fanouts)), pct(fanouts, 0.95), pct(fanouts, 0.99))
}
