package gplus

import (
	"context"
	"testing"
)

func TestRegisterModel(t *testing.T) {
	t.Run("正常注册后字段可解析", func(t *testing.T) {
		UnregisterModel[TestUser]()
		u := new(TestUser)
		RegisterModel(u)
		col, err := resolveColumnName(&u.Name)
		assertError(t, err, false, "注册后字段应可解析")
		assertEqual(t, "username", col, "列名应为 username")
	})

	t.Run("幂等-重复注册不panic", func(t *testing.T) {
		UnregisterModel[TestUser]()
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
		UnregisterModel[TestUser]()
		RegisterModel(TestUser{}) // 非指针，应跳过
		u := new(TestUser)
		_, err := resolveColumnName(&u.Name)
		assertError(t, err, true, "非指针注册无效，新指针字段应不可解析")
	})
}

func TestUnregisterModel(t *testing.T) {
	t.Run("注销后字段指针失效", func(t *testing.T) {
		UnregisterModel[TestUser]()
		u := new(TestUser)
		RegisterModel(u)
		_, err := resolveColumnName(&u.Name)
		assertError(t, err, false, "注册后应有效")

		UnregisterModel[TestUser]()
		_, err = resolveColumnName(&u.Name)
		assertError(t, err, true, "注销后原字段指针应失效")
	})

	t.Run("重复注销-不panic", func(t *testing.T) {
		UnregisterModel[TestUser]()
		UnregisterModel[TestUser]() // 再次注销，不应 panic
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
