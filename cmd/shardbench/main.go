package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/d60-Lab/gin-template/internal/model"
	"github.com/d60-Lab/gin-template/internal/repository"
)

const (
	// 测试参数
	UserCount       = 10000   // 1万用户
	OrdersPerUser   = 10      // 每个用户10个订单
	BenchDuration   = 30      // 查询压测时长（秒）
	ConcurrentLevel = 100     // 并发数
	
	// 数据库连接参数
	SingleDBPort = 5434
	ShardDBStartPort = 5440
)

type BenchResult struct {
	Name            string
	Duration        time.Duration
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	QPS             float64
	AvgLatency      time.Duration
	P50Latency      time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration
	Latencies       []time.Duration
}

func main() {
	ctx := context.Background()
	
	fmt.Println("===== 分库分表性能压测 =====")
	fmt.Printf("用户数: %d\n", UserCount)
	fmt.Printf("每用户订单数: %d\n", OrdersPerUser)
	fmt.Printf("总订单数: %d (单库+分库 = %d)\n", UserCount*OrdersPerUser, UserCount*OrdersPerUser*2)
	fmt.Printf("查询压测时长: 每场景 %d秒\n", BenchDuration)
	fmt.Printf("并发数: %d\n", ConcurrentLevel)
	fmt.Printf("\n⏱️  预计总耗时: 8-15分钟（插入约10分钟 + 查询约3分钟）\n")
	fmt.Printf("💡 如需快速测试，请修改 UserCount = 1000\n\n")
	
	// ========== 单库压测 ==========
	fmt.Println(">>> 准备单库环境...")
	singleRepo := prepareSingleDB()
	if singleRepo == nil {
		fmt.Println("单库初始化失败")
		return
	}
	defer singleRepo.Close()
	
	fmt.Println(">>> 生成单库测试数据...")
	singleOrders := generateTestOrders()
	fmt.Printf("生成了 %d 个测试订单\n\n", len(singleOrders))
	
	fmt.Println("===== 单库压测 - 插入订单 =====")
	singleInsertResult := benchInsert(ctx, singleRepo, singleOrders, "单库")
	printBenchResult(singleInsertResult)
	
	if singleInsertResult.FailedRequests > 0 {
		fmt.Printf("⚠️  警告：有 %d 个插入失败，查询测试可能不准确\n", singleInsertResult.FailedRequests)
	}
	
	time.Sleep(1 * time.Second)
	
	fmt.Println("\n===== 单库压测 - 按订单ID查询 =====")
	singleQueryByIDResult := benchQueryByOrderID(ctx, singleRepo, singleOrders, "单库")
	printBenchResult(singleQueryByIDResult)
	
	fmt.Println("\n===== 单库压测 - 按用户ID查询 =====")
	singleQueryByUserResult := benchQueryByUserID(ctx, singleRepo, "单库")
	printBenchResult(singleQueryByUserResult)
	
	// 清理单库数据
	fmt.Println("\n>>> 清理单库数据...")
	singleRepo.Close()
	
	// ========== 分库分表压测 ==========
	fmt.Println("\n>>> 准备分库分表环境...")
	shardedRepo := prepareShardedDB()
	if shardedRepo == nil {
		fmt.Println("分库分表初始化失败")
		return
	}
	defer shardedRepo.Close()
	
	fmt.Println(">>> 生成分库分表测试数据...")
	shardedOrders := generateTestOrders()
	fmt.Printf("生成了 %d 个测试订单\n\n", len(shardedOrders))
	
	fmt.Println("===== 分库分表压测 - 插入订单 =====")
	shardedInsertResult := benchInsert(ctx, shardedRepo, shardedOrders, "分库分表")
	printBenchResult(shardedInsertResult)
	
	if shardedInsertResult.FailedRequests > 0 {
		fmt.Printf("⚠️  警告：有 %d 个插入失败，查询测试可能不准确\n", shardedInsertResult.FailedRequests)
	}
	
	time.Sleep(1 * time.Second)
	
	fmt.Println("\n===== 分库分表压测 - 按订单ID查询 =====")
	shardedQueryByIDResult := benchQueryByOrderID(ctx, shardedRepo, shardedOrders, "分库分表")
	printBenchResult(shardedQueryByIDResult)
	
	fmt.Println("\n===== 分库分表压测 - 按用户ID查询 =====")
	shardedQueryByUserResult := benchQueryByUserID(ctx, shardedRepo, "分库分表")
	printBenchResult(shardedQueryByUserResult)
	
	// ========== 打印对比总结 ==========
	fmt.Println("\n===== 性能对比总结 =====")
	printComparison("插入订单", singleInsertResult, shardedInsertResult)
	printComparison("按订单ID查询", singleQueryByIDResult, shardedQueryByIDResult)
	printComparison("按用户ID查询", singleQueryByUserResult, shardedQueryByUserResult)
	
	fmt.Println("\n✅ 压测完成！")
}

