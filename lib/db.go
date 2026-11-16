package lib

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"shortlink/models"
)

var (
	DB      *gorm.DB
	DBError error
)

func ConnectDB() bool {
	DB, DBError = gorm.Open(
		sqlite.Open("data.db?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"),
		&gorm.Config{},
	)
	if DBError != nil {
		fmt.Println("データベースに接続できませんでした: ", DBError)
		return false
	}

	if err := DB.AutoMigrate(&models.Links{}); err != nil {
		fmt.Println("データベースのMigrationに失敗しました: ", err)
		return false
	}
	fmt.Println("データベースに接続し、Migrateしました。")

	return true
}
