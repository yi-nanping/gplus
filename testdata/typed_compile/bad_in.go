//go:build ignore

package typedcompile

import (
	"context"

	"github.com/yi-nanping/gplus"
)

func wrongIn() {
	q, m := gplus.NewQuery[struct{ Age int }](context.Background())
	gplus.In(q, &m.Age, []string{"wrong"})
}
