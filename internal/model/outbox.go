package model

import "time"

// Outbox 事件外发盒（用于本地 fanout 基准模拟）
type Outbox struct {
    ID         string    `gorm:"primaryKey;type:varchar(36)"`
    PostID     string    `gorm:"type:varchar(36);uniqueIndex"`
    AuthorID   string    `gorm:"type:varchar(36);index:idx_outbox_author"`
    CreatedAt  time.Time `gorm:"index"`
    Status     string    `gorm:"type:varchar(16);index"` // pending, processing, done
    ProcessedAt *time.Time
    FanoutCount int64
}

func (Outbox) TableName() string { return "outbox" }
