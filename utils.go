package gplus

import (
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm/schema"
)

// 提取 GORM 内部常用的缩写词
var commonInitialisms = []string{"API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML", "HTTP", "HTTPS", "ID", "IP", "JSON", "LHS", "QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SQL", "SSH", "TCP", "TLS", "TTL", "UDP", "UI", "UID", "UUID", "URI", "URL", "UTF8", "VM", "XML", "XMPP", "XSRF", "XSS"}
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

	// 1. 尝试从缓存读取 (Key 可以是 Type + Tag + Label 的组合)
	cacheKey := t.String() + tag + label
	if val, ok := columnCache.Load(cacheKey); ok {
		return val.(map[uintptr]string)
	}

	res := make(map[uintptr]string)
	parseFields(t, tag, label, 0, res)

	// 2. 存入缓存
	columnCache.Store(cacheKey, res)
	return res
}

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
				fieldType = fieldType.Elem()
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
				buf.WriteByte(v + 32)
			} else {
				if i > 0 && value[i-1] != '_' && value[i+1] != '_' {
					buf.WriteByte('_')
				}
				buf.WriteByte(v + 32)
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
		buf.WriteByte(lastChar + 32)
	} else {
		buf.WriteByte(lastChar)
	}
	return buf.String()
}
