package model

// fork: O3 — organization handlers evict a member's cached profile after
// changing membership or quota.
func InvalidateUserCache(userId int) error { return invalidateUserCache(userId) }
