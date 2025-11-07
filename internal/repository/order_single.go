package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/d60-Lab/gin-template/internal/model"
)

// SingleDBOrderRepository 单库订单仓储实现
type SingleDBOrderRepository struct {
	db *gorm.DB
}

// NewSingleDBOrderRepository 创建单库订单仓储
func NewSingleDBOrderRepository(db *gorm.DB) OrderRepository {
	return &SingleDBOrderRepository{db: db}
}

// Create 创建订单
func (r *SingleDBOrderRepository) Create(ctx context.Context, order *model.Order) error {
	return r.db.WithContext(ctx).Create(order).Error
}

// GetByOrderID 根据订单ID查询订单
func (r *SingleDBOrderRepository) GetByOrderID(ctx context.Context, orderID int64) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).Where("order_id = ?", orderID).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetByUserID 根据用户ID查询订单列表
func (r *SingleDBOrderRepository) GetByUserID(ctx context.Context, userID int64, limit int) ([]*model.Order, error) {
	var orders []*model.Order
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}

// UpdateStatus 更新订单状态
func (r *SingleDBOrderRepository) UpdateStatus(ctx context.Context, orderID int64, status int8) error {
	return r.db.WithContext(ctx).
		Model(&model.Order{}).
		Where("order_id = ?", orderID).
		Update("status", status).Error
}

// Count 统计订单数量
func (r *SingleDBOrderRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Order{}).Count(&count).Error
	return count, err
}

// Close 关闭数据库连接
func (r *SingleDBOrderRepository) Close() error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// InitSchema 初始化数据库表结构
func (r *SingleDBOrderRepository) InitSchema() error {
	// 创建订单表
	if err := r.db.AutoMigrate(&model.Order{}); err != nil {
		return fmt.Errorf("failed to migrate orders table: %w", err)
	}
	
	return nil
}
