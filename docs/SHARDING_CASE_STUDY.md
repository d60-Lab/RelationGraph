# 分库分表案例研究

## 概述

本项目实现了一个完整的分库分表性能对比压测案例，用于验证分库分表方案在不同场景下的性能表现。

## 实现内容

### 1. 数据库架构

#### 单库方案
- **数据库**: 1个 PostgreSQL 18 实例
- **端口**: 5434
- **表**: 1张 `orders` 表
- **容量**: 支持百万级订单

#### 分库分表方案
- **数据库**: 8个 PostgreSQL 18 实例
- **端口**: 5440-5447
- **分片策略**: 8库 x 8表 = 64个物理分片
- **路由算法**:
  ```go
  // 按订单ID分片 (精确路由)
  db_index = (order_id >> 8) % 8
  table_index = order_id % 8
  
  // 按用户ID路由 (需查询同库的所有表)
  db_index = user_id % 8
  ```

### 2. 代码实现

#### 模型层
- `internal/model/order.go`: 订单数据模型

#### 仓储层
- `internal/repository/order_repository.go`: 订单仓储接口
- `internal/repository/order_single.go`: 单库实现
- `internal/repository/order_sharded.go`: 分库分表实现

核心接口:
```go
type OrderRepository interface {
    Create(ctx context.Context, order *Order) error
    GetByOrderID(ctx context.Context, orderID int64) (*Order, error)
    GetByUserID(ctx context.Context, userID int64, limit int) ([]*Order, error)
    UpdateStatus(ctx context.Context, orderID int64, status int8) error
    Count(ctx context.Context) (int64, error)
    Close() error
}
```

#### 压测程序
- `cmd/shardbench/main.go`: 完整的压测实现

压测场景:
1. **插入性能测试**: 100万订单写入
2. **点查性能测试**: 按订单ID查询 (30秒)
3. **范围查询测试**: 按用户ID查询订单列表 (30秒)

### 3. 基础设施

#### Docker Compose
- `docker-compose.yml`: 包含9个数据库实例的配置
- 每个数据库配置了合理的连接数和缓冲区大小

#### 自动化脚本
- `scripts/run-sharding-bench.sh`: 一键运行压测
- `Makefile`: 提供便捷的命令
  - `make bench-sharding-setup`: 启动数据库
  - `make bench-sharding`: 运行完整压测
  - `make bench-sharding-clean`: 清理环境

### 4. 文档

- `blogs/how-to-use-cache.md`: 分库分表最佳实践文章
- `docs/sharding-benchmark.md`: 压测详细说明
- `docs/SHARDING_CASE_STUDY.md`: 本文档

## 性能特点

### 单库方案
**优势:**
- 架构简单，易于维护
- 事务支持完整
- 无跨库查询问题
- 适合中小规模业务

**劣势:**
- 单点性能瓶颈
- 连接数限制
- 数据量受限

### 分库分表方案
**优势:**
- 线性扩展能力
- 高并发支持
- 数据分散，减少锁竞争
- IO并行化

**劣势:**
- 架构复杂度高
- 跨片查询需要应用层聚合
- 分布式事务问题
- 运维成本增加

## 技术亮点

### 1. 智能路由
```go
// 精确路由 - 直接定位到分片
func RouteByOrderID(orderID int64) (dbIndex, tableIndex int) {
    dbIndex = int((orderID >> 8) % ShardCount)
    tableIndex = int(orderID % TableCount)
    return
}

// 范围路由 - 查询同库的所有表
func RouteByUserID(userID int64) int {
    return int(userID % ShardCount)
}
```

### 2. 并发查询优化
按用户ID查询时，使用 goroutine 并发查询同一数据库的8张表:
```go
for tblIdx := 0; tblIdx < TableCount; tblIdx++ {
    wg.Add(1)
    go func(tableIndex int) {
        defer wg.Done()
        // 查询单表
        // ...
    }(tblIdx)
}
wg.Wait()
```

### 3. 性能指标统计
- QPS (每秒请求数)
- 延迟分布 (P50/P95/P99)
- 成功率统计
- 实时并发控制

## 使用场景

### 适合分库分表的场景
1. **数据量大** (> 2000万行)
2. **高并发写入** (> 5000 TPS)
3. **明确的分片键** (如 user_id, order_id)
4. **读多写多**，单库无法满足

### 不适合分库分表的场景
1. **小数据量** (< 500万行)
2. **复杂关联查询**
3. **强事务要求**
4. **团队技术储备不足**

## 扩展方向

### 1. 添加读写分离
为每个分片添加主从复制，读请求路由到从库:
```
db_0_master (5440)
├── db_0_slave_1 (5450)
└── db_0_slave_2 (5451)
```

### 2. 一致性哈希
使用一致性哈希算法减少扩容时的数据迁移量:
```go
type ConsistentHash struct {
    circle map[uint32]string
    sortedHashes []uint32
}
```

### 3. 数据归档
实现冷热数据分离，历史订单归档到低成本存储:
```sql
-- 归档表
CREATE TABLE orders_archive_202401 AS 
SELECT * FROM orders WHERE created_at < '2024-02-01';
```

### 4. 分布式事务
集成分布式事务框架 (如 Seata):
```go
// TCC 模式
type OrderTCC interface {
    Try(ctx context.Context, order *Order) error
    Confirm(ctx context.Context, orderID int64) error
    Cancel(ctx context.Context, orderID int64) error
}
```

### 5. 监控和告警
- 分片数据倾斜监控
- 慢查询统计
- QPS 趋势分析
- 自动容量规划

## 最佳实践总结

### 1. 设计阶段
- ✅ 选择合适的分片键 (覆盖 90% 查询)
- ✅ 预留扩展空间 (逻辑分片 > 物理分片)
- ✅ 避免跨片事务
- ✅ 设计数据归档策略

### 2. 实现阶段
- ✅ 封装路由逻辑
- ✅ 统一异常处理
- ✅ 添加重试机制
- ✅ 记录详细日志

### 3. 测试阶段
- ✅ 压力测试
- ✅ 数据倾斜测试
- ✅ 故障演练
- ✅ 扩容演练

### 4. 运维阶段
- ✅ 完善的监控
- ✅ 自动化运维
- ✅ 容量规划
- ✅ 应急预案

## 总结

本案例通过实际代码和压测验证了分库分表方案的性能优势，同时也揭示了其复杂性。在实际项目中，应该根据业务规模、团队能力和发展阶段选择合适的方案：

- **初创期** (< 100万数据): 单库 + 索引优化
- **成长期** (100-2000万): 单库 + 读写分离
- **成熟期** (> 2000万): 分库分表
- **超大规模** (> 10亿): 分布式数据库 (TiDB/CockroachDB)

**记住: 不要过早优化，但要提前规划。**

## 相关资源

- 📖 [分库分表最佳实践文章](../blogs/shareking-db.md)
- 📖 [压测详细说明](./sharding-benchmark.md)
- 🚀 [快速开始脚本](../scripts/run-sharding-bench.sh)
- 💻 [压测程序源码](../cmd/shardbench/main.go)
