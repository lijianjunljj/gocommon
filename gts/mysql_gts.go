package gts

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	configLogic "myserver/src/config/tables"

	"github.com/lijianjunljj/gocommon/curd"
	"gorm.io/gorm"
)

var (
	Gts *MysqlGts
)

// NotSyncToDatabase 是否需要同步到数据库的标记类型
// 注意：这是一个自定义类型（底层是 bool），用于在模型中匿名嵌入：
//
//	type FigureAttribute struct {
//	    ...
//	    gts.NotSyncToDatabase `json:"-"`
//	}
//
// 通过反射只识别这个具体类型，避免其它匿名 bool 字段误触发。
type NotSyncToDatabase bool

func InitGts() {
	if Gts == nil {
		Gts = NewMysqlGts(curd.Mysql())
		// 初始化配置表（必须在初始化其他表之前）
		configLogic.Init()
		// 初始化表
		// 启动同步
		Gts.StartSync()
	}
}

// InitGtsWithModels 初始化 GTS 并注册额外的模型
func InitGtsWithModels(models ...interface{}) error {
	if Gts == nil {
		InitGts()
	}
	return Gts.InitTable(models...)
}

// TableName 接口，用于获取表名
type TableName interface {
	TableName() string
}

// MysqlGts MySQL 内存表管理工具，类似 Erlang ETS
type MysqlGts struct {
	db           *gorm.DB               // GORM 数据库实例
	tables       map[string]*TableCache // 表缓存，key 为表名
	opLogsMap    sync.Map               // 每表一个无锁队列：key=表名(string), value=*tableOpLogQueue，recordOperation 只锁当前表
	mu           sync.RWMutex           // 读写锁（仅保护 tables，不再保护 opLogs）
	syncInterval time.Duration          // 同步间隔（秒）
	batchSize    int                    // 每次处理的操作日志数量
	stopChan     chan struct{}          // 停止信号
	syncRunning  bool                   // 同步是否正在运行
}

// ModelReflectInfo 模型反射信息缓存
type ModelReflectInfo struct {
	ModelType      reflect.Type      // 模型类型
	ModelValueType reflect.Type      // 模型值类型（非指针）
	FieldMap       map[string]int    // 字段名 -> 字段索引映射
	FieldInfo      []FieldInfo       // 字段信息列表
	PrimaryKeyIdx  int               // 主键字段索引
	CamelCaseCache map[string]string // 下划线字段名 -> 驼峰字段名缓存
	cacheMu        sync.RWMutex      // 保护 CamelCaseCache 的读写锁
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name         string       // 字段名
	Index        int          // 字段索引
	Type         reflect.Type // 字段类型
	CanSet       bool         // 是否可设置
	CanInterface bool         // 是否可获取接口值
}

// TableCache 表缓存结构
// data 使用 sync.Map 以避免对整张表加锁，结合 rowLocks 做行级并发控制
type TableCache struct {
	data        sync.Map          // 数据缓存，key 为主键 ID(string)，value 为模型实例
	loaded      bool              // 是否已加载
	mu          sync.RWMutex      // 仅用于保护 loaded 等元数据，不再作为表级数据锁
	rowLocks    sync.Map          // 行级别的读写锁，key 为主键 ID，value 为 *sync.RWMutex
	primaryKey  string            // 主键字段名，默认为 "ID"
	reflectInfo *ModelReflectInfo // 反射信息缓存
	// 联合二级索引：index[表字段][字段值] = id 集合；多条件查询时取交集
	// index: key=fieldName(string), value=*sync.Map(key=valueStr, value=*sync.Map(key=id, value=struct{}))
	index         sync.Map
	indexReverse  sync.Map // key=id(string), value=*sync.Map(key="field|value", value=struct{})，用于 Update/Delete 时清理与重建
	indexedFields sync.Map // key=fieldName(string), value=struct{}，记录参与过索引的字段，用于重建
}

// OperationLog 操作日志
type OperationLog struct {
	Operation string                 // 操作类型：insert, update, delete
	Data      map[string]interface{} // 操作数据
	ID        string                 // 主键 ID
	Timestamp time.Time              // 操作时间
}

// tableOpLogQueue 单表操作日志队列，仅本表加锁，减少与其它表的竞争
type tableOpLogQueue struct {
	mu   sync.Mutex
	logs []*OperationLog
}

// NewMysqlGts 创建新的 MysqlGts 实例
func NewMysqlGts(db *gorm.DB) *MysqlGts {
	return &MysqlGts{
		db:           db,
		tables:       make(map[string]*TableCache),
		syncInterval: 1 * time.Second, // 默认 1 秒同步一次
		batchSize:    100,             // 默认每次处理 100 条
		stopChan:     make(chan struct{}),
		syncRunning:  false,
	}
}

// buildIndexKey 根据 First/Find 的查询条件构建一个稳定的字符串 key，用于二级索引。
// 逻辑简单粗暴：依次将 conds 序列化为字符串并用 "|" 连接。
// 注意：仅用于缓存命中加速，不参与业务逻辑判断。
func buildIndexKey(conds ...interface{}) string {
	if len(conds) == 0 {
		return ""
	}
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		parts = append(parts, fmt.Sprint(c))
	}
	return strings.Join(parts, "|")
}

// fieldValuePair 联合索引的 (表字段, 字段值) 对
type fieldValuePair struct {
	Field string
	Value string
}

// parseCondsToFieldValues 从 GORM 风格 conds 解析出 (字段, 值) 列表，用于联合索引。
// 支持以下几种典型形式：
//  1. 单条：Find(&game, "anchor_info_id = ?", 123)
//  2. 多字段：Find(&game, "anchor_info_id = ? and level = ?", 123, 0)
//  3. 常量 + 占位混合：Find(&game, "anchor_info_id = ? and level = 0", 123)
func parseCondsToFieldValues(conds ...interface{}) []fieldValuePair {
	var pairs []fieldValuePair
	if len(conds) == 0 {
		return pairs
	}

	// 目前主要处理第一段 string 条件 + 后续参数的常见写法
	queryStr, ok := conds[0].(string)
	if !ok {
		return pairs
	}
	queryStr = strings.TrimSpace(queryStr)
	if queryStr == "" {
		return pairs
	}

	// 归一化空白，便于按 " and " 切分
	normalized := collapseSpaces(strings.ToLower(queryStr))
	segments := strings.Split(normalized, " and ")
	// 保留原始 queryStr 做右值截取，避免大小写影响
	raw := collapseSpaces(queryStr)

	values := conds[1:]
	valIdx := 0

	// 用一个游标在 raw 串上同步前进，和 normalized 的 segments 保持结构一致
	rawRest := raw
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// 在 rawRest 中找到当前 seg 的位置，以便获得原始片段（含具体常量）
		pos := strings.Index(strings.ToLower(rawRest), seg)
		if pos < 0 {
			continue
		}
		part := strings.TrimSpace(rawRest[pos : pos+len(seg)])
		rawRest = rawRest[pos+len(seg):]

		// 拆分 field = expr
		idx := strings.Index(part, "=")
		if idx <= 0 || idx == len(part)-1 {
			continue
		}
		field := strings.TrimSpace(part[:idx])
		right := strings.TrimSpace(part[idx+1:])
		if field == "" || right == "" {
			continue
		}

		// 右侧以 ? 开头：使用 conds 中的值
		if right[0] == '?' {
			if valIdx >= len(values) {
				continue
			}
			value := fmt.Sprint(values[valIdx])
			valIdx++
			pairs = append(pairs, fieldValuePair{Field: field, Value: value})
			continue
		}

		// 右侧是常量，例如 0 / 1 / 'foo'，直接使用字面量
		lit := right
		if strings.HasPrefix(lit, "'") && strings.HasSuffix(lit, "'") && len(lit) >= 2 {
			lit = lit[1 : len(lit)-1]
		}
		pairs = append(pairs, fieldValuePair{Field: field, Value: lit})
	}

	return pairs
}

/*
	旧实现：仅支持 "field = ?" 且一个 conds 对应一个字段，不支持 "level = 0" 这类常量写法。
	保留注释供参考。

	// 提取所有字段名，例如 "anchor_info_id = ? and level = ?" -> ["anchor_info_id","level"]
	fields := extractAllFieldNames(queryStr)
	if len(fields) == 0 {
		return pairs
	}

	// 统计 ? 的个数，决定最多能消费多少个值
	placeholderCount := 0
	for _, ch := range queryStr {
		if ch == '?' {
			placeholderCount++
		}
	}
	// 实际可用的值数量 = min(placeholderCount, len(conds)-1)
	maxVals := placeholderCount
	if maxVals > len(conds)-1 {
		maxVals = len(conds) - 1
	}
	if maxVals <= 0 {
		return pairs
	}

	// 将前 maxVals 个字段名与 conds[1:maxVals+1] 依次配对
	for i := 0; i < maxVals && i < len(fields); i++ {
		field := strings.TrimSpace(fields[i])
		if field == "" {
			continue
		}
		value := fmt.Sprint(conds[1+i])
		pairs = append(pairs, fieldValuePair{Field: field, Value: value})
	}

	return pairs
}
*/

