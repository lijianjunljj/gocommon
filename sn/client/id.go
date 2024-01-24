package client

import (
	"github.com/lijianjunljj/gocommon/sn/logic"
	"github.com/lijianjunljj/gocommon/sn/service"
	util "github.com/lijianjunljj/gocommon/utils"
)

// IDClient ID客户端连接
type IDClient struct {
}

// NewIDClient 获取ID实例
func NewIDClient() *IDClient {
	return &IDClient{}
}

// GetID 获取ID
func (c *IDClient) GetID() (string, error) {
	svc := service.NewIDService()
	l := logic.NewIDLogic(svc)
	return util.Int64ToStr(l.GetID()), nil
}

// GetUnixID 获取时间戳ID
func (c *IDClient) GetUnixID() (string, error) {
	svc := service.NewIDService()
	l := logic.NewIDLogic(svc)
	return util.Int64ToStr(l.GetUnixID()), nil
}
