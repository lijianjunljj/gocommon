package curd

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/lijianjunljj/gocommon/utils"

	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var mysql func() *gorm.DB

// Search 搜索结构模型
type Search struct {
	PageSize   int                    `json:"pageSize" validate:"max=100"`
	PageNum    int                    `json:"pageNum"  validate:"max=100"`
	Conditions map[string]interface{} `json:"conditions"`
	SortField  string                 `json:"sortField"`
	SortOrder  string                 `json:"sortOrder"`
}

func (that *Search) Check() error {
	if that.PageSize >= 100 {
		return errors.New("参数错误")
	}
	if that.PageNum >= 100 {
		return errors.New("参数错误")
	}
	return nil
}

// Model 基础模型
type Model struct {
	ID         string `json:"id" gorm:"type:varchar(30);primary_key"`
	CreateBy   string `json:"create_by" gorm:"type:varchar(30)"`
	CreateTime int64  `json:"create_time"`
	UpdateTime int64  `json:"update_time"`
	IsChanged  int8   `json:"is_changed" gorm:"type:tinyint(4) DEFAULT 0"`
	mysql      func() *gorm.DB
	where      string
}

type ModelIdInt struct {
	ID         uint64 `json:"id"          gorm:"primary_key;AUTO_INCREMENT"`
	CreateBy   string `json:"create_by" gorm:"type:varchar(30)"`
	CreateTime int64  `json:"create_time"`
	UpdateTime int64  `json:"update_time"`
	mysql      func() *gorm.DB
	where      string
}

// Where 设置查询条件
func (m *ModelIdInt) Where(where string) *ModelIdInt {
	m.where = where
	return m
}

// Query 解析参数链式查询
func (m *ModelIdInt) Query(search *Search, isHook bool, model interface{}, isPages bool) (int64, error) {
	modelBase := &Model{
		ID:    strconv.FormatUint(m.ID, 10),
		where: m.where,
	}
	return modelBase.Query(search, isHook, model, isPages)
}

// List 通用分页列表查询
func (m *ModelIdInt) List(search *Search, isHook bool, models interface{}) (int64, error) {
	return m.Query(search, isHook, models, true)
}

// All 通用所有列表查询
func (m *ModelIdInt) All(search *Search, isHook bool, models interface{}) error {
	_, err := m.Query(search, isHook, models, false)
	return err
}

// Detail 通用详情查询
func (m *ModelIdInt) Detail(model interface{}) error {
	modelBase := &Model{
		ID: strconv.FormatUint(m.ID, 10),
	}
	return modelBase.Detail(model)
}

// Add 通用新增功能
func (m *ModelIdInt) Add(model interface{}) error {
	modelBase := &Model{}
	return modelBase.Add(model)
}

// Edit 通用编辑功能
func (m *ModelIdInt) Edit(model interface{}) error {
	modelBase := &Model{}
	return modelBase.Edit(model)
}

// Delete 通用删除功能
func (m *ModelIdInt) Delete(model interface{}) error {
	modelBase := &Model{}
	return modelBase.Delete(model)
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
func (m *Model) Where(where string) *Model {
	m.where = where
	return m
}

// Query 解析参数链式查询
func (m *Model) Query(search *Search, isHook bool, model interface{}, isPages bool) (int64, error) {
	var count int64
	db := mysql().Model(model)
	for key, value := range search.Conditions {
		fieldName := utils.CamelToLine(key)
		str := utils.ToStr(value)
		tp := reflect.TypeOf(value)
		if key == "deadline" {
			db = db.Where("deadline > ?", str)
		} else if key == "startTime" {
			db = db.Where("create_time > ?", str)
		} else if key == "endTime" {
			db = db.Where("create_time < ?", str)
		} else if tp.Kind().String() == "string" && key != "user_id" {
			db = db.Where(fieldName+" LIKE  ?", "%"+str+"%")
		} else if tp.Kind().String() == "slice" {
			db = db.Where(fieldName+" IN  (?)", StrToSlice(str))
		} else {
			db = db.Where(fieldName+" =  ?", str)
		}
	}

	if m.where != "" {
		db = db.Where(m.where)
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
	// 直接使用 First，GORM 会根据模型的主键字段自动处理
	// 如果 model 的 ID 字段已经有值，GORM 会自动使用该值作为查询条件
	// 如果 model 的 ID 字段为空，但 m.ID 有值，需要手动设置
	modelValue := reflect.ValueOf(model).Elem()
	modelType := reflect.TypeOf(model).Elem()

	// 查找 ID 字段
	idField, found := modelType.FieldByName("ID")
	if found {
		idValue := modelValue.FieldByName("ID")
		switch idField.Type.Kind() {
		case reflect.String:
			if idValue.String() == "" && m.ID != "" {
				// 如果 model 的 ID 为空，但 m.ID 有值，设置 model 的 ID
				idValue.SetString(m.ID)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if idValue.Uint() == 0 && m.ID != "" {
				// 如果 model 的 ID 为 0，但 m.ID 有值，尝试转换并设置
				if parsedID, err := strconv.ParseUint(m.ID, 10, 64); err == nil {
					idValue.SetUint(parsedID)
				}
			}
		}
	}

	// 直接使用 First，GORM 会根据模型的主键字段自动处理
	result := mysql().First(model)
	return result.Error
}

// Add 通用新增功能
func (m *Model) Add(model interface{}) error {
	result := mysql().Omit(clause.Associations).Create(model)
	return result.Error
}

// Edit 通用编辑功能
func (m *Model) Edit(model interface{}) error {
	result := mysql().Omit(clause.Associations).Save(model)
	return result.Error
}

// Delete 通用删除功能
func (m *Model) Delete(model interface{}) error {
	result := mysql().Debug().Delete(model)
	return result.Error
}
