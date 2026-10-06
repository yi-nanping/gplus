package gplus

// Eq 为字段和值添加编译期类型约束，并返回原构建器。
// field 必须来自 NewQuery、NewUpdater 或同链 As 返回的模型实例。
// 其余字段校验、错误累积和执行行为与构建器的 Eq 方法一致。
func Eq[V any, B interface{ Eq(any, any) B }](b B, field *V, value V) B {
	return b.Eq(field, value)
}

// In 为字段和候选值切片添加编译期类型约束，并返回原构建器。
// field 必须来自 NewQuery、NewUpdater 或同链 As 返回的模型实例。
// 空切片及字段校验沿用构建器的 In 方法。
func In[V any, B interface{ In(any, any) B }](b B, field *V, values []V) B {
	return b.In(field, values)
}

// Set 为更新字段和值添加编译期类型约束，并返回原更新构建器。
// field 必须来自 NewUpdater 或同链 As 返回的模型实例；零值照常写入。
func Set[T, V any](u *Updater[T], field *V, value V) *Updater[T] {
	return u.Set(field, value)
}
