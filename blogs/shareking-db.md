# 分库分表最佳实践

## 引言

随着业务的快速增长，单一数据库面临的性能瓶颈和存储限制问题日益突出。分库分表作为一种经典的数据库扩展方案，已经成为大规模互联网应用的标准实践。本文将从实际场景出发，探讨分库分表的演进过程、面临的挑战以及应对方案。

## ⚠️ 核心认知：分库分表的本质

> **分库分表不是性能优化的银弹，而是用"系统复杂度"换"单机性能突破"的妥协方案。**

### 分库分表解决了什么？

✅ **解决单机硬件瓶颈**：
- CPU 瓶颈：单机 CPU 核心数有限，分库后多机并行处理
- 内存瓶颈：单机内存有限，分库后每个实例的工作集更小
- 磁盘瓶颈：单机 IOPS 有限，分库后 IO 分散到多个磁盘
- 网络瓶颈：单机网卡带宽有限（如 10Gbps），分库后总带宽提升

✅ **解决单表数据量问题**：
- 索引深度降低（B+Tree 层级减少，查询更快）
- 单表数据量小，全表扫描更快
- 备份恢复时间缩短

### 分库分表带来了什么问题？

❌ **性能不一定提升，某些场景反而降低**：

| 场景 | 单库性能 | 分库性能 | 说明 |
|------|---------|---------|------|
| 按主键查询 | 1ms | 1-2ms | 分库增加路由开销 |
| 带分片键的查询 | 5ms | 3-5ms | 只查一个分片，性能相当 |
| 不带分片键的查询 | 10ms | **80ms** | 需要查 8 个分片并合并 |
| 跨表 JOIN | 5ms | **禁止** | 无法跨库 JOIN |
| 分布式事务 | 1ms | **50-500ms** | 2PC/TCC 性能极差 |
| 分页查询（全局） | 10ms | **80ms+** | 需要从每个分片取数据合并 |

**网络开销示例**：
```
单库查询:
应用 -> MySQL (1次网络往返, ~1ms)

分库查询（不带分片键）:
应用 -> 8个 MySQL (8次网络往返并发, ~2-5ms)
应用层聚合 (CPU 排序、去重, ~5ms)
总耗时: ~10ms (比单库慢 10 倍)
```

❌ **维护成本指数级增长**：

**运维复杂度**：
```
单库:
- 监控: 1 个实例
- 备份: 1 个库，1TB 数据，1 小时
- 恢复: 1 个库，1 小时
- 扩容: 垂直扩容（加 CPU/内存）
- 故障处理: 切换 1 个主从

分库分表 (8 库):
- 监控: 8 个实例 × 64 张表 = 512 个对象
- 备份: 8 个库，8TB 数据，需要 8 小时（串行）或复杂的并行方案
- 恢复: 需要恢复 8 个库，协调一致性
- 扩容: 数据迁移（75% 数据需要移动，停服或双写）
- 故障处理: 可能需要切换多个实例，影响面更大
```

**开发复杂度**：
```go
// 单库: 简单直接
order := db.Query("SELECT * FROM orders WHERE id = ?", orderID)

// 分库: 需要路由逻辑
shard := shardRouter.GetShard(orderID)  // 路由
order := shard.Query("SELECT * FROM orders_" + getTableSuffix(orderID) + " WHERE id = ?", orderID)

// 单库: 支持事务
db.Transaction(func(tx) {
    tx.Exec("UPDATE accounts SET balance = balance - 100 WHERE user_id = ?", 1)
    tx.Exec("UPDATE accounts SET balance = balance + 100 WHERE user_id = ?", 2)
})

// 分库: 需要分布式事务或最终一致性方案（代码量 10 倍）
saga := NewSaga()
saga.AddStep(step1, compensate1)
saga.AddStep(step2, compensate2)
saga.Execute()  // 100+ 行代码
```

❌ **灵活性大幅降低**：
- 不能随意加字段做查询（必须考虑是否包含分片键）
- 不能随意 JOIN（需要提前设计好数据冗余）
- 业务迭代困难（表结构变更需要改 64 张表）
- A/B 测试困难（数据隔离复杂）

### 什么时候才应该分库分表？

**单库能撑就别分！先尝试这些优化**：

```
阶段 1: SQL 优化 (成本: 0, 收益: 10-100x)
├── 添加索引
├── 优化慢查询
└── 去除不必要的 SELECT *

阶段 2: 读写分离 (成本: 低, 收益: 3-5x)
├── 1 主 3 从
├── 读请求走从库
└── 缓存热点数据 (Redis)

阶段 3: 垂直拆分 (成本: 中, 收益: 2-3x)
├── 按业务模块拆分数据库
├── 大字段拆分到独立表
└── 冷热数据分离

阶段 4: 分库分表 (成本: 高, 收益: 5-10x，但复杂度 +100x)
├── 单表 > 2000 万
├── 单库 QPS > 5000
└── 单库存储 > 1TB
```

**真实成本对比**：

| 项目 | 单库 | 分库分表 (8 库 64 表) |
|------|------|---------------------|
| 开发成本 | 1 个月 | **3-6 个月**（路由、聚合、迁移） |
| 硬件成本 | 1 台 (16C64G) = $1000/月 | 8 台 × $1000 = **$8000/月** |
| 运维人力 | 1 人 | **2-3 人**（监控、巡检、应急） |
| 故障恢复时间 | 1 小时 | **4-8 小时**（多库协调） |
| 新人上手时间 | 1 周 | **1-2 个月**（理解路由逻辑） |

### 正确的心态

✅ **正确**：
- 分库分表是**不得已的选择**，不是炫技
- 数据量没到千万级别，不要考虑分库
- 先优化 SQL、加缓存、读写分离，能撑 90% 的业务
- 分库分表后团队要有专门的 DBA 和架构师

❌ **错误**：
- "微服务时代，每个服务都要分库分表" 
- "大厂都在用，我们也要用"
- "提前设计好分库分表，以后就不用改了"（扩容仍然是大工程）

---

## 📋 决策清单：分库分表的适用边界

### ✅ 可以用分库分表的场景（必须同时满足）

| 条件 | 说明 | 示例 |
|------|------|------|
| **1. 查询模式固定** | 90%+ 的查询都带同一个字段 | 用户查自己的订单（带 user_id） |
| **2. 分片键分布均匀** | 不存在热点数据，数据分布基本均匀 | user_id 是均匀的，不是 VIP 少数人占 80% 数据 |
| **3. 无复杂关联查询** | 不需要跨表 JOIN，或已做好数据冗余 | 订单详情页已冗余用户信息，不需要 JOIN users |
| **4. 无全局统计需求** | 或全局统计可以走离线 T+1 | 实时订单总数可以用 Redis 计数器，不需要 SELECT COUNT(*) |
| **5. 可接受最终一致性** | 业务允许秒级延迟（如同步到 ES） | 后台报表允许 5 秒延迟 |
| **6. 团队有 DBA 能力** | 有人能设计、实施、运维分库分表方案 | 团队至少 2 个懂数据库的高级工程师 |

