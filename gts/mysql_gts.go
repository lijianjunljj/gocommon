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
//   type FigureAttribute struct {
//       ...
//       gts.NotSyncToDatabase `json:"-"`
//   }
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
	db           *gorm.DB                   // GORM 数据库实例
	tables       map[string]*TableCache     // 表缓存，key 为表名
	opLogs       map[string][]*OperationLog // 操作日志，key 为表名
	mu           sync.RWMutex               // 读写锁
	syncInterval time.Duration              // 同步间隔（秒）
	batchSize    int                        // 每次处理的操作日志数量
	stopChan     chan struct{}              // 停止信号
	syncRunning  bool                       // 同步是否正在运行
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
	// index 为二级索引：key 为查询条件序列化后的字符串，value 为主键 ID(string)
	// 仅用于加速常用查询场景（例如 api_type+room_id，user_info_id 等），真实数据仍以 data + DB 为准。
	index sync.Map
}

// OperationLog 操作日志
type OperationLog struct {
	Operation string                 // 操作类型：insert, update, delete
	Data      map[string]interface{} // 操作数据
	ID        string                 // 主键 ID
	Timestamp time.Time              // 操作时间
}

// NewMysqlGts 创建新的 MysqlGts 实例
func NewMysqlGts(db *gorm.DB) *MysqlGts {
	return &MysqlGts{
		db:           db,
		tables:       make(map[string]*TableCache),
		opLogs:       make(map[string][]*OperationLog),
		syncInterval: 1 * time.Second, // 默认 5 秒同步一次
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

		// 初始化操作日志
		m.opLogs[tableName] = make([]*OperationLog, 0)
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

// Load 加载整表数据到缓存
func (m *MysqlGts) Load(model interface{}) error {
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

	// 从数据库加载数据
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// 创建切片用于存储查询结果
	sliceType := reflect.SliceOf(reflect.PtrTo(modelType))
	sliceValue := reflect.New(sliceType)
	slice := sliceValue.Interface()

	// 查询所有数据
	if err := m.db.Find(slice).Error; err != nil {
		return fmt.Errorf("加载表数据失败: %v", err)
	}

	// 将数据存入缓存（使用行锁 + sync.Map，避免表级锁）
	sliceVal := reflect.ValueOf(slice).Elem()
	for i := 0; i < sliceVal.Len(); i++ {
		item := sliceVal.Index(i).Interface()
		id := m.getIDValue(item, tableCache.primaryKey, tableCache)
		if id != "" && id != "0" {
			rowLock := tableCache.getRowLock(id)
			rowLock.Lock()
			tableCache.data.Store(id, item)
			rowLock.Unlock()
		}
	}

	tableCache.mu.Lock()
	tableCache.loaded = true
	tableCache.mu.Unlock()
	return nil
}

// First 查询第一条记录
func (m *MysqlGts) First(model interface{}, conds ...interface{}) error {
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
		if err := m.Load(model); err != nil {
			return err
		}
	}

	// 预先构建条件索引 key，便于后续使用二级索引加速查找
	indexKey := ""
	if len(conds) > 0 {
		indexKey = buildIndexKey(conds...)
	}

	// 1. 最先尝试从二级索引中命中（避免全表遍历/复杂匹配）
	if indexKey != "" {
		if v, ok := tableCache.index.Load(indexKey); ok {
			if idStr, ok2 := v.(string); ok2 && idStr != "" {
				rowLock := tableCache.getRowLock(idStr)
				rowLock.RLock()
				if cached, ok3 := tableCache.data.Load(idStr); ok3 {
					err := m.copyModel(cached, model)
					rowLock.RUnlock()
					return err
				}
				rowLock.RUnlock()
			}
		}
	}

	// 2. 然后处理常见的按主键 ID 查询的简单场景：
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
						err := m.copyModel(cached, model)
						rowLock.RUnlock()
						return err
					}
					rowLock.RUnlock()
				}
			}
		}
	}

	// 从缓存中查找（只使用行锁 + sync.Map，不使用表级锁），按条件遍历匹配
	var foundItem interface{}
	found := false
	// 如果有查询条件，需要匹配
	if len(conds) > 0 {
		// 简单的条件匹配（可以根据需要扩展）
		tableCache.data.Range(func(_, v interface{}) bool {
			if m.matchConditions(v, conds) {
				foundItem = v
				found = true
				return false
			}
			return true
		})
		if !found {
			// fmt.Printf("[First] 缓存中未找到匹配记录\n")
		}
	} else {
		// 没有条件，返回第一条
		tableCache.data.Range(func(_, v interface{}) bool {
			foundItem = v
			found = true
			return false
		})
	}

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
			// 命中后更新二级索引，后续可直接通过 indexKey + 主键 ID 加速
			if indexKey != "" {
				tableCache.index.Store(indexKey, foundID)
			}
			return err
		}
		return m.copyModel(foundItem, model)
	}

	// 缓存中没找到，去数据库查询
	query := m.db.Model(model)
	if len(conds) > 0 {
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
	}

	// 从数据库查询
	if err := query.First(model).Error; err != nil {
		return err
	}

	// 查询成功，将结果添加到缓存
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
			// 同步更新二级索引
			if indexKey != "" {
				tableCache.index.Store(indexKey, id)
			}
		}
	}

	return nil
}

