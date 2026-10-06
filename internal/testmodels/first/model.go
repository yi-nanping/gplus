package collisionmodel

// Record 与另一导入路径下的同名模型用于验证缓存类型隔离。
type Record struct {
	ID      int64
	Name    string `gorm:"column:first_name"`
	Version int64  `gorm:"column:first_version" gplus:"version"`
}

func (Record) TableName() string { return "first_records" }
