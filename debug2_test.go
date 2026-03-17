package gplus

import (
	"fmt"
	"testing"

	"gorm.io/gorm/schema"
)

func TestDebugParse(t *testing.T) {
	s := schema.ParseTagSetting("", ";")
	fmt.Printf("ParseTagSetting empty: %#v\n", s)
	s2 := schema.ParseTagSetting("column:id", ";")
	fmt.Printf("ParseTagSetting column:id: %#v\n", s2)
}