// Find 查询多条记录
func (m *MysqlGts) Find(dest interface{}, conds ...interface{}) error {
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

	// 如果缓存未加载，先加载
	tableCache.mu.RLock()
	loaded := tableCache.loaded
	tableCache.mu.RUnlock()

	if !loaded {
		if err := m.Load(model); err != nil {
			return err
		}
	}

	var cachedResults []interface{}
	// 遍历 sync.Map 匹配条件
	tableCache.data.Range(func(_, v interface{}) bool {
		if len(conds) == 0 || m.matchConditions(v, conds) {
			cachedResults = append(cachedResults, v)
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

	// 如果缓存中有结果，直接返回（不再查询数据库）
	// 如果需要查询数据库，可以取消下面的注释
	// 但通常缓存加载后，所有数据都在缓存中，所以这里先返回缓存结果
	// 如果缓存结果为空，再去数据库查询
	if len(cachedResults) > 0 {
		return nil
	}

	// 缓存中没找到，去数据库查询
	query := m.db.Model(model)
	if len(conds) > 0 {
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
	}

	// 从数据库查询
	if err := query.Find(dest).Error; err != nil {
		return err
	}

	// 查询成功，将结果添加到缓存
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

			rowLock.Unlock()
		}
	}

	return nil
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

	// 记录操作日志
	m.recordOperation(tableName, "update", id, item)

	return nil
}

// Delete 删除记录
func (m *MysqlGts) Delete(model interface{}, conds ...interface{}) error {
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
		if err := m.Load(model); err != nil {
			return err
		}
	}

	var toDelete []string
	var deleteItems []interface{}
	// 遍历 sync.Map，找出要删除的记录
	tableCache.data.Range(func(k, v interface{}) bool {
		id, ok := k.(string)
		if !ok {
			return true
		}
		if len(conds) == 0 || m.matchConditions(v, conds) {
			toDelete = append(toDelete, id)
			deleteItems = append(deleteItems, v)
		}
		return true
	})

	if len(toDelete) == 0 {
		return gorm.ErrRecordNotFound
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
		// 清理行级锁（可选，sync.Map 会自动管理）
		tableCache.rowLocks.Delete(id)
	}

	// 记录操作日志
	for i, id := range toDelete {
		m.recordOperation(tableName, "delete", id, deleteItems[i])
	}

	return nil
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

		// 检查是否还有未处理的操作日志
		m.mu.RLock()
		empty := true
		for _, logs := range m.opLogs {
			if len(logs) > 0 {
				empty = false
				break
			}
		}
		m.mu.RUnlock()

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

// syncToDatabase 同步操作日志到数据库
func (m *MysqlGts) syncToDatabase() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for tableName, logs := range m.opLogs {
		if len(logs) == 0 {
			continue
		}

		// fmt.Printf("[syncToDatabase] 表: %s, 待处理操作日志数量: %d\n", tableName, len(logs))

		// 获取要处理的数量
		processCount := m.batchSize
		if len(logs) < processCount {
			processCount = len(logs)
		}

		// 处理操作日志
		toProcess := logs[:processCount]
		remaining := logs[processCount:]

		// // 打印待处理的操作日志详情
		// for i, log := range toProcess {
		// 	// fmt.Printf("[syncToDatabase] 表: %s, 操作日志[%d]: 操作=%s, ID=%s, 时间=%v\n",
		// 	// 	tableName, i, log.Operation, log.ID, log.Timestamp)
		// }

		// 执行数据库操作
		for _, log := range toProcess {
			if err := m.executeOperation(tableName, log); err != nil {
				// 如果操作失败，可以选择记录错误或重试
				// fmt.Printf("[GTS 同步失败] 表: %s, 操作: %s, ID: %s, 错误: %v\n", tableName, log.Operation, log.ID, err)
			} else {
				// fmt.Printf("[GTS 同步成功] 表: %s, 操作: %s, ID: %s\n", tableName, log.Operation, log.ID)
			}
		}

		// 更新操作日志列表
		m.opLogs[tableName] = remaining
		// fmt.Printf("[syncToDatabase] 表: %s, 剩余操作日志数量: %d\n", tableName, len(remaining))
	}
}