**典型场景**：
- ✅ 用户订单系统（用户查自己的订单）
- ✅ 社交动态（用户查自己的动态）
- ✅ IoT 设备数据（查某个设备的数据）
- ✅ 用户消息系统（查某个用户的消息）

---

### ❌ 不能用分库分表的场景（产品必须妥协）

#### 场景1：多维度查询（电商后台）

**需求**：
```sql
-- 管理后台需要按多个维度组合查询
SELECT * FROM orders 
WHERE 
  (status = 'pending' OR status = 'paid')
  AND created_at > '2024-01-01'
  AND amount > 1000
  AND province = '北京'
ORDER BY created_at DESC
LIMIT 20;
```

**问题**：
- 分片键只能选一个（如 user_id），其他维度查询需要扫全部分片
- 即使分库分表，这种查询仍然需要 500-1000ms

**产品层面的妥协方案**：

| 方案 | 说明 | 用户体验 |
|------|------|----------|
| **方案1：限制查询条件** | 必须选择时间范围（如最近7天） | "请选择查询时间范围（最多7天）" |
| **方案2：限制结果数量** | 只返回前 1000 条，不支持深度分页 | "最多显示 1000 条结果，请细化搜索条件" |
| **方案3：异步导出** | 查询结果通过异步任务导出 CSV | "查询较慢，请留下邮箱，完成后发送给您" |
| **方案4：T+1 离线报表** | 不提供实时查询，只有昨天的数据 | "数据更新时间：昨天 23:59" |

**示例对话**：
```
产品经理：我要能按所有字段随意组合查询！
DBA：不行，分库分表后做不到。
产品经理：那我们分库分表干嘛？
DBA：为了让用户查自己订单快。后台查询必须限制条件或用离线报表。
产品经理：运营怎么办？
DBA：运营的需求走数据仓库，T+1 更新。实时的用 Elasticsearch，5秒延迟。
产品经理：（妥协）好吧，那加个提示："后台查询请限定时间范围"。
```

---

#### 场景2：全局排序 + 分页（社交 Feed 流）

**需求**：
```sql
-- 显示全站最新的 20 条动态
SELECT * FROM posts 
ORDER BY created_at DESC 
LIMIT 20 OFFSET 100;
```

**问题**：
- 分库后需要从每个分片取数据再排序
- 深度分页（OFFSET 100）需要从每个分片取 120 条，应用层排序后丢弃大部分

**产品层面的妥协方案**：

| 方案 | 说明 | 用户体验 |
|------|------|----------|
| **方案1：只看关注的人** | 不再有"全站动态"，只显示关注的人 | 用户只能看关注的人的动态（带 user_id 分片键） |
| **方案2：推荐算法** | 不按时间排序，按推荐分数排序 | "为你推荐"（推荐系统提前计算好） |
| **方案3：禁止深度翻页** | 只能看前 100 条 | "没有更多了，请关注更多人" |
| **方案4：游标分页** | 不支持跳页，只能"加载更多" | 移动端下拉刷新模式 |

**真实案例：微博、Twitter**
- 早期：可以看全站最新微博（单库时代）
- 现在：只能看关注的人 + 算法推荐（分库后必须妥协）

---

#### 场景3：全局唯一性约束（用户注册）

**需求**：
```sql
-- 用户名、邮箱、手机号全局唯一
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    username VARCHAR(50) UNIQUE,
    email VARCHAR(100) UNIQUE,
    phone VARCHAR(20) UNIQUE
);
```

**问题**：
- 按 user_id 分片后，无法保证 username 在所有分片唯一
- 插入前需要查所有分片检查是否重复

**产品层面的妥协方案**：

| 方案 | 说明 | 用户体验 |
|------|------|----------|
| **方案1：独立唯一性表** | username/email/phone 单独一张不分片的表 | 注册需要查 2 次数据库（先查唯一性表，再插入 users） |
| **方案2：唯一性检查服务** | 用 Redis/Bloom Filter 做预检查 | 可能误判（Bloom Filter），需要再查数据库确认 |
| **方案3：放宽唯一性** | 不再要求全局唯一，加前缀区分 | 用户名改成 "user_123456"（用户体验差） |
| **方案4：手机号分片** | 按手机号分片（而不是 user_id） | 限制：以后按 user_id 查询就慢了 |

---

#### 场景4：跨用户数据聚合（电商对账）

**需求**：
```sql
-- 财务对账：今天所有订单的总金额
SELECT SUM(amount) FROM orders 
WHERE created_at > '2024-11-07 00:00:00';

-- 实时看板：各状态订单数量
SELECT status, COUNT(*) FROM orders 
GROUP BY status;
```

**问题**：
- 分库后需要查所有分片再聚合
- 数据量大时查询超时（如 1 亿订单）

**产品层面的妥协方案**：

| 方案 | 说明 | 用户体验 |
|------|------|----------|
| **方案1：实时计数器** | 用 Redis 维护订单总金额、各状态数量 | 存在延迟（最终一致性），可能少几百元 |
| **方案2：T+1 离线计算** | 昨天的数据用离线任务算好 | "数据截止到昨天 23:59" |
| **方案3：近似值** | 采样统计（如 1% 订单） | "大约 100 万订单"（不精确） |
| **方案4：限定时间范围** | 只支持查最近 7 天 | "请选择时间范围（最多 7 天）" |

**真实案例：支付宝对账**
- 实时余额：Redis 计数器（可能有几分钱误差）
- 精确对账：凌晨跑批，T+1 数据

---

#### 场景5：复杂事务（转账）

**需求**：
```go
// A 转账给 B
BEGIN;
UPDATE accounts SET balance = balance - 100 WHERE user_id = A;
UPDATE accounts SET balance = balance + 100 WHERE user_id = B;
COMMIT;
```

**问题**：
- A 和 B 可能在不同分片
- 分布式事务（2PC/TCC）性能差，容易超时

**产品层面的妥协方案**：

| 方案 | 说明 | 用户体验 |
|------|------|----------|
| **方案1：最终一致性** | 先扣 A，异步加 B（可能延迟几秒） | "转账处理中，请稍后查看余额" |
| **方案2：限制转账对象** | 只能转给同一分片的用户 | "暂不支持跨地区转账"（按地区分片） |
| **方案3：中心账户** | 所有余额放中心账户表（不分片） | 限制：用户数不能太多（几百万上限） |
| **方案4：限制并发** | 限流（如 QPS 100），保证 2PC 不超时 | 大促时"系统繁忙，请稍后重试" |

**真实案例：微信红包**
- 不是实时到账，而是先记录，异步处理
- 春节抢红包高峰：限流 + 排队

---

### ⚠️ 关键决策点

**分库分表前，产品经理必须回答**：

1. ✅ 我们能否接受某些查询变慢（100ms → 500ms）？
2. ✅ 我们能否限制后台查询条件（必须选时间范围）？
3. ✅ 我们能否接受全局统计数据有延迟（T+1）？
4. ✅ 我们能否放弃某些功能（如全站动态、深度分页）？
5. ✅ 我们能否接受某些操作变成异步（转账"处理中"）？

