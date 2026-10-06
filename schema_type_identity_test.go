package gplus

import (
	"context"
	"reflect"
	"strings"
	"testing"

	first "github.com/yi-nanping/gplus/internal/testmodels/first"
	second "github.com/yi-nanping/gplus/internal/testmodels/second"
	"gorm.io/gorm"
)

func TestSchema_TypeIdentity(t *testing.T) {
	if reflect.TypeOf(first.Record{}).String() != reflect.TypeOf(second.Record{}).String() {
		t.Fatal("测试模型须具有相同类型显示名")
	}

	reset := func(t *testing.T) {
		t.Helper()
		unregisterModel[first.Record]()
		unregisterModel[second.Record]()
		t.Cleanup(func() {
			unregisterModel[first.Record]()
			unregisterModel[second.Record]()
		})
	}

	t.Run("column_mapping", func(t *testing.T) {
		reset(t)
		a := reflectStructSchema(first.Record{}, "gorm", "COLUMN")
		b := reflectStructSchema(second.Record{}, "gorm", "COLUMN")
		nameA, _ := reflect.TypeOf(first.Record{}).FieldByName("Name")
		nameB, _ := reflect.TypeOf(second.Record{}).FieldByName("Name")
		if a[nameA.Offset] != "first_name" || b[nameB.Offset] != "second_name" {
			t.Fatalf("同名类型的列映射应独立: first=%q, second=%q", a[nameA.Offset], b[nameB.Offset])
		}
	})

	t.Run("implicit_registration", func(t *testing.T) {
		reset(t)
		defer func() {
			if v := recover(); v != nil {
				t.Errorf("不同导入路径的同名模型不应 panic: %v", v)
			}
		}()
		_, a := NewQuery[first.Record](context.Background())
		q, b := NewQuery[second.Record](context.Background())
		q.Eq(&b.Name, "B")
		var rows []second.Record
		result := newDryRunDB(t).Session(&gorm.Session{DryRun: true}).Model(b).Scopes(q.BuildQuery()).Find(&rows)
		if result.Error != nil || !strings.Contains(result.Statement.SQL.String(), `"second_name" = ?`) {
			t.Fatalf("第二个模型应使用独立字段映射: err=%v, sql=%s", result.Error, result.Statement.SQL.String())
		}
		if Model[first.Record]() != a || Model[second.Record]() != b {
			t.Fatal("每个模型类型应保留自己的规范实例")
		}
	})

	t.Run("explicit_registration", func(t *testing.T) {
		reset(t)
		a, b := new(first.Record), new(second.Record)
		RegisterModel(a, b)
		colA, errA := resolveColumnName(&a.Name)
		colB, errB := resolveColumnName(&b.Name)
		if errA != nil || errB != nil || colA != "first_name" || colB != "second_name" {
			t.Fatalf("显式注册应保留各自字段映射: first=%q/%v, second=%q/%v", colA, errA, colB, errB)
		}
		unregisterModel[first.Record]()
		if col, err := resolveColumnName(&b.Name); err != nil || col != "second_name" {
			t.Fatalf("注销一个类型不应影响另一个: column=%q, err=%v", col, err)
		}
	})

	t.Run("optimistic_lock_metadata", func(t *testing.T) {
		firstType, secondType := reflect.TypeOf(first.Record{}), reflect.TypeOf(second.Record{})
		versionFieldCache.Delete(firstType)
		versionFieldCache.Delete(secondType)
		t.Cleanup(func() {
			versionFieldCache.Delete(firstType)
			versionFieldCache.Delete(secondType)
		})
		a := getVersionField[first.Record]()
		b := getVersionField[second.Record]()
		if a == nil || b == nil {
			t.Fatal("两个模型都应识别到版本字段")
		}
		fieldB, _ := secondType.FieldByName("Version")
		if a.columnName != "first_version" || b.columnName != "second_version" || b.kind != reflect.Uint32 || b.offset != fieldB.Offset {
			t.Fatalf("版本元信息应按真实类型隔离: first=%+v, second=%+v", a, b)
		}
	})
}
