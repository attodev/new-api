package model

import "gorm.io/gorm"

// fork: O13 — Open WebUI link table. Called from migrateDB (model/main.go) and
// from package TestMains that need the link schema.
func MigrateForkOpenWebUITables(db *gorm.DB) error {
	return db.AutoMigrate(&OpenWebUILink{})
}