**如果上面有 2 个以上回答 "不能"，那么：**
- ❌ 不要分库分表
- ✅ 考虑换 NewSQL（TiDB / CockroachDB）
- ✅ 或者重新设计产品，简化需求

---

### 💡 经验总结

> **分库分表不是技术问题，是产品和技术的妥协问题。**

**成功的分库分表项目**：
- 产品理解限制，主动简化需求（如去掉全站动态）
- 技术提供替代方案（如离线报表、推荐算法）
- 双方共同定义可接受的用户体验（如"查询较慢"提示）

**失败的分库分表项目**：
- 产品坚持要所有功能，技术硬着头皮实现
- 结果：性能没提升，系统更复杂，故障更多
- 最终：花半年时间回滚，或者换 NewSQL 数据库

**一句话**：
- 如果产品不愿意妥协，就不要分库分表，直接上 TiDB。
- 如果预算有限（TiDB 太贵），那产品必须妥协。

---

## 一、什么场景需要分库分表

### 1.1 数据量瓶颈

**单表数据量过大**
- 当单表数据量超过 **1000-2000万** 行时，MySQL 的查询性能会明显下降
- B+Tree 索引层级增加，磁盘 I/O 次数增多
- 即使使用了索引，查询响应时间也会显著增加

```sql
-- 单表 5000万+ 数据，即使有索引，查询也很慢
SELECT * FROM orders WHERE user_id = 12345 AND created_at > '2024-01-01';
-- 响应时间: 500ms+
```

**数据库存储容量限制**
- 单机磁盘容量有限（如 2TB SSD）
- 数据增长速度超过硬件扩容速度
- 备份和恢复时间过长（TB 级数据备份可能需要数小时）

### 1.2 并发瓶颈

**单库连接数限制**
- MySQL 默认最大连接数 151，调整后也难以支撑 10000+ 并发
- 连接池耗尽导致应用请求超时
- 热点数据访问引发锁竞争

**写入性能瓶颈**
- 单库 TPS 通常在 3000-5000（取决于硬件配置）
- 高并发写入场景（如秒杀、大促）下，单库无法满足需求
- 主从复制延迟加剧（binlog 积压）

**读性能瓶颈**
- 虽然可以通过主从读写分离缓解，但仍有局限：
  - **从库数量有限**（通常 3-5 个），无法无限扩展
  - **从库复制延迟**导致某些业务必须读主库（如：用户刚下单，立即查询订单详情）
  - **强一致性需求**的查询仍需走主库（如：金额校验、库存扣减后的查询）
  - **热点数据**会同时压垮主库和所有从库（如：秒杀商品、爆款订单）
  - **单表过大**时，即使查从库也慢（索引深度问题无法通过读写分离解决）

### 1.3 业务隔离需求

**多租户系统**
- 不同租户的数据需要物理隔离（数据安全、性能隔离）
- VIP 客户需要独立的数据库资源保证 SLA

**核心业务与边缘业务分离**
- 订单、支付等核心业务需要独立数据库保证高可用
- 日志、统计等边缘业务可能影响核心业务性能

### 1.4 典型场景判断矩阵

| 场景 | 数据量 | QPS | 是否需要分库分表 | 推荐方案 |
|------|--------|-----|------------------|----------|
| 初创业务 | < 500万 | < 1000 | ❌ | 单库 + 主从 |
| 成长期业务 | 500万-2000万 | 1000-5000 | ⚠️ 观察 | 读写分离 + 索引优化 |
| 成熟业务 | > 2000万 | > 5000 | ✅ 是 | 分库分表 |
| 超大规模 | > 10亿 | > 50000 | ✅ 必须 | 分库分表 + 中间件 |

## 二、分库分表带来的问题

### 2.1 事务问题

**跨库事务无法保证 ACID**

```go
// 原本简单的转账事务
BEGIN;
UPDATE accounts SET balance = balance - 100 WHERE user_id = 1001; // 在 db_0
UPDATE accounts SET balance = balance + 100 WHERE user_id = 2002; // 在 db_1
COMMIT;

// 分库后无法使用数据库事务
```

**解决方案：**
1. **分布式事务**
   - 2PC（两阶段提交）：性能差，超时问题多
   - TCC（Try-Confirm-Cancel）：业务侵入性强
   - SAGA：适合长事务，需要补偿逻辑

2. **最终一致性**
   ```go
   // 使用消息队列实现最终一致性
   // 1. 本地事务：扣款 + 发送消息
   tx.Begin()
   tx.Exec("UPDATE accounts SET balance = balance - 100 WHERE user_id = ?", 1001)
   tx.Exec("INSERT INTO outbox (event_type, payload) VALUES (?, ?)", "TRANSFER", data)
   tx.Commit()
   
   // 2. 消费消息：执行加款
   consumer.Handle(func(msg Message) {
       db.Exec("UPDATE accounts SET balance = balance + 100 WHERE user_id = ?", 2002)
   })
   ```

3. **避免跨库事务**
   - 业务设计时尽量避免跨库操作
   - 将强关联的数据放在同一个分片

### 2.2 跨库查询和 JOIN 问题

**无法执行跨库 JOIN**

```sql
-- 原本的 JOIN 查询
SELECT o.*, u.name, u.email 
FROM orders o 
INNER JOIN users u ON o.user_id = u.id 
WHERE o.status = 'pending';

-- 分库后 orders 和 users 可能在不同数据库，无法 JOIN
```

**解决方案：**

1. **应用层 JOIN**
   ```go
   // 分两次查询，应用层组装数据
   orders := queryOrders("SELECT * FROM orders WHERE status = ?", "pending")
   userIDs := extractUserIDs(orders)
   users := queryUsers("SELECT * FROM users WHERE id IN (?)", userIDs)
   
   // 应用层合并
   result := mergeOrdersWithUsers(orders, users)
   ```

2. **数据冗余**
   ```sql
   -- 在 orders 表中冗余 user_name, user_email
   CREATE TABLE orders (
       id BIGINT PRIMARY KEY,
       user_id BIGINT,
       user_name VARCHAR(100),  -- 冗余字段
       user_email VARCHAR(100), -- 冗余字段
       amount DECIMAL(10,2),
       status VARCHAR(20)
   );
   ```

