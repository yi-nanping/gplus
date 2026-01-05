package gplus

import (
	"testing"
	"time"
)

// BaseUser 模拟嵌入结构体
type BaseUser struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TestUser 模拟复杂的业务实体
type TestUser struct {
	BaseUser         // 嵌入字段
	Name     string  `gorm:"column:username"` // 自定义列名
	Age      int     `gorm:"column:age"`
	Email    string  // 默认驼峰转蛇形 -> email
	IsActive bool    `gorm:"column:is_active"`
	Score    float64 `gorm:"column:score"`
	Ignore   string  `gorm:"-"` // 忽略字段
}

// assertEqual 断言相等
func assertEqual(t testing.TB, expected, actual any, msg string) {
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// assertError 断言错误
func assertError(t testing.TB, err error, expectError bool, msg string) {
	if expectError && err == nil {
		t.Errorf("%s: expected error but got nil", msg)
	}
	if !expectError && err != nil {
		t.Errorf("%s: expected no error but got %v", msg, err)
	}
}
