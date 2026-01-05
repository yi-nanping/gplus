package gplus

import "testing"

func TestBuilder_QuoteColumn(t *testing.T) {
	// 假设使用 MySQL 风格的转义符
	qL, qR := "`", "`"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "普通列名",
			input: "name",
			want:  "`name`",
		},
		{
			name:  "带表名的列",
			input: "users.name",
			want:  "`users`.`name`",
		},
		{
			name:  "带别名(AS)",
			input: "users.name AS u_name",
			want:  "`users`.`name` AS `u_name`",
		},
		{
			name:  "复杂函数(不应转义)",
			input: "count(id)",
			want:  "count(id)",
		},
		{
			name:  "算术表达式(不应转义)",
			input: "age + 1",
			want:  "age + 1",
		},
		{
			name:  "星号(不应转义)",
			input: "*",
			want:  "*",
		},
		{
			name:  "表名.*",
			input: "users.*",
			want:  "`users`.*", // 特殊处理：*通常不转义，但前缀表名可能被转义
		},
		{
			name:  "空字符串",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteColumn(tt.input, qL, qR)
			// 注意：具体的输出取决于你的 quoteColumn 实现细节
			// 这里假设你使用了我之前提供的优化版逻辑
			if got != tt.want {
				t.Errorf("quoteColumn(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
