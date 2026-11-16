package models

import (
	"database/sql"
	"time"
)

type Links struct {
	ID         uint   `gorm:"primaryKey"`
	Code       string `gorm:"not null"`
	Url        string `gorm:"not null"`
	IsBan      sql.NullBool
	BanReason  sql.NullString
	isWarn     sql.NullBool
	WarnReason sql.NullString
	CreatedAt  time.Time
	UpdatedAt  time.Time
	CreatorIP  string `gorm:"not null"`
}
