package base

import (
	"github.com/gin-gonic/gin"
	"github.com/lijianjunljj/gocommon/sn/client"
)

// API 服务
type API interface {
	List(search *Search, isHook bool, model interface{}) (int64, error)
	All(search *Search, isHook bool, model interface{}) error
	Detail(model interface{}) error
	Add(model interface{}) error
	Edit(model interface{}) error
	Delete(model interface{}) error
}

// Service 服务层
type Service struct {
	Ctx       *gin.Context
	Model     interface{}
	API       API
	IDRpc     *client.IDClient
	GetUnixID func() (string, error)
}

// NewService 实例化服务
func NewService(model interface{}) *Service {
	return &Service{
		Model: model,
		API:   model.(API),
		IDRpc: client.NewIDClient(),
	}
}