3. **宽表设计**
   
   **方案A：宽表也分库分表**（推荐）
   ```sql
   -- 将 orders 和 users 的常用字段合并，按 user_id 分片
   CREATE TABLE order_detail_wide (
       order_id BIGINT PRIMARY KEY,
       user_id BIGINT,           -- 分片键
       user_name VARCHAR(100),   -- 来自 users 表
       user_level INT,           -- 来自 users 表
       amount DECIMAL(10,2),
       status VARCHAR(20),
       created_at TIMESTAMP,
       INDEX idx_user (user_id)
   );
   ```
   - 宽表按 `user_id` 分片，与 `users` 表保持相同分片策略
   - 查询时无需跨库 JOIN，单库内查询
   - **适合场景**：订单详情页、用户订单列表等高频查询
   
   **方案B：独立宽表库**（中心化查询）
   ```
   分片库: 
     - user_db_0 ~ user_db_7 (用户主表，按 user_id 分片)
     - order_db_0 ~ order_db_7 (订单主表，按 user_id 分片)
   
   独立宽表库:
     - wide_table_db (不分片，或单独分片)
       ├── order_user_wide (订单+用户宽表)
       └── 通过 CDC 或消息队列同步数据
   ```
   - **优点**：支持跨多个分片键的复杂查询
   - **缺点**：数据同步延迟、额外存储成本
   - **适合场景**：后台报表、BI 分析、多维度查询
   
   **数据同步策略**
   ```go
   // 订单创建时，同步更新宽表
   func CreateOrder(order *Order) error {
       // 1. 查询用户信息
       user := getUserByID(order.UserID)
       
       // 2. 写入订单分片表
       insertOrder(order)
       
       // 3. 写入宽表（异步）
       wideRow := OrderWide{
           OrderID:   order.OrderID,
           UserID:    order.UserID,
           UserName:  user.Name,
           UserLevel: user.Level,
           Amount:    order.Amount,
           Status:    order.Status,
       }
       
       // 通过消息队列异步同步
       mq.Publish("order.wide.sync", wideRow)
       return nil
   }
   ```
   
   - **适合读多写少的场景**：宽表冗余了多张表的数据，更新成本高

4. **搜索引擎**
   - 将需要复杂查询的数据同步到 Elasticsearch
   - 适合全文搜索、聚合统计等场景

### 2.3 分布式 ID 生成

**自增 ID 不可用**
- 多个数据库分片无法保证全局唯一自增 ID
- 需要统一的分布式 ID 生成方案

**解决方案：**

1. **Snowflake 算法**（推荐）
   ```
   64位 ID 结构:
   [1位未使用][41位时间戳][10位机器ID][12位序列号]
   
   优点: 趋势递增、高性能(单机100万+/s)、无依赖
   缺点: 依赖机器时钟，时钟回拨需要处理
   ```

2. **数据库号段模式**
   ```sql
   -- 维护一个全局 ID 分配表
   CREATE TABLE id_generator (
       biz_type VARCHAR(50) PRIMARY KEY,
       max_id BIGINT,
       step INT
   );
   
   -- 应用批量获取 ID 段 [max_id, max_id+step)
   UPDATE id_generator SET max_id = max_id + step WHERE biz_type = 'order';
   ```

3. **UUID**
   - 优点：简单、无依赖
   - 缺点：36 字符、无序（影响索引性能）、不友好

4. **Redis INCR**
   ```go
   // 使用 Redis 生成全局唯一 ID
   id := redis.Incr("order:id")
   ```

### 2.4 分页查询问题

**ORDER BY + LIMIT 失效**

```sql
-- 原查询：获取最新 10 条订单
SELECT * FROM orders ORDER BY created_at DESC LIMIT 10;

-- 分库后需要：
-- 1. 从每个分片取 10 条（假设 8 个分片，需查询 80 条）
-- 2. 应用层合并排序，取前 10 条
```

这个问题在分库分表场景下**非常棘手**，没有完美解决方案，只能根据业务场景选择最合适的妥协方案。

**解决方案：**

#### 方案1：限定查询范围（推荐）

**核心思想**：分页查询必须带分片键，避免跨片查询

```sql
-- ✅ 好的查询：按 user_id 查询该用户的订单（只查一个分片）
SELECT * FROM orders 
WHERE user_id = 12345 
ORDER BY created_at DESC 
LIMIT 10 OFFSET 20;

-- ❌ 坏的查询：全局订单分页（需要查所有分片）
SELECT * FROM orders 
ORDER BY created_at DESC 
LIMIT 10 OFFSET 20;
```

**实现策略**：
- 业务上**禁止全局分页查询**
- 用户只能查自己的订单（带 user_id）
- 管理后台按时间范围 + 状态等条件查询（缩小范围）

```go
// 强制要求带分片键
func ListOrders(userID int64, page int) ([]*Order, error) {
    if userID == 0 {
        return nil, errors.New("user_id is required")
    }
    
    // 只查该用户所在的分片
    shardIndex := userID % 8
    return queryFromShard(shardIndex, userID, page)
}
```

**优点**：性能最好，只查一个分片
**缺点**：限制了业务灵活性，无法全局分页

---

#### 方案2：游标分页 + 分片键

```go
// 第一页：带分片键（user_id）
SELECT * FROM orders 
WHERE user_id = ? 
ORDER BY id DESC 
LIMIT 10;
// 返回最后一条 id = 12345

// 第二页：继续使用游标
SELECT * FROM orders 
WHERE user_id = ? AND id < 12345 
ORDER BY id DESC 
LIMIT 10;
```

**优点**：性能好，避免 OFFSET 扫描
**缺点**：
- 仍然需要带分片键（无法全局游标分页）
- 不能跳页（只能上一页/下一页）

---

#### 方案3：搜索引擎（ES / ClickHouse）

**适用场景**：后台管理、BI 报表、复杂多维度查询

```
架构:
┌──────────────┐
│  MySQL 分片  │ ──CDC/MQ──> ┌──────────────┐
│  (OLTP)      │              │ Elasticsearch│
│  实时写入    │              │ (OLAP)       │
└──────────────┘              │  全局查询    │
                              └──────────────┘
```

**数据同步**：
```go
// 订单写入 MySQL 后，异步同步到 ES
func CreateOrder(order *Order) error {
    // 1. 写入 MySQL 分片（强一致）
    if err := mysqlRepo.Insert(order); err != nil {
        return err
    }
    
    // 2. 发送到消息队列，异步同步到 ES（最终一致）
    kafka.Publish("order.created", order)
    return nil
}

// ES 消费者
func syncToES(order *Order) {
    esClient.Index("orders", order.OrderID, order)
}
```

**查询分流**：
```go
// C 端用户查询：走 MySQL（实时、带分片键）
func UserQueryOrders(userID int64) ([]*Order, error) {
    return mysqlRepo.GetByUserID(userID)
}

// B 端后台查询：走 ES（允许延迟、支持复杂查询）
func AdminSearchOrders(query *SearchQuery) ([]*Order, error) {
    return esClient.Search("orders", query)
}
```

**优点**：
- 支持复杂查询（多维度筛选、全文搜索、聚合统计）
- 性能好（ES 分布式查询优化）

**缺点**：
- **成本高**：数据量大需要大集群（如 1TB MySQL 需要 3TB+ ES 集群）
- **数据延迟**：通常 1-5 秒延迟（最终一致性）
- **运维复杂**：需要维护 ES 集群、监控同步状态
- **双写复杂度**：数据一致性保障（如同步失败重试、数据校验）

