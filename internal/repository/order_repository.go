package repository

import (
	"context"
	"github.com/d60-Lab/gin-template/internal/model"
)

// OrderRepository 订单仓储接口
type OrderRepository interface {
	// Create 创建订单
	Create(ctx context.Context, order *model.Order) error
	
	// GetByOrderID 根据订单ID查询订单
	GetByOrderID(ctx context.Context, orderID int64) (*model.Order, error)
	
	// GetByUserID 根据用户ID查询订单列表
	GetByUserID(ctx context.Context, userID int64, limit int) ([]*model.Order, error)
	
	// UpdateStatus 更新订单状态
	UpdateStatus(ctx context.Context, orderID int64, status int8) error
	
	// Count 统计订单数量
	Count(ctx context.Context) (int64, error)
	
	// Close 关闭数据库连接
	Close() error
}