// indexAddId 将 id 加入联合索引的 (field, value) 集合，并记录到 indexReverse 与 indexedFields
func (m *MysqlGts) indexAddId(tableCache *TableCache, id string, pairs []fieldValuePair) {
	if len(pairs) == 0 {
		// 没有任何字段时，也至少按主键 ID 建一个索引项
		if tableCache != nil && tableCache.primaryKey != "" {
			pairs = append(pairs, fieldValuePair{
				Field: strings.ToLower(tableCache.primaryKey),
				Value: id,
			})
		} else {
			return
		}
	}

	// 如果传入的 pairs 中没有主键字段，也强制补上一条主键索引，避免只按非主键字段建索引导致后续清理困难
	if tableCache != nil && tableCache.primaryKey != "" {
		hasPrimary := false
		for _, p := range pairs {
			if p.Field == tableCache.primaryKey {
				hasPrimary = true
				break
			}
		}
		if !hasPrimary {
			pairs = append(pairs, fieldValuePair{
				Field: strings.ToLower(tableCache.primaryKey),
				Value: id,
			})
		}
	}

	reverseSet, _ := tableCache.indexReverse.LoadOrStore(id, &sync.Map{})
	revMap := reverseSet.(*sync.Map)
	for _, p := range pairs {
		tableCache.indexedFields.Store(p.Field, struct{}{})
		fieldMapVal, _ := tableCache.index.LoadOrStore(p.Field, &sync.Map{})
		fieldMap := fieldMapVal.(*sync.Map)
		valueSetVal, _ := fieldMap.LoadOrStore(p.Value, &sync.Map{})
		valueSet := valueSetVal.(*sync.Map)
		valueSet.Store(id, struct{}{})
		revMap.Store(p.Field+"|"+p.Value, struct{}{})
	}
}

// indexRemoveId 从联合索引中移除该 id 在所有 (field, value) 集合中的记录，并清理 indexReverse
func (m *MysqlGts) indexRemoveId(tableCache *TableCache, id string) {
	revVal, ok := tableCache.indexReverse.Load(id)
	if !ok {
		return
	}
	revMap := revVal.(*sync.Map)
	revMap.Range(func(k, _ interface{}) bool {
		keyStr := k.(string)
		parts := strings.SplitN(keyStr, "|", 2)
		if len(parts) != 2 {
			return true
		}
		field, valueStr := parts[0], parts[1]
		fieldMapVal, ok := tableCache.index.Load(field)
		if !ok {
			return true
		}
		fieldMap := fieldMapVal.(*sync.Map)
		valueSetVal, ok := fieldMap.Load(valueStr)
		if !ok {
			return true
		}
		valueSet := valueSetVal.(*sync.Map)
		valueSet.Delete(id)

		//如果valueSet为空，则删除fieldMap

		empty := true
		valueSet.Range(func(_, _ interface{}) bool {
			empty = false
			return false
		})
		if empty {
			fieldMap.Delete(valueStr)
		}

		return true
	})
	tableCache.indexReverse.Delete(id)
}

// indexFieldsForId 从 indexReverse 中取出该 id 曾参与过的字段名列表（用于 Update 后重建）
func (m *MysqlGts) indexFieldsForId(tableCache *TableCache, id string) []string {
	revVal, ok := tableCache.indexReverse.Load(id)
	if !ok {
		return nil
	}
	revMap := revVal.(*sync.Map)
	seen := make(map[string]struct{})
	var fields []string
	revMap.Range(func(k, _ interface{}) bool {
		keyStr := k.(string)
		idx := strings.Index(keyStr, "|")
		if idx > 0 {
			field := keyStr[:idx]
			if _, ok := seen[field]; !ok {
				seen[field] = struct{}{}
				fields = append(fields, field)
			}
		}
		return true
	})
	return fields
}

// indexRebuildForId 根据 model 当前字段值，将该 id 重新加入联合索引（Update 后调用）
func (m *MysqlGts) indexRebuildForId(tableCache *TableCache, id string, model interface{}, fields []string) {
	if len(fields) == 0 {
		return
	}
	pairs := make([]fieldValuePair, 0, len(fields))
	for _, f := range fields {
		v := m.getFieldValue(model, f, tableCache)
		pairs = append(pairs, fieldValuePair{Field: f, Value: v})
	}
	m.indexAddId(tableCache, id, pairs)
}

// indexLookupIds 多条件取交集：根据 (field, value) 列表从联合索引查出 id 集合（交集）
func (m *MysqlGts) indexLookupIds(tableCache *TableCache, pairs []fieldValuePair) []string {
	if len(pairs) == 0 {
		return nil
	}
	var intersect []string
	for i, p := range pairs {
		fieldMapVal, ok := tableCache.index.Load(p.Field)
		if !ok {
			return nil
		}
		fieldMap := fieldMapVal.(*sync.Map)
		valueSetVal, ok := fieldMap.Load(p.Value)
		if !ok {
			return nil
		}
		valueSet := valueSetVal.(*sync.Map)
		var ids []string
		valueSet.Range(func(k, _ interface{}) bool {
			ids = append(ids, k.(string))
			return true
		})
		if i == 0 {
			intersect = ids
		} else {
			set := make(map[string]struct{})
			for _, id := range intersect {
				set[id] = struct{}{}
			}
			var next []string
			for _, id := range ids {
				if _, ok := set[id]; ok {
					next = append(next, id)
				}
			}
			intersect = next
		}
		if len(intersect) == 0 {
			return nil
		}
	}
	return intersect
}

// SetSyncInterval 设置同步间隔（秒）
func (m *MysqlGts) SetSyncInterval(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncInterval = time.Duration(seconds) * time.Second
}

// SetBatchSize 设置每次处理的操作日志数量
func (m *MysqlGts) SetBatchSize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchSize = size
}

// InitTable 初始化一个或多个表
func (m *MysqlGts) InitTable(models ...interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, model := range models {
		tableName, err := m.getTableName(model)
		if err != nil {
			return fmt.Errorf("获取表名失败: %v", err)
		}

		// 如果表已存在，跳过
		if _, exists := m.tables[tableName]; exists {
			continue
		}

		// 获取主键字段名
		primaryKey := m.getPrimaryKey(model)

		// 预先缓存反射信息
		reflectInfo := m.cacheReflectInfo(model, primaryKey)

		// 初始化表缓存
		m.tables[tableName] = &TableCache{
			data:        sync.Map{},
			loaded:      false,
			rowLocks:    sync.Map{},
			primaryKey:  primaryKey,
			reflectInfo: reflectInfo,
		}

	}

	return nil
}

// cacheReflectInfo 缓存模型的反射信息
func (m *MysqlGts) cacheReflectInfo(model interface{}, primaryKey string) *ModelReflectInfo {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	info := &ModelReflectInfo{
		ModelType:      reflect.TypeOf(model),
		ModelValueType: modelType,
		FieldMap:       make(map[string]int),
		FieldInfo:      make([]FieldInfo, 0, modelType.NumField()),
		PrimaryKeyIdx:  -1,
		CamelCaseCache: make(map[string]string),
	}

	// 遍历所有字段，缓存字段信息
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		fieldName := field.Name

		// 缓存字段映射
		info.FieldMap[fieldName] = i

		// 缓存字段信息
		fieldInfo := FieldInfo{
			Name:         fieldName,
			Index:        i,
			Type:         field.Type,
			CanSet:       true, // 在运行时检查
			CanInterface: true, // 在运行时检查
		}
		info.FieldInfo = append(info.FieldInfo, fieldInfo)

		// 记录主键字段索引
		if fieldName == primaryKey {
			info.PrimaryKeyIdx = i
		}
	}

	return info
}