// prepareSingleDB 准备单库环境
func prepareSingleDB() repository.OrderRepository {
	dsn := fmt.Sprintf("host=localhost user=postgres password=postgres dbname=gin_template port=%d sslmode=disable", SingleDBPort)
	
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Printf("连接单库失败: %v\n", err)
		return nil
	}
	
	// 设置连接池
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(200)
	sqlDB.SetMaxIdleConns(50)
	
	repo := repository.NewSingleDBOrderRepository(db)
	
	// 清理旧数据
	db.Exec("DROP TABLE IF EXISTS orders")
	
	// 初始化表结构
	if err := repo.(*repository.SingleDBOrderRepository).InitSchema(); err != nil {
		fmt.Printf("初始化单库表结构失败: %v\n", err)
		return nil
	}
	
	fmt.Println("单库环境准备完成")
	return repo
}

// prepareShardedDB 准备分库分表环境
func prepareShardedDB() repository.OrderRepository {
	var dbs []*gorm.DB
	
	for i := 0; i < repository.ShardCount; i++ {
		port := ShardDBStartPort + i
		dbName := fmt.Sprintf("orders_shard_%d", i)
		dsn := fmt.Sprintf("host=localhost user=postgres password=postgres dbname=%s port=%d sslmode=disable", dbName, port)
		
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			fmt.Printf("连接分片数据库 %d 失败: %v\n", i, err)
			return nil
		}
		
		// 设置连接池
		sqlDB, _ := db.DB()
		sqlDB.SetMaxOpenConns(150)
		sqlDB.SetMaxIdleConns(30)
		
		dbs = append(dbs, db)
		
		// 清理旧数据
		for j := 0; j < repository.TableCount; j++ {
			tableName := fmt.Sprintf("orders_%d", j)
			db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
		}
	}
	
	repo, err := repository.NewShardedOrderRepository(dbs)
	if err != nil {
		fmt.Printf("创建分库分表仓储失败: %v\n", err)
		return nil
	}
	
	// 初始化表结构
	if err := repo.(*repository.ShardedOrderRepository).InitSchema(); err != nil {
		fmt.Printf("初始化分库分表表结构失败: %v\n", err)
		return nil
	}
	
	fmt.Println("分库分表环境准备完成")
	return repo
}

// generateTestOrders 生成测试订单数据
func generateTestOrders() []*model.Order {
	orders := make([]*model.Order, 0, UserCount*OrdersPerUser)
	baseTime := time.Now().Add(-30 * 24 * time.Hour) // 从30天前开始
	
	for userID := int64(1); userID <= UserCount; userID++ {
		for i := 0; i < OrdersPerUser; i++ {
			orderID := userID*1000 + int64(i) // 简单的订单ID生成规则
			order := &model.Order{
				OrderID:   orderID,
				UserID:    userID,
				Amount:    float64(rand.Intn(10000)) / 100.0, // 0-100元
				Status:    int8(rand.Intn(5)),
				CreatedAt: baseTime.Add(time.Duration(rand.Intn(30*24*60)) * time.Minute),
			}
			orders = append(orders, order)
		}
	}
	
	return orders
}

// benchInsert 压测插入性能（插入所有数据，不限制时间）
func benchInsert(ctx context.Context, repo repository.OrderRepository, orders []*model.Order, name string) *BenchResult {
	var (
		totalRequests   int64
		successRequests int64
		failedRequests  int64
		latencies       []time.Duration
		latencyMu       sync.Mutex
		wg              sync.WaitGroup
	)
	
	totalOrders := int64(len(orders))
	fmt.Printf("开始插入 %d 个订单...\n", totalOrders)
	
	startTime := time.Now()
	
	// 启动进度显示 goroutine
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(&totalRequests)
				if current == 0 {
					continue
				}
				elapsed := time.Since(startTime)
				progress := float64(current) / float64(totalOrders) * 100
				qps := float64(current) / elapsed.Seconds()
				remaining := float64(totalOrders-current) / qps
				fmt.Printf("  📊 进度: %d/%d (%.1f%%) | ⏱️  已用时: %v | ⏳ 预计剩余: %.0f秒 | 🚀 QPS: %.0f\n",
					current, totalOrders, progress, elapsed.Round(time.Second), remaining, qps)
			case <-progressDone:
				return
			}
		}
	}()
	
	// 启动多个并发 goroutine，插入所有数据
	for i := 0; i < ConcurrentLevel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			
			// 每个 worker 处理自己的订单
			for orderIndex := workerID; orderIndex < len(orders); orderIndex += ConcurrentLevel {
				order := orders[orderIndex]
				
				reqStart := time.Now()
				err := repo.Create(ctx, order)
				latency := time.Since(reqStart)
				
				atomic.AddInt64(&totalRequests, 1)
				if err != nil {
					atomic.AddInt64(&failedRequests, 1)
					// 打印前10个错误帮助调试
					if failedRequests <= 10 {
						fmt.Printf("插入失败 [%d]: %v (order_id=%d, user_id=%d)\n", 
							failedRequests, err, order.OrderID, order.UserID)
					}
				} else {
					atomic.AddInt64(&successRequests, 1)
				}
				
				latencyMu.Lock()
				latencies = append(latencies, latency)
				latencyMu.Unlock()
			}
		}(i)
	}
	
	// 等待所有 worker 完成
	wg.Wait()
	
	// 停止进度显示
	close(progressDone)
	
	duration := time.Since(startTime)
	
	fmt.Printf("✅ 插入完成！耗时: %v\n", duration.Round(time.Second))
	
	return calculateResult(name, duration, totalRequests, successRequests, failedRequests, latencies)
}

