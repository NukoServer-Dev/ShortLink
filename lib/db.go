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
	DB, DBError = gorm.Open(sqlite.Open("data.db"), &gorm.Config{})
	if DBError != nil {
		fmt.Println("データベースに接続できませんでした: ", DBError)
		return false
	}

	DB.AutoMigrate(&models.Links{})
	fmt.Println("データベースに接続し、Migrateしました。")

	return true
}
