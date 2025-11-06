package service

import (
    "context"
    "errors"

    "github.com/d60-Lab/gin-template/internal/repository"
)

var (
    ErrFollowSelf = errors.New("cannot follow self")
)

// RelationshipService 关系链服务
type RelationshipService interface {
    Follow(ctx context.Context, fromUserID, toUserID string) error
    Unfollow(ctx context.Context, fromUserID, toUserID string) error
    ListFollowing(ctx context.Context, userID string, page, pageSize int) ([]string, error)
    ListFans(ctx context.Context, userID string, page, pageSize int) ([]string, error)
}

type relationshipService struct {
    followRepo  repository.FollowRepository
    fanRepo     repository.FanRepository
    replicator  *FanReplicator
}

func NewRelationshipService(followRepo repository.FollowRepository, fanRepo repository.FanRepository, replicator *FanReplicator) RelationshipService {
    return &relationshipService{followRepo: followRepo, fanRepo: fanRepo, replicator: replicator}
}

func (s *relationshipService) Follow(ctx context.Context, fromUserID, toUserID string) error {
    if fromUserID == toUserID {
        return ErrFollowSelf
    }
    if err := s.followRepo.Create(ctx, fromUserID, toUserID); err != nil {
        return err
    }
    if s.replicator != nil {
        s.replicator.EnqueueAdd(toUserID, fromUserID)
    }
    return nil
}

func (s *relationshipService) Unfollow(ctx context.Context, fromUserID, toUserID string) error {
    if err := s.followRepo.Delete(ctx, fromUserID, toUserID); err != nil {
        return err
    }
    if s.replicator != nil {
        s.replicator.EnqueueRemove(toUserID, fromUserID)
    }
    return nil
}

func (s *relationshipService) ListFollowing(ctx context.Context, userID string, page, pageSize int) ([]string, error) {
    if page < 1 { page = 1 }
    if pageSize < 1 { pageSize = 10 }
    offset := (page - 1) * pageSize
    items, err := s.followRepo.ListFollowings(ctx, userID, offset, pageSize)
    if err != nil { return nil, err }
    res := make([]string, len(items))
    for i, it := range items { res[i] = it.FolloweeID }
    return res, nil
}

func (s *relationshipService) ListFans(ctx context.Context, userID string, page, pageSize int) ([]string, error) {
    if page < 1 { page = 1 }
    if pageSize < 1 { pageSize = 10 }
    offset := (page - 1) * pageSize
    items, err := s.fanRepo.ListFans(ctx, userID, offset, pageSize)
    if err != nil { return nil, err }
    res := make([]string, len(items))
    for i, it := range items { res[i] = it.FanID }
    return res, nil
}
