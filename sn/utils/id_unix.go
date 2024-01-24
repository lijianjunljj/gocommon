package utils

import (
	"fmt"
	"github.com/lijianjunljj/gocommon/utils"
)

const (
	//startTime 开始服务时间戳
	startTime = 1604468294 //开始时间戳秒
	//workerIDBits 机器ID占位数，5位可提供0-31
	workerIDBits = 5
	//sequenceIDBits 每秒序列号占位数，20位，最大1048576-1
	sequenceIDBits = 20
)

var unixID *UnixID

// UnixID 定义时间戳ID结构体
type UnixID struct {
	//WorkerID 机器ID
	WorkerID int64
	//LastTimeUnix 上次生成ID的时间戳
	LastTimeUnix int64
	//SequenceID 一秒内序列号0-1048576
	SequenceID int64
	//生成序列的掩码(20位所对应的最大整数值)
	Mask int64
}

// UnixIDOption 定义更新UnixID回调函数
type UnixIDOption func(*UnixID)

// NewUnixID 创建UnixID实例
func NewUnixID(opts ...UnixIDOption) *UnixID {
	id := &UnixID{}
	for _, opt := range opts {
		opt(id)
	}
	id.Mask = -1 ^ (-1 << sequenceIDBits)
	return id

}

// WithWorkerID 设置机器ID
func WithWorkerID(workerID int64) UnixIDOption {
	if workerID > 31 || workerID < 0 {
		panic("workerID must be in between [0, 31]")
	}
	return func(id *UnixID) {
		id.WorkerID = workerID
	}
}

// Next 线程安全的获得下一个 ID 的方法
func (id *UnixID) Next() int64 {
	timestamp := utils.TimeUnix()
	//如果当前时间小于上一次ID生成的时间戳: 说明系统时钟回退过 - 这个时候应当抛出异常
	if timestamp < id.LastTimeUnix {
		panic(fmt.Sprintf("Clock moved backwards.  Refusing to generate id for %d milliseconds", id.LastTimeUnix-timestamp))
	}
	//如果是同一时间生成的，则进行秒内序列
	if id.LastTimeUnix == timestamp {
		id.SequenceID = (id.SequenceID + 1) & id.Mask
		//秒内序列溢出 即 序列 > 1048575
		if id.SequenceID == 0 {
			//阻塞到下一个秒,获得新的时间戳
			timestamp = NextTimeUnix(id.LastTimeUnix)
		}
	} else {
		id.SequenceID = 0
	}
	id.LastTimeUnix = timestamp
	return ((timestamp - startTime) << (workerIDBits + sequenceIDBits)) | (id.WorkerID << sequenceIDBits) | id.SequenceID
}

// NextTimeUnix 阻塞到下一秒 即直到获得新的时间戳
func NextTimeUnix(lastTimeUnix int64) int64 {
	timestamp := utils.TimeUnix()
	for {
		if timestamp <= lastTimeUnix {
			timestamp = utils.TimeUnix()
		} else {
			break
		}

	}
	return timestamp
}

// InitUnixID 初始化时间戳组合ID
func InitUnixID() {
	unixID = NewUnixID()
}

// GetUnixID 获取时间戳组合ID
func GetUnixID() int64 {
	if unixID == nil {
		InitUnixID()
	}
	return unixID.Next()
}
