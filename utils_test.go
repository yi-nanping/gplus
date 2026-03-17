package gplus

import (
	"reflect"
	"sync"
	"testing"
)

// utils_test.go 专属结构体，避免与 model_test.go 中的 TestUser 冲突

type utilsSimple struct {
	Name string `gorm:"column:name"`
	Age  int    `gorm:"column:age"`
}

type utilsNoTag struct {
	FirstName string
	LastAge   int
}

type utilsIgnore struct {
	Keep string `gorm:"column:keep"`
	Drop string `gorm:"-"`
}

type UtilsEmbedBase struct {
	ID   int64  `gorm:"column:id"`
	Code string `gorm:"column:code"`
}

type utilsEmbedded struct {
	UtilsEmbedBase
	Extra string `gorm:"column:extra"`
}

// UtilsEmbedBaseSmall 仅含一个字段，避免指针嵌入时偏移量与外层字段碰撞
type UtilsEmbedBaseSmall struct {
	ID int64 `gorm:"column:id"`
}

type utilsPtrEmbed struct {
	*UtilsEmbedBaseSmall
	Note string `gorm:"column:note"`
}

type utilsTagEmbed struct {
	Inner UtilsEmbedBase `gorm:"embedded"`
	Label string         `gorm:"column:label"`
}

func TestToDBName(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"空字符串", "", ""},
		{"单个小写字母", "a", "a"},
		{"单个大写字母", "A", "a"},
		{"两个大写字母", "AB", "ab"},
		{"简单驼峰", "UserName", "user_name"},
		{"首字母小写驼峰", "userName", "user_name"},
		{"已是蛇形", "user_name", "user_name"},
		{"缩写词UserID", "UserID", "user_id"},
		{"缩写词APIKey", "APIKey", "api_key"},
		{"缩写词HTTPSConfig", "HTTPSConfig", "https_config"},
		{"缩写词XMLParser", "XMLParser", "xml_parser"},
		{"内嵌缩写词MyXMLParser", "MyXMLParser", "my_xml_parser"},
		{"数字后缀Version2", "Version2", "version2"},
		{"数字中间V2Ray", "V2Ray", "v2_ray"},
		{"纯缩写ID", "ID", "id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toDBName(tc.input)
			assertEqual(t, tc.expected, got, "toDBName("+tc.input+")")
		})
	}
}

func TestNsColumnName(t *testing.T) {
	samples := []string{"UserName", "APIKey", "HTTPSConfig"}
	for _, s := range samples {
		t.Run(s, func(t *testing.T) {
			assertEqual(t, toDBName(s), nsColumnName(s), "nsColumnName("+s+")")
		})
	}
}

