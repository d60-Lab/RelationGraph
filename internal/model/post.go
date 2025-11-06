package model

import "time"

// Post 内容主体（仅示例所需字段）
type Post struct {
    ID        string    `gorm:"primaryKey;type:varchar(36)"`
    AuthorID  string    `gorm:"type:varchar(36);index:idx_post_author"`
    Payload   string    `gorm:"type:text"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

func (Post) TableName() string { return "posts" }
