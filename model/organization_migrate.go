package model

import "gorm.io/gorm"

// fork: O3 — organization tables. Called from migrateDB (model/main.go) and
// from package TestMains that need the organization schema.
func MigrateForkOrganizationTables(db *gorm.DB) error {
	return db.AutoMigrate(
		&Organization{},
		&OrganizationSubscriptionPlan{},
		&OrganizationUserSubscription{},
		&OrganizationSubscriptionPreConsumeRecord{},
	)
}
