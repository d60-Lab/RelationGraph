package repository

import (
    "context"
    "fmt"
    "math/rand"
    "testing"
    "time"

    "gorm.io/driver/sqlite"
    "gorm.io/gorm"

    "github.com/d60-Lab/gin-template/internal/model"
)

func setupRelBenchDB(b *testing.B) *gorm.DB {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil {
        b.Fatalf("open db: %v", err)
    }
    if err := db.AutoMigrate(&model.User{}, &model.Follow{}, &model.Fan{}); err != nil {
        b.Fatalf("migrate: %v", err)
    }
    return db
}

func BenchmarkFollowWrite_And_FanRedundancy(b *testing.B) {
    db := setupRelBenchDB(b)
    followRepo := NewFollowRepository(db)
    fanRepo := NewFanRepository(db)
    ctx := context.Background()

    // 预创建部分用户
    users := make([]model.User, 1000)
    for i := range users { users[i] = model.User{ID: fmt.Sprintf("u%04d", i), Username: fmt.Sprintf("u%04d", i), Email: fmt.Sprintf("u%04d@example.com", i), Password: "p"} }
    if err := db.Create(&users).Error; err != nil { b.Fatalf("seed users: %v", err) }

    rand.Seed(time.Now().UnixNano())
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        from := users[rand.Intn(len(users))].ID
        to := users[rand.Intn(len(users))].ID
        if from == to { continue }
        _ = followRepo.Create(ctx, from, to)
        _ = fanRepo.Create(ctx, to, from)
    }
}

func BenchmarkQueryFansAndFollowing(b *testing.B) {
    db := setupRelBenchDB(b)
    followRepo := NewFollowRepository(db)
    fanRepo := NewFanRepository(db)
    ctx := context.Background()

    // 构造：一个用户 U0 有 N 个粉丝，同时 U0 也关注 N 个用户
    const N = 5000
    u0 := model.User{ID: "u0", Username: "u0", Email: "u0@example.com", Password: "p"}
    _ = db.Create(&u0).Error
    for i := 1; i <= N; i++ {
        uid := fmt.Sprintf("u%v", i)
        _ = db.Create(&model.User{ID: uid, Username: uid, Email: uid+"@example.com", Password: "p"}).Error
        _ = followRepo.Create(ctx, uid, u0.ID)  // 关注 u0
        _ = fanRepo.Create(ctx, u0.ID, uid)     // 冗余到 fans
        _ = followRepo.Create(ctx, u0.ID, uid)  // u0 关注别人
        _ = fanRepo.Create(ctx, uid, u0.ID)
    }

    b.ResetTimer()
    b.Run("ListFans", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            _, _ = fanRepo.ListFans(ctx, u0.ID, 0, 50)
        }
    })

    b.Run("ListFollowing", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            _, _ = followRepo.ListFollowings(ctx, u0.ID, 0, 50)
        }
    })
}