**成本估算**：
```
假设：
- MySQL: 8个分片，总数据 1TB，10亿订单
- ES: 需要 3 个副本保证高可用

存储成本:
- MySQL: 1TB × 8 = 8TB
- ES: 1TB × 3 (副本) × 1.5 (索引开销) = 4.5TB
- 总存储: 12.5TB

硬件成本:
- MySQL: 8台 (16C32G, 2TB SSD) ≈ $8000/月
- ES: 12台 (16C32G, 1TB SSD) ≈ $12000/月
- 总成本: $20000/月
```

---

#### 方案4：分页近似算法（数据量超大时）

**场景**：类似 Google 搜索，深度分页时给出"大约有 1000 万条结果"

```go
// 只提供前 N 页的精确分页，超过后禁止
const MaxPage = 100

func ListOrders(page int) ([]*Order, error) {
    if page > MaxPage {
        return nil, errors.New("too many pages, please refine your search")
    }
    
    // 从每个分片取数据，合并
    // ...
}
```

**用户体验优化**：
- 第 1-10 页：正常分页
- 第 11-100 页：提示"结果过多，请添加筛选条件"
- 超过 100 页：禁止访问

---

#### 真实场景选择

| 场景 | 推荐方案 | 原因 |
|------|----------|------|
| C 端用户查订单 | 方案1（限定查询范围） | 用户只查自己的数据，带 user_id |
| B 端管理后台 | 方案3（ES） | 需要复杂查询、多维度筛选 |
| 订单列表翻页 | 方案2（游标分页） | 性能好，大部分用户只看前几页 |
| BI 报表/数据分析 | ClickHouse | 专为 OLAP 设计，比 ES 更便宜 |

---

#### 总结

分库分表后的分页查询**没有银弹**：
- ✅ **能避免就避免**：业务设计时限定查询范围（带分片键）
- ✅ **接受妥协**：禁止深度分页、用游标分页替代 OFFSET
- ✅ **愿意花钱**：ES/ClickHouse 解决复杂查询，但成本翻倍
- ❌ **不要幻想**：没有又快又便宜又灵活的完美方案

### 2.5 唯一约束问题

**全局唯一约束失效**

```sql
-- 原表：email 全局唯一
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    email VARCHAR(100) UNIQUE,
    name VARCHAR(100)
);

-- 分库后无法保证 email 在所有分片中唯一
```

**解决方案：**

1. **路由键包含唯一字段**
   ```go
   // 如果 email 需要唯一，按 email hash 分片
   shardIndex := hash(email) % shardCount
   ```

2. **全局唯一性表**
   ```sql
   -- 单独维护一张全局唯一性检查表（不分片）
   CREATE TABLE unique_emails (
       email VARCHAR(100) PRIMARY KEY,
       user_id BIGINT
   );
   ```

3. **分布式锁 + 查询**
   ```go
   // 插入前先检查所有分片
   lock := redis.Lock("email:" + email)
   if existsInAnyShared(email) {
       return errors.New("email exists")
   }
   insert(user)
   ```

### 2.6 分片键选择的两难困境（最痛的问题）

> **核心矛盾：一份数据只能有一个分片键，所有不带分片键的查询性能都会大幅降低。**

#### 问题说明

假设订单表按 `user_id` 分片：

```sql
-- ✅ 带分片键的查询：只查 1 个分片，性能好
SELECT * FROM orders WHERE user_id = 12345;  
-- 查询 db_1.orders_5 (1次数据库查询, 5ms)

-- ❌ 不带分片键的查询：需要查所有分片，性能暴跌
SELECT * FROM orders WHERE order_id = 98765;
-- 查询 db_0~db_7 的所有表 (64次查询, 应用层聚合, 80-200ms)
```

**性能对比**：

| 查询类型 | 单库性能 | 分库性能 | 性能变化 |
|---------|---------|---------|---------|
| WHERE user_id = ? | 5ms | 3-5ms | ✅ 相当或更快 |
| WHERE order_id = ? | 5ms | **80-200ms** | ❌ **慢 16-40 倍** |
| WHERE status = ? | 10ms | **200-500ms** | ❌ **慢 20-50 倍** |
| WHERE created_at > ? | 10ms | **300-1000ms** | ❌ **慢 30-100 倍** |

#### 真实案例：电商订单系统

**业务查询场景**：
1. 用户查询自己的订单：`WHERE user_id = ?` （90%）
2. 客服通过订单号查询：`WHERE order_id = ?` （8%）
3. 管理后台按状态查询：`WHERE status = 'pending'` （1%）
4. 财务对账查询：`WHERE created_at BETWEEN ? AND ?` （1%）

**按 user_id 分片的后果**：

```go
// ✅ 场景 1: 用户查订单 (90% 流量，性能好)
func GetUserOrders(userID int64) ([]*Order, error) {
    shardIndex := userID % 8
    // 只查 1 个分片
    return queryFromShard(shardIndex, "SELECT * FROM orders WHERE user_id = ?", userID)
}

// ❌ 场景 2: 客服查订单 (8% 流量，性能差)
func GetOrderByID(orderID int64) (*Order, error) {
    // 不知道在哪个分片，必须查所有分片
    var wg sync.WaitGroup
    resultChan := make(chan *Order, 8)
    
    for shardIndex := 0; shardIndex < 8; shardIndex++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            // 每个分片还要查 8 张表
            for tableIdx := 0; tableIdx < 8; tableIdx++ {
                order := queryFromShard(idx, tableIdx, "SELECT * FROM orders_? WHERE order_id = ?", orderID)
                if order != nil {
                    resultChan <- order
                    return
                }
            }
        }(shardIndex)
    }
    
    wg.Wait()
    // 64 次数据库查询！
}

// ❌ 场景 3: 管理后台查询 (1% 流量，性能极差)
func GetPendingOrders() ([]*Order, error) {
    // 需要查所有分片的所有表，然后聚合
    // 64 次查询 + 应用层排序 + 分页
    // 响应时间: 500-1000ms
}
```

#### 解决方案的权衡

**方案 1：多套数据，按不同分片键分片**（代价巨大）

```
数据集 1: 按 user_id 分片 (8 库，1TB)
├── 用于用户查询自己的订单

数据集 2: 按 order_id 分片 (8 库，1TB)
├── 用于客服通过订单号查询

数据集 3: 不分片，同步到 ES (0.5TB)
├── 用于管理后台复杂查询

总成本:
- 存储: 1TB × 3 = 3TB (数据翻 3 倍)
- 机器: 24 台数据库 + 12 台 ES = 36 台
- 数据同步: 3 套 CDC 链路
- 一致性问题: 3 份数据的同步延迟和一致性
```

**优点**: 每种查询都快
**缺点**: 
- 存储成本翻 3 倍
- 机器成本翻 3 倍  
- 数据一致性复杂（更新 1 条数据要同步 3 个地方）
- 运维成本指数增长

---

**方案 2：牺牲低频查询性能**（务实）

