package gplus

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

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

func prefixFieldPointers(m *prefixModel) map[string]any {
	return map[string]any{
		"PrefixAnonymousValue.Street": &m.Street,
		"PrefixAnonymousPointer.Zone": &m.Zone,
		"Billing.City":                &m.Billing.City,
		"Billing.Code":                &m.Billing.Code,
		"Shipping.City":               &m.Shipping.City,
		"Shipping.Code":               &m.Shipping.Code,
		"Nested.Details.City":         &m.Nested.Details.City,
		"Nested.Details.Code":         &m.Nested.Details.Code,
		"Nested.Home.City":            &m.Nested.Home.City,
		"Nested.Home.Code":            &m.Nested.Home.Code,
		"Complex.Details.City":        &m.Complex.Details.City,
		"Complex.Home.Code":           &m.Complex.Home.Code,
	}
}

func assertPrefixPointersInitialized(t *testing.T, m *prefixModel) {
	t.Helper()
	if m.PrefixAnonymousPointer == nil || m.Shipping == nil || m.Nested.Details == nil || m.Complex == nil || m.Complex.Details == nil {
		t.Fatalf("embedded pointer fields were not initialized: %+v", m)
	}
}

func TestEmbeddedPrefix_SchemaAndAliasMatchGORM(t *testing.T) {
	unregisterModel[prefixModel]()
	t.Cleanup(func() { unregisterModel[prefixModel]() })
	_, m := NewQuery[prefixModel](context.Background())
	assertPrefixPointersInitialized(t, m)
	gormSchema, err := schema.Parse(&prefixModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for bindName, ptr := range prefixFieldPointers(m) {
		got, err := resolveColumnName(ptr)
		want := gormSchema.FieldsByBindName[bindName].DBName
		if err != nil || got != want {
			t.Errorf("%s column=%q err=%v, GORM=%q", bindName, got, err, want)
		}
	}
	if _, err := resolveColumnName(&m.Billing.Ignored); !errors.Is(err, ErrColumnNotFound) {
		t.Fatalf("ignored field registered: %v", err)
	}
	if _, err := resolveColumnName(&m.Ignored.City); !errors.Is(err, ErrColumnNotFound) {
		t.Fatalf("ignored embed registered: %v", err)
	}
	q, alias := NewQueryAs[prefixModel](context.Background(), "p")
	assertPrefixPointersInitialized(t, alias)
	for bindName, ptr := range prefixFieldPointers(alias) {
		name, err := q.resolveColumnName(reflect.ValueOf(ptr).Pointer())
		want := "p." + gormSchema.FieldsByBindName[bindName].DBName
		if err != nil || name != want {
			t.Errorf("alias %s column=%q err=%v, want=%q", bindName, name, err, want)
		}
	}
	oldPtr := &alias.Nested.Details.City
	q.Clear()
	if _, err := q.resolveColumnName(reflect.ValueOf(oldPtr).Pointer()); !errors.Is(err, ErrAliasRevoked) {
		t.Fatalf("cleared pointer alias must be revoked: %v", err)
	}
}

func TestEmbeddedPrefix_UnregisterCleansNestedPointers(t *testing.T) {
	unregisterModel[prefixModel]()
	t.Cleanup(func() { unregisterModel[prefixModel]() })
	m := &prefixModel{PrefixAnonymousPointer: &PrefixAnonymousPointer{}, Shipping: &PrefixAddress{}, Nested: PrefixNested{Details: &PrefixAddress{}}, Complex: &PrefixNested{Details: &PrefixAddress{}}}
	RegisterModel(m)
	for _, ptr := range prefixFieldPointers(m) {
		if _, err := resolveColumnName(ptr); err != nil {
			t.Fatalf("registration missing field %T: %v", ptr, err)
		}
	}
	other := Model[utilsSimple]()
	unregisterModel[prefixModel]()
	for _, ptr := range prefixFieldPointers(m) {
		if _, err := resolveColumnName(ptr); !errors.Is(err, ErrColumnNotFound) {
			t.Fatalf("unregistered field still resolves: %v", err)
		}
	}
	if got, err := resolveColumnName(&other.Name); err != nil || got != "name" {
		t.Fatalf("unrelated model removed: %q %v", got, err)
	}
}

func TestEmbeddedPrefix_NestedVersionMetadata(t *testing.T) {
	type versionBlock struct {
		Lock PrefixVersionFields `gorm:"embedded;embeddedPrefix:inner_"`
	}
	type nestedVersion struct {
		ID    int64
		Block versionBlock `gorm:"embedded;embeddedPrefix:outer_"`
	}
	var model nestedVersion
	info := getVersionField[nestedVersion]()
	if info == nil || info.columnName != "outer_inner_revision" {
		t.Fatalf("nested version metadata=%+v", info)
	}
	wantOffset := reflect.ValueOf(&model.Block.Lock.Version).Pointer() - reflect.ValueOf(&model).Pointer()
	if info.offset != wantOffset {
		t.Fatalf("version offset=%d want=%d", info.offset, wantOffset)
	}
	if got := reflectStructSchema(&model, "gorm", "COLUMN")[info.offset]; got != info.columnName {
		t.Fatalf("version column=%q differs from schema %q", info.columnName, got)
	}
}

func TestEmbeddedPrefix_TypeMetadataAndCustomTag(t *testing.T) {
	type ignoredPointers struct {
		Ignored *PrefixAddress `gorm:"-;embedded" custom:"embedded;embeddedPrefix:custom_"`
		Plain   *PrefixAddress
	}
	if hasGormPtrEmbeds(reflect.TypeOf(utilsSimple{})) || hasGormPtrEmbeds(reflect.TypeOf(ignoredPointers{})) {
		t.Fatal("ordinary or ignored pointers must not trigger GORM embedded traversal")
	}
	ignored := ignoredPointers{}
	initPtrEmbeds(reflect.ValueOf(&ignored).Elem())
	if ignored.Ignored != nil || ignored.Plain != nil {
		t.Fatal("ignored or non-embedded pointers initialized")
	}
	// gorm 的类型缓存不能让其他标签的指针遍历提前返回。
	ignored.Ignored = &PrefixAddress{}
	visits := 0
	walkPtrEmbedFields(reflect.ValueOf(&ignored).Elem(), "custom", "", func(inner reflect.Value, prefix string) {
		visits++
		if prefix != "custom_" || inner.Addr().Interface() != ignored.Ignored {
			t.Fatalf("unexpected custom embed: %q %+v", prefix, inner.Interface())
		}
	})
	if visits != 1 {
		t.Fatalf("custom embedded pointer visits=%d want=1", visits)
	}
	if !hasGormPtrEmbeds(reflect.TypeOf(PrefixNested{})) {
		t.Fatal("pointer inside a value embedded layer was missed")
	}
	nested := PrefixNested{}
	initPtrEmbeds(reflect.ValueOf(&nested).Elem())
	if nested.Details == nil {
		t.Fatal("nested embedded pointer was not initialized")
	}
}
