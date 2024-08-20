package curd

import (
	"github.com/lijianjunljj/gocommon/config"
	"github.com/lijianjunljj/gocommon/db"
	"gorm.io/gorm"
	"sync"
)

var (
	mysqlInstance *db.Mysql
	once          sync.Once
	configs       *config.MysqlOptions
)

func Init(options *config.MysqlOptions) {
	configs = options
	WithMysql(Mysql)
}
func WithMysql(mysqlFunc func() *gorm.DB) {
	mysql = mysqlFunc
}
func Mysql() *gorm.DB {
	GetInstance()
	return mysqlInstance.DB()
}
func GetInstance() *db.Mysql {
	once.Do(func() {
		mysqlInstance = db.NewMysql(false, configs)
		mysqlInstance.Connect()
	})
	return mysqlInstance
}

func AutoMigrate(dst ...interface{}) {
	GetInstance()
	mysqlInstance.AutoMigrate(dst...)

}
