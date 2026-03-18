package gplus

import (
	"reflect"
	"strings"
	"sync"

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
