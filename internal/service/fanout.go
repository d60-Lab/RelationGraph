package service

import (
    "context"
    "time"

    "github.com/google/uuid"
    "gorm.io/gorm"
    "gorm.io/gorm/clause"

    "github.com/d60-Lab/gin-template/internal/model"
    "github.com/d60-Lab/gin-template/internal/repository"
)

// FanoutWorker 从 outbox 拉取事件并写入 inbox（仅本地基准模拟）
type FanoutWorker struct {
    db           *gorm.DB
    fanRepo      repository.FanRepository
    batchSize    int
    claimLimit   int
    pollInterval time.Duration
    workers      int
    metricsCh    chan time.Duration // outbox->processed latency
}

func NewFanoutWorker(db *gorm.DB, fanRepo repository.FanRepository, workers, batchSize, claimLimit int, pollInterval time.Duration) *FanoutWorker {
    if workers <= 0 { workers = 4 }
    if batchSize <= 0 { batchSize = 500 }
    if claimLimit <= 0 { claimLimit = 128 }
    if pollInterval <= 0 { pollInterval = 50 * time.Millisecond }
    return &FanoutWorker{db: db, fanRepo: fanRepo, workers: workers, batchSize: batchSize, claimLimit: claimLimit, pollInterval: pollInterval, metricsCh: make(chan time.Duration, 65536)}
}

func (w *FanoutWorker) Metrics() <-chan time.Duration { return w.metricsCh }

// Start 启动若干 worker 轮询处理 outbox；返回停止函数。
func (w *FanoutWorker) Start() func(context.Context) error {
    stop := make(chan struct{})
    for i := 0; i < w.workers; i++ {
        go w.loop(stop)
    }
    return func(ctx context.Context) error { close(stop); return nil }
}

func (w *FanoutWorker) loop(stop <-chan struct{}) {
    ticker := time.NewTicker(w.pollInterval)
    defer ticker.Stop()
    for {
        select {
        case <-stop:
            return
        case <-ticker.C:
            _ = w.processOnce(context.Background())
        }
    }
}

// processOnce: claim一批 pending outbox 并扇出
func (w *FanoutWorker) processOnce(ctx context.Context) error {
    // claim batch using SELECT ... FOR UPDATE SKIP LOCKED
    type ob struct{ ID string; PostID string; AuthorID string; CreatedAt time.Time }
    var batch []ob
    err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := tx.Raw(`
            SELECT id, post_id, author_id, created_at
            FROM outbox
            WHERE status = 'pending'
            ORDER BY created_at
            LIMIT ?
            FOR UPDATE SKIP LOCKED
        `, w.claimLimit).Scan(&batch).Error; err != nil { return err }
        if len(batch) == 0 { return nil }
        ids := make([]string, len(batch))
        for i, b := range batch { ids[i] = b.ID }
        return tx.Model(&model.Outbox{}).Where("id IN ?", ids).Update("status", "processing").Error
    })
    if err != nil { return err }
    if len(batch) == 0 { return nil }

    // process each outbox
    for _, b := range batch {
        // fetch fans in pages
        offset := 0
        page := w.batchSize
        totalWritten := int64(0)
        for {
            fans, err := w.fanRepo.ListFans(ctx, b.AuthorID, offset, page)
            if err != nil { break }
            if len(fans) == 0 { break }
            records := make([]model.Inbox, 0, len(fans))
            now := time.Now()
            score := now.UnixNano()
            for _, f := range fans {
                records = append(records, model.Inbox{ID: uuid.New().String(), UserID: f.FanID, PostID: b.PostID, Score: score, CreatedAt: now})
            }
            // upsert ignore duplicates
            _ = w.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&records).Error
            totalWritten += int64(len(records))
            if len(fans) < page { break }
            offset += page
        }
        now := time.Now()
        _ = w.db.WithContext(ctx).Model(&model.Outbox{}).
            Where("id = ?", b.ID).
            Updates(map[string]any{"status": "done", "processed_at": now, "fanout_count": totalWritten}).Error
        // record latency
        if !b.CreatedAt.IsZero() {
            select { case w.metricsCh <- time.Since(b.CreatedAt): default: }
        }
    }
    return nil
}
