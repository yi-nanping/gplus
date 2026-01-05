package gplus

import (
	"context"
	"testing"
)

func TestSchema_ResolveColumnName(t *testing.T) {
	// 初始化：注册模型（模拟系统启动）
	// 注意：NewQuery 内部会触发 registerModel，但为了测试纯净性，我们显式关注解析逻辑
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
