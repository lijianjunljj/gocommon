package service

import (
	"github.com/lijianjunljj/gocommon/sn/utils"
)

// IDService ID服务
type IDService struct {
	GetID     func() int64
	GetUnixID func() int64
}

// NewIDService 实例化ID服务
func NewIDService() *IDService {
	return &IDService{
		GetUnixID: utils.GetUnixID,
	}
}
