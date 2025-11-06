package service

import (
    "context"
    "time"

    "github.com/google/uuid"
    "gorm.io/gorm"

    "github.com/d60-Lab/gin-template/internal/model"
)

// Publisher 负责事务内写 posts + outbox
type Publisher struct { db *gorm.DB }

func NewPublisher(db *gorm.DB) *Publisher { return &Publisher{db: db} }

// Publish 在一个事务内落地 Post 与 Outbox 事件
func (p *Publisher) Publish(ctx context.Context, authorID, payload string) (string, error) {
    postID := uuid.New().String()
    now := time.Now()
    err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        post := &model.Post{ID: postID, AuthorID: authorID, Payload: payload, CreatedAt: now, UpdatedAt: now}
        if err := tx.Create(post).Error; err != nil { return err }
        out := &model.Outbox{ID: uuid.New().String(), PostID: postID, AuthorID: authorID, CreatedAt: now, Status: "pending"}
        if err := tx.Create(out).Error; err != nil { return err }
        return nil
    })
    if err != nil { return "", err }
    return postID, nil
}
