package repository

import (
    "context"

    "github.com/google/uuid"
    "gorm.io/gorm"
    "gorm.io/gorm/clause"

    "github.com/d60-Lab/gin-template/internal/model"
)

type FollowRepository interface {
    Create(ctx context.Context, followerID, followeeID string) error
    Delete(ctx context.Context, followerID, followeeID string) error
    Exists(ctx context.Context, followerID, followeeID string) (bool, error)
    ListFollowings(ctx context.Context, followerID string, offset, limit int) ([]*model.Follow, error)
}

type followRepository struct {
    db *gorm.DB
}

func NewFollowRepository(db *gorm.DB) FollowRepository { return &followRepository{db: db} }

func (r *followRepository) Create(ctx context.Context, followerID, followeeID string) error {
    f := &model.Follow{ID: uuid.New().String(), FollowerID: followerID, FolloweeID: followeeID}
    // 幂等：重复关注不报错
    return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(f).Error
}

func (r *followRepository) Delete(ctx context.Context, followerID, followeeID string) error {
    return r.db.WithContext(ctx).
        Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
        Delete(&model.Follow{}).Error
}

func (r *followRepository) Exists(ctx context.Context, followerID, followeeID string) (bool, error) {
    var cnt int64
    if err := r.db.WithContext(ctx).
        Model(&model.Follow{}).
        Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
        Count(&cnt).Error; err != nil {
        return false, err
    }
    return cnt > 0, nil
}

func (r *followRepository) ListFollowings(ctx context.Context, followerID string, offset, limit int) ([]*model.Follow, error) {
    var res []*model.Follow
    err := r.db.WithContext(ctx).Where("follower_id = ?", followerID).Offset(offset).Limit(limit).Find(&res).Error
    return res, err
}
