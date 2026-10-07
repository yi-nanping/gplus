// check-driver-deps 在独立消费者中验证数据库测试驱动不会进入核心编译依赖。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const library = "github.com/yi-nanping/gplus"

var driverPrefixes = []string{
	"gorm.io/driver/", "github.com/glebarez/", "github.com/godoes/gorm-",
	"github.com/go-sql-driver/mysql", "github.com/jackc/", "github.com/sijms/go-ora/",
	"github.com/mattn/go-sqlite3", "modernc.org/sqlite",
}

func run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if !strings.EqualFold(key, "GOWORK") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return out, nil
}

func version(dir, module string) (string, error) {
	out, err := run(dir, "list", "-m", "-f", "{{.Version}}", module)
	return strings.TrimSpace(string(out)), err
}

func isDriver(path string) bool {
	for _, prefix := range driverPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	out, err := run(root, "mod", "edit", "-json")
	if err != nil {
		return err
	}
	var manifest struct {
		Module  struct{ Path string }
		Require []struct{ Path string }
		Replace []any
	}
	if err := json.Unmarshal(out, &manifest); err != nil {
		return err
	}
	if manifest.Module.Path != library {
		return fmt.Errorf("请从 gplus 仓库根目录运行")
	}
	for _, requirement := range manifest.Require {
		if isDriver(requirement.Path) {
			return fmt.Errorf("核心模块声明了测试驱动: %s", requirement.Path)
		}
	}
	if len(manifest.Replace) != 0 {
		return fmt.Errorf("核心模块不能携带测试 replace")
	}
	gormVersion, err := version(root, "gorm.io/gorm")
	if err != nil {
		return err
	}
	checks := []struct {
		name, driver, alias string
		allowed             []string
	}{
		{name: "none"},
		{name: "mysql", driver: "gorm.io/driver/mysql", alias: "mysql", allowed: []string{"gorm.io/driver/mysql", "github.com/go-sql-driver/mysql"}},
		{name: "postgres", driver: "gorm.io/driver/postgres", alias: "postgres", allowed: []string{"gorm.io/driver/postgres", "github.com/jackc/"}},
	}
	for _, tc := range checks {
		if err := checkConsumer(root, gormVersion, tc.name, tc.driver, tc.alias, tc.allowed); err != nil {
			return err
		}
		fmt.Printf("PASS consumer=%s GOWORK=off: build, selected version, driver package dependencies\n", tc.name)
	}
	return nil
}

func checkConsumer(root, gormVersion, name, driver, alias string, allowed []string) error {
	dir, err := os.MkdirTemp("", "gplus-consumer-"+name+"-")
	if err != nil {
		return err
	}
	// 删除范围仅限本次创建的临时消费者目录。
	rel, err := filepath.Rel(os.TempDir(), dir)
	if err != nil || strings.HasPrefix(rel, "..") || !strings.HasPrefix(filepath.Base(dir), "gplus-consumer-") {
		return fmt.Errorf("临时消费者目录不在预期范围: %s", dir)
	}
	defer os.RemoveAll(dir)
	manifest := "module example.com/gplus-consumer/" + name + "\n\ngo 1.24\n\nrequire (\n" +
		library + " v0.0.0\ngorm.io/gorm " + gormVersion + "\n"
	driverVersion := ""
	if driver != "" {
		driverVersion, err = version(filepath.Join(root, "tests"), driver)
		if err != nil {
			return err
		}
		manifest += driver + " " + driverVersion + "\n"
	}
	manifest += ")\n\nreplace " + library + " => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	imports := "\"" + library + "\"\n\"gorm.io/gorm\"\n"
	setup := "var db *gorm.DB"
	if driver != "" {
		imports += "\"" + driver + "\"\n"
		setup = "db, err := gorm.Open(" + alias + ".Open(\"unused-dsn\"), &gorm.Config{}); if err != nil { panic(err) }"
	}
	// 仅编译，不执行 main；不会连接数据库。
	source := "package main\nimport (\n" + imports + ")\n" +
		"type User struct { ID uint }\nfunc main() { " + setup + "; _ = gplus.NewRepository[uint, User](db) }\n"
	for file, data := range map[string]string{"go.mod": manifest, "main.go": source} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(data), 0600); err != nil {
			return err
		}
	}
	if _, err := run(dir, "mod", "tidy"); err != nil {
		return err
	}
	if _, err := run(dir, "build", "-mod=readonly", "."); err != nil {
		return err
	}
	if driver != "" {
		selected, err := version(dir, driver)
		if err != nil {
			return err
		}
		if selected != driverVersion {
			return fmt.Errorf("consumer=%s 驱动版本被改变: %s -> %s", name, driverVersion, selected)
		}
	}
	out, err := run(dir, "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}}", ".")
	if err != nil {
		return err
	}
	seenSelected := driver == ""
	for _, path := range strings.Fields(string(out)) {
		if path == driver {
			seenSelected = true
		}
		if !isDriver(path) {
			continue
		}
		permitted := false
		for _, prefix := range allowed {
			if strings.HasPrefix(path, prefix) {
				permitted = true
				break
			}
		}
		if !permitted {
			return fmt.Errorf("consumer=%s 编译依赖包含未选择驱动: %s", name, path)
		}
	}
	if !seenSelected {
		return fmt.Errorf("consumer=%s 未验证到所选驱动", name)
	}
	return nil
}
