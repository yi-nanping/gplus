package gplus

import (
	"context"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type typedRecord struct {
	ID       int64 `gorm:"primaryKey"`
	Name     string
	Age      int
	Disabled bool
}

func TestTypedHelpers_QueryAndZeroValueUpdate(t *testing.T) {
	repo, db := setupTestDB[typedRecord](t)
	ctx := context.Background()
	seeds := []typedRecord{
		{Name: "Alice", Age: 28, Disabled: true},
		{Name: "Bob", Age: 28, Disabled: true},
	}
	if err := db.Create(&seeds).Error; err != nil {
		t.Fatal(err)
	}

	q, model := NewQuery[typedRecord](ctx)
	if Eq(q, &model.Name, "Alice") != q || In(q, &model.Age, []int{28, 30}) != q {
		t.Fatal("辅助函数应返回原构建器")
	}
	plain, pm := NewQuery[typedRecord](ctx)
	plain.Eq(&pm.Name, "Alice").In(&pm.Age, []int{28, 30})
	gotSQL, err := q.ToSQL(db)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL, err := plain.ToSQL(db)
	if err != nil || gotSQL != wantSQL {
		t.Fatalf("查询 SQL 不一致: got=%s want=%s err=%v", gotSQL, wantSQL, err)
	}
	rows, err := repo.List(q)
	if err != nil || len(rows) != 1 || rows[0].Name != "Alice" {
		t.Fatalf("查询结果错误: rows=%v err=%v", rows, err)
	}

	u, um := NewUpdater[typedRecord](ctx)
	if Set(u, &um.Name, "") != u {
		t.Fatal("Set 应返回原构建器")
	}
	Set(u, &um.Age, 0)
	Set(u, &um.Disabled, false)
	Eq(u, &um.ID, seeds[0].ID)
	In(u, &um.Age, []int{28})
	plainUpdate, pum := NewUpdater[typedRecord](ctx)
	plainUpdate.Set(&pum.Name, "").Set(&pum.Age, 0).Set(&pum.Disabled, false).
		Eq(&pum.ID, seeds[0].ID).In(&pum.Age, []int{28})
	gotSQL, err = u.ToSQL(db)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL, err = plainUpdate.ToSQL(db)
	if err != nil || gotSQL != wantSQL {
		t.Fatalf("更新 SQL 不一致: got=%s want=%s err=%v", gotSQL, wantSQL, err)
	}
	affected, err := repo.UpdateByCond(u)
	if err != nil || affected != 1 {
		t.Fatalf("更新结果错误: affected=%d err=%v", affected, err)
	}
	var got []typedRecord
	if err := db.Order("id").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "" || got[0].Age != 0 || got[0].Disabled || got[1] != seeds[1] {
		t.Fatalf("零值更新或更新范围错误: %v", got)
	}
}

func TestTypedHelpers_InvalidFieldStillRejected(t *testing.T) {
	repo, _ := setupTestDB[typedRecord](t)
	q, _ := NewQuery[typedRecord](context.Background())
	Eq(q, new(int), 1)
	if q.GetError() == nil {
		t.Fatal("类型匹配的外部字段指针仍应被拒绝")
	}
	if _, err := repo.List(q); err == nil {
		t.Fatal("无效字段不能执行查询")
	}
	u, um := NewUpdater[typedRecord](context.Background())
	Set(u, new(string), "value")
	Eq(u, &um.ID, int64(1))
	if u.GetError() == nil {
		t.Fatal("Set 不应绕过字段注册校验")
	}
	if _, err := repo.UpdateByCond(u); err == nil {
		t.Fatal("无效字段不能执行更新")
	}
}

// 一次读取真实包的导出类型，再在内存检查各样例；不为每个失败样例重建包。
func TestTypedHelpers_CompileConstraints(t *testing.T) {
	cmd := exec.Command("go", "list", "-mod=readonly", "-export", "-deps", "-f", "{{.ImportPath}} {{.Export}}", ".")
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("读取包导出类型失败: %v\n%s", err, out)
	}
	exports := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) == 2 {
			exports[parts[0]] = parts[1]
		}
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(exports[path])
	})
	for _, name := range []string{"valid.go", "bad_eq.go", "bad_in.go", "bad_set.go"} {
		t.Run(name, func(t *testing.T) {
			file, err := parser.ParseFile(fset, filepath.Join("testdata", "typed_compile", name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			var typeErrors []error
			config := types.Config{Importer: imp, Error: func(err error) { typeErrors = append(typeErrors, err) }}
			_, err = config.Check("typedcompile", fset, []*ast.File{file}, nil)
			if name == "valid.go" {
				if err != nil {
					t.Fatalf("匹配类型应通过编译: %v", typeErrors)
				}
				return
			}
			if err == nil {
				t.Fatal("错误类型应在编译期被拒绝")
			}
			want := map[string]string{
				"bad_eq.go":  "as int value in argument to gplus.Eq",
				"bad_in.go":  "does not match inferred type []int",
				"bad_set.go": "as string value in argument to gplus.Set",
			}[name]
			if len(typeErrors) != 1 || !strings.Contains(typeErrors[0].Error(), want) {
				t.Fatalf("期望泛型实参类型不匹配，实际: %v", typeErrors)
			}
		})
	}
}
