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
	// modelLockMap 用于确保每个模型解析的原子性
	modelLockMap sync.Map

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

// registerModel 注册模型，解析并缓存字段映射关系
// 通常在 Repository 初始化或首次查询时自动调用
func registerModel(models ...any) {
	for _, model := range models {
		t := reflect.TypeOf(model)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		modelName := t.String()
		// 双重检查，避免重复解析
		if _, loaded := modelInstanceCache.Load(modelName); loaded {
			continue
		}

		// 2. 使用命名锁确保原子性
		lock, _ := modelLockMap.LoadOrStore(modelName, &sync.Once{})
		lock.(*sync.Once).Do(func() {
			// 3. 解析字段映射关系
			colMap := reflectStructSchema(model, "gorm", "COLUMN")
			for ptr, name := range colMap {
				columnNameCache.Store(ptr, name)
			}
			modelInstanceCache.Store(modelName, model)
		})
	}
}

// getModelInstance 获取泛型对应的实例（用于获取字段指针）
// 注意：返回的是单例，仅用于获取字段地址，不可修改值
func getModelInstance[T any]() *T {
	// 获取 T 的类型字符串
	dummy := new(T)
	typeStr := reflect.TypeOf(dummy).Elem().String()

	if v, ok := modelInstanceCache.Load(typeStr); ok {
		return v.(*T)
	}

	// 首次访问，注册模型
	registerModel(dummy)
	return dummy
}