```go
// 高频查询 (90%)：优化，走分片
func GetUserOrders(userID int64) ([]*Order, error) {
    // 性能: 5ms
    return queryFromShard(userID%8, userID)
}

// 低频查询 (10%)：降级，慢就慢
func GetOrderByID(orderID int64) (*Order, error) {
    // 方案 2.1: 查所有分片 (性能差但功能可用)
    // 性能: 100-200ms
    return queryAllShards(orderID)
    
    // 方案 2.2: 加缓存
    // 先查 Redis (90% 命中, 1ms)
    if order := redis.Get("order:" + orderID); order != nil {
        return order
    }
    // 未命中再查所有分片 (10%, 100ms)
    return queryAllShards(orderID)
}

// 极低频查询 (1%)：禁止或走 ES
func AdminSearchOrders(query *Query) ([]*Order, error) {
    // 方案 2.3: 同步到 ES，走 ES 查询
    // 数据延迟: 1-5 秒
    // 性能: 50-100ms
    return esClient.Search("orders", query)
}
```

**优点**: 成本可控，覆盖大部分场景
**缺点**: 低频查询慢，需要向业务妥协

---

**方案 3：复合分片键**（有限的改进）

某些场景下可以用复合分片键，但仍然有限制：

```go
// 假设大部分查询都同时带 user_id 和 order_id
// 可以用 order_id 的低位做表分片
func Route(userID, orderID int64) (dbIndex, tableIndex int) {
    dbIndex = int(userID % 8)        // user_id 决定库
    tableIndex = int(orderID % 8)    // order_id 决定表
    return
}

// ✅ 同时带两个字段：精确路由
SELECT * FROM orders WHERE user_id = ? AND order_id = ?;

// ⚠️ 只带 user_id：查该库的所有表（8 次查询）
SELECT * FROM orders WHERE user_id = ?;

// ❌ 只带 order_id：查所有库的对应表（8 次查询）
SELECT * FROM orders WHERE order_id = ?;
```

**优点**: 一定程度上缓解问题
**缺点**: 
- 只能帮助到同时带两个字段的查询
- 单字段查询仍然慢
- 设计复杂，容易出错

---

#### 分片键选择的决策矩阵

| 因素 | 权重 | 说明 |
|------|------|------|
| **查询频率** | ⭐⭐⭐⭐⭐ | 90% 的查询用什么字段？选它做分片键 |
| **数据分布均匀性** | ⭐⭐⭐⭐ | 避免热点数据（如按商品ID分片，爆款商品会压垮单分片）|
| **业务不变性** | ⭐⭐⭐ | 分片键一旦确定很难改，选相对稳定的字段 |
| **关联查询需求** | ⭐⭐ | 相关的表用相同分片键（如 users 和 orders 都按 user_id）|

**错误示例**：

```
❌ 按 order_status 分片
理由: status 只有 5 个值，数据分布极度不均
结果: "待支付"订单占 70%，导致单分片过载

❌ 按 product_id 分片  
理由: 爆款商品导致热点数据
结果: 618 大促时某个商品的订单全在一个分片，压垮单库

❌ 按 created_time 分片
理由: 新订单都写入最新的分片
结果: 最新分片压力巨大，老分片空闲（时间倾斜）
```

**正确示例**：

```
✅ 电商订单按 user_id 分片
理由: 
- 90% 查询都是用户查自己的订单
- 用户ID分布均匀
- 用户ID不会变

✅ 社交动态按 user_id 分片
理由:
- 99% 查询都是查某个用户的动态
- 用户ID分布均匀

✅ IoT 设备数据按 device_id 分片
理由:
- 100% 查询都是查某个设备的数据
- 设备ID分布均匀
```

#### 残酷的真相

> **一旦选定分片键，就决定了系统的性能瓶颈和可扩展性边界。**

- 选对了分片键：系统可以线性扩展，高频查询性能优秀
- 选错了分片键：要么性能差，要么数据倾斜，要么需要重构（代价巨大）

**重构成本**：
```
改分片键 = 重新设计 + 数据迁移 + 停服风险

时间成本: 3-6 个月
人力成本: 5-10 人团队
业务影响: 需要停服维护窗口或复杂的双写方案
```

所以：**分库分表前一定要深入理解业务的查询模式，宁可晚点分，也不要分错！**

---

### 2.7 扩容和数据迁移

**分片数固定后难以扩容**

```
初始: 4 个分片, user_id % 4
扩容: 8 个分片, user_id % 8

问题: user_id=5 原在 shard_1 (5%4=1), 扩容后在 shard_5 (5%8=5)
需要迁移 75% 的数据！
```

**解决方案：**

1. **一致性哈希**
   - 减少扩容时的数据迁移量（约 1/N）
   - 虚拟节点技术平衡数据分布

2. **预分片 + 逻辑分库**
   ```
   物理库: 4 个 (db_0, db_1, db_2, db_3)
   逻辑分片: 64 个 (shard_0 ~ shard_63)
   
   映射: shard_0~15 -> db_0
         shard_16~31 -> db_1
         ...
   
   扩容时只需调整映射，无需修改路由算法
   ```

3. **双写迁移方案**
   ```
   Phase 1: 双写（旧库 + 新库）
   Phase 2: 数据补齐（迁移历史数据）
   Phase 3: 切换读流量到新库
   Phase 4: 下线旧库
   ```

## 三、分库分表的应对方案和最佳实践

### 3.1 分片策略选择

#### 3.1.1 垂直分库

**按业务模块拆分**

```
原库: business_db
├── users
├── orders
├── products
├── payments
└── logs

拆分后:
user_db    -> users
order_db   -> orders
product_db -> products
payment_db -> payments
log_db     -> logs
```

**适用场景：**
- 不同业务模块的数据量和访问模式差异大
- 需要不同的备份、容灾策略
- 团队按业务线划分

#### 3.1.2 垂直分表

**大表拆分为主表 + 扩展表**

```sql
-- 原表: 包含基础字段和大字段
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    username VARCHAR(50),
    email VARCHAR(100),
    profile_text TEXT,      -- 大字段
    settings JSON,          -- 大字段
    created_at TIMESTAMP
);

-- 拆分后
-- 主表: 高频访问字段
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    username VARCHAR(50),
    email VARCHAR(100),
    created_at TIMESTAMP
);

-- 扩展表: 低频大字段
CREATE TABLE user_profiles (
    user_id BIGINT PRIMARY KEY,
    profile_text TEXT,
    settings JSON
);
```

#### 3.1.3 水平分库分表

**Hash 分片**（推荐）

```go
// 按 user_id hash 分片
func GetShardIndex(userID int64, shardCount int) int {
    return int(userID % int64(shardCount))
}

// 优点: 数据分布均匀
// 缺点: 范围查询需要查所有分片
```

**Range 分片**

```go
// 按时间范围分片
// orders_202401 (2024-01)
// orders_202402 (2024-02)
// ...

// 优点: 适合按时间查询和归档
// 缺点: 数据分布不均（最新分片压力大）
```

**地理位置分片**

```
db_north (华北用户)
db_east  (华东用户)
db_south (华南用户)

优点: 降低网络延迟
缺点: 数据分布不均
```

### 3.2 分片键（Sharding Key）设计原则

**1. 高频查询字段**
```sql
-- ✅ 按 user_id 分片，大部分查询都带 user_id
SELECT * FROM orders WHERE user_id = ? AND status = ?;

-- ❌ 按 user_id 分片，但查询 order_id 需要查所有分片
SELECT * FROM orders WHERE order_id = ?;
```

