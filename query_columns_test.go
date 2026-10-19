package gplus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type columnEntity struct {
	ID       int64
	FileCode string `gorm:"column:object_key"`
	Name     string `gorm:"column:label_text"`
	Ignored  string `gorm:"-"`
}

func (columnEntity) TableName() string { return "column_entities" }

type columnRelation struct {
	ID        int64
	FileCode  string `gorm:"column:relation_key"`
	DeletedAt gorm.DeletedAt
}

func (columnRelation) TableName() string { return "column_relations" }

type columnCompany struct {
	ID   int64
	Name string
}

func TestTypedColumns_DefaultTableNaming(t *testing.T) {
	q, model := NewQuery[columnCompany](context.Background())
	q.SelectAs(&model.Name, "name").EqColumn(&model.ID, &model.ID)
	sql, _ := columnQuerySQL(t, q)
	assertSQL(t, sql, `"column_companies"."name" AS "name"`, `"column_companies"."id" = "column_companies"."id"`)
}

func columnQuerySQL[T any](t *testing.T, q *Query[T]) (string, []any) {
	t.Helper()
	result := q.ToDB(newDryRunDB(t)).Find(&[]T{})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	return result.Statement.SQL.String(), result.Statement.Vars
}

func TestSelectAs_MixedProjection(t *testing.T) {
	q, m := NewQuery[columnEntity](context.Background())
	o := As[columnEntity](q, "o")
	q.CrossJoinAs(o).Select(&m.ID).SelectAs(&o.Name, "order").
		SelectExpr(Lit("literal")).SelectRaw("?", 7)
	sql, vars := columnQuerySQL(t, q)
	assertSQL(t, sql, `"o"."label_text" AS "order"`)
	if !reflect.DeepEqual(vars, []any{"literal", 7}) {
		t.Fatalf("projection vars=%v", vars)
	}
	if len(q.selects) != 4 {
		t.Fatalf("projection count=%d", len(q.selects))
	}
}

func TestSelectAs_ResultAliasValidation(t *testing.T) {
	for _, alias := range []string{"", "1name", "a.b", "a b", "name;DROP", `a"b`, strings.Repeat("a", 33)} {
		t.Run(alias, func(t *testing.T) {
			q, m := NewQuery[columnEntity](context.Background())
			q.SelectAs(&m.Name, alias)
			if !errors.Is(q.GetError(), ErrAliasInvalidName) || len(q.selects) != 0 {
				t.Fatalf("alias=%q err=%v selects=%v", alias, q.GetError(), q.selects)
			}
		})
	}
	q, m := NewQuery[columnEntity](context.Background())
	q.SelectAs(&m.Name, strings.Repeat("a", 32))
	if err := q.GetError(); err != nil {
		t.Fatal(err)
	}
}

func TestTypedColumns_DialectQuoting(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres", "sqlite", "dm"} {
		t.Run(dialect, func(t *testing.T) {
			q, m := NewQuery[columnEntity](context.Background())
			q.SelectAs(&m.Name, "order").EqColumn(&m.FileCode, &m.Name)
			db := newDryRunDB(t)
			db.Config.Dialector = testMockDialector{dialectName: dialect}
			left, right := getQuoteChar(db)
			result := q.applySelects(db, left, right)
			projection := `"column_entities"."label_text" AS "order"`
			want := `"column_entities"."object_key" = "column_entities"."label_text"`
			if dialect == "mysql" {
				projection = "`column_entities`.`label_text` AS `order`"
				want = "`column_entities`.`object_key` = `column_entities`.`label_text`"
			}
			if len(result.Statement.Selects) != 1 || result.Statement.Selects[0] != projection {
				t.Fatalf("selects=%v want=%s", result.Statement.Selects, projection)
			}
			conditionSQL, vars, ok := buildLeafSQL(q.conditions[0], left, right)
			if !ok || conditionSQL != want || len(vars) != 0 {
				t.Fatalf("condition=%s vars=%v want=%s", conditionSQL, vars, want)
			}
		})
	}
}

func TestEqColumn_JoinAndValueConditions(t *testing.T) {
	q, model := NewQuery[columnEntity](context.Background())
	other := As[columnEntity](q, "other")
	q.CrossJoinAs(other).EqColumn(&model.FileCode, &other.FileCode).Eq(&model.Name, "alice")
	sql, vars := columnQuerySQL(t, q)
	assertSQL(t, sql, `"column_entities"."object_key" = "other"."object_key"`)
	if !reflect.DeepEqual(vars, []any{"alice"}) {
		t.Fatalf("only values should be bound: %v", vars)
	}
}

func TestSelectAs_SchemaQualifiedMainAlias(t *testing.T) {
	q, model := NewQueryAs[columnEntity](context.Background(), "e")
	q.Table("audit.column_entities").SelectAs(&model.Name, "order")
	sql, _ := columnQuerySQL(t, q)
	assertSQL(t, sql, `FROM "audit"."column_entities" AS "e"`, `"e"."label_text" AS "order"`)
}

