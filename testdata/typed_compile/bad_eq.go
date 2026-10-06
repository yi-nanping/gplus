//go:build ignore

package typedcompile

import (
	"context"

	"github.com/yi-nanping/gplus"
)

func wrongEq() {
	q, m := gplus.NewQuery[struct{ Age int }](context.Background())
	gplus.Eq(q, &m.Age, "wrong")
}