// Load 加载数据到缓存；可选传入查询条件。
// 传入条件时，按条件从数据库加载并写入缓存。
// 未传入条件时，若 model 带有主键 ID 则按主键查询加载该条，否则加载整表。
func (m *MysqlGts) Load(model interface{}, conds ...interface{}) error {
	tableName, err := m.getTableName(model)
	if err != nil {
		return fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	// 未传入条件时，使用唯一主键 ID 查询（若 model 有主键值）
	if len(conds) == 0 {
		id := m.getIDValue(model, tableCache.primaryKey, tableCache)
		if id != "" && id != "0" {
			conds = []interface{}{tableCache.primaryKey + " = ?", id}
		}
	}

	// 从数据库加载数据（复用 FindFromDB 的查询与回写缓存逻辑）
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// 创建切片用于存储查询结果
	sliceType := reflect.SliceOf(reflect.PtrTo(modelType))
	sliceValue := reflect.New(sliceType)
	slice := sliceValue.Interface()

	if err := m.FindFromDB(slice, conds...); err != nil {
		return fmt.Errorf("加载表数据失败: %v", err)
	}

	tableCache.mu.Lock()
	tableCache.loaded = true
	tableCache.mu.Unlock()
	return nil
}

// First 查询第一条记录
func (m *MysqlGts) First(model interface{}, conds ...interface{}) error {
	// 先尝试从缓存中查询
	found, err := m.FirstFromCache(model, conds...)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	// 缓存未命中，再从数据库查询并写回缓存
	return m.FirstFromDB(model, conds...)
}

// FirstFromCache 仅从缓存中查询第一条记录（不访问数据库）
// 返回值 found 表示是否在缓存中命中记录
func (m *MysqlGts) FirstFromCache(model interface{}, conds ...interface{}) (found bool, err error) {
	tableName, err := m.getTableName(model)
	if err != nil {
		return false, fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	// 如果缓存未加载，先加载
	tableCache.mu.RLock()
	loaded := tableCache.loaded
	tableCache.mu.RUnlock()

	if !loaded {
		if err := m.Load(model, conds...); err != nil {
			return false, err
		} else {
			// fmt.Println("缓存未命中，但加载成功")
		}
	}
	// 1. 然后处理常见的按主键 ID 查询的简单场景：
	//   First(model, "id = ?", id)
	// 这种情况可以直接通过主键命中，避免遍历整个表和反射匹配。
	if len(conds) > 0 {
		if queryStr, ok := conds[0].(string); ok && len(conds) >= 2 {
			normalized := strings.ToLower(strings.TrimSpace(queryStr))
			if normalized == "id = ?" || normalized == "id=?" || normalized == "id= ?" {
				idStr := fmt.Sprint(conds[1])
				if idStr != "" && idStr != "0" {
					// 使用行级读锁 + sync.Map 直接命中
					rowLock := tableCache.getRowLock(idStr)
					rowLock.RLock()
					if cached, ok := tableCache.data.Load(idStr); ok {
						// fmt.Println("通过主键命中", cached)
						err := m.copyModel(cached, model)
						rowLock.RUnlock()
						return true, err
					}
					rowLock.RUnlock()
				}
			}
		}
	}

	// 2. 联合二级索引：多条件取交集，取第一条
	pairs := parseCondsToFieldValues(conds...)
	if len(pairs) > 0 {
		ids := m.indexLookupIds(tableCache, pairs)
		if len(ids) > 0 {
			idStr := ids[0]
			rowLock := tableCache.getRowLock(idStr)
			rowLock.RLock()
			if cached, ok := tableCache.data.Load(idStr); ok {
				err := m.copyModel(cached, model)
				rowLock.RUnlock()
				return true, err
			}
			rowLock.RUnlock()
		}
	}

	// 从缓存中查找（只使用行锁 + sync.Map，不使用表级锁），按条件遍历匹配
	var foundItem interface{}
	found = false
	compiled := m.compileConds(conds, tableCache)
	// 如果有查询条件，需要匹配
	if len(conds) > 0 {
		// 简单的条件匹配（可以根据需要扩展）
		tableCache.data.Range(func(_, v interface{}) bool {
			if compiled != nil {
				if m.matchConditionsCompiled(v, compiled, tableCache) {
					foundItem = v
					found = true
					return false
				}
			} else if m.matchConditions(v, conds, tableCache) {
				foundItem = v
				found = true
				return false
			}
			return true
		})
	} else {
		// 没有条件，返回第一条
		tableCache.data.Range(func(_, v interface{}) bool {
			foundItem = v
			found = true
			return false
		})
	}

	// fmt.Println("found：%V", found)
	// fmt.Println("foundItem：%V", foundItem)

	// 如果缓存中找到，使用行级读锁来安全地复制数据
	if found {
		// 获取找到记录的 ID
		foundID := m.getIDValue(foundItem, tableCache.primaryKey, tableCache)
		if foundID != "" {
			// 获取行级读锁
			rowLock := tableCache.getRowLock(foundID)
			rowLock.RLock()
			err := m.copyModel(foundItem, model)
			rowLock.RUnlock()
			// 命中后回写联合二级索引
			if len(pairs) > 0 {
				m.indexAddId(tableCache, foundID, pairs)
			}
			return true, err
		}
		return true, m.copyModel(foundItem, model)
	}

	// 缓存未命中
	return false, nil
}

// FirstFromDB 仅从数据库查询第一条记录，并回填到缓存中
func (m *MysqlGts) FirstFromDB(model interface{}, conds ...interface{}) error {
	tableName, err := m.getTableName(model)
	if err != nil {
		return fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	pairs := parseCondsToFieldValues(conds...)

	// 从数据库查询
	query := m.buildQuery(model, conds...)

	if err := query.First(model).Error; err != nil {
		return err
	}

	// 查询成功，将结果添加到缓存并更新联合二级索引
	id := m.getIDValue(model, tableCache.primaryKey, tableCache)
	if id != "" && id != "0" {
		// 获取行级写锁
		rowLock := tableCache.getRowLock(id)
		rowLock.Lock()
		defer rowLock.Unlock()

		// 创建副本以避免引用问题
		modelType := reflect.TypeOf(model)
		if modelType.Kind() == reflect.Ptr {
			modelType = modelType.Elem()
		}
		cachedItem := reflect.New(modelType).Interface()
		if err := m.copyModel(model, cachedItem); err == nil {
			tableCache.data.Store(id, cachedItem)
			m.indexAddId(tableCache, id, pairs)
		}
	}

	return nil
}

// Find 查询多条记录
func (m *MysqlGts) Find(dest interface{}, conds ...interface{}) error {
	// 先尝试从缓存中查询
	found, err := m.FindFromCache(dest, conds...)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	// 缓存未命中，再从数据库查询并写回缓存
	return m.FindFromDB(dest, conds...)
}

// FindFromCache 仅从缓存中查询多条记录（不访问数据库）
// 返回值 found 表示是否在缓存中命中至少一条记录
func (m *MysqlGts) FindFromCache(dest interface{}, conds ...interface{}) (found bool, err error) {
	tableName, err := m.getTableNameFromDest(dest)
	if err != nil {
		return false, fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	// 需要从 dest 中获取模型类型
	destType := reflect.TypeOf(dest).Elem()
	modelType := destType.Elem()
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	model := reflect.New(modelType).Interface()

	// 如果缓存未加载，先加载
	tableCache.mu.RLock()
	loaded := tableCache.loaded
	tableCache.mu.RUnlock()

	if !loaded {
		if err := m.Load(model, conds...); err != nil {
			return false, err
		} else {
		}
	}

	// 联合二级索引：多条件取交集
	pairs := parseCondsToFieldValues(conds...)
	compiled := m.compileConds(conds, tableCache)
	indexHitIds := make(map[string]struct{})
	var cachedResults []interface{}
	if len(pairs) > 0 {
		ids := m.indexLookupIds(tableCache, pairs)
		for _, idStr := range ids {
			rowLock := tableCache.getRowLock(idStr)
			rowLock.RLock()
			if cached, ok := tableCache.data.Load(idStr); ok {
				if len(conds) == 0 {
					cachedResults = append(cachedResults, cached)
					indexHitIds[idStr] = struct{}{}
				} else if compiled != nil {
					if m.matchConditionsCompiled(cached, compiled, tableCache) {
						cachedResults = append(cachedResults, cached)
						indexHitIds[idStr] = struct{}{}
					}
				} else if m.matchConditions(cached, conds, tableCache) {
					cachedResults = append(cachedResults, cached)
					indexHitIds[idStr] = struct{}{}
				}
			}
			rowLock.RUnlock()
		}
	}

	// 遍历 sync.Map 匹配条件（跳过已从联合索引命中的 id，避免重复）
	tableCache.data.Range(func(_, v interface{}) bool {
		id := m.getIDValue(v, tableCache.primaryKey, tableCache)
		if _, skip := indexHitIds[id]; skip {
			return true
		}
		if len(conds) == 0 {
			cachedResults = append(cachedResults, v)
		} else if compiled != nil {
			if m.matchConditionsCompiled(v, compiled, tableCache) {
				cachedResults = append(cachedResults, v)
				if len(pairs) > 0 {
					m.indexAddId(tableCache, id, pairs)
				}
			}
		} else if m.matchConditions(v, conds, tableCache) {
			cachedResults = append(cachedResults, v)
			if len(pairs) > 0 {
				m.indexAddId(tableCache, id, pairs)
			}
		}
		return true
	})

	// 创建结果切片
	destValue := reflect.ValueOf(dest).Elem()

	// 如果缓存中有结果，先添加到 dest
	for _, item := range cachedResults {
		newItem := reflect.New(modelType).Interface()
		if err := m.copyModel(item, newItem); err != nil {
			continue
		}
		// 根据目标切片元素类型追加，避免 *T / T 类型不匹配导致的 panic
		elemValue := reflect.ValueOf(newItem)
		// 如果目标元素不是指针而 newItem 是指针，则取 Elem
		if destValue.Type().Elem().Kind() != elemValue.Kind() && elemValue.Kind() == reflect.Ptr &&
			destValue.Type().Elem() == elemValue.Type().Elem() {
			elemValue = elemValue.Elem()
		}
		destValue.Set(reflect.Append(destValue, elemValue))
	}

	if len(cachedResults) > 0 {
		return true, nil
	}

	// 缓存未命中
	return false, nil
}

// FindFromDB 仅从数据库查询多条记录，并回填到缓存中
func (m *MysqlGts) FindFromDB(dest interface{}, conds ...interface{}) error {
	tableName, err := m.getTableNameFromDest(dest)
	if err != nil {
		return fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	// 需要从 dest 中获取模型类型
	destType := reflect.TypeOf(dest).Elem()
	modelType := destType.Elem()
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	model := reflect.New(modelType).Interface()

	pairs := parseCondsToFieldValues(conds...)

	// 从数据库查询
	query := m.buildQuery(model, conds...)

	if err := query.Find(dest).Error; err != nil {
		return err
	}

	// 查询成功，将结果添加到缓存并更新联合二级索引
	destVal := reflect.ValueOf(dest).Elem()
	for i := 0; i < destVal.Len(); i++ {
		item := destVal.Index(i).Interface()
		id := m.getIDValue(item, tableCache.primaryKey, tableCache)
		if id != "" && id != "0" {
			// 获取行级写锁
			rowLock := tableCache.getRowLock(id)
			rowLock.Lock()

			// 创建副本以避免引用问题
			cachedItem := reflect.New(modelType).Interface()
			if err := m.copyModel(item, cachedItem); err == nil {
				tableCache.data.Store(id, cachedItem)
			}
			m.indexAddId(tableCache, id, pairs)

			rowLock.Unlock()
		}
	}

	return nil
}

// buildQuery 根据传入的模型和条件构造 GORM 查询（统一处理占位符与参数数量）
func (m *MysqlGts) buildQuery(model interface{}, conds ...interface{}) *gorm.DB {
	query := m.db.Model(model)
	if len(conds) == 0 {
		return query
	}

	// 处理查询条件
	// GORM 风格的查询条件：第一个参数是查询字符串，后续参数是值
	if queryStr, ok := conds[0].(string); ok {
		// 统计查询字符串中 ? 的数量
		placeholderCount := 0
		for _, char := range queryStr {
			if char == '?' {
				placeholderCount++
			}
		}

		// 如果查询字符串包含占位符，需要传递相应数量的参数
		if placeholderCount > 0 {
			if len(conds) > placeholderCount {
				// 传递所有参数值
				query = query.Where(queryStr, conds[1:1+placeholderCount]...)
			} else if len(conds) == placeholderCount+1 {
				// 参数数量刚好匹配
				query = query.Where(queryStr, conds[1:]...)
			} else {
				// 参数不足，只传递已有的参数
				query = query.Where(queryStr, conds[1:]...)
			}
		} else {
			// 没有占位符，直接使用查询字符串
			query = query.Where(queryStr)
		}
	} else {
		// 第一个参数不是字符串，可能是主键值
		query = query.Where(conds[0])
	}
	return query
}

// Update 更新记录
func (m *MysqlGts) Update(model interface{}, values map[string]interface{}) error {
	// fmt.Println("Update model: %v, values: %v", model, values)
	tableName, err := m.getTableName(model)
	if err != nil {
		return fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	// 如果缓存未加载，先加载
	tableCache.mu.RLock()
	loaded := tableCache.loaded
	tableCache.mu.RUnlock()

	if !loaded {
		// 无条件时 Load 内部会按 model 的主键 ID 查询
		if err := m.Load(model); err != nil {
			return err
		}
	}

	id := m.getIDValue(model, tableCache.primaryKey, tableCache)
	if id == "" {
		return errors.New("无法获取主键 ID")
	}
	// 获取行级锁
	rowLock := tableCache.getRowLock(id)
	rowLock.Lock()
	defer rowLock.Unlock()

	// 使用表级读锁来读取数据（因为需要访问 data map）
	v, exists := tableCache.data.Load(id)
	if !exists {
		return gorm.ErrRecordNotFound
	}
	item := v
	// 更新数据（使用缓存的反射信息）
	itemValue := reflect.ValueOf(item).Elem()
	for key, value := range values {
		camelKey := m.toCamelCase(key, tableCache)
		field := itemValue.FieldByName(camelKey)
		if field.IsValid() && field.CanSet() {
			field.Set(reflect.ValueOf(value))
		}
	}

	// 使用行级锁 + sync.Map 更新 data
	tableCache.data.Store(id, item)

	// 重建二级索引：先取出该 id 曾参与的字段名，移除旧索引，再按当前 model 回填
	fields := m.indexFieldsForId(tableCache, id)
	m.indexRemoveId(tableCache, id)
	m.indexRebuildForId(tableCache, id, item, fields)

	// 记录操作日志
	m.recordOperation(tableName, "update", id, item)

	return nil
}
func (m *MysqlGts) Delete(model interface{}, conds ...interface{}) error {
	tableName, toDeleteIds, deleteItems, err := m.DeleteFromCache(model, conds...)
	if err != nil {
		return err
	}
	// 记录操作日志
	for i, id := range toDeleteIds {
		m.recordOperation(tableName, "delete", id, deleteItems[i])
	}

	return nil
}
func (m *MysqlGts) UnLoadFromCache(model interface{}, conds ...interface{}) error {
	_, _, _, err := m.DeleteFromCache(model, conds...)
	return err
}

// Delete 删除记录
func (m *MysqlGts) DeleteFromCache(model interface{}, conds ...interface{}) (string, []string, []interface{}, error) {
	tableName, err := m.getTableName(model)
	if err != nil {
		return "", nil, nil, fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return "", nil, nil, fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	// 未传入条件时，必须依赖 model 的主键 ID 精确删除单条记录；
	// 若无法获取主键 ID，则直接报错，避免误删整表或产生歧义。
	var primaryID string
	if len(conds) == 0 {
		id := m.getIDValue(model, tableCache.primaryKey, tableCache)
		if id == "" || id == "0" {
			return "", nil, nil, fmt.Errorf("DeleteFromCache 未传入条件且无法从 model 获取主键 ID（字段名: %s），请确认模型包含有效的 ID 字段", tableCache.primaryKey)
		}
		primaryID = id
	}

	// 如果缓存未加载，先加载
	tableCache.mu.RLock()
	loaded := tableCache.loaded
	tableCache.mu.RUnlock()

	if !loaded {
		return "", nil, nil, errors.New("缓存未加载")
	}

	// 特殊分支：未传入条件且拿到了主键 ID，直接按主键精确删除，避免全表遍历和 matchConditions
	if primaryID != "" {
		// 获取该行的行级锁
		rowLock := tableCache.getRowLock(primaryID)
		rowLock.Lock()
		defer rowLock.Unlock()

		// 从缓存中加载要删除的记录
		v, ok := tableCache.data.Load(primaryID)
		if !ok {
			return "", nil, nil, gorm.ErrRecordNotFound
		}

		// 删除缓存与索引
		tableCache.data.Delete(primaryID)
		tableCache.rowLocks.Delete(primaryID)
		m.indexRemoveId(tableCache, primaryID)
		return tableName, []string{primaryID}, []interface{}{v}, nil
	}

	var toDelete []string
	var deleteItems []interface{}
	var compiled []compiledCond

	// 若有查询条件，优先使用联合二级索引命中 id，减少全表遍历
	indexHitIds := make(map[string]struct{})
	if len(conds) > 0 {
		pairs := parseCondsToFieldValues(conds...)
		compiled = m.compileConds(conds, tableCache)
		if len(pairs) > 0 {
			ids := m.indexLookupIds(tableCache, pairs)
			for _, idStr := range ids {
				rowLock := tableCache.getRowLock(idStr)
				rowLock.RLock()
				if cached, ok := tableCache.data.Load(idStr); ok {
					if compiled != nil {
						if m.matchConditionsCompiled(cached, compiled, tableCache) {
							toDelete = append(toDelete, idStr)
							deleteItems = append(deleteItems, cached)
							indexHitIds[idStr] = struct{}{}
						}
					} else if m.matchConditions(cached, conds, tableCache) {
						toDelete = append(toDelete, idStr)
						deleteItems = append(deleteItems, cached)
						indexHitIds[idStr] = struct{}{}
					}
				}
				rowLock.RUnlock()
			}
		}
	}

	fmt.Println("indexHitIds", indexHitIds)

	// 遍历 sync.Map，找出剩余需要删除的记录（跳过已通过索引命中的 id）
	tableCache.data.Range(func(k, v interface{}) bool {
		id, ok := k.(string)
		if !ok {
			return true
		}
		// 已通过索引命中的，跳过
		if _, skip := indexHitIds[id]; skip {
			return true
		}
		// 无条件时不会走到这里（primaryID 分支已提前返回），这里只处理带条件的兜底删除
		if len(conds) == 0 {
			toDelete = append(toDelete, id)
			deleteItems = append(deleteItems, v)
		} else if compiled != nil {
			if m.matchConditionsCompiled(v, compiled, tableCache) {
				toDelete = append(toDelete, id)
				deleteItems = append(deleteItems, v)
			}
		} else if m.matchConditions(v, conds, tableCache) {
			toDelete = append(toDelete, id)
			deleteItems = append(deleteItems, v)
		}
		return true
	})

	if len(toDelete) == 0 {
		return "", nil, nil, gorm.ErrRecordNotFound
	}

	// 获取所有要删除记录的行级锁（按顺序获取，避免死锁）
	rowLocks := make([]*sync.RWMutex, len(toDelete))
	for i, id := range toDelete {
		rowLocks[i] = tableCache.getRowLock(id)
		rowLocks[i].Lock()
	}

	// 释放所有行级锁
	defer func() {
		for _, lock := range rowLocks {
			lock.Unlock()
		}
	}()

	for _, id := range toDelete {
		tableCache.data.Delete(id)
		tableCache.rowLocks.Delete(id)
		// 仅移除被删除记录对应的二级索引，避免整表重建带来的性能开销
		m.indexRemoveId(tableCache, id)
	}

	return tableName, toDelete, deleteItems, nil
}

// Create 创建新记录
func (m *MysqlGts) Create(model interface{}) error {
	tableName, err := m.getTableName(model)
	if err != nil {
		return fmt.Errorf("获取表名失败: %v", err)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("表 %s 未初始化，请先调用 InitTable", tableName)
	}

	id := m.getIDValue(model, tableCache.primaryKey, tableCache)
	if id == "" || id == "0" {
		return fmt.Errorf("无法获取主键 ID 或 ID 为 0, id: %s", id)
	}

	// 获取行级锁
	rowLock := tableCache.getRowLock(id)
	rowLock.Lock()
	defer rowLock.Unlock()

	// 使用表级写锁来检查和更新 data map
	// 检查是否已存在
	if _, exists := tableCache.data.Load(id); exists {
		return errors.New("记录已存在")
	}

	// 直接添加到缓存，不需要先加载整表
	tableCache.data.Store(id, model)
	// 标记为已加载（因为至少有一条记录了）
	tableCache.mu.Lock()
	tableCache.loaded = true
	tableCache.mu.Unlock()

	// 根据 model 当前字段值构造联合索引键值对，并写入二级索引：
	// 仅对当前表的 indexedFields 中已有的字段建索引，避免对所有字段都建索引。
	if tableCache != nil {
		var fields []string
		tableCache.indexedFields.Range(func(k, _ interface{}) bool {
			if name, ok := k.(string); ok && name != "" {
				fields = append(fields, name)
			}
			return true
		})
		if len(fields) > 0 {
			pairs := make([]fieldValuePair, 0, len(fields))
			for _, f := range fields {
				v := m.getFieldValue(model, f, tableCache)
				pairs = append(pairs, fieldValuePair{
					Field: f,
					Value: v,
				})
			}
			m.indexAddId(tableCache, id, pairs)
		}
	}

	// 记录操作日志
	m.recordOperation(tableName, "insert", id, model)

	return nil
}

// StartSync 启动定时同步
func (m *MysqlGts) StartSync() {
	m.mu.Lock()
	if m.syncRunning {
		m.mu.Unlock()
		return
	}
	m.syncRunning = true
	m.mu.Unlock()

	go m.syncLoop()
}

// StopSync 停止定时同步
func (m *MysqlGts) StopSync() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.syncRunning {
		return
	}
	m.syncRunning = false
	close(m.stopChan)
	m.stopChan = make(chan struct{})
}

// FlushAllSync 立即将所有未同步的操作日志刷入数据库。
// 一般用于进程退出前，确保内存中的变更都已持久化。
func (m *MysqlGts) FlushAllSync() {
	if m == nil {
		return
	}

	// 先停止后台定时同步，避免与手动刷盘并发竞争
	m.StopSync()

	for {
		// 先“摘取一批”待处理日志并执行
		m.syncToDatabaseV2()

		// 检查是否还有未处理的操作日志（遍历每表队列，不占 m.mu）
		empty := true
		m.opLogsMap.Range(func(_, value interface{}) bool {
			q := value.(*tableOpLogQueue)
			q.mu.Lock()
			has := len(q.logs) > 0
			q.mu.Unlock()
			if has {
				empty = false
				return false // 停止 Range
			}
			return true
		})

		if empty {
			return
		}

		// 若仍有剩余日志，稍作等待再继续，避免紧急自旋
		time.Sleep(10 * time.Millisecond)
	}
}

// FlushAll 便捷方法：在包级 Gts 上执行 FlushAllSync。
func FlushAll() {
	if Gts != nil {
		Gts.FlushAllSync()
	}
}

// syncLoop 同步循环
func (m *MysqlGts) syncLoop() {
	ticker := time.NewTicker(m.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.syncToDatabaseV2()
		case <-m.stopChan:
			return
		}
	}
}

// syncToDatabase 同步操作日志到数据库（按表加锁摘取批次，锁外执行 DB，与 syncToDatabaseV2 一致）
func (m *MysqlGts) syncToDatabase() {
	m.syncToDatabaseV2()
}

// syncToDatabaseV2 同步操作日志到数据库（按表加锁摘取批次，锁外执行 DB，减少对 m.mu 的竞争）
func (m *MysqlGts) syncToDatabaseV2() {
	batch := make(map[string][]*OperationLog)

	// 按表遍历，每表只锁自己的 queue，摘取一批后立即释放
	m.opLogsMap.Range(func(key, value interface{}) bool {
		tableName := key.(string)
		q := value.(*tableOpLogQueue)
		q.mu.Lock()
		if len(q.logs) == 0 {
			q.mu.Unlock()
			return true
		}
		processCount := m.batchSize
		if len(q.logs) < processCount {
			processCount = len(q.logs)
		}
		toProcess := make([]*OperationLog, processCount)
		copy(toProcess, q.logs[:processCount])
		q.logs = q.logs[processCount:]
		q.mu.Unlock()

		batch[tableName] = toProcess
		return true
	})

	for tableName, logs := range batch {
		for _, log := range logs {
			_ = m.executeOperation(tableName, log)
		}
	}
}

// executeOperation 执行单个操作
func (m *MysqlGts) executeOperation(tableName string, log *OperationLog) error {
	tableCache, exists := m.tables[tableName]
	if !exists {
		return fmt.Errorf("表 %s 不存在", tableName)
	}

	switch log.Operation {
	case "insert":
		// 获取行级读锁来读取数据
		rowLock := tableCache.getRowLock(log.ID)
		rowLock.RLock()
		v, exists := tableCache.data.Load(log.ID)
		rowLock.RUnlock()

		if !exists {
			// 如果缓存中没有，说明可能已经被删除，跳过
			return nil
		}

		// 创建记录
		item := v
		if err := m.db.Create(item).Error; err != nil {
			return err
		}

	case "update":
		// 获取行级读锁来读取数据
		rowLock := tableCache.getRowLock(log.ID)
		rowLock.RLock()
		v, exists := tableCache.data.Load(log.ID)
		rowLock.RUnlock()

		if !exists {
			// 如果缓存中没有，说明可能已经被删除，跳过
			return nil
		}

		// 更新记录
		item := v
		if err := m.db.Save(item).Error; err != nil {
			return err
		}

	case "delete":
		// 对于删除操作，我们需要获取模型类型
		// 由于数据已经从缓存中删除，我们需要从表的其他记录中获取模型类型
		var sampleModel interface{}
		// 尝试从缓存中获取一个示例来确定模型类型
		tableCache.data.Range(func(_, v interface{}) bool {
			sampleModel = v
			return false
		})

		// 如果缓存中没有数据，尝试从操作日志的数据中恢复
		// 但操作日志中存储的是 map，我们需要另一种方式
		if sampleModel == nil {
			// 如果缓存完全为空，我们无法确定模型类型
			// 这种情况下，我们可以尝试使用 GORM 的 Table 方法直接删除
			// 但需要知道表名，我们可以通过 tableName 参数获取
			// 使用原生 SQL 删除
			query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", tableName, tableCache.primaryKey)
			if err := m.db.Exec(query, log.ID).Error; err != nil {
				return err
			}
			return nil
		}

		// 创建模型实例并设置主键
		modelType := reflect.TypeOf(sampleModel)
		if modelType.Kind() == reflect.Ptr {
			modelType = modelType.Elem()
		}
		deleteModel := reflect.New(modelType).Interface()
		if err := m.setIDValue(deleteModel, tableCache.primaryKey, log.ID); err != nil {
			return err
		}

		// 删除记录
		if err := m.db.Delete(deleteModel).Error; err != nil {
			return err
		}
	}

	return nil
}

// getOrCreateOpLogQueue 获取或创建该表的操作日志队列（每表独立，仅锁本表）
func (m *MysqlGts) getOrCreateOpLogQueue(tableName string) *tableOpLogQueue {
	val, _ := m.opLogsMap.LoadOrStore(tableName, &tableOpLogQueue{logs: make([]*OperationLog, 0)})
	return val.(*tableOpLogQueue)
}

// recordOperation 记录操作日志（只锁当前表队列，不占全局 m.mu，减少写竞争）
func (m *MysqlGts) recordOperation(tableName, operation, id string, data interface{}) {
	// 如果模型声明了 NotSyncToDatabase 且为 true，则不记录该模型的操作日志
	if data != nil && shouldSkipOperationLog(data) {
		return
	}

	q := m.getOrCreateOpLogQueue(tableName)
	q.mu.Lock()
	defer q.mu.Unlock()

	// 检查最后一条操作日志，如果操作类型和ID都相同，则跳过
	if len(q.logs) > 0 {
		lastLog := q.logs[len(q.logs)-1]
		if lastLog.Operation == operation && lastLog.ID == id {
			lastLog.Timestamp = time.Now()
			return
		}
	}

	log := &OperationLog{
		Operation: operation,
		ID:        id,
		Data:      nil,
		Timestamp: time.Now(),
	}
	q.logs = append(q.logs, log)
}

// 预先缓存 NotSyncToDatabase 的反射类型，避免每次调用 reflect.TypeOf
var notSyncType = reflect.TypeOf(NotSyncToDatabase(false))

// shouldSkipOperationLog 判断模型是否声明了 NotSyncToDatabase 并且为 true
// 约定：在模型中匿名嵌入 gts.NotSyncToDatabase，例如：
//
//	type FigureAttribute struct {
//	    ...
//	    gts.NotSyncToDatabase `json:"-"`
//	}
//
// 当该布尔值为 true 时，不记录该模型的 OperationLog。
func shouldSkipOperationLog(model interface{}) bool {
	v := reflect.ValueOf(model)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false
	}

	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// 只检查匿名且类型为 NotSyncToDatabase 的字段
		if f.Anonymous && f.Type == notSyncType {
			fieldVal := v.Field(i)
			if fieldVal.IsValid() && fieldVal.Kind() == reflect.Bool && fieldVal.Bool() {
				return true
			}
		}
	}
	return false
}

// 辅助函数

// getTableName 获取表名
func (m *MysqlGts) getTableName(model interface{}) (string, error) {
	if tn, ok := model.(TableName); ok {
		return tn.TableName(), nil
	}

	// 尝试通过反射调用 TableName 方法
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	method := reflect.ValueOf(model).MethodByName("TableName")
	if method.IsValid() {
		results := method.Call(nil)
		if len(results) > 0 {
			return results[0].String(), nil
		}
	}

	return "", errors.New("模型未实现 TableName 方法")
}

// getTableNameFromDest 从目标切片获取表名
func (m *MysqlGts) getTableNameFromDest(dest interface{}) (string, error) {
	destType := reflect.TypeOf(dest)
	if destType.Kind() != reflect.Ptr {
		return "", errors.New("dest 必须是指针类型")
	}

	elemType := destType.Elem()
	if elemType.Kind() != reflect.Slice {
		return "", errors.New("dest 必须是切片类型")
	}

	modelType := elemType.Elem()
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	model := reflect.New(modelType).Interface()
	return m.getTableName(model)
}

// getPrimaryKey 获取主键字段名
func (m *MysqlGts) getPrimaryKey(model interface{}) string {
	// 默认主键为 "ID"
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// 检查是否有 ID 字段
	if _, found := modelType.FieldByName("ID"); found {
		return "ID"
	}

	// 可以扩展支持其他主键字段
	return "ID"
}

// getIDValue 获取主键值（使用缓存的反射信息）
func (m *MysqlGts) getIDValue(model interface{}, primaryKey string, tableCache *TableCache) string {
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	// 如果提供了 tableCache 且有反射信息，使用缓存的字段索引
	if tableCache != nil && tableCache.reflectInfo != nil && tableCache.reflectInfo.PrimaryKeyIdx >= 0 {
		field := modelValue.Field(tableCache.reflectInfo.PrimaryKeyIdx)
		if !field.IsValid() {
			return ""
		}
		switch field.Kind() {
		case reflect.String:
			return field.String()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return fmt.Sprintf("%d", field.Uint())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return fmt.Sprintf("%d", field.Int())
		}
		return ""
	}

	// 回退到原来的方法
	field := modelValue.FieldByName(primaryKey)
	if !field.IsValid() {
		return ""
	}

	switch field.Kind() {
	case reflect.String:
		return field.String()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", field.Uint())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", field.Int())
	}

	return ""
}

// getFieldValue 根据表字段名（如 user_info_id）从 model 中取当前值，返回字符串，用于重建二级索引
func (m *MysqlGts) getFieldValue(model interface{}, fieldName string, tableCache *TableCache) string {
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}
	camelName := m.toCamelCase(fieldName, tableCache)
	field := modelValue.FieldByName(camelName)
	if !field.IsValid() {
		return ""
	}
	return fmt.Sprint(field.Interface())
}

// setIDValue 设置主键值
func (m *MysqlGts) setIDValue(model interface{}, primaryKey, id string) error {
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	field := modelValue.FieldByName(primaryKey)
	if !field.IsValid() || !field.CanSet() {
		return errors.New("无法设置主键字段")
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(id)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var val uint64
		fmt.Sscanf(id, "%d", &val)
		field.SetUint(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var val int64
		fmt.Sscanf(id, "%d", &val)
		field.SetInt(val)
	default:
		return errors.New("不支持的主键类型")
	}

	return nil
}

// copyModel 复制模型数据
func (m *MysqlGts) copyModel(src, dst interface{}) error {
	srcValue := reflect.ValueOf(src)
	dstValue := reflect.ValueOf(dst)

	if srcValue.Kind() == reflect.Ptr {
		srcValue = srcValue.Elem()
	}
	if dstValue.Kind() == reflect.Ptr {
		dstValue = dstValue.Elem()
	}

	if srcValue.Type() != dstValue.Type() {
		return errors.New("类型不匹配")
	}

	dstValue.Set(srcValue)
	return nil
}

// getRowLock 获取或创建指定 ID 的行级锁
func (tc *TableCache) getRowLock(id string) *sync.RWMutex {
	// 使用 sync.Map 的 LoadOrStore 来原子性地获取或创建锁
	lock, _ := tc.rowLocks.LoadOrStore(id, &sync.RWMutex{})
	return lock.(*sync.RWMutex)
}

// compiledCond 预解析后的查询子条件（当前只支持等值匹配）
type compiledCond struct {
	Field string      // 结构体字段名（已转换为 CamelCase）
	Value interface{} // 比较值
}

// compileConds 将 GORM 风格的 conds 预解析为 compiledCond 列表（仅处理第一个 string 查询条件）
// 解析失败时返回 nil，调用方可回退到原始 matchConditions 逻辑。
func (m *MysqlGts) compileConds(conds []interface{}, tableCache *TableCache) []compiledCond {
	if len(conds) == 0 {
		return nil
	}

	for i := 0; i < len(conds); i++ {
		cond := conds[i]

		queryStr, ok := cond.(string)
		if !ok {
			continue
		}
		qs := strings.TrimSpace(queryStr)
		if qs == "" || !containsSubstring(qs, "=") {
			continue
		}

		normalized := collapseSpaces(strings.ToLower(qs))
		segments := strings.Split(normalized, " and ")
		raw := collapseSpaces(qs)

		values := conds[i+1:]
		valIdx := 0
		rawRest := raw

		var compiled []compiledCond

		for _, seg := range segments {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}

			pos := strings.Index(strings.ToLower(rawRest), seg)
			if pos < 0 {
				return nil
			}
			part := strings.TrimSpace(rawRest[pos : pos+len(seg)])
			rawRest = rawRest[pos+len(seg):]

			idxEq := strings.Index(part, "=")
			if idxEq <= 0 || idxEq == len(part)-1 {
				return nil
			}
			fieldName := strings.TrimSpace(part[:idxEq])
			right := strings.TrimSpace(part[idxEq+1:])
			if fieldName == "" || right == "" {
				return nil
			}

			camelFieldName := m.toCamelCase(fieldName, tableCache)

			var want interface{}
			if len(right) > 0 && right[0] == '?' {
				if valIdx >= len(values) {
					return nil
				}
				want = values[valIdx]
				valIdx++
			} else {
				lit := right
				if strings.HasPrefix(lit, "'") && strings.HasSuffix(lit, "'") && len(lit) >= 2 {
					lit = lit[1 : len(lit)-1]
				}
				want = lit
			}

			compiled = append(compiled, compiledCond{
				Field: camelFieldName,
				Value: want,
			})
		}

		return compiled
	}

	return nil
}

// matchConditionsCompiled 使用预解析后的条件匹配 item，避免重复解析 queryStr
func (m *MysqlGts) matchConditionsCompiled(item interface{}, compiled []compiledCond, tableCache *TableCache) bool {
	if len(compiled) == 0 {
		return true
	}

	itemValue := reflect.ValueOf(item)
	if itemValue.Kind() == reflect.Ptr {
		itemValue = itemValue.Elem()
	}

	for _, c := range compiled {
		field := itemValue.FieldByName(c.Field)
		if !field.IsValid() {
			return false
		}
		fieldValue := field.Interface()

		// 快路径：常见基础类型直接比较，避免 reflect.DeepEqual 和 fmt.Sprintf
		switch fv := fieldValue.(type) {
		case int, int8, int16, int32, int64:
			switch vv := c.Value.(type) {
			case int, int8, int16, int32, int64:
				if fmt.Sprint(fv) != fmt.Sprint(vv) {
					return false
				}
				continue
			}
		case uint, uint8, uint16, uint32, uint64:
			switch vv := c.Value.(type) {
			case uint, uint8, uint16, uint32, uint64:
				if fmt.Sprint(fv) != fmt.Sprint(vv) {
					return false
				}
				continue
			}
		case string:
			if vs, ok := c.Value.(string); ok {
				if fv != vs {
					return false
				}
				continue
			}
		}

		// 兜底：类型不匹配时走字符串比较，否则用 DeepEqual
		fk := reflect.TypeOf(fieldValue).Kind()
		vk := reflect.TypeOf(c.Value).Kind()
		if fk != vk {
			if fmt.Sprintf("%v", fieldValue) != fmt.Sprintf("%v", c.Value) {
				return false
			}
		} else if !reflect.DeepEqual(fieldValue, c.Value) {
			return false
		}
	}

	return true
}

// matchConditions 匹配查询条件；可选传入 tableCache，避免每行重复 getTableName + RLock
func (m *MysqlGts) matchConditions(item interface{}, conds []interface{}, tableCache *TableCache) bool {
	if len(conds) == 0 {
		return true
	}

	itemValue := reflect.ValueOf(item)
	if itemValue.Kind() == reflect.Ptr {
		itemValue = itemValue.Elem()
	}

	// 如未显式传入 tableCache，则按旧逻辑从 item 推断表名并读取
	if tableCache == nil {
		if tableName, err := m.getTableName(item); err == nil {
			m.mu.RLock()
			if tc, exists := m.tables[tableName]; exists {
				tableCache = tc
			}
			m.mu.RUnlock()
		}
	}

	//fmt.Println("conds=", conds)

	// 处理 GORM 风格的查询条件
	// 例如: First(&user, "123") 或 First(&user, "id = ?", "123")
	for i := 0; i < len(conds); i++ {
		cond := conds[i]
		// fmt.Println("cond=", cond)
		// 情况1: 直接传入主键值（字符串或数字）
		if idStr, ok := cond.(string); ok && !containsSubstring(idStr, "=") {
			// 如果字符串不包含 "="，可能是主键值
			itemID := m.getIDValue(item, "ID", tableCache)
			if itemID == idStr {
				return true
			}
		} else if idInt, ok := cond.(int); ok {
			itemID := m.getIDValue(item, "ID", tableCache)
			if itemID == fmt.Sprintf("%d", idInt) {
				return true
			}
		} else if idUint, ok := cond.(uint); ok {
			itemID := m.getIDValue(item, "ID", tableCache)
			if itemID == fmt.Sprintf("%d", idUint) {
				return true
			}
		} else if idUint64, ok := cond.(uint64); ok {
			itemID := m.getIDValue(item, "ID", tableCache)
			if itemID == fmt.Sprintf("%d", idUint64) {
				return true
			}
		}

		// 情况2: 查询条件字符串，例如：
		//   "id = ?"
		//   "user_info_id = ? AND card_id = ?"
		//   "anchor_info_id = ? and level = 0"
		if queryStr, ok := cond.(string); ok {
			qs := strings.TrimSpace(queryStr)
			if qs == "" || !containsSubstring(qs, "=") {
				continue
			}

			// 归一化空白并按 AND 拆分子条件
			normalized := collapseSpaces(strings.ToLower(qs))
			segments := strings.Split(normalized, " and ")
			raw := collapseSpaces(qs)

			values := conds[i+1:]
			valIdx := 0
			rawRest := raw

			for _, seg := range segments {
				seg = strings.TrimSpace(seg)
				if seg == "" {
					continue
				}
				// 在 rawRest 中找到当前 seg 的原始片段
				pos := strings.Index(strings.ToLower(rawRest), seg)
				if pos < 0 {
					return false
				}
				part := strings.TrimSpace(rawRest[pos : pos+len(seg)])
				rawRest = rawRest[pos+len(seg):]

				// 拆分 field = expr
				idxEq := strings.Index(part, "=")
				if idxEq <= 0 || idxEq == len(part)-1 {
					return false
				}
				fieldName := strings.TrimSpace(part[:idxEq])
				right := strings.TrimSpace(part[idxEq+1:])
				if fieldName == "" || right == "" {
					return false
				}

				// 找到对应字段
				camelFieldName := m.toCamelCase(fieldName, tableCache)
				field := itemValue.FieldByName(camelFieldName)
				if !field.IsValid() {
					return false
				}
				fieldValue := field.Interface()

				var want interface{}
				// 右侧以 ? 开头：从 conds 取值
				if len(right) > 0 && right[0] == '?' {
					if valIdx >= len(values) {
						return false
					}
					want = values[valIdx]
					valIdx++
				} else {
					// 常量：直接用字符串字面值（去掉成对单引号）
					lit := right
					if strings.HasPrefix(lit, "'") && strings.HasSuffix(lit, "'") && len(lit) >= 2 {
						lit = lit[1 : len(lit)-1]
					}
					want = lit
				}

				// 比较 fieldValue 与 want
				fk := reflect.TypeOf(fieldValue).Kind()
				vk := reflect.TypeOf(want).Kind()
				equal := false
				if fk != vk {
					equal = fmt.Sprintf("%v", fieldValue) == fmt.Sprintf("%v", want)
				} else {
					equal = reflect.DeepEqual(fieldValue, want)
				}
				if !equal {
					return false
				}
			}

			// 所有子条件都匹配
			return true
		}
	}

	// 所有条件都匹配
	return true
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// extractFieldName 从查询条件中提取字段名
func extractFieldName(query string) string {
	names := extractAllFieldNames(query)
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// extractAllFieldNames 从查询条件中提取所有字段名，支持 "api_type = ?"、"api_type =  ?"、"user_info_id = ? AND card_id = ?" 等
func extractAllFieldNames(query string) []string {
	// 先将连续空格合并为一个，便于统一匹配 " = ?" 或 "=?"
	rest := collapseSpaces(query)
	var names []string
	for {
		idx := strings.Index(rest, " = ?")
		skip := 4
		if idx < 0 {
			idx = strings.Index(rest, "=?")
			skip = 2
		}
		if idx < 0 {
			break
		}
		end := idx
		start := idx
		for start > 0 {
			c := rest[start-1]
			if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				break
			}
			start--
		}
		if start < end {
			names = append(names, strings.TrimSpace(rest[start:end]))
		}
		rest = rest[idx+skip:]
	}
	return names
}

// collapseSpaces 将字符串中连续空格合并为一个空格
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// modelToMap 将模型转换为 map（用于操作日志）
func (m *MysqlGts) modelToMap(model interface{}) map[string]interface{} {
	// 获取表名以获取反射信息
	tableName, err := m.getTableName(model)
	if err != nil {
		// 如果获取表名失败，使用原来的方法
		return m.modelToMapSlow(model)
	}

	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()

	if !exists || tableCache.reflectInfo == nil {
		// 如果反射信息不存在，使用原来的方法
		return m.modelToMapSlow(model)
	}

	// 使用缓存的反射信息
	result := make(map[string]interface{})
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	info := tableCache.reflectInfo
	for _, fieldInfo := range info.FieldInfo {
		fieldValue := modelValue.Field(fieldInfo.Index)
		if fieldValue.CanInterface() {
			result[fieldInfo.Name] = fieldValue.Interface()
		}
	}

	return result
}

// modelToMapSlow 慢速版本（不使用缓存）
func (m *MysqlGts) modelToMapSlow(model interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}

	modelType := modelValue.Type()
	for i := 0; i < modelValue.NumField(); i++ {
		field := modelType.Field(i)
		fieldValue := modelValue.Field(i)
		if fieldValue.CanInterface() {
			result[field.Name] = fieldValue.Interface()
		}
	}

	return result
}

// toCamelCase 将下划线命名转换为驼峰命名（简单实现）
// 支持常见缩写词：id -> ID, url -> URL, http -> HTTP 等
// 如果提供了 tableCache，会使用缓存的转换结果
func (m *MysqlGts) toCamelCase(s string, tableCache *TableCache) string {
	// 如果提供了 tableCache，先检查缓存
	if tableCache != nil && tableCache.reflectInfo != nil {
		// 先尝试读锁读取缓存
		tableCache.reflectInfo.cacheMu.RLock()
		if cached, exists := tableCache.reflectInfo.CamelCaseCache[s]; exists {
			tableCache.reflectInfo.cacheMu.RUnlock()
			return cached
		}
		tableCache.reflectInfo.cacheMu.RUnlock()

		// 缓存未命中，需要写入，升级为写锁
		result := m.toCamelCaseSlow(s)
		tableCache.reflectInfo.cacheMu.Lock()
		// 双重检查，避免并发写入重复值
		if cached, exists := tableCache.reflectInfo.CamelCaseCache[s]; exists {
			tableCache.reflectInfo.cacheMu.Unlock()
			return cached
		}
		tableCache.reflectInfo.CamelCaseCache[s] = result
		tableCache.reflectInfo.cacheMu.Unlock()
		return result
	}

	// 没有缓存，使用慢速版本
	return m.toCamelCaseSlow(s)
}

// toCamelCaseSlow 慢速版本（不使用缓存）
func (m *MysqlGts) toCamelCaseSlow(s string) string {
	// 如果输入已经是驼峰命名（首字母大写），直接返回
	if len(s) == 0 {
		return s
	}

	// 检查是否包含下划线
	hasUnderscore := false
	for _, char := range s {
		if char == '_' {
			hasUnderscore = true
			break
		}
	}

	// 常见缩写词映射（小写 -> 大写）
	abbreviations := map[string]string{
		"id":    "ID",
		"url":   "URL",
		"http":  "HTTP",
		"https": "HTTPS",
		"api":   "API",
		"json":  "JSON",
		"xml":   "XML",
		"html":  "HTML",
		"css":   "CSS",
		"js":    "JS",
		"ip":    "IP",
		"uid":   "UID",
		"gid":   "GID",
	}

	if !hasUnderscore {
		// 如果没有下划线，先检查是否是缩写词
		if abbr, exists := abbreviations[strings.ToLower(s)]; exists {
			return abbr
		}
		// 如果不是缩写词，假设已经是驼峰命名，但需要首字母大写
		if s[0] >= 'a' && s[0] <= 'z' {
			return string(s[0]-32) + s[1:]
		}
		return s
	}

	// 转换下划线命名到驼峰命名
	result := ""
	parts := make([]string, 0)
	current := ""

	// 先分割成部分
	for _, char := range s {
		if char == '_' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	// 转换每个部分
	for _, part := range parts {
		lowerPart := ""
		for _, char := range part {
			if char >= 'A' && char <= 'Z' {
				lowerPart += string(char + 32)
			} else {
				lowerPart += string(char)
			}
		}

		// 检查是否是缩写词
		if abbr, ok := abbreviations[lowerPart]; ok {
			result += abbr
		} else {
			// 首字母大写
			if len(part) > 0 {
				if part[0] >= 'a' && part[0] <= 'z' {
					result += string(part[0]-32) + part[1:]
				} else {
					result += part
				}
			}
		}
	}

	return result
}

// GetTableStatus 获取表的状态信息（用于调试和监控）
func (m *MysqlGts) GetTableStatus(tableName string) (loaded bool, recordCount int, pendingOps int) {
	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	if !exists {
		m.mu.RUnlock()
		return false, 0, 0
	}

	// 先读取 loaded 状态
	tableCache.mu.RLock()
	loaded = tableCache.loaded
	tableCache.mu.RUnlock()

	// 再统计记录数量（遍历 sync.Map，避免表级数据锁）
	recordCount = 0
	tableCache.data.Range(func(_, _ interface{}) bool {
		recordCount++
		return true
	})
	m.mu.RUnlock()

	// 待同步条数从该表的 queue 读，不占 m.mu
	if v, ok := m.opLogsMap.Load(tableName); ok {
		q := v.(*tableOpLogQueue)
		q.mu.Lock()
		pendingOps = len(q.logs)
		q.mu.Unlock()
	}

	return loaded, recordCount, pendingOps
}

// ClearCache 清空指定表的缓存（谨慎使用）
func (m *MysqlGts) ClearCache(tableName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tableCache, exists := m.tables[tableName]
	if !exists {
		return fmt.Errorf("表 %s 不存在", tableName)
	}

	tableCache.mu.Lock()
	tableCache.loaded = false
	tableCache.mu.Unlock()

	// 清空 data
	tableCache.data.Range(func(k, _ interface{}) bool {
		tableCache.data.Delete(k)
		return true
	})

	// 清空联合二级索引及已索引字段记录
	tableCache.index.Range(func(k, _ interface{}) bool {
		tableCache.index.Delete(k)
		return true
	})
	tableCache.indexReverse.Range(func(k, _ interface{}) bool {
		tableCache.indexReverse.Delete(k)
		return true
	})
	tableCache.indexedFields.Range(func(k, _ interface{}) bool {
		tableCache.indexedFields.Delete(k)
		return true
	})

	return nil
}

// 打印已经索引过的字段
func (m *MysqlGts) DebugPrintIndexedFields(tableName string) {
	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	if !exists {
		fmt.Printf("[GTS][IndexedFields] table %s not found\n", tableName)
		m.mu.RUnlock()
		return
	}
	tableCache.indexedFields.Range(func(k, _ interface{}) bool {
		fieldName, ok := k.(string)
		if !ok {
			return true
		}
		fmt.Printf("  field=%s\n", fieldName)
		return true
	})
	m.mu.RUnlock()
	fmt.Printf("[GTS][IndexedFields] table=%s\n", tableName)
}

// DebugPrintTableIndex 打印指定表的二级索引（仅用于调试）
func (m *MysqlGts) DebugPrintTableIndex(tableName string) {
	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()
	if !exists {
		fmt.Printf("[GTS][Index] table %s not found\n", tableName)
		return
	}

	fmt.Printf("[GTS][Index] table=%s\n", tableName)
	tableCache.index.Range(func(fieldKey, fieldVal interface{}) bool {
		fieldName, ok := fieldKey.(string)
		if !ok {
			return true
		}
		valueMap, ok := fieldVal.(*sync.Map)
		if !ok {
			return true
		}
		valueMap.Range(func(vKey, vVal interface{}) bool {
			valueStr, ok := vKey.(string)
			if !ok {
				return true
			}
			idSet, ok := vVal.(*sync.Map)
			if !ok {
				return true
			}
			ids := make([]string, 0)
			idSet.Range(func(idKey, _ interface{}) bool {
				if idStr, ok := idKey.(string); ok {
					ids = append(ids, idStr)
				}
				return true
			})
			fmt.Printf("  field=%s value=%s ids=%v\n", fieldName, valueStr, ids)
			return true
		})
		return true
	})
}

// DebugPrintTableData 打印指定表的所有缓存数据（仅用于调试）
func (m *MysqlGts) DebugPrintTableData(tableName string) {
	m.mu.RLock()
	tableCache, exists := m.tables[tableName]
	m.mu.RUnlock()
	if !exists {
		fmt.Printf("[GTS][Data] table %s not found\n", tableName)
		return
	}

	fmt.Printf("[GTS][Data] table=%s\n", tableName)
	tableCache.data.Range(func(idKey, val interface{}) bool {
		idStr, ok := idKey.(string)
		if !ok {
			return true
		}
		// 使用 modelToMap 将结构体转为 map 便于阅读
		dataMap := m.modelToMap(val)
		fmt.Printf("  id=%s data=%v\n", idStr, dataMap)
		return true
	})
}
