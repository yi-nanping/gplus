package gplus

import (
	"errors"
	"reflect"
	"sync"
)

var (
	// 缓存字段指针地址 -> 列名
	columnNameCache sync.Map
	// 缓存类型名 -> 实例 (用于获取空结构体指针)
	modelInstanceCache sync.Map

	ErrColumnNotFound = errors.New("gplus: column name not found for pointer")
	ErrInvalidPointer = errors.New("gplus: argument must be a struct field pointer")
)

// resolveColumnName 解析字段名为数据库列名
func resolveColumnName(v any) (string, error) {
	if v == nil {
		return "", ErrInvalidPointer
	}

	// 如果直接传字符串，直接返回 但字符串不能为空
	if s, ok := v.(string); ok {
		if s == "" {
			return "", ErrInvalidPointer
		}
		return s, nil
	}

	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Pointer {
		return "", ErrInvalidPointer
	}

	// 尝试从缓存获取
	if name, ok := columnNameCache.Load(val.Pointer()); ok {
		return name.(string), nil
	}

	return "", ErrColumnNotFound
}

// registerModel 注册模型，解析并缓存字段映射关系。
// 通常在 Repository 初始化或首次查询时自动调用。
// 并发安全：多个 goroutine 同时注册同一类型时，只有第一个写入者的
// 指针会成为规范单例，其余调用无副作用。
func registerModel(models ...any) {
	for _, model := range models {
		val := reflect.ValueOf(model)
		t := reflect.TypeOf(model)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		// 必须是指针才能获取基地址
		if val.Kind() != reflect.Pointer {
			continue
		}

		modelName := t.String()

		// LoadOrStore 保证只有第一个写入者继续执行字段注册，
		// 后续并发调用直接返回，不会产生第二个规范指针。
		if _, loaded := modelInstanceCache.LoadOrStore(modelName, model); loaded {
			continue
		}

		// 仅第一个写入者执行：将偏移量转换为绝对地址并缓存
		baseAddr := val.Pointer()
		offsetMap := reflectStructSchema(model, "gorm", "COLUMN")
		for offset, name := range offsetMap {
			columnNameCache.Store(baseAddr+offset, name)
		}
	}
}

// getModelInstance 获取泛型对应的规范单例指针（仅用于获取字段地址，不可修改值）。
// 并发安全：通过 LoadOrStore 确保只有一个 new(T) 的结果成为单例。
func getModelInstance[T any]() *T {
	typeStr := reflect.TypeOf((*T)(nil)).Elem().String()

	// 快速路径：已注册直接返回
	if v, ok := modelInstanceCache.Load(typeStr); ok {
		return v.(*T)
	}

	// 慢路径：分配新实例，通过 LoadOrStore 竞争写入权
	ptr := new(T)
	actual, loaded := modelInstanceCache.LoadOrStore(typeStr, ptr)
	if loaded {
		// 另一个 goroutine 已写入，丢弃 ptr，使用已注册的单例
		return actual.(*T)
	}

	// 本 goroutine 赢得写入权，注册字段地址
	baseAddr := reflect.ValueOf(ptr).Pointer()
	offsetMap := reflectStructSchema(ptr, "gorm", "COLUMN")
	for offset, name := range offsetMap {
		columnNameCache.Store(baseAddr+offset, name)
	}
	return ptr
}
