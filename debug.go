package gplus

import (
	"fmt"

	"gorm.io/gorm"
)

// ToSQL 将当前查询转换为 SELECT SQL 字符串，不执行实际查询。
// 返回的 SQL 已将参数内联，仅用于调试展示，不可直接作为参数化查询使用。
// db 提供方言信息（引号类型），不会发出任何网络请求。
func (q *Query[T]) ToSQL(db *gorm.DB) (string, error) {
	if q == nil {
		return "", ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return "", err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return "", err
	}
	var dest []T
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.WithContext(q.Context()).Model(new(T)).Scopes(q.BuildQuery()).Find(&dest)
	})
	return sql, nil
}

// ToCountSQL 将当前查询转换为 COUNT SQL 字符串，不执行实际查询。
// 对应 Page() 内部的 COUNT 路径，可用于确认分页计数时的实际 SQL。
func (q *Query[T]) ToCountSQL(db *gorm.DB) (string, error) {
	if q == nil {
		return "", ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return "", err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return "", err
	}
	var count int64
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.WithContext(q.Context()).Model(new(T)).Scopes(q.BuildCount()).Count(&count)
	})
	return sql, nil
}

// ToSQL 将当前更新操作转换为 UPDATE SQL 字符串，不执行实际更新。
// 返回的 SQL 已将参数内联，仅用于调试展示。
func (u *Updater[T]) ToSQL(db *gorm.DB) (string, error) {
	if u == nil || u.IsEmpty() {
		return "", ErrUpdateEmpty
	}
	if err := u.GetError(); err != nil {
		return "", err
	}
	if err := u.DataRuleBuilder().GetError(); err != nil {
		return "", err
	}
	if len(u.conditions) == 0 {
		return "", ErrUpdateNoCondition
	}
	var model T
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.WithContext(u.Context()).Model(&model).Scopes(u.BuildUpdate()).Updates(u.setMap)
	})
	return sql, nil
}

// ToSQL 将查询转换为 SELECT SQL 字符串，使用 Repository 内部的 DB，不执行实际查询。
func (r *Repository[D, T]) ToSQL(q *Query[T]) (string, error) {
	if q == nil {
		return "", ErrQueryNil
	}
	return q.ToSQL(r.db)
}

// ToCountSQL 将查询转换为 COUNT SQL 字符串，使用 Repository 内部的 DB，不执行实际查询。
func (r *Repository[D, T]) ToCountSQL(q *Query[T]) (string, error) {
	if q == nil {
		return "", ErrQueryNil
	}
	return q.ToCountSQL(r.db)
}

// ToUpdateSQL 将更新操作转换为 UPDATE SQL 字符串，使用 Repository 内部的 DB，不执行实际查询。
func (r *Repository[D, T]) ToUpdateSQL(u *Updater[T]) (string, error) {
	if u == nil {
		return "", fmt.Errorf("%w: %w", ErrUpdateEmpty, ErrQueryNil)
	}
	return u.ToSQL(r.db)
}
