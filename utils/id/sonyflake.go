package id

import (
	"errors"
	"os"
	"strconv"

	"github.com/sony/sonyflake"
)

var sf *sonyflake.Sonyflake

func init() {
	st := sonyflake.Settings{}

	// 从环境变量读取机器ID，如果设置了 SONYFLAKE_MACHINE_ID
	if machineIDStr := os.Getenv("SONYFLAKE_MACHINE_ID"); machineIDStr != "" {
		if machineID, err := strconv.ParseUint(machineIDStr, 10, 16); err == nil {
			st.MachineID = func() (uint16, error) {
				return uint16(machineID), nil
			}
		}
	}

	// 如果未设置 MachineID，sonyflake 会默认使用私有 IP 地址的低 16 位
	sf = sonyflake.NewSonyflake(st)
}

// InitSonyflake 自定义初始化 Sonyflake，允许用户自定义机器ID
// machineIDFunc: 返回机器ID的函数，如果为 nil 则使用默认方式（IP地址）
func InitSonyflake(machineIDFunc func() (uint16, error)) {
	st := sonyflake.Settings{}
	if machineIDFunc != nil {
		st.MachineID = machineIDFunc
	}
	sf = sonyflake.NewSonyflake(st)
}

// GetID 获取唯一ID
func GetID() (uint64, error) {
	if sf == nil {
		return 0, errors.New("sonyflake not initialized")
	}
	return sf.NextID()
}
