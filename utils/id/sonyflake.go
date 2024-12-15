package id

import "github.com/sony/sonyflake"

var sf *sonyflake.Sonyflake

func init() {
	st := sonyflake.Settings{}
	sf = sonyflake.NewSonyflake(st)
}

func GetID() (uint64, error) {
	return sf.NextID()
}