// syncToDatabaseV2 同步操作日志到数据库（锁外执行 DB）
func (m *MysqlGts) syncToDatabaseV2() {
	// 在持有全局锁的情况下，只“摘取一批”待处理日志并更新 m.opLogs，
	// 不在锁内做任何耗时的数据库操作，避免阻塞 First/Find 等读操作。
	batch := make(map[string][]*OperationLog)

	m.mu.Lock()
	for tableName, logs := range m.opLogs {
		if len(logs) == 0 {
			continue
		}

		processCount := m.batchSize
		if len(logs) < processCount {
			processCount = len(logs)
		}
		if processCount <= 0 {
			continue
		}

		toProcess := make([]*OperationLog, processCount)
		copy(toProcess, logs[:processCount])
		batch[tableName] = toProcess

		// 剩余日志留待下次同步
		m.opLogs[tableName] = logs[processCount:]
	}
	m.mu.Unlock()

	// 在不持有全局锁的情况下真正执行数据库操作
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

// recordOperation 记录操作日志
func (m *MysqlGts) recordOperation(tableName, operation, id string, data interface{}) {
	// 如果模型声明了 NotSyncToDatabase 且为 true，则不记录该模型的操作日志
	if data != nil && shouldSkipOperationLog(data) {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查最后一条操作日志，如果操作类型和ID都相同，则跳过
	logs := m.opLogs[tableName]
	if len(logs) > 0 {
		lastLog := logs[len(logs)-1]
		if lastLog.Operation == operation && lastLog.ID == id {
			// 相同操作，更新时间戳即可，不添加新日志
			lastLog.Timestamp = time.Now()
			return
		}
	}

	// 优化：延迟转换 modelToMap，只在需要时转换（目前先不转换，减少开销）
	log := &OperationLog{
		Operation: operation,
		ID:        id,
		Data:      nil, // 延迟转换，减少反射开销
		Timestamp: time.Now(),
	}

	// 记录操作日志
	m.opLogs[tableName] = append(m.opLogs[tableName], log)
}

// 预先缓存 NotSyncToDatabase 的反射类型，避免每次调用 reflect.TypeOf
var notSyncType = reflect.TypeOf(NotSyncToDatabase(false))

// shouldSkipOperationLog 判断模型是否声明了 NotSyncToDatabase 并且为 true
// 约定：在模型中匿名嵌入 gts.NotSyncToDatabase，例如：
//   type FigureAttribute struct {
//       ...
//       gts.NotSyncToDatabase `json:"-"`
//   }
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

// matchConditions 匹配查询条件
func (m *MysqlGts) matchConditions(item interface{}, conds []interface{}) bool {
	if len(conds) == 0 {
		return true
	}

	itemValue := reflect.ValueOf(item)
	if itemValue.Kind() == reflect.Ptr {
		itemValue = itemValue.Elem()
	}

	// 获取 tableCache（需要从 item 获取表名）
	var tableCache *TableCache
	if tableName, err := m.getTableName(item); err == nil {
		m.mu.RLock()
		if tc, exists := m.tables[tableName]; exists {
			tableCache = tc
		}
		m.mu.RUnlock()
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

		// 情况2: 查询条件字符串，例如 "id = ?" 或 "user_info_id = ? AND card_id = ?"
		if queryStr, ok := cond.(string); ok {
			// fmt.Println("queryStr=", queryStr)
			// 检查是否包含 "=" 和 "?"
			if containsSubstring(queryStr, "=") && containsSubstring(queryStr, "?") {
				fieldNames := extractAllFieldNames(queryStr)
				// fmt.Println("fieldNames=", fieldNames)
				numValues := len(fieldNames)
				// fmt.Println("numValues=", numValues)
				// fmt.Println("i=", i)
				// fmt.Println("conds=", conds)
				if numValues == 0 || i+numValues > len(conds) {
					// fmt.Println("numValues == 0 || i+numValues > len(conds)")
					return false
				}
				// fmt.Println("fieldNames=", fieldNames)
				// fmt.Println("numValues=", numValues)
				// 查询参数从 conds[i+1] 起，共 numValues 个
				valueStart := i + 1
				// fmt.Println("valueStart=", valueStart)
				i += numValues // 本轮消耗掉后面的值参数
				// fmt.Println("i=", i)
				// 多条件：所有字段都需匹配
				for j, fieldName := range fieldNames {
					value := conds[valueStart+j]
					//	fmt.Println("value=", value)
					camelFieldName := m.toCamelCase(fieldName, tableCache)
					//fmt.Println("camelFieldName=", camelFieldName)
					field := itemValue.FieldByName(camelFieldName)
					//fmt.Println("field=", field)
					if !field.IsValid() {
						return false
					}
					fieldValue := field.Interface()
					//fmt.Println("fieldValue=", fieldValue)
					fieldValueKind := field.Kind()
					valueKind := reflect.TypeOf(value).Kind()
					//fmt.Println("fieldValueKind=", fieldValueKind)
					//fmt.Println("valueKind=", valueKind)
					var equal bool
					if fieldValueKind != valueKind {
						fieldValueStr := fmt.Sprintf("%v", fieldValue)
						valueStr := fmt.Sprintf("%v", value)
						equal = (fieldValueStr == valueStr)
						//fmt.Println("equal=", equal)
					} else {
						equal = reflect.DeepEqual(fieldValue, value)
						//fmt.Println("equal=", equal)
					}
					if !equal {
						return false
					}
				}
				// 所有条件都匹配
				return true
			}
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

	pendingOps = len(m.opLogs[tableName])
	m.mu.RUnlock()

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

	// 清空 sync.Map
	tableCache.data.Range(func(k, _ interface{}) bool {
		tableCache.data.Delete(k)
		return true
	})

	return nil
}