func TestReflectStructSchema(t *testing.T) {
	t.Run("简单结构体tag列名", func(t *testing.T) {
		ty := reflect.TypeOf(utilsSimple{})
		key := schemaCacheKey{ty.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m := reflectStructSchema(utilsSimple{}, "gorm", "column")
		assertEqual(t, 2, len(m), "字段数量")
		assertEqual(t, "name", m[ty.Field(0).Offset], "Name 列名")
		assertEqual(t, "age", m[ty.Field(1).Offset], "Age 列名")
	})

	t.Run("无tag驼峰转蛇形", func(t *testing.T) {
		ty := reflect.TypeOf(utilsNoTag{})
		key := schemaCacheKey{ty.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m := reflectStructSchema(utilsNoTag{}, "gorm", "column")
		assertEqual(t, 2, len(m), "字段数量")
		assertEqual(t, "first_name", m[ty.Field(0).Offset], "FirstName 列名")
		assertEqual(t, "last_age", m[ty.Field(1).Offset], "LastAge 列名")
	})

	t.Run("忽略gorm减号字段", func(t *testing.T) {
		ty := reflect.TypeOf(utilsIgnore{})
		key := schemaCacheKey{ty.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m := reflectStructSchema(utilsIgnore{}, "gorm", "column")
		assertEqual(t, 1, len(m), "字段数量")
		assertEqual(t, "keep", m[ty.Field(0).Offset], "Keep 列名")
		_, exists := m[ty.Field(1).Offset]
		assertEqual(t, false, exists, "Drop 字段应被忽略")
	})

	t.Run("嵌入结构体偏移量累加", func(t *testing.T) {
		outerTy := reflect.TypeOf(utilsEmbedded{})
		baseTy := reflect.TypeOf(UtilsEmbedBase{})
		key := schemaCacheKey{outerTy.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m := reflectStructSchema(utilsEmbedded{}, "gorm", "column")
		assertEqual(t, 3, len(m), "字段数量")
		baseOffset := outerTy.Field(0).Offset
		assertEqual(t, "id", m[baseOffset+baseTy.Field(0).Offset], "ID 列名")
		assertEqual(t, "code", m[baseOffset+baseTy.Field(1).Offset], "Code 列名")
		assertEqual(t, "extra", m[outerTy.Field(1).Offset], "Extra 列名")
	})

	t.Run("指针嵌入结构体", func(t *testing.T) {
		outerTy := reflect.TypeOf(utilsPtrEmbed{})
		smallTy := reflect.TypeOf(UtilsEmbedBaseSmall{})
		key := schemaCacheKey{outerTy.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m := reflectStructSchema(utilsPtrEmbed{}, "gorm", "column")
		assertEqual(t, 2, len(m), "字段数量")
		baseOffset := outerTy.Field(0).Offset
		assertEqual(t, "id", m[baseOffset+smallTy.Field(0).Offset], "ID 列名")
		assertEqual(t, "note", m[outerTy.Field(1).Offset], "Note 列名")
	})

	t.Run("EMBEDDED标签嵌入", func(t *testing.T) {
		outerTy := reflect.TypeOf(utilsTagEmbed{})
		baseTy := reflect.TypeOf(UtilsEmbedBase{})
		key := schemaCacheKey{outerTy.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m := reflectStructSchema(utilsTagEmbed{}, "gorm", "column")
		assertEqual(t, 3, len(m), "字段数量")
		innerOffset := outerTy.Field(0).Offset
		assertEqual(t, "id", m[innerOffset+baseTy.Field(0).Offset], "ID 列名")
		assertEqual(t, "code", m[innerOffset+baseTy.Field(1).Offset], "Code 列名")
		assertEqual(t, "label", m[outerTy.Field(1).Offset], "Label 列名")
	})

	t.Run("缓存命中返回相同实例", func(t *testing.T) {
		ty := reflect.TypeOf(utilsSimple{})
		key := schemaCacheKey{ty.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		m1 := reflectStructSchema(utilsSimple{}, "gorm", "column")
		m2 := reflectStructSchema(utilsSimple{}, "gorm", "column")
		p1 := reflect.ValueOf(m1).Pointer()
		p2 := reflect.ValueOf(m2).Pointer()
		assertEqual(t, p1, p2, "两次调用应返回同一 map 实例")
	})

	t.Run("指针传入与值传入结果一致", func(t *testing.T) {
		ty := reflect.TypeOf(utilsSimple{})
		key := schemaCacheKey{ty.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		mVal := reflectStructSchema(utilsSimple{}, "gorm", "column")
		mPtr := reflectStructSchema(&utilsSimple{}, "gorm", "column")
		p1 := reflect.ValueOf(mVal).Pointer()
		p2 := reflect.ValueOf(mPtr).Pointer()
		assertEqual(t, p1, p2, "指针与值传入应返回同一缓存实例")
	})

	t.Run("并发调用无竞态", func(t *testing.T) {
		ty := reflect.TypeOf(utilsSimple{})
		key := schemaCacheKey{ty.String(), "gorm", "column"}
		t.Cleanup(func() { columnCache.Delete(key) })

		const goroutines = 20
		var wg sync.WaitGroup
		results := make([]map[uintptr]string, goroutines)
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			i := i
			go func() {
				defer wg.Done()
				results[i] = reflectStructSchema(utilsSimple{}, "gorm", "column")
			}()
		}
		wg.Wait()

		p0 := reflect.ValueOf(results[0]).Pointer()
		for i := 1; i < goroutines; i++ {
			pi := reflect.ValueOf(results[i]).Pointer()
			assertEqual(t, p0, pi, "并发调用应返回同一缓存实例")
		}
	})

	t.Run("nil传入触发panic", func(t *testing.T) {
		assertPanics(t, func() {
			reflectStructSchema(nil, "gorm", "column")
		}, "nil 传入应 panic")
	})

	t.Run("非结构体传入触发panic", func(t *testing.T) {
		assertPanics(t, func() {
			reflectStructSchema(42, "gorm", "column")
		}, "int 传入应 panic")
	})
}
