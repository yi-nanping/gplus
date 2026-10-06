package typedcompile

import (
	"context"

	"github.com/yi-nanping/gplus"
)

type score int

type record struct {
	ID       int64
	Name     string
	Score    score
	Optional *int
}

func matchingTypes() {
	q, m := gplus.NewQuery[record](context.Background())
	gplus.Eq(q, &m.ID, int64(1)).Order(&m.ID, true)
	gplus.In(q, &m.ID, []int64{1, 2})
	gplus.Eq(q, &m.Score, score(3))
	gplus.In(q, &m.Score, []score{3, 4})
	gplus.Eq(q, &m.Optional, (*int)(nil))
	gplus.In(q, &m.Optional, []*int{nil})

	u, um := gplus.NewUpdater[record](context.Background())
	gplus.Set(u, &um.Name, "").Eq(&um.ID, 1)
	gplus.Set(u, &um.Score, score(0))
	gplus.Set(u, &um.Optional, (*int)(nil))
	gplus.Eq(u, &um.ID, int64(1))
	gplus.In(u, &um.ID, []int64{1, 2})

	var on *gplus.OnBuilder
	gplus.Eq(on, &m.Score, score(3))
	gplus.In(on, &m.Score, []score{3, 4})
}
