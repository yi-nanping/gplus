package gplus

import (
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"gorm.io/gorm/schema"
)

// 提取 GORM 内部常用的缩写词
var commonInitialisms = []string{"API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML", "HTTPS", "HTTP", "ID", "IP", "JSON", "LHS", "QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SQL", "SSH", "TCP", "TLS", "TTL", "UDP", "UI", "UID", "UUID", "URI", "URL", "UTF8", "VM", "XML", "XMPP", "XSRF", "XSS"}
var commonInitialismsReplacer *strings.Replacer

func init() {
	var oldnew []string
	for _, initialism := range commonInitialisms {
		// 替代 strings.Title: 将 "ID" 转换为 "Id"
		// 逻辑：第一个字母大写 + 其余部分小写
		upperFirst := string(initialism[0]) + strings.ToLower(initialism[1:])
		oldnew = append(oldnew, initialism, upperFirst)
	}
	commonInitialismsReplacer = strings.NewReplacer(oldnew...)
}

// 增加全局缓存，减少反射开销
var columnCache sync.Map

// schemaCacheKey 作为 columnCache 的复合 key，避免字符串拼接碰撞
type schemaCacheKey struct{ typeName, tag, label string }

// ColumnInfo 存储字段偏移量和列名的关系
type ColumnInfo struct {
	Offset     uintptr
	ColumnName string
}

// reflectStructSchema 通过类型缓存列名映射
func reflectStructSchema(model any, tag, label string) map[uintptr]string {
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 1. 尝试从缓存读取，使用结构体 key 避免字符串拼接碰撞
	cacheKey := schemaCacheKey{t.String(), tag, label}
	if val, ok := columnCache.Load(cacheKey); ok {
		return val.(map[uintptr]string)
	}

	res := make(map[uintptr]string)
	parseFields(t, tag, label, 0, res)

	// 2. LoadOrStore 防止并发重复写入，始终返回缓存中的权威副本
	actual, _ := columnCache.LoadOrStore(cacheKey, res)
	return actual.(map[uintptr]string)
}

// initPtrEmbeds 递归初始化结构体中所有 nil 指针匿名嵌入字段。
// 确保规范单例的所有指针嵌入字段均已分配，以便在运行时获取真实内存地址。
func initPtrEmbeds(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		// 获取字段类型
		f := t.Field(i)
		// 是匿名嵌入 && 是指针类型 && 指针指向结构体
		if !f.Anonymous || f.Type.Kind() != reflect.Ptr || f.Type.Elem().Kind() != reflect.Struct {
			continue
		}
		// 获取字段值
		fv := v.Field(i)
		if fv.IsNil() {
			// 分配新实例
			fv.Set(reflect.New(f.Type.Elem()))
		}
		// 递归处理
		initPtrEmbeds(fv.Elem())
	}
}

// parseFields 解析字段
func parseFields(t reflect.Type, tag, label string, baseOffset uintptr, res map[uintptr]string) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 排除非导出字段
		if !field.IsExported() {
			continue
		}

		tagVal := field.Tag.Get(tag)
		tagSetting := schema.ParseTagSetting(tagVal, ";")

		// 处理忽略符号
		if _, ok := tagSetting["-"]; ok {
			continue
		}

		// 计算当前字段相对于根结构体的偏移量
		currentOffset := baseOffset + field.Offset

		// 递归处理：匿名嵌套或标记为 EMBEDDED 的字段
		_, isEmbedded := tagSetting["EMBEDDED"]
		if field.Anonymous || isEmbedded {
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				// 指针嵌入字段（如 *EmbedStruct）：内层字段的真实地址由独立堆分配决定，
				// 无法通过外层结构体 baseAddr+offset 推算，跳过以避免错误地址映射。
				continue
			}
			if fieldType.Kind() == reflect.Struct {
				parseFields(fieldType, tag, label, currentOffset, res)
			}
			continue
		}

		// 解析列名
		columnName := ""
		// 如果标签中存在指定标签，则使用标签中的值，否则使用默认的命名转换规则
		if val, ok := tagSetting[strings.ToUpper(label)]; ok {
			columnName = val
		} else {
			// 这里的命名转换可以根据需要替换
			columnName = nsColumnName(field.Name)
		}
		res[currentOffset] = columnName
	}
}

// === 乐观锁 ===

// versionFieldInfo 记录模型中乐观锁字段的元信息。
type versionFieldInfo struct {
	offset     uintptr      // 相对于结构体基地址的偏移量
	columnName string       // DB 列名
	kind       reflect.Kind // 字段类型（仅支持整数族）
}

var (
	versionFieldCache sync.Map
	// noVersionSentinel 表示"已扫描，无 version 字段"，避免 nil interface 歧义。
	noVersionSentinel = &versionFieldInfo{}
)

// findVersionField 递归扫描结构体，返回标注 `gplus:"version"` 的字段信息。
func findVersionField(t reflect.Type, baseOffset uintptr) *versionFieldInfo {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		currentOffset := baseOffset + field.Offset

		// 递归处理非指针匿名嵌入字段（与 parseFields 保持一致）
		if field.Anonymous {
			ft := field.Type
			if ft.Kind() == reflect.Ptr {
				continue // 指针嵌入，偏移量不可推算
			}
			if ft.Kind() == reflect.Struct {
				if info := findVersionField(ft, currentOffset); info != nil {
					return info
				}
			}
			continue
		}

		if field.Tag.Get("gplus") != "version" {
			continue
		}

		kind := field.Type.Kind()
		switch kind {
		case reflect.Int, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint32, reflect.Uint64:
		default:
			continue // 不支持的类型，忽略
		}

		tagSetting := schema.ParseTagSetting(field.Tag.Get("gorm"), ";")
		colName := tagSetting["COLUMN"]
		if colName == "" {
			colName = nsColumnName(field.Name)
		}
		return &versionFieldInfo{offset: currentOffset, columnName: colName, kind: kind}
	}
	return nil
}