**2. 数据分布均匀**
```go
// ✅ user_id 分布均匀
shardIndex := userID % shardCount

// ❌ 城市分布不均（一线城市数据占 70%）
shardIndex := cityID % shardCount
```

**3. 避免热点数据**
```go
// ❌ 按商品 ID 分片，热门商品导致单分片压力大
shardIndex := productID % shardCount

// ✅ 按订单 ID 或用户 ID 分片
shardIndex := orderID % shardCount
```

**4. 最小化跨片查询**
```
设计原则: 将强关联的数据放在同一分片

例如: 用户、订单、地址都按 user_id 分片
这样查询用户订单时无需跨片
```

### 3.3 分库分表中间件选型

#### 3.3.1 客户端 Sharding（应用层）

**Gorm Sharding Plugin (Go)**

```go
import "gorm.io/sharding"

db.Use(sharding.Register(sharding.Config{
    ShardingKey: "user_id",
    NumberOfShards: 8,
    ShardingAlgorithm: func(value any) (suffix string, err error) {
        if uid, ok := value.(int64); ok {
            return fmt.Sprintf("_%02d", uid%8), nil
        }
        return "", errors.New("invalid user_id")
    },
}))

// 自动路由到正确的分片
db.Where("user_id = ?", 123).Find(&orders)
```

**ShardingSphere-JDBC (Java)**

```yaml
sharding:
  tables:
    orders:
      actualDataNodes: ds_$->{0..7}.orders_$->{0..15}
      databaseStrategy:
        standard:
          shardingColumn: user_id
          shardingAlgorithmName: db_mod
      tableStrategy:
        standard:
          shardingColumn: order_id
          shardingAlgorithmName: table_mod
```

**优点:**
- 性能高（无网络开销）
- 灵活（代码控制路由逻辑）

**缺点:**
- 业务侵入性强
- 升级需要重新发布应用

#### 3.3.2 代理 Sharding（中间件层）

**MyCat / ShardingSphere-Proxy**

```
应用 -> MyCat (3306) -> 后端 MySQL (db_0, db_1, db_2, ...)

应用无感知，发送普通 SQL 即可
```

**优点:**
- 对应用透明
- 支持多语言
- 统一管理分片规则

**缺点:**
- 额外的网络跳转（+1-2ms）
- 代理成为单点（需要 HA）

### 3.4 读写分离与分库分表结合

```
架构设计:
┌─────────────┐
│   应用层    │
└─────────────┘
       │
┌──────▼───────┐
│ Sharding层  │ (路由到不同分片)
└──────────────┘
       │
┌──────▼───────┬───────────┬───────────┐
│   db_0       │   db_1    │   db_2    │
│  ┌──────┐    │  ┌──────┐ │  ┌──────┐ │
│  │Master│    │  │Master│ │  │Master│ │
│  └───┬──┘    │  └───┬──┘ │  └───┬──┘ │
│      │       │      │    │      │    │
│  ┌───▼──┬──┐ │  ┌───▼──┬─│  ┌───▼──┬ │
│  │Slave1│S2│ │  │Slave1│S│  │Slave1│S│
│  └──────┴──┘ │  └──────┴─│  └──────┴─│
└──────────────┴───────────┴───────────┘
```

**实现示例:**

```go
type ShardingDB struct {
    masters []MasterDB
    slaves  [][]SlaveDB
}

func (s *ShardingDB) Query(userID int64, sql string) {
    shardIndex := userID % int64(len(s.masters))
    
    // 读请求路由到从库
    slaveIndex := rand.Intn(len(s.slaves[shardIndex]))
    return s.slaves[shardIndex][slaveIndex].Query(sql)
}

func (s *ShardingDB) Exec(userID int64, sql string) {
    shardIndex := userID % int64(len(s.masters))
    
    // 写请求路由到主库
    return s.masters[shardIndex].Exec(sql)
}
```

### 3.5 监控与运维

**关键指标监控:**

1. **分片数据倾斜监控**
   ```sql
   -- 定期检查各分片数据量
   SELECT 'db_0' as db, COUNT(*) FROM db_0.orders
   UNION ALL
   SELECT 'db_1', COUNT(*) FROM db_1.orders
   UNION ALL
   ...
   ```

2. **慢查询监控**
   ```bash
   # 监控跨片查询（需遍历所有分片的查询）
   # 标记未带分片键的查询
   ```

3. **分片键覆盖率**
   ```
   统计查询中分片键的使用比例
   目标: >95% 的查询包含分片键
   ```

**运维工具:**
- 数据迁移工具（如 gh-ost, pt-online-schema-change）
- 数据校验工具（源分片 vs 目标分片数据一致性）
- 自动化扩容脚本

### 3.6 渐进式演进路径

**阶段 1: 单库（0-100万数据）**
```
单 MySQL + 主从 + 缓存
成本低，维护简单
```

**阶段 2: 读写分离（100万-1000万）**
```
1主3从 + Redis 缓存
解决读压力，成本可控
```

**阶段 3: 垂直分库（1000万-5000万）**
```
按业务拆分数据库
降低单库压力，团队独立迭代
```

**阶段 4: 水平分表（5000万+）**
```
单库内分表（如 orders_0 ~ orders_7）
降低单表数据量，索引性能提升
```

**阶段 5: 水平分库分表（1亿+）**
```
8个库 x 8张表 = 64个物理分片
完整的分库分表方案
```

## 四、实战案例：订单系统分库分表

> 💡 **本仓库已实现完整的压测案例**，可以直接运行查看实际效果！
> 
> 📖 详细文档: [docs/sharding-benchmark.md](../docs/sharding-benchmark.md)
> 
> 🚀 快速运行: `make bench-sharding` 或 `./scripts/run-sharding-bench.sh`

### 4.1 业务背景

- 日订单量: 500万
- 历史订单: 50亿+
- QPS: 峰值 10万
- 查询模式: 
  - 90% 按 user_id 查询
  - 9% 按 order_id 查询
  - 1% 后台复杂查询

### 4.1.1 压测环境

本仓库实现的压测案例：
- **测试数据**: 100万订单 (10万用户，每用户10个订单)
- **单库方案**: 1个 PostgreSQL (端口 5434)
- **分库方案**: 8个 PostgreSQL (端口 5440-5447)，64个物理分片
- **压测场景**: 插入、按订单ID查询、按用户ID查询
- **并发数**: 100
- **压测时长**: 30秒/场景

### 4.2 方案设计

**分片规则:**

```go
// 分库: 8个物理库 (db_0 ~ db_7)
// 分表: 每库8张表 (orders_0 ~ orders_7)
// 总分片: 64

func Route(orderID int64) (dbIndex, tableIndex int) {
    // 高 8 位确定库，低 8 位确定表
    dbIndex = int((orderID >> 8) % 8)
    tableIndex = int(orderID % 8)
    return
}
```

**表结构:**

