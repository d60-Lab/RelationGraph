package model

import "time"

// Inbox 时间线项（按 user_id 切分）
type Inbox struct {
    ID        string    `gorm:"primaryKey;type:varchar(36)"`
    UserID    string    `gorm:"type:varchar(36);index:idx_inbox_user;uniqueIndex:ux_inbox_user_post"`
    PostID    string    `gorm:"type:varchar(36);index:idx_inbox_post;uniqueIndex:ux_inbox_user_post"`
    // 复合唯一键，避免重复 (user, post)
    // ux_inbox_user_post = (user_id, post_id)
    Score     int64     `gorm:"index:idx_inbox_user_score"`
    CreatedAt time.Time `gorm:"index:idx_inbox_user_score"`
}

func (Inbox) TableName() string { return "inbox" }