// benchQueryByOrderID 压测按订单ID查询
func benchQueryByOrderID(ctx context.Context, repo repository.OrderRepository, orders []*model.Order, name string) *BenchResult {
	var (
		totalRequests   int64
		successRequests int64
		failedRequests  int64
		latencies       []time.Duration
		latencyMu       sync.Mutex
		wg              sync.WaitGroup
	)
	
	// 只使用成功插入的订单ID
	validOrders := orders
	if len(validOrders) == 0 {
		fmt.Println("⚠️  没有可查询的订单数据")
		return &BenchResult{Name: name}
	}
	
	// 限制查询数据集大小，避免超时
	if len(validOrders) > 1000 {
		validOrders = validOrders[:1000]
	}
	
	fmt.Printf("使用 %d 个订单进行查询测试（将运行 %d 秒）...\n", len(validOrders), BenchDuration)
	
	startTime := time.Now()
	stopTime := startTime.Add(BenchDuration * time.Second)
	
	// 启动进度显示 goroutine
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(&totalRequests)
				success := atomic.LoadInt64(&successRequests)
				elapsed := time.Since(startTime)
				remaining := BenchDuration - int(elapsed.Seconds())
				if remaining < 0 {
					remaining = 0
				}
				qps := float64(current) / elapsed.Seconds()
				successRate := float64(success) / float64(current) * 100
				fmt.Printf("  📊 查询中: %d 请求 | ✅ 成功率: %.1f%% | ⏱️  已运行: %ds | ⏳ 剩余: %ds | 🚀 QPS: %.0f\n",
					current, successRate, int(elapsed.Seconds()), remaining, qps)
			case <-progressDone:
				return
			}
		}
	}()
	
	// 启动多个并发 goroutine
	for i := 0; i < ConcurrentLevel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for time.Now().Before(stopTime) {
				// 随机选择一个订单ID查询
				order := validOrders[rand.Intn(len(validOrders))]
				
				reqStart := time.Now()
				_, err := repo.GetByOrderID(ctx, order.OrderID)
				latency := time.Since(reqStart)
				
				atomic.AddInt64(&totalRequests, 1)
				if err != nil {
					atomic.AddInt64(&failedRequests, 1)
					// 打印前几个错误
					if failedRequests <= 3 {
						fmt.Printf("查询失败 [%d]: %v (order_id=%d)\n", failedRequests, err, order.OrderID)
					}
				} else {
					atomic.AddInt64(&successRequests, 1)
				}
				
				latencyMu.Lock()
				latencies = append(latencies, latency)
				latencyMu.Unlock()
			}
		}()
	}
	
	wg.Wait()
	
	// 停止进度显示
	close(progressDone)
	
	duration := time.Since(startTime)
	
	return calculateResult(name, duration, totalRequests, successRequests, failedRequests, latencies)
}

