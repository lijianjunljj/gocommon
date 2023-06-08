package misc

import (
	"fmt"
	"github.com/lijianjunljj/gocommon/db"
	"github.com/lijianjunljj/gocommon/config"
	"gorm.io/gorm"
)

var DB db.AbstractDatabase

func Init(dbType string, configFunc func() interface{}, tables ...interface{}) {
	fmt.Println("dbType", dbType)
	if dbType == "mysql" {
		conf := configFunc()
		fmt.Println("conf", conf)
		DB = db.NewMysql(false, conf.(*config.MysqlOptions))
		DB.AutoMigrate(tables...)
	}
}

func GetDB() *gorm.DB {
	if DB == nil {
		Init(Config.DbType, func() interface{} {
			return Config.Mysql
		}, Config.AutoAutoMigrateTables...)
	}
	return DB.DB()
}