```sql
CREATE TABLE orders_0 (
    order_id BIGINT PRIMARY KEY,       -- Snowflake ID
    user_id BIGINT NOT NULL,
    amount DECIMAL(10,2),
    status TINYINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    INDEX idx_user_created (user_id, created_at),
    INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- orders_1, orders_2, ..., orders_7 结构相同
```

**数据访问层:**

```go
type OrderRepo struct {
    shards [8][8]*sql.DB
}

// 按 order_id 查询（精确路由）
func (r *OrderRepo) GetByOrderID(orderID int64) (*Order, error) {
    dbIdx, tblIdx := Route(orderID)
    sql := fmt.Sprintf("SELECT * FROM orders_%d WHERE order_id = ?", tblIdx)
    return r.shards[dbIdx][tblIdx].QueryRow(sql, orderID).Scan(...)
}

// 按 user_id 查询（需要遍历该用户所在库的所有表）
func (r *OrderRepo) GetByUserID(userID int64) ([]*Order, error) {
    dbIdx := int(userID % 8)
    
    var orders []*Order
    for tblIdx := 0; tblIdx < 8; tblIdx++ {
        sql := fmt.Sprintf("SELECT * FROM orders_%d WHERE user_id = ? ORDER BY created_at DESC LIMIT 100", tblIdx)
        rows, _ := r.shards[dbIdx][tblIdx].Query(sql, userID)
        // 解析结果...
        orders = append(orders, ...)
    }
    
    // 应用层排序 + 分页
    sort.Slice(orders, func(i, j int) bool {
        return orders[i].CreatedAt.After(orders[j].CreatedAt)
    })
    return orders[:20], nil // 返回前20条
}
```

### 4.3 后台查询优化

**方案: 同步到 ClickHouse / Elasticsearch**

```go
// 订单创建时同步到 ES
func CreateOrder(order *Order) error {
    // 1. 写入 MySQL 分片
    dbIdx, tblIdx := Route(order.OrderID)
    err := insertToMySQL(dbIdx, tblIdx, order)
    
    // 2. 异步同步到 ES（通过消息队列）
    kafka.Publish("order.created", order)
    
    return err
}

// ES 消费者
func SyncToES(order *Order) {
    esClient.Index("orders", order.OrderID, order)
}

// 后台复杂查询走 ES
func AdminSearch(query *SearchQuery) ([]*Order, error) {
    return esClient.Search("orders", query)
}
```

### 4.4 迁移方案

**从单库迁移到 8 库 64 表:**

```bash
# Step 1: 双写阶段（代码发布）
# 同时写入旧库和新分片

# Step 2: 历史数据迁移
# 使用 Canal 或 Debezium 同步 binlog
canal.start("old_db.orders", "shard_cluster")

# Step 3: 数据校验
# 校验旧库和新库数据一致性
python verify_data.py --source old_db --target shards

# Step 4: 切换读流量
# 灰度切换: 1% -> 10% -> 50% -> 100%
config.set("read_from_shards_percent", 100)

# Step 5: 下线旧库
# 停止双写，删除旧库访问代码
```

## 五、总结与建议

### 5.1 核心要点

1. **不要过早分库分表**: 单表 2000万内，单库 QPS 5000 内无需分库分表
2. **优先垂直拆分**: 成本低，风险小，解决大部分场景
3. **分片键选择至关重要**: 决定了 90% 的查询性能
4. **拥抱最终一致性**: 放弃强事务，换取线性扩展能力
5. **预留扩展空间**: 使用逻辑分片 + 物理库映射，方便后续扩容

### 5.2 技术选型建议

| 场景 | 推荐方案 |
|------|----------|
| Go 项目 | Gorm Sharding / 自研分片层 |
| Java 项目 | ShardingSphere-JDBC |
| 多语言团队 | ShardingSphere-Proxy / MyCat |
| 超大规模 | TiDB / OceanBase (分布式数据库) |

### 5.3 避坑指南

❌ **不要做的事:**
- 分片数不是 2 的幂次（如 10 个分片，扩容困难）
- 分片键选择低区分度字段（如 status）
- 使用跨片 JOIN 和分布式事务
- 深度分页（OFFSET 10000）

✅ **应该做的事:**
- 充分的性能测试和压测
- 完善的监控和报警
- 详细的 runbook（扩容、迁移、故障处理）
- 分片数预留 2-3 倍冗余（如当前够用 4 个分片，部署 8 个）

### 5.4 未来趋势

随着 **NewSQL** 和 **云原生数据库** 的成熟，传统的分库分表方案可能逐渐被替代：

- **TiDB**: 自动水平扩展，兼容 MySQL 协议
- **CockroachDB**: 全球分布式数据库
- **PolarDB-X / OceanBase**: 云厂商的分布式数据库
- **Vitess**: YouTube 开源的 MySQL 分库分表方案

**但在此之前，深入理解分库分表的原理和实践，仍然是每个后端工程师的必修课。**

---

## 六、动手实践：运行本仓库的压测案例

想要真实体验单库 vs 分库分表的性能差异？本仓库提供了完整的压测实现！

### 快速开始

```bash
# 方法 1: 使用 Makefile
make bench-sharding

# 方法 2: 使用脚本
./scripts/run-sharding-bench.sh

# 方法 3: 手动运行
# 1. 启动数据库
docker-compose up -d postgres shard_db_0 shard_db_1 shard_db_2 shard_db_3 shard_db_4 shard_db_5 shard_db_6 shard_db_7

# 2. 等待数据库启动
sleep 10

# 3. 运行压测
go run cmd/shardbench/main.go
```

### 压测内容

- ✅ **插入性能对比**: 100万订单写入速度
- ✅ **点查性能对比**: 按订单ID精确查询
- ✅ **范围查询对比**: 按用户ID查询订单列表
- ✅ **延迟分布对比**: P50/P95/P99 延迟统计
- ✅ **QPS对比**: 每秒请求数对比

### 期望结果

根据硬件配置不同，通常能看到：
- **插入性能**: 分库分表方案提升 **150-250%**
- **点查性能**: 分库分表方案提升 **200-400%**
- **范围查询**: 分库分表方案提升 **30-80%**

### 代码结构

```
├── cmd/shardbench/          # 压测程序入口
├── internal/
│   ├── model/order.go       # 订单模型
│   └── repository/
│       ├── order_single.go  # 单库实现
│       └── order_sharded.go # 分库分表实现
├── docker-compose.yml       # 9个数据库配置
└── docs/sharding-benchmark.md  # 详细文档
```

### 清理环境

```bash
make bench-sharding-clean
```

详细文档请查看: [docs/sharding-benchmark.md](../docs/sharding-benchmark.md)

---

**参考资料:**
- 《Designing Data-Intensive Applications》 - Martin Kleppmann
- 阿里云分库分表最佳实践: https://help.aliyun.com/document_detail/...
- ShardingSphere 官方文档: https://shardingsphere.apache.org/
- 《数据密集型应用系统设计》中文版
- **本仓库压测案例**: [docs/sharding-benchmark.md](../docs/sharding-benchmark.md)