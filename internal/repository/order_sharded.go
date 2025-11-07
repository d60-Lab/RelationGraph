package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"gorm.io/gorm"

	"github.com/d60-Lab/gin-template/internal/model"
)

const (
	// ShardCount 分片数量 (8个数据库 x 8张表 = 64个分片)
	ShardCount = 8
	TableCount = 8
)

// ShardedOrderRepository 分库分表订单仓储实现
type ShardedOrderRepository struct {
	// shards[dbIndex][tableIndex] = *gorm.DB
	shards [][]*gorm.DB
}

// NewShardedOrderRepository 创建分库分表订单仓储
func NewShardedOrderRepository(dbs []*gorm.DB) (OrderRepository, error) {
	if len(dbs) != ShardCount {
		return nil, fmt.Errorf("expected %d databases, got %d", ShardCount, len(dbs))
	}

	shards := make([][]*gorm.DB, ShardCount)
	for i := 0; i < ShardCount; i++ {
		shards[i] = make([]*gorm.DB, TableCount)
		for j := 0; j < TableCount; j++ {
			shards[i][j] = dbs[i]
		}
	}

	return &ShardedOrderRepository{shards: shards}, nil
}

// RouteByOrderID 根据订单ID路由到对应的分片
// 规则: 高位确定库，低位确定表
func RouteByOrderID(orderID int64) (dbIndex, tableIndex int) {
	// 使用高位和低位分别确定库和表
	dbIndex = int((orderID >> 8) % ShardCount)
	tableIndex = int(orderID % TableCount)
	return
}

// RouteByUserID 根据用户ID路由到对应的数据库
func RouteByUserID(userID int64) int {
	return int(userID % ShardCount)
}

// getTableName 获取分表名称
func getTableName(tableIndex int) string {
	return fmt.Sprintf("orders_%d", tableIndex)
}

// Create 创建订单
func (r *ShardedOrderRepository) Create(ctx context.Context, order *model.Order) error {
	dbIdx, tblIdx := RouteByOrderID(order.OrderID)
	tableName := getTableName(tblIdx)
	
	return r.shards[dbIdx][tblIdx].WithContext(ctx).
		Table(tableName).
		Create(order).Error
}

// GetByOrderID 根据订单ID查询订单 (精确路由)
func (r *ShardedOrderRepository) GetByOrderID(ctx context.Context, orderID int64) (*model.Order, error) {
	dbIdx, tblIdx := RouteByOrderID(orderID)
	tableName := getTableName(tblIdx)
	
	var order model.Order
	err := r.shards[dbIdx][tblIdx].WithContext(ctx).
		Table(tableName).
		Where("order_id = ?", orderID).
		First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetByUserID 根据用户ID查询订单列表 (需要查询该用户所在库的所有表)
func (r *ShardedOrderRepository) GetByUserID(ctx context.Context, userID int64, limit int) ([]*model.Order, error) {
	dbIdx := RouteByUserID(userID)
	
	// 并发查询该库的所有表
	var wg sync.WaitGroup
	resultChan := make(chan []*model.Order, TableCount)
	errChan := make(chan error, TableCount)
	
	for tblIdx := 0; tblIdx < TableCount; tblIdx++ {
		wg.Add(1)
		go func(tableIndex int) {
			defer wg.Done()
			
			tableName := getTableName(tableIndex)
			var orders []*model.Order
			err := r.shards[dbIdx][tableIndex].WithContext(ctx).
				Table(tableName).
				Where("user_id = ?", userID).
				Order("created_at DESC").
				Limit(limit * 2). // 多取一些，应用层再排序
				Find(&orders).Error
			
			if err != nil {
				errChan <- err
				return
			}
			resultChan <- orders
		}(tblIdx)
	}
	
	// 等待所有查询完成
	wg.Wait()
	close(resultChan)
	close(errChan)
	
	// 检查错误
	if len(errChan) > 0 {
		return nil, <-errChan
	}
	
	// 合并结果
	var allOrders []*model.Order
	for orders := range resultChan {
		allOrders = append(allOrders, orders...)
	}
	
	// 按创建时间排序
	sort.Slice(allOrders, func(i, j int) bool {
		return allOrders[i].CreatedAt.After(allOrders[j].CreatedAt)
	})
	
	// 返回前 limit 条
	if len(allOrders) > limit {
		allOrders = allOrders[:limit]
	}
	
	return allOrders, nil
}

// UpdateStatus 更新订单状态
func (r *ShardedOrderRepository) UpdateStatus(ctx context.Context, orderID int64, status int8) error {
	dbIdx, tblIdx := RouteByOrderID(orderID)
	tableName := getTableName(tblIdx)
	
	return r.shards[dbIdx][tblIdx].WithContext(ctx).
		Table(tableName).
		Where("order_id = ?", orderID).
		Update("status", status).Error
}

// Count 统计订单数量 (需要查询所有分片)
func (r *ShardedOrderRepository) Count(ctx context.Context) (int64, error) {
	var totalCount int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, ShardCount*TableCount)
	
	for dbIdx := 0; dbIdx < ShardCount; dbIdx++ {
		for tblIdx := 0; tblIdx < TableCount; tblIdx++ {
			wg.Add(1)
			go func(di, ti int) {
				defer wg.Done()
				
				tableName := getTableName(ti)
				var count int64
				err := r.shards[di][ti].WithContext(ctx).
					Table(tableName).
					Count(&count).Error
				
				if err != nil {
					errChan <- err
					return
				}
				
				mu.Lock()
				totalCount += count
				mu.Unlock()
			}(dbIdx, tblIdx)
		}
	}
	
	wg.Wait()
	close(errChan)
	
	if len(errChan) > 0 {
		return 0, <-errChan
	}
	
	return totalCount, nil
}

// Close 关闭所有数据库连接
func (r *ShardedOrderRepository) Close() error {
	// 使用 map 去重，因为同一个数据库被引用了多次
	dbMap := make(map[*gorm.DB]bool)
	for i := 0; i < ShardCount; i++ {
		dbMap[r.shards[i][0]] = true
	}
	
	for db := range dbMap {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		if err := sqlDB.Close(); err != nil {
			return err
		}
	}
	
	return nil
}

// InitSchema 初始化所有分片的表结构
func (r *ShardedOrderRepository) InitSchema() error {
	for dbIdx := 0; dbIdx < ShardCount; dbIdx++ {
		db := r.shards[dbIdx][0]
		
		// 为每个数据库创建 8 张分表
		for tblIdx := 0; tblIdx < TableCount; tblIdx++ {
			tableName := getTableName(tblIdx)
			
			// 创建表
			if err := db.Table(tableName).AutoMigrate(&model.Order{}); err != nil {
				return fmt.Errorf("failed to migrate table %s in db %d: %w", tableName, dbIdx, err)
			}
		}
	}
	
	return nil
}
