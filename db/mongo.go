package db

import (
	"context"
	"fmt"
	"time"

	"github.com/lijianjunljj/gocommon/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Mongo struct {
	config   *config.MongoOptions
	client   *mongo.Client
	database *mongo.Database
}

func NewMongo(config *config.MongoOptions) *Mongo {
	return &Mongo{config: config}
}

func (m *Mongo) Connect() *mongo.Database {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.config.ConnectTimeout)*time.Second)
	defer cancel()
	clientOpts := options.Client().ApplyURI(m.config.URI)
	if m.config.Username != "" && m.config.Password != "" {
		clientOpts.SetAuth(options.Credential{
			Username:   m.config.Username,
			Password:   m.config.Password,
			AuthSource: m.config.AuthSource,
		})
	}
	if m.config.MaxPoolSize > 0 {
		clientOpts.SetMaxPoolSize(m.config.MaxPoolSize)
	}
	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		panic(fmt.Sprintf("mongo connect fail: %v", err))
	}
	m.client = client
	m.database = client.Database(m.config.Database)
	return m.database
}

func (m *Mongo) DB() *mongo.Database {
	if m.database == nil {
		m.Connect()
	}
	return m.database
}

// AutoMigrate 仅作占位，MongoDB无需像SQL那样迁移表结构
func (m *Mongo) AutoMigrate(collections ...interface{}) {
	// 可选：实现集合的创建或索引初始化
}
