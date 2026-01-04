package gplus

const (
	OpEq         = "="
	OpNe         = "<>"
	OpGt         = ">"
	OpGe         = ">="
	OpLt         = "<"
	OpLe         = "<="
	OpLike       = "LIKE"
	OpNotLike    = "NOT LIKE"
	OpIn         = "IN"
	OpNotIn      = "NOT IN"
	OpIsNull     = "IS NULL"
	OpIsNotNull  = "IS NOT NULL"
	OpBetween    = "BETWEEN"
	OpNotBetween = "NOT BETWEEN"

	KeyAnd  = "AND"
	KeyOr   = "OR"
	KeyDesc = "DESC"
	KeyAsc  = "ASC"

	// JoinLeft 左连接 返回左表中的所有记录，即使右表中没有匹配的记录（保留左表）。
	JoinLeft = "LEFT JOIN"
	// JoinRight 右连接 返回右表中的所有记录，即使左表中没有匹配的记录（保留右表）。
	JoinRight = "RIGHT JOIN"
	// JoinInner 内连接 只返回两个表中都存在的记录 (交集)。
	JoinInner = "INNER JOIN"
	// JoinOuter 外连接 返回左表中的所有记录，即使右表中没有匹配的记录。
	JoinOuter = "OUTER JOIN"
	// JoinNatural 自然连接 返回两个表中相同列名和数据类型的所有记录。
	JoinNatural = "NATURAL JOIN"
	// JoinFull 全连接 返回左表中的所有记录，即使右表中没有匹配的记录。
	JoinFull = "FULL OUTER JOIN"
	// JoinUnion UNION 连接 返回两个表中的所有记录，不论它们是否匹配。
	// 请注意，UNION 内部的 SELECT 语句必须拥有相同数量的列。列也必须拥有相似的数据类型。
	// 同时，每条 SELECT 语句中的列的顺序必须相同。UNION 只选取记录，而UNION ALL会列出所有记录。
	JoinUnion = "UNION"
	// JoinCross 交叉连接 返回两个表中的所有记录，不论它们是否匹配。
	// 因为其就是把表A和表B的数据进行一个N*M的组合，即笛卡尔积。
	// 表达式如下：SELECT * FROM TableA CROSS JOIN TableB
	// 这个笛卡尔乘积会产生 4 x 4 = 16 条记录
	JoinCross = "CROSS JOIN"
)
