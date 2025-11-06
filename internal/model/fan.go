package model

import "time"

// Fan 粉丝关系（B 的粉丝是 A）冗余自 Follow
type Fan struct {
    ID     string    `gorm:"primaryKey;type:varchar(36)"`
    UserID string    `gorm:"type:varchar(36);index:idx_fan_user;index:idx_fan_pair,unique;not null"`
    FanID  string    `gorm:"type:varchar(36);not null;index:idx_fan_pair,unique"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

func (Fan) TableName() string { return "fans" }
