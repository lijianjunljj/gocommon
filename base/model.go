package base

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/lijianjunljj/gocommon/utils"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"strings"
)

// Search 搜索结构模型
type Search struct {
	PageSize  int                    `json:"pageSize"`
	PageNum   int                    `json:"pageNum"`
	Search    map[string]interface{} `json:"search"`
	SortField string                 `json:"sortField"`
	SortOrder string                 `json:"sortOrder"`
}

// Model 基础模型
type Model struct {
	ID         string `json:"id" gorm:"type:varchar(30)"`
	CreateBy   string `json:"create_by" gorm:"type:varchar(30)"`
	CreateTime int64  `json:"create_time"`
	UpdateTime int64  `json:"update_time"`
	IsChanged  int8   `json:"is_changed" gorm:"type:tinyint(4) DEFAULT 0"`
}

type ModelIdInt struct {
	ID         uint64 `json:"id"          gorm:"primary_key;AUTO_INCREMENT"`
	CreateBy   string `json:"create_by" gorm:"type:varchar(30)"`
	CreateTime int64  `json:"create_time"`
	UpdateTime int64  `json:"update_time"`
}

// Params 入参ID模型
type Params struct {
	ID string
}
type JSONArray []map[string]interface{}

func (j JSONArray) Value() (driver.Value, error) {
	return json.Marshal(j)
}

func (j *JSONArray) Scan(data interface{}) error {
	return json.Unmarshal(data.([]byte), &j)
}

// Extra 附加处理函数
type Extra func(i interface{}) error

// StrToSlice 切片打印得字符串转切片
func StrToSlice(str string) []string {
	str = strings.ReplaceAll(str, " ", ",")
	str = strings.ReplaceAll(str, "[", "")
	str = strings.ReplaceAll(str, "]", "")
	return strings.Split(str, ",")
}
func (m *Model) Mysql() *gorm.DB {
	return nil
}

// Query 解析参数链式查询
func (m *Model) Query(search *Search, isHook bool, model interface{}, isPages bool) (int64, error) {
	var count int64
	db := m.Mysql().Model(model)
	for key, value := range search.Search {
		fieldName := utils.CamelToLine(key)
		str := utils.ToStr(value)
		tp := reflect.TypeOf(value)
		if key == "deadline" {
			db = db.Where("deadline > ?", str)
		} else if key == "startTime" {
			db = db.Where("create_time > ?", str)
		} else if key == "endTime" {
			db = db.Where("create_time < ?", str)
		} else if tp.Kind().String() == "string" {
			db = db.Where(fieldName+" LIKE  ?", "%"+str+"%")
		} else if tp.Kind().String() == "slice" {
			db = db.Where(fieldName+" IN  (?)", StrToSlice(str))
		} else {
			db = db.Where(fieldName+" =  ?", str)
		}
	}
	db = db.Order(search.SortField + " " + search.SortOrder)
	var result *gorm.DB
	if isPages {
		result = db.Count(&count)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return count, result.Error
		}
		db = db.Offset((search.PageNum - 1) * search.PageSize).Limit(search.PageSize)
	}
	if isHook {
		result = db.Find(model)
	} else {
		result = db.Session(&gorm.Session{SkipHooks: true}).Find(model)
	}

	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return count, result.Error
	}
	return count, nil
}

// List 通用分页列表查询
func (m *Model) List(search *Search, isHook bool, models interface{}) (int64, error) {
	return m.Query(search, isHook, models, true)
}

// All 通用所有列表查询
func (m *Model) All(search *Search, isHook bool, models interface{}) error {
	_, err := m.Query(search, isHook, models, false)
	return err
}

// Detail 通用详情查询
func (m *Model) Detail(model interface{}) error {
	result := m.Mysql().Find(model, m.ID)
	return result.Error
}

// Add 通用新增功能
func (m *Model) Add(model interface{}) error {
	result := m.Mysql().Omit(clause.Associations).Create(model)
	return result.Error
}

// Edit 通用编辑功能
func (m *Model) Edit(model interface{}) error {
	result := m.Mysql().Omit(clause.Associations).Save(model)
	return result.Error
}

// Delete 通用删除功能
func (m *Model) Delete(model interface{}) error {
	result := m.Mysql().Delete(model)
	return result.Error
}
