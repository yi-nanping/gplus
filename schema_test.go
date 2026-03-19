package gplus

import (
	"context"
	"testing"
)

func TestRegisterModel(t *testing.T) {
	t.Run("正常注册后字段可解析", func(t *testing.T) {
		unregisterModel[TestUser]()
		u := new(TestUser)
		RegisterModel(u)
		col, err := resolveColumnName(&u.Name)
		assertError(t, err, false, "注册后字段应可解析")
		assertEqual(t, "username", col, "列名应为 username")
	})

	t.Run("幂等-重复注册不panic", func(t *testing.T) {
		unregisterModel[TestUser]()
		u := new(TestUser)
		RegisterModel(u)
		RegisterModel(u) // 第二次静默跳过
		col, err := resolveColumnName(&u.Name)
		assertError(t, err, false, "重复注册后字段应仍可解析")
		assertEqual(t, "username", col, "列名应为 username")
	})

	t.Run("无类型nil-不panic", func(t *testing.T) {
		RegisterModel(nil) // 不应 panic
	})

	t.Run("typed-nil指针-不panic", func(t *testing.T) {
		RegisterModel((*TestUser)(nil)) // 不应 panic
	})

	t.Run("非指针值-跳过不注册", func(t *testing.T) {
		unregisterModel[TestUser]()
		RegisterModel(TestUser{}) // 非指针，应跳过
		u := new(TestUser)
		_, err := resolveColumnName(&u.Name)
		assertError(t, err, true, "非指针注册无效，新指针字段应不可解析")
	})
}

func TestUnregisterModel(t *testing.T) {
	t.Run("注销后字段指针失效", func(t *testing.T) {
		unregisterModel[TestUser]()
		u := new(TestUser)
		RegisterModel(u)
		_, err := resolveColumnName(&u.Name)
		assertError(t, err, false, "注册后应有效")

		unregisterModel[TestUser]()
		_, err = resolveColumnName(&u.Name)
		assertError(t, err, true, "注销后原字段指针应失效")
	})

	t.Run("重复注销-不panic", func(t *testing.T) {
		unregisterModel[TestUser]()
		unregisterModel[TestUser]() // 再次注销，不应 panic
	})
}

func TestSchema_ResolveColumnName(t *testing.T) {
	// 初始化：注册模型（模拟系统启动）
	// 注意：NewQuery 内部会触发 RegisterModel，但为了测试纯净性，我们显式关注解析逻辑
	ctx := context.Background()

	// 获取查询对象和用于取地址的实体模板
	_, u := NewQuery[TestUser](ctx)

	tests := []struct {
		name    string
		input   any
		wantCol string
		wantErr bool
	}{
		{
			name:    "普通字段-自定义Tag",
			input:   &u.Name,
			wantCol: "username",
			wantErr: false,
		},
		{
			name:    "普通字段-默认蛇形命名",
			input:   &u.Email,
			wantCol: "email",
			wantErr: false,
		},
		{
			name:    "嵌入结构体字段",
			input:   &u.ID,
			wantCol: "id",
			wantErr: false,
		},
		{
			name:    "嵌入结构体字段-时间",
			input:   &u.CreatedAt,
			wantCol: "created_at",
			wantErr: false,
		},
		{
			name:    "直接传入字符串(Raw)",
			input:   "raw_column",
			wantCol: "raw_column",
			wantErr: false,
		},
		{
			name:    "错误情况-Nil指针",
			input:   nil,
			wantCol: "",
			wantErr: true,
		},
		{
			name:    "错误情况-非结构体字段的指针(外部变量)",
			input:   new(int), // 一个全新的int指针，不在 u 的内存范围内
			wantCol: "",
			wantErr: true,
		},
		{
			name:    "错误情况-值传递而非指针",
			input:   u.Age, // 传的是值 0
			wantCol: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 解析字段名为数据库列名
			got, err := resolveColumnName(tt.input)
			assertError(t, err, tt.wantErr, "Error status mismatch")
			if !tt.wantErr {
				assertEqual(t, tt.wantCol, got, "Column name mismatch")
			}
		})
	}
}

// TestSchema_PtrEmbedField 验证指针嵌入字段的端到端行为：
// 值字段和指针嵌入的内层字段均可正常解析。
func TestSchema_PtrEmbedField(t *testing.T) {
	unregisterModel[utilsPtrEmbed]()
	u := &utilsPtrEmbed{
		UtilsEmbedBaseSmall: &UtilsEmbedBaseSmall{ID: 1},
	}
	RegisterModel(u)
	t.Cleanup(func() { unregisterModel[utilsPtrEmbed]() })

	t.Run("值字段Note可正常解析", func(t *testing.T) {
		col, err := resolveColumnName(&u.Note)
		assertError(t, err, false, "普通值字段应可解析")
		assertEqual(t, "note", col, "列名应为 note")
	})

	t.Run("指针嵌入内层字段可正常解析", func(t *testing.T) {
		col, err := resolveColumnName(&u.ID)
		assertError(t, err, false, "指针嵌入字段应可解析")
		assertEqual(t, "id", col, "列名应为 id")
	})

	t.Run("nil指针嵌入时RegisterModel不panic且值字段可解析", func(t *testing.T) {
		unregisterModel[utilsPtrEmbed]()
		uNil := &utilsPtrEmbed{} // UtilsEmbedBaseSmall 为 nil
		RegisterModel(uNil)      // 不应 panic，nil 嵌入字段被跳过
		col, err := resolveColumnName(&uNil.Note)
		assertError(t, err, false, "nil嵌入时值字段仍应可解析")
		assertEqual(t, "note", col, "列名应为 note")
	})

	t.Run("getModelInstance自动初始化指针嵌入并可解析", func(t *testing.T) {
		unregisterModel[utilsPtrEmbed]()
		instance := getModelInstance[utilsPtrEmbed]()
		col, err := resolveColumnName(&instance.Note)
		assertError(t, err, false, "getModelInstance后值字段应可解析")
		assertEqual(t, "note", col, "列名应为 note")
		col2, err2 := resolveColumnName(&instance.ID)
		assertError(t, err2, false, "getModelInstance后指针嵌入字段应可解析")
		assertEqual(t, "id", col2, "列名应为 id")
	})
}