// benchQueryByUserID 压测按用户ID查询
func benchQueryByUserID(ctx context.Context, repo repository.OrderRepository, name string) *BenchResult {
	var (
		totalRequests   int64
		successRequests int64
		failedRequests  int64
		latencies       []time.Duration
		latencyMu       sync.Mutex
		wg              sync.WaitGroup
	)
	
	fmt.Printf("开始按用户ID查询测试（将运行 %d 秒）...\n", BenchDuration)
	
	startTime := time.Now()
	stopTime := startTime.Add(BenchDuration * time.Second)
	
	// 启动进度显示 goroutine
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				current := atomic.LoadInt64(&totalRequests)
				success := atomic.LoadInt64(&successRequests)
				elapsed := time.Since(startTime)
				remaining := BenchDuration - int(elapsed.Seconds())
				if remaining < 0 {
					remaining = 0
				}
				qps := float64(current) / elapsed.Seconds()
				successRate := float64(success) / float64(current) * 100
				fmt.Printf("  📊 查询中: %d 请求 | ✅ 成功率: %.1f%% | ⏱️  已运行: %ds | ⏳ 剩余: %ds | 🚀 QPS: %.0f\n",
					current, successRate, int(elapsed.Seconds()), remaining, qps)
			case <-progressDone:
				return
			}
		}
	}()
	
	// 启动多个并发 goroutine
	for i := 0; i < ConcurrentLevel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			for time.Now().Before(stopTime) {
				// 随机选择一个用户ID查询
				userID := int64(rand.Intn(UserCount) + 1)
				
				reqStart := time.Now()
				_, err := repo.GetByUserID(ctx, userID, 20)
				latency := time.Since(reqStart)
				
				atomic.AddInt64(&totalRequests, 1)
				if err != nil {
					atomic.AddInt64(&failedRequests, 1)
				} else {
					atomic.AddInt64(&successRequests, 1)
				}
				
				latencyMu.Lock()
				latencies = append(latencies, latency)
				latencyMu.Unlock()
			}
		}()
	}
	
	wg.Wait()
	
	// 停止进度显示
	close(progressDone)
	
	duration := time.Since(startTime)
	
	return calculateResult(name, duration, totalRequests, successRequests, failedRequests, latencies)
}

// calculateResult 计算压测结果
func calculateResult(name string, duration time.Duration, total, success, failed int64, latencies []time.Duration) *BenchResult {
	// 计算 QPS
	qps := float64(total) / duration.Seconds()
	
	// 计算平均延迟
	var totalLatency time.Duration
	for _, l := range latencies {
		totalLatency += l
	}
	avgLatency := totalLatency / time.Duration(len(latencies))
	
	// 计算百分位延迟
	sortedLatencies := make([]time.Duration, len(latencies))
	copy(sortedLatencies, latencies)
	sortLatencies(sortedLatencies)
	
	p50 := percentile(sortedLatencies, 0.50)
	p95 := percentile(sortedLatencies, 0.95)
	p99 := percentile(sortedLatencies, 0.99)
	
	return &BenchResult{
		Name:            name,
		Duration:        duration,
		TotalRequests:   total,
		SuccessRequests: success,
		FailedRequests:  failed,
		QPS:             qps,
		AvgLatency:      avgLatency,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		Latencies:       sortedLatencies,
	}
}

// sortLatencies 对延迟列表排序
func sortLatencies(latencies []time.Duration) {
	// 简单的冒泡排序（对于大数据集应使用快速排序）
	n := len(latencies)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if latencies[j] > latencies[j+1] {
				latencies[j], latencies[j+1] = latencies[j+1], latencies[j]
			}
		}
	}
}

// percentile 计算百分位数
func percentile(sortedLatencies []time.Duration, p float64) time.Duration {
	if len(sortedLatencies) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(sortedLatencies)) * p)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedLatencies) {
		index = len(sortedLatencies) - 1
	}
	return sortedLatencies[index]
}

// printBenchResult 打印压测结果
func printBenchResult(result *BenchResult) {
	fmt.Printf("名称: %s\n", result.Name)
	fmt.Printf("耗时: %v\n", result.Duration)
	fmt.Printf("总请求数: %d\n", result.TotalRequests)
	fmt.Printf("成功请求: %d\n", result.SuccessRequests)
	fmt.Printf("失败请求: %d\n", result.FailedRequests)
	fmt.Printf("QPS: %.2f\n", result.QPS)
	fmt.Printf("平均延迟: %v\n", result.AvgLatency)
	fmt.Printf("P50 延迟: %v\n", result.P50Latency)
	fmt.Printf("P95 延迟: %v\n", result.P95Latency)
	fmt.Printf("P99 延迟: %v\n", result.P99Latency)
}

// printComparison 打印对比结果
func printComparison(operation string, single, sharded *BenchResult) {
	fmt.Printf("\n--- %s ---\n", operation)
	fmt.Printf("单库 QPS: %.2f\n", single.QPS)
	fmt.Printf("分库 QPS: %.2f\n", sharded.QPS)
	improvement := (sharded.QPS - single.QPS) / single.QPS * 100
	fmt.Printf("性能提升: %.2f%%\n", improvement)
	fmt.Printf("单库 P95: %v\n", single.P95Latency)
	fmt.Printf("分库 P95: %v\n", sharded.P95Latency)
	
	if sharded.QPS > single.QPS {
		fmt.Printf("✅ 分库分表方案更优\n")
	} else {
		fmt.Printf("⚠️  单库方案更优\n")
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
