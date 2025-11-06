package service

import (
    "context"
    "time"

    "go.uber.org/zap"

    "github.com/d60-Lab/gin-template/internal/repository"
    "github.com/d60-Lab/gin-template/pkg/logger"
)

type replicateAction int

const (
    actionAdd replicateAction = iota + 1
    actionRemove
)

type replicateJob struct {
    action replicateAction
    userID string
    fanID  string
    enqAt  time.Time
}

// FanReplicator 简单的本地异步冗余执行器（服务异步冗余）
type FanReplicator struct {
    fanRepo repository.FanRepository
    ch      chan replicateJob
    metricsCh chan time.Duration
}

func NewFanReplicator(fanRepo repository.FanRepository, queueSize int) *FanReplicator {
    if queueSize <= 0 {
        queueSize = 10000
    }
    return &FanReplicator{fanRepo: fanRepo, ch: make(chan replicateJob, queueSize), metricsCh: make(chan time.Duration, 65536)}
}

func (r *FanReplicator) Start(workers int) func(context.Context) error {
    if workers <= 0 {
        workers = 4
    }
    stopCh := make(chan struct{})
    for i := 0; i < workers; i++ {
        go func() {
            for {
                select {
                case job := <-r.ch:
                    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
                    switch job.action {
                    case actionAdd:
                        _ = r.fanRepo.Create(ctx, job.userID, job.fanID)
                    case actionRemove:
                        _ = r.fanRepo.Delete(ctx, job.userID, job.fanID)
                    }
                    cancel()
                    if !job.enqAt.IsZero() {
                        select {
                        case r.metricsCh <- time.Since(job.enqAt):
                        default:
                        }
                    }
                case <-stopCh:
                    return
                }
            }
        }()
    }
    return func(ctx context.Context) error {
        close(stopCh)
        // 等待队列自然排空一小段时间
        timeout := time.After(2 * time.Second)
        for {
            select {
            case <-timeout:
                return nil
            default:
                if len(r.ch) == 0 {
                    return nil
                }
                time.Sleep(50 * time.Millisecond)
            }
        }
    }
}

func (r *FanReplicator) EnqueueAdd(userID, fanID string) {
    select {
    case r.ch <- replicateJob{action: actionAdd, userID: userID, fanID: fanID, enqAt: time.Now()}:
    default:
        logger.Warn("replicator queue full, drop add", zap.String("user", userID), zap.String("fan", fanID))
    }
}

func (r *FanReplicator) EnqueueRemove(userID, fanID string) {
    select {
    case r.ch <- replicateJob{action: actionRemove, userID: userID, fanID: fanID, enqAt: time.Now()}:
    default:
        logger.Warn("replicator queue full, drop remove", zap.String("user", userID), zap.String("fan", fanID))
    }
}

// Metrics 返回复制落地耗时的只读通道（每处理一条发送一次 duration）。
func (r *FanReplicator) Metrics() <-chan time.Duration { return r.metricsCh }

// QueueLen 返回当前队列长度（采样值）。
func (r *FanReplicator) QueueLen() int { return len(r.ch) }
