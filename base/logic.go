package base

import (
	"errors"
	"github.com/lijianjunljj/gocommon/utils"
	"reflect"
	"regexp"
	"strings"
)

// Logic 逻辑层
type Logic struct {
	models interface{}
	svc    *Service
}

const (
	AttrTypeFull int8 = iota
	AttrTypeClean
	AttrTypeHealth
)

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
	search.SortField = utils.CamelToLine(search.SortField)
}

// NewLogic 初始化逻辑
func NewLogic(service interface{}, models ...interface{}) *Logic {
	var mods interface{}
	if len(models) >= 1 {
		mods = models[0]
	}
	return &Logic{
		models: mods,
		svc:    service.(*Service),
	}
}

// List 分页列表
func (l *Logic) List(search *Search, isHook bool, extras ...Extra) (int64, error) {
	parseSearch(search)
	var temp = make(map[string]interface{})
	for key, value := range search.Search {
		str := utils.ToStr(value)
		tp := reflect.TypeOf(value)
		if tp.Kind().String() != "slice" {
			if ok, _ := regexp.Match("^[\u4e00-\u9fa5a-z0-9_-]{0,}$", []byte(str)); !ok {
				return 0, errors.New("查询参数包含非法字符")
			}
		}
		if str != "" {
			temp[key] = value
		}
	}
	search.Search = temp

	count, err := l.svc.API.List(search, isHook, l.models)
	if err != nil {
		return count, err
	}
	for _, extra := range extras {
		err = extra(l.models)
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

// All 不分页列表
func (l *Logic) All(search *Search, isHook bool, extras ...Extra) error {
	parseSearch(search)
	err := l.svc.API.All(search, isHook, l.models)
	if err != nil {
		return err
	}
	for _, extra := range extras {
		err = extra(l.models)
		if err != nil {
			return err
		}
	}
	return err
}

// Add 新增
func (l *Logic) Add(userID string, extras ...Extra) error {
	extraNum := len(extras)
	if extraNum > 0 {
		err := extras[0](l.svc.Model)
		if err != nil {
			return err
		}
	}

	modelTypes := reflect.TypeOf(l.svc.Model).Elem()

	id, _ := modelTypes.FieldByName("ID")
	modelValue := reflect.ValueOf(l.svc.Model).Elem()
	switch id.Type.String() {
	case "string":
		ID, err := l.svc.IDRpc.GetUnixID()
		if err != nil {
			return err
		}
		modelValue.FieldByName("ID").SetString(ID)
		break
	default:
		break
	}
	modelValue.FieldByName("CreateBy").SetString(utils.ToStr(userID))
	modelValue.FieldByName("CreateTime").SetInt(utils.TimeUnix())
	modelValue.FieldByName("UpdateTime").SetInt(utils.TimeUnix())

	err := l.svc.API.Add(l.svc.Model)
	if extraNum > 1 {
		err = extras[1](l.svc.Model)
		if err != nil {
			return err
		}
	}
	return err
}

// Edit 修改
func (l *Logic) Edit(extras ...Extra) error {
	mv := reflect.ValueOf(l.svc.Model).Elem()
	if ID := mv.FieldByName("ID").Interface().(string); ID == "" {
		return errors.New("ID不能为空")
	}
	extraNum := len(extras)
	if extraNum > 0 {
		err := extras[0](l.svc.Model)
		if err != nil {
			return err
		}
	}
	modelValue := reflect.ValueOf(l.svc.Model).Elem()
	modelValue.FieldByName("UpdateTime").SetInt(utils.TimeUnix())
	err := l.svc.API.Edit(l.svc.Model)
	if extraNum > 1 {
		err = extras[1](l.svc.Model)
		if err != nil {
			return err
		}
	}
	return err
}

// Detail 详情
func (l *Logic) Detail(extras ...Extra) error {
	mv := reflect.ValueOf(l.svc.Model).Elem()
	if ID := mv.FieldByName("ID").Interface().(string); ID == "" {
		return errors.New("ID不能为空")
	}
	err := l.svc.API.Detail(l.svc.Model)
	for _, extra := range extras {
		extra(l.svc.Model)
	}
	return err
}

// Delete 删除
func (l *Logic) Delete(str string, extras ...Extra) error {
	ids := StrToSlice(str)
	if len(ids) == 0 {
		return errors.New("ID不能为空")
	}
	for _, item := range ids {
		mv := reflect.ValueOf(l.svc.Model).Elem()
		mv.FieldByName("ID").SetString(item)
		err := l.svc.API.Delete(l.svc.Model)
		if err != nil {
			return err
		}
	}

	for _, extra := range extras {
		err := extra(l.svc.Model)
		if err != nil {
			return err
		}
	}
	return nil
}
