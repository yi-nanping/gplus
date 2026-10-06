package collisionmodel

// Record 使用不同字段布局和列名，但保持相同包名及类型名。
type Record struct {
	ID      int64
	Version uint32 `gorm:"column:second_version" gplus:"version"`
	Name    string `gorm:"column:second_name"`
}

func (Record) TableName() string { return "second_records" }
