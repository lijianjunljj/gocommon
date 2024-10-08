package curd

import (
	"errors"
	util "github.com/lijianjunljj/gocommon/utils"
	"gorm.io/gorm"
	"reflect"
)

// isInArray 匹配字符串
func isInArray(str string, array []string) bool {
	if len(array) == 0 {
		return false
	}
	for _, mystr := range array {
		if mystr == str {
			return true
		}
	}
	return false
}

func IsInArray(str string, array []string) bool {
	if len(array) == 0 {
		return false
	}
	for _, mystr := range array {
		if mystr == str {
			return true
		}
	}
	return false
}

// SearchQuery 解析参数链式查询
func SearchQuery(db *gorm.DB, search *Search, model interface{}, fuzzyfieldarray []string, isPages bool, isHook bool, fuzzySearchAllow bool) (int64, error) {
	// 初始化搜索参数
	parseSearch(search)
	var count int64
	// 搜索参数
	for key, value := range search.Conditions {
		fieldName := util.CamelToLine(key)
		str := util.ToStr(value)
		tp := reflect.TypeOf(value)
		// 结束时间
		if key == "deadline" {
			db = db.Where("deadline > ?", str)
			// 开始时间
		} else if key == "startTime" {
			db = db.Where("create_time > ?", str)
			// 结束时间
		} else if key == "endTime" {
			db = db.Where("create_time < ?", str)
			// 搜索值为字符串 模糊搜索开启
		} else if tp.Kind().String() == "string" && fuzzySearchAllow {
			// 在模糊搜索字段内 模糊搜索
			if isInArray(fieldName, fuzzyfieldarray) {
				db = db.Where(fieldName+" LIKE  ?", "%"+str+"%")
				// 不在模糊搜索字段内 禁用模糊搜索
			} else {
				db = db.Where(fieldName+" =  ?", str)
			}
			// 搜索值为切片，范围搜索
		} else if tp.Kind().String() == "slice" {
			db = db.Where(fieldName+" IN  (?)", StrToSlice(str))
			// 精确搜索
		} else {
			db = db.Where(fieldName+" =  ?", str)
		}
	}
	// 排序
	db = db.Order(search.SortField + " " + search.SortOrder)
	var result *gorm.DB
	// 分页处理
	if isPages {
		result = db.Count(&count)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return count, result.Error
		}
		db = db.Offset((search.PageNum - 1) * search.PageSize).Limit(search.PageSize)
	}
	// 钩子处理
	if isHook {
		result = db.Find(model)
	} else {
		result = db.Session(&gorm.Session{SkipHooks: true}).Find(model)
	}
	// 无记录
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return 0, result.Error
	}
	return count, nil
}
