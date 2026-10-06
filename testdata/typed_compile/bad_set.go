//go:build ignore

package typedcompile

import (
	"context"

	"github.com/yi-nanping/gplus"
)

func wrongSet() {
	u, m := gplus.NewUpdater[struct{ Name string }](context.Background())
	gplus.Set(u, &m.Name, 123)
}
