package repository

import (
    "context"

    "github.com/google/uuid"
    "gorm.io/gorm"
    "gorm.io/gorm/clause"

    "github.com/d60-Lab/gin-template/internal/model"
)

type FanRepository interface {
    Create(ctx context.Context, userID, fanID string) error
    Delete(ctx context.Context, userID, fanID string) error
    ListFans(ctx context.Context, userID string, offset, limit int) ([]*model.Fan, error)
}

type fanRepository struct{ db *gorm.DB }

func NewFanRepository(db *gorm.DB) FanRepository { return &fanRepository{db: db} }

func (r *fanRepository) Create(ctx context.Context, userID, fanID string) error {
    f := &model.Fan{ID: uuid.New().String(), UserID: userID, FanID: fanID}
    return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(f).Error
}

func (r *fanRepository) Delete(ctx context.Context, userID, fanID string) error {
    return r.db.WithContext(ctx).Where("user_id = ? AND fan_id = ?", userID, fanID).Delete(&model.Fan{}).Error
}

func (r *fanRepository) ListFans(ctx context.Context, userID string, offset, limit int) ([]*model.Fan, error) {
    var res []*model.Fan
    err := r.db.WithContext(ctx).Where("user_id = ?", userID).Offset(offset).Limit(limit).Find(&res).Error
    return res, err
}
