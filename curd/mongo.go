package curd

import (
	"sync"

	"github.com/lijianjunljj/gocommon/config"
	"github.com/lijianjunljj/gocommon/db"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	mongoInstance *db.Mongo
	onceMongo     sync.Once
	mongoConfigs  *config.MongoOptions
	mongoFunc     func() *mongo.Database
)

func InitMongo(options *config.MongoOptions) {
	mongoConfigs = options
	WithMongo(Mongo)
}

func WithMongo(mongoFunction func() *mongo.Database) {
	mongoFunc = mongoFunction
}

func Mongo() *mongo.Database {
	GetMongoInstance()
	return mongoInstance.DB()
}

func GetMongoInstance() *db.Mongo {
	onceMongo.Do(func() {
		mongoInstance = db.NewMongo(mongoConfigs)
		mongoInstance.Connect()
	})
	return mongoInstance
}

func AutoMigrateMongo(collections ...interface{}) {
	GetMongoInstance()
	mongoInstance.AutoMigrate(collections...)
}
