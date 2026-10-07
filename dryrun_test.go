package gplus

import (
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// unitDialector 只为包内白盒测试构建 SQL，不注册驱动或执行数据库操作。
// 真实驱动的 SQL 与数据库行为由 tests 模块另外验证。
type unitDialector struct{}

func (unitDialector) Name() string { return "sqlite" }
func (unitDialector) Initialize(db *gorm.DB) error {
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
	return nil
}
func (unitDialector) Migrator(*gorm.DB) gorm.Migrator       { return nil }
func (unitDialector) DataTypeOf(field *schema.Field) string { return string(field.DataType) }
func (unitDialector) DefaultValueOf(*schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}
func (unitDialector) BindVarTo(writer clause.Writer, _ *gorm.Statement, _ any) {
	writer.WriteByte('?')
}
func (unitDialector) QuoteTo(writer clause.Writer, name string) {
	writer.WriteByte('`')
	for _, char := range name {
		switch char {
		case '.':
			writer.WriteString("`.`")
		case '`':
			writer.WriteString("``")
		default:
			writer.WriteString(string(char))
		}
	}
	writer.WriteByte('`')
}
func (unitDialector) Explain(sql string, vars ...any) string {
	return logger.ExplainSQL(sql, nil, "'", vars...)
}

func newDryRunDB(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(unitDialector{}, &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
		DisableAutomaticPing:   true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
