package model

// fork: O4 — rows for the Excel export; password and legacy access token are omitted.
func GetAllUsersForExport() ([]*User, error) {
	var users []*User
	err := DB.Omit("password", "access_token").Order("id asc").Find(&users).Error
	return users, err
}
