package curd

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Txn struct {
	isCommit bool
	Tx       *gorm.DB
}

func NewTxn(tx *gorm.DB) *Txn {
	that := &Txn{Tx: tx}
	that.PreTxn()
	return that
}

func (that *Txn) TryCommit() error {
	if that.isCommit {
		err := that.Tx.Commit().Error
		if err != nil {
			return err
		}
	}
	return nil
}
func (that *Txn) PreTxn() {
	that.isCommit = false
	if that.Tx == nil {
		that.Tx = Mysql().Begin().Omit(clause.Associations).Session(&gorm.Session{})
		that.isCommit = true
	}
}
