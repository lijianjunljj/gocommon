package misc

import (
	"gorm.io/gorm"
	"time"
)

type BaseModelUnixID struct {
	ID         string `json:"id" gorm:"type:varchar(30);primary_key"`
	CreateBy   string `json:"-"  gorm:"type:varchar(30)"`
	CreateTime int64  `json:"create_time"`
	UpdateTime int64  `json:"-"`
}

type BaseModelInc struct {
	ID        uint64         `json:"id"          gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
	CreateBy  string         `json:"created_by"  gorm:"type:varchar(30)"`
}
