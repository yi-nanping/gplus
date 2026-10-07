package gplus_test

import "testing"

type PrefixAddress struct {
	City    string
	Code    string `gorm:"column:postal"`
	Ignored string `gorm:"-"`
}

type PrefixAnonymousValue struct{ Street string }
type PrefixAnonymousPointer struct{ Zone string }
type PrefixNested struct {
	Details *PrefixAddress `gorm:"embedded;embeddedPrefix:detail_"`
	Home    PrefixAddress  `gorm:"embedded;embeddedPrefix:home_"`
}

type prefixModel struct {
	ID                      int64 `gorm:"primaryKey"`
	PrefixAnonymousValue    `gorm:"embeddedPrefix:anon_"`
	*PrefixAnonymousPointer `gorm:"embeddedPrefix:pointer_"`
	Billing                 PrefixAddress  `gorm:"embedded;embeddedPrefix:bill_"`
	Shipping                *PrefixAddress `gorm:"embedded;embeddedPrefix:ship_"`
	Nested                  PrefixNested   `gorm:"embedded;embeddedPrefix:outer_"`
	Complex                 *PrefixNested  `gorm:"embedded;embeddedPrefix:complex_"`
	Ignored                 PrefixAddress  `gorm:"-;embedded;embeddedPrefix:ignore_"`
}

func assertPrefixPointersInitialized(t *testing.T, m *prefixModel) {
	t.Helper()
	if m.PrefixAnonymousPointer == nil || m.Shipping == nil || m.Nested.Details == nil || m.Complex == nil || m.Complex.Details == nil {
		t.Fatalf("embedded pointer fields were not initialized: %+v", m)
	}
}
