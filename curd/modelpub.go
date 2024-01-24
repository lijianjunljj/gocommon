package modelpub

import (
	"errors"
	util "game_server/lib/util"
	"reflect"
	"strings"

	"gorm.io/gorm"
)

// Pages 分页列表数据结构
type Pages struct {
	Data     interface{} `json:"data"`
	Count    int64       `json:"count"`
	PageSize int         `json:"pageSize"`
	PageNum  int         `json:"pageNum"`
}

// Search 搜索结构模型
type Search struct {
	PageSize  int                    `json:"pageSize"`
	PageNum   int                    `json:"pageNum"`
	Search    map[string]interface{} `json:"search"`
	SortField string                 `json:"sortField"`
	SortOrder string                 `json:"sortOrder"`
}
type UserNft struct {
	CollectionId string `json:"collection_id"`
	NftId        string `json:"nft_id"`
	NftName      string `json:"nft_name"`
	Picture      string `json:"picture"`
	Coin         string `json:"coin"`
}

// StrToSlice 切片打印得字符串转切片
func StrToSlice(str string) []string {
	str = strings.ReplaceAll(str, " ", ",")
	str = strings.ReplaceAll(str, "[", "")
	str = strings.ReplaceAll(str, "]", "")
	return strings.Split(str, ",")
}

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
func parseSearch(search *Search) {
	if search.PageNum == 0 {
		search.PageNum = 1
	}
	if search.PageSize == 0 {
		search.PageSize = 10
	}
	if search.SortField == "" {
		search.SortField = "create_time"
	}
	if search.SortOrder == "" {
		search.SortOrder = "desc"
	}
	search.SortOrder = strings.Replace(search.SortOrder, "end", "", 1)
	search.SortField = util.CamelToLine(search.SortField)
}
func parseSearch2(search *Search) {
	if search.PageNum == 0 {
		search.PageNum = 1
	}
	if search.PageSize == 0 {
		search.PageSize = 10
	}
	// 20220629 修改默认排序
	// 由创建时间修改为id
	if search.SortField == "" {
		// search.SortField = "created_at"
		search.SortField = "id"
	}
	if search.SortOrder == "" {
		search.SortOrder = "desc"
	}
	search.SortOrder = strings.Replace(search.SortOrder, "end", "", 1)
	search.SortField = util.CamelToLine(search.SortField)
}

// SearchQuery 解析参数链式查询
func SearchQuery(db *gorm.DB, search *Search, model interface{}, fazzyfieldarray []string, isPages bool, isHook bool, fazzySearchAllow bool) (int64, error) {
	// 初始化搜索参数
	parseSearch(search)
	var count int64
	// 搜索参数
	for key, value := range search.Search {
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
		} else if tp.Kind().String() == "string" && fazzySearchAllow {
			// 在模糊搜索字段内 模糊搜索
			if isInArray(fieldName, fazzyfieldarray) {
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

// SearchQuery 解析参数链式查询
func SearchQueryNew(db *gorm.DB, search *Search, model interface{}, fazzyfieldarray []string, isPages bool, isHook bool, fazzySearchAllow bool) (int64, error) {
	// 初始化搜索参数
	parseSearch2(search)
	var count int64
	// 搜索参数
	for key, value := range search.Search {
		fieldName := util.CamelToLine(key)
		str := util.ToStr(value)
		tp := reflect.TypeOf(value)

		// 创建时间
		if key == "created_at" {
			db = db.Where("created_at > ?", str)
			// 开始时间
		} else if key == "start_time" {
			db = db.Where("start_time > ?", str)
			// 结束时间
		} else if key == "end_time" {
			db = db.Where("end_time < ?", str)
			// 搜索值为字符串 模糊搜索开启
		} else if tp.Kind().String() == "string" && fazzySearchAllow {
			// 在模糊搜索字段内 模糊搜索
			if isInArray(fieldName, fazzyfieldarray) {
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
