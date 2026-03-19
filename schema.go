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
	// 保护 getModelInstance 慢路径的初始化，确保 columnNameCache 全部写入后才对外暴露指针
	modelInitMu sync.Mutex

	ErrColumnNotFound = errors.New("gplus: column name not found for pointer")
	ErrInvalidPointer = errors.New("gplus: argument must be a struct field pointer")
)

// unregisterModel 从缓存中移除指定模型的注册信息（仅供包内测试使用）。
// 适用场景：测试隔离，避免全局缓存在子测试间产生残留状态。
func unregisterModel[T any]() {
	typeStr := reflect.TypeOf((*T)(nil)).Elem().String()
	modelInitMu.Lock()
	defer modelInitMu.Unlock()
	v, ok := modelInstanceCache.LoadAndDelete(typeStr)
	if !ok {
		return
	}
	// 清理该实例对应的所有字段地址缓存
	baseAddr := reflect.ValueOf(v).Pointer()
	offsetMap := reflectStructSchema(v, "gorm", "COLUMN")
	for offset := range offsetMap {
		columnNameCache.Delete(baseAddr + offset) // 只清理值字段
	}
	// 清理指针嵌入字段注册的绝对地址
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr && !val.IsNil() {
		unregisterPtrEmbedFields(val.Elem(), "gorm", "COLUMN")
	}
	// 清理 reflectStructSchema 的 schema 级缓存（须在 reflectStructSchema 调用之后）
	columnCache.Delete(schemaCacheKey{typeStr, "gorm", "COLUMN"})
}

// registerPtrEmbedFields 运行时注册指针嵌入字段的绝对地址到列名映射。
// 指针嵌入的内层字段由独立堆分配，无法通过静态 baseAddr+offset 推算，
// 必须在实例分配后解引用指针字段，取得真实地址再注册。支持多层嵌套递归。
func registerPtrEmbedFields(val reflect.Value, tag, label string) {
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.Anonymous || f.Type.Kind() != reflect.Ptr || f.Type.Elem().Kind() != reflect.Struct {
			continue // 只处理指针匿名嵌入字段
		}
		fv := val.Field(i)
		if fv.IsNil() {
			continue // nil 无地址，跳过
		}
		inner := fv.Elem()                                                   // 解引用，拿到内层实例
		innerBaseAddr := inner.UnsafeAddr()                                  // 内层实例真实堆地址
		innerOffsetMap := reflectStructSchema(inner.Interface(), tag, label) // 内层字段 offset → 列名
		for offset, name := range innerOffsetMap {
			columnNameCache.Store(innerBaseAddr+offset, name) // 注册绝对地址
		}
		registerPtrEmbedFields(inner, tag, label) // 递归，支持多层嵌套
	}
}

// unregisterPtrEmbedFields 清理指针嵌入字段注册的绝对地址映射。
// 与 registerPtrEmbedFields 配对使用，确保 unregisterModel 完整清理缓存，不留悬空条目。
func unregisterPtrEmbedFields(val reflect.Value, tag, label string) {
	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// 不是指针匿名嵌入
		if !f.Anonymous || f.Type.Kind() != reflect.Ptr || f.Type.Elem().Kind() != reflect.Struct {
			continue
		}
		fv := val.Field(i)
		if fv.IsNil() {
			continue
		}
		inner := fv.Elem()
		innerBaseAddr := inner.UnsafeAddr()
		innerOffsetMap := reflectStructSchema(inner.Interface(), tag, label)
		for offset := range innerOffsetMap {
			columnNameCache.Delete(innerBaseAddr + offset) // 删除绝对地址条目
		}
		unregisterPtrEmbedFields(inner, tag, label) // 递归清理多层嵌套
	}
}

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

// RegisterModel 注册模型，解析并缓存字段映射关系。
// 通常在应用启动时显式调用，也可在首次查询时由框架自动触发。
// 并发安全：多个 goroutine 同时注册同一类型时，只有第一个写入者的
// 指针会成为规范单例，其余调用无副作用。
// 传入 nil（无类型 nil）或 typed-nil 指针时会被静默跳过。
func RegisterModel(models ...any) {
	for _, model := range models {
		// 跳过无类型 nil
		if model == nil {
			continue
		}
		val := reflect.ValueOf(model)
		t := reflect.TypeOf(model)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		// 必须是指针才能获取基地址
		if val.Kind() != reflect.Pointer {
			continue
		}

		// 跳过 typed-nil 指针，避免 baseAddr=0 污染缓存
		if val.IsNil() {
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
		// 注册指针嵌入字段的运行时绝对地址
		registerPtrEmbedFields(val.Elem(), "gorm", "COLUMN")
	}
}

// getModelInstance 获取泛型对应的规范单例指针（仅用于获取字段地址，不可修改值）。
// 并发安全：快速路径无锁；慢路径通过 modelInitMu 互斥，确保 columnNameCache
// 全部写入完成后才将指针写入 modelInstanceCache，消除半初始化竞态窗口。
func getModelInstance[T any]() *T {
	typeStr := reflect.TypeOf((*T)(nil)).Elem().String()

	// 快速路径：已注册直接返回，无锁
	if v, ok := modelInstanceCache.Load(typeStr); ok {
		return v.(*T)
	}

	// 慢路径：加锁，保证初始化原子完成
	modelInitMu.Lock()
	defer modelInitMu.Unlock()

	// 双重检查：加锁后再确认一次，防止重复初始化
	if v, ok := modelInstanceCache.Load(typeStr); ok {
		return v.(*T)
	}

	ptr := new(T)
	ptrVal := reflect.ValueOf(ptr).Elem()
	initPtrEmbeds(ptrVal)

	// 注册值字段的绝对地址
	baseAddr := reflect.ValueOf(ptr).Pointer()
	offsetMap := reflectStructSchema(ptr, "gorm", "COLUMN")
	for offset, name := range offsetMap {
		columnNameCache.Store(baseAddr+offset, name)
	}
	// 注册指针嵌入字段的运行时绝对地址
	registerPtrEmbedFields(ptrVal, "gorm", "COLUMN")

	// 所有 columnNameCache 条目写入完成后，才对外暴露指针
	modelInstanceCache.Store(typeStr, ptr)
	return ptr
}