func TestEqColumn_CorrelatedScopes(t *testing.T) {
	for _, aliases := range []bool{false, true} {
		t.Run(map[bool]string{false: "table_names", true: "aliases"}[aliases], func(t *testing.T) {
			q, entity := NewQuery[columnEntity](context.Background())
			var sub *Query[columnRelation]
			var relation *columnRelation
			outer, inner := "column_entities", "column_relations"
			if aliases {
				q, entity = NewQueryAs[columnEntity](context.Background(), "e")
				sub, relation = SubQueryAs[columnRelation](q, "r")
				outer, inner = "e", "r"
			} else {
				sub, relation = SubQuery[columnRelation](q)
			}
			sub.EqColumn(&relation.FileCode, &entity.FileCode)
			q.NotExists(sub)
			sql, vars := columnQuerySQL(t, q)
			assertSQL(t, sql, quoteColumn(inner+".relation_key", `"`, `"`)+" = "+quoteColumn(outer+".object_key", `"`, `"`), "NOT EXISTS")
			assertSQL(t, stripIdentQuotes(sql), inner+".deleted_at IS NULL")
			if len(vars) != 0 {
				t.Fatalf("column references must not be bound: %v", vars)
			}
			if aliases {
				assertSQL(t, stripIdentQuotes(sql), "FROM column_relations AS r")
			}
		})
	}
}

func TestEqColumn_NestedAndGroupedScopes(t *testing.T) {
	q, entity := NewQueryAs[columnEntity](context.Background(), "e")
	sub, relation := SubQueryAs[columnRelation](q, "r")
	deep, inner := SubQueryAs[columnEntity](sub, "d")
	deep.And(func(group *Query[columnEntity]) {
		group.EqColumn(&inner.FileCode, &entity.FileCode)
	}).Or(func(group *Query[columnEntity]) {
		group.EqColumn(&inner.ID, &relation.ID)
	})
	sub.Exists(deep)
	q.Exists(sub)
	sql, vars := columnQuerySQL(t, q)
	assertSQL(t, sql, `"d"."object_key" = "e"."object_key"`, `"d"."id" = "r"."id"`)
	if len(vars) != 0 {
		t.Fatalf("vars=%v", vars)
	}
}

func TestTypedColumns_RejectInvalidFields(t *testing.T) {
	for _, api := range []string{"SelectAs", "EqColumnLeft", "EqColumnRight"} {
		t.Run(api, func(t *testing.T) {
			_, foreign := NewQuery[columnRelation](context.Background())
			other, _ := NewQuery[columnEntity](context.Background())
			alias := As[columnEntity](other, "foreign_alias")
			local := &columnEntity{}
			_, m0 := NewQuery[columnEntity](context.Background())
			for _, field := range []any{nil, (*string)(nil), "label_text", 1, &local.Name, &foreign.FileCode, &alias.Name, &m0.Ignored} {
				q, m := NewQuery[columnEntity](context.Background())
				switch api {
				case "SelectAs":
					q.SelectAs(field, "name")
				case "EqColumnLeft":
					q.EqColumn(field, &m.Name)
				default:
					q.EqColumn(&m.Name, field)
				}
				if q.GetError() == nil || len(q.selects) != 0 || len(q.conditions) != 0 {
					t.Fatalf("field=%T err=%v", field, q.GetError())
				}
				for _, build := range []func(*gorm.DB) *gorm.DB{q.BuildQuery(), q.BuildCount(), q.BuildUpdate(), q.BuildDelete()} {
					result := build(newDryRunDB(t).Model(&columnEntity{})).Find(&[]columnEntity{})
					if result.Error == nil || result.Statement.SQL.Len() != 0 {
						t.Fatalf("invalid reference executed SQL=%s err=%v", result.Statement.SQL.String(), result.Error)
					}
				}
			}
		})
	}
}

func TestTypedColumns_RevokedAlias(t *testing.T) {
	for _, selectAs := range []bool{false, true} {
		q, alias := NewQueryAs[columnEntity](context.Background(), "e")
		q.Clear()
		if selectAs {
			q.SelectAs(&alias.Name, "name")
		} else {
			q.EqColumn(&alias.Name, &alias.Name)
		}
		if !errors.Is(q.GetError(), ErrAliasRevoked) {
			t.Fatalf("err=%v", q.GetError())
		}
	}
}

func TestTypedColumns_EmbeddedPointers(t *testing.T) {
	q, model := NewQuery[prefixModel](context.Background())
	q.SelectAs(&model.Complex.Details.City, "city").EqColumn(&model.Shipping.City, &model.Nested.Home.City)
	if err := q.GetError(); err != nil {
		t.Fatal(err)
	}
	if q.selects[0].expr != "prefix_models.complex_detail_city" || q.conditions[0].expr != "prefix_models.ship_city" || q.conditions[0].rightColumn != "prefix_models.outer_home_city" {
		t.Fatalf("select=%+v condition=%+v", q.selects, q.conditions)
	}
}
