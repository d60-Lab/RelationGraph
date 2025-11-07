package model

import (
	"time"
)

// Order 订单模型
type Order struct {
	OrderID   int64     `json:"order_id" gorm:"primaryKey;autoIncrement:false"`
	UserID    int64     `json:"user_id" gorm:"index:idx_user_created;not null"`
	Amount    float64   `json:"amount" gorm:"type:decimal(10,2);not null"`
	Status    int8      `json:"status" gorm:"index;not null;default:0"` // 0:pending, 1:paid, 2:shipped, 3:completed, 4:cancelled
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_user_created;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `json:"updated_at" gorm:"not null;default:CURRENT_TIMESTAMP"`
}

// TableName 指定表名 (用于单库)
func (Order) TableName() string {
	return "orders"
}

// OrderStatus 订单状态常量
const (
	OrderStatusPending   = 0
	OrderStatusPaid      = 1
	OrderStatusShipped   = 2
	OrderStatusCompleted = 3
	OrderStatusCancelled = 4
)