// getVersionField 返回类型 T 的乐观锁字段信息，无 version 字段时返回 nil。结果缓存，并发安全。
func getVersionField[T any]() *versionFieldInfo {
	typeStr := reflect.TypeOf((*T)(nil)).Elem().String()
	if v, ok := versionFieldCache.Load(typeStr); ok {
		info := v.(*versionFieldInfo)
		if info == noVersionSentinel {
			return nil
		}
		return info
	}
	t := reflect.TypeOf((*T)(nil)).Elem()
	info := findVersionField(t, 0)
	if info == nil {
		versionFieldCache.Store(typeStr, noVersionSentinel)
		return nil
	}
	versionFieldCache.Store(typeStr, info)
	return info
}

// readVersionValue 从 entity 指针读取 version 字段值（统一返回 int64）。
func readVersionValue(entityPtr unsafe.Pointer, vInfo *versionFieldInfo) int64 {
	ptr := unsafe.Add(entityPtr, vInfo.offset)
	switch vInfo.kind {
	case reflect.Int:
		return int64(*(*int)(ptr))
	case reflect.Int32:
		return int64(*(*int32)(ptr))
	case reflect.Int64:
		return *(*int64)(ptr)
	case reflect.Uint:
		return int64(*(*uint)(ptr))
	case reflect.Uint32:
		return int64(*(*uint32)(ptr))
	case reflect.Uint64:
		return int64(*(*uint64)(ptr))
	default:
		return 0
	}
}

// writeVersionValue 向 entity 指针写入新的 version 字段值。
func writeVersionValue(entityPtr unsafe.Pointer, vInfo *versionFieldInfo, newVal int64) {
	ptr := unsafe.Add(entityPtr, vInfo.offset)
	switch vInfo.kind {
	case reflect.Int:
		*(*int)(ptr) = int(newVal)
	case reflect.Int32:
		*(*int32)(ptr) = int32(newVal)
	case reflect.Int64:
		*(*int64)(ptr) = newVal
	case reflect.Uint:
		*(*uint)(ptr) = uint(newVal)
	case reflect.Uint32:
		*(*uint32)(ptr) = uint32(newVal)
	case reflect.Uint64:
		*(*uint64)(ptr) = uint64(newVal)
	}
}

// buildUpdateMap 从 entity 提取非零字段构建 map，供 Updates(map) 使用。
// 排除主键字段和 version 字段（version 由调用方追加 gorm.Expr 表达式）。
func buildUpdateMap(entity any, vInfo *versionFieldInfo) map[string]any {
	t := reflect.TypeOf(entity)
	v := reflect.ValueOf(entity)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		v = v.Elem()
	}
	offsetMap := reflectStructSchema(entity, "gorm", "COLUMN")
	result := make(map[string]any)
	fillUpdateMap(t, v, 0, offsetMap, vInfo.offset, result)
	return result
}

// fillUpdateMap 递归遍历结构体字段，将非零、非主键、非 version 的字段填入 result。
func fillUpdateMap(t reflect.Type, v reflect.Value, baseOffset uintptr, offsetMap map[uintptr]string, excludeVersionOffset uintptr, result map[string]any) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		currentOffset := baseOffset + field.Offset
		fv := v.Field(i)

		// 递归处理非指针匿名嵌入字段
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			fillUpdateMap(field.Type, fv, currentOffset, offsetMap, excludeVersionOffset, result)
			continue
		}

		if currentOffset == excludeVersionOffset {
			continue // 跳过 version 字段，由调用方追加 Expr
		}

		colName, ok := offsetMap[currentOffset]
		if !ok {
			continue
		}

		// 排除主键字段（不放进 SET）
		tagSetting := schema.ParseTagSetting(field.Tag.Get("gorm"), ";")
		if _, isPK := tagSetting["PRIMARYKEY"]; isPK {
			continue
		}

		if fv.IsZero() {
			continue
		}

		result[colName] = fv.Interface()
	}
}

// nsColumnName 驼峰转下划线
func nsColumnName(name string) string {
	return toDBName(name)
}

// toDBName 将结构体字段名转换为蛇形命名 (复刻 GORM 逻辑)
func toDBName(name string) string {
	if name == "" {
		return ""
	}

	// 1. 处理缩写词替换，如 UserID -> UserId
	value := commonInitialismsReplacer.Replace(name)

	var (
		buf        strings.Builder
		lastCase   bool
		nextCase   bool
		nextNumber bool
		curCase    = value[0] <= 'Z' && value[0] >= 'A'
	)

	buf.Grow(len(value) + 4)

	for i := 0; i < len(value)-1; i++ {
		v := value[i]
		nextCase = value[i+1] <= 'Z' && value[i+1] >= 'A'
		nextNumber = value[i+1] >= '0' && value[i+1] <= '9'

		if curCase {
			if lastCase && (nextCase || nextNumber) {
				buf.WriteByte(v + ('a' - 'A'))
			} else {
				if i > 0 && value[i-1] != '_' && value[i+1] != '_' {
					buf.WriteByte('_')
				}
				buf.WriteByte(v + ('a' - 'A'))
			}
		} else {
			buf.WriteByte(v)
		}

		lastCase = curCase
		curCase = nextCase
	}

	lastChar := value[len(value)-1]
	if curCase {
		if !lastCase && len(value) > 1 {
			buf.WriteByte('_')
		}
		buf.WriteByte(lastChar + ('a' - 'A'))
	} else {
		buf.WriteByte(lastChar)
	}
	return buf.String()
}
