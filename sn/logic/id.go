package logic

import (
	"github.com/lijianjunljj/gocommon/sn/service"
)

// IDLogic ID逻辑层
type IDLogic struct {
	svc *service.IDService
}

// NewIDLogic 初始化ID逻辑
func NewIDLogic(service *service.IDService) *IDLogic {
	return &IDLogic{
		svc: service,
	}
}

// GetID 获取ID
func (l *IDLogic) GetID() int64 {
	return l.svc.GetID()
}

// GetUnixID 获取时间戳ID
func (l *IDLogic) GetUnixID() int64 {
	return l.svc.GetUnixID()
}
