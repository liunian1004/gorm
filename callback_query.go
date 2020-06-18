package gorm

import (
	"errors"
	"fmt"
	"reflect"
)

// Define callbacks for querying
func init() {
	DefaultCallback.Query().Register("gorm:query", queryCallback)
	DefaultCallback.Query().Register("gorm:preload", preloadCallback)
	DefaultCallback.Query().Register("gorm:after_query", afterQueryCallback)
}

// queryCallback used to query data from database
// 从数据库查询数据
func queryCallback(scope *Scope) {
	// 跳过
	if _, skip := scope.InstanceGet("gorm:skip_query_callback"); skip {
		return
	}

	//we are only preloading relations, dont touch base model
	// Preload 时跳过
	if _, skip := scope.InstanceGet("gorm:only_preload"); skip {
		return
	}

	// 打印日志，传入当前时间作为参数
	defer scope.trace(scope.db.nowFunc())

	var (
		isSlice, isPtr bool
		resultType     reflect.Type // 类型
		results        = scope.IndirectValue() // scope.Value 上 interface{} 值的 reflect.Value 实例
	)

	// orderBy 对应 gorm:order_by_primary_key 的值
	if orderBy, ok := scope.Get("gorm:order_by_primary_key"); ok {
		// 查找主键
		if primaryField := scope.PrimaryField(); primaryField != nil {
			// 设置 Order
			scope.Search.Order(fmt.Sprintf("%v.%v %v", scope.QuotedTableName(), scope.Quote(primaryField.DBName), orderBy))
		}
	}

	// 修改结果存放的位置
	if value, ok := scope.Get("gorm:query_destination"); ok {
		results = indirect(reflect.ValueOf(value))
	}

	// 如果输出的 scope.Value 是 slice 类型，确定内部元素是否为指针
	if kind := results.Kind(); kind == reflect.Slice {
		isSlice = true
		// 内部元素类型
		resultType = results.Type().Elem()
		// 创建一个 slice，Set 到 results
		results.Set(reflect.MakeSlice(results.Type(), 0, 0))

		// 如果 slice 里面的元素是指针，取出对应的类型
		if resultType.Kind() == reflect.Ptr {
			isPtr = true
			resultType = resultType.Elem()
		}
	} else if kind != reflect.Struct {
		scope.Err(errors.New("unsupported destination, should be slice or struct"))
		return
	}

	// 准备查询语句 scope.SQL 是最终需要执行的语句
	scope.prepareQuerySQL()

	if !scope.HasError() {
		scope.db.RowsAffected = 0
		if str, ok := scope.Get("gorm:query_option"); ok {
			scope.SQL += addExtraSpaceIfExist(fmt.Sprint(str))
		}

		if rows, err := scope.SQLDB().Query(scope.SQL, scope.SQLVars...); scope.Err(err) == nil {
			defer rows.Close()

			// Columns returns the column names.
			columns, _ := rows.Columns()

			// 循环取出查询回的数据，组装数据到 go type
			for rows.Next() {
				scope.db.RowsAffected++

				elem := results
				if isSlice {
					elem = reflect.New(resultType).Elem()
				}

				// 扫描数据到 elem
				// 传入 Fields 是 elem Field 的引用？直接修改它们等于设置 elem 的字段值
				scope.scan(rows, columns, scope.New(elem.Addr().Interface()).Fields())

				if isSlice {
					if isPtr {
						results.Set(reflect.Append(results, elem.Addr()))
					} else {
						results.Set(reflect.Append(results, elem))
					}
				}
			}

			if err := rows.Err(); err != nil {
				scope.Err(err)
			} else if scope.db.RowsAffected == 0 && !isSlice {
				scope.Err(ErrRecordNotFound)
			}
		}
	}
}

// afterQueryCallback will invoke `AfterFind` method after querying
func afterQueryCallback(scope *Scope) {
	if !scope.HasError() {
		scope.CallMethod("AfterFind")
	}
}
