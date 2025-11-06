package model

import (
    "time"
)

// Follow 关注关系（A 关注 B）
type Follow struct {
    ID         string    `gorm:"primaryKey;type:varchar(36)"`
    FollowerID string    `gorm:"type:varchar(36);index:idx_follow_follower;index:idx_follow_pair,unique;not null"`
    FolloweeID string    `gorm:"type:varchar(36);not null;index:idx_follow_pair,unique"`
    // 复合唯一键，避免重复关注
    // idx_follow_pair = (follower_id, followee_id)
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

func (Follow) TableName() string { return "follows" }
