package model

import (
	"context"
	"errors"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TossTransactionReconciliationCursor is a durable high-water mark for one
// Toss MID namespace. Webhooks have a finite retry window, so transaction
// reconciliation must resume from persisted provider time after long outages.
type TossTransactionReconciliationCursor struct {
	SourceKey        string `json:"-" gorm:"type:varchar(64);primaryKey"`
	CursorTime       int64  `json:"-" gorm:"index"`
	RecentCursorTime int64  `json:"-" gorm:"default:0"`
	LeaseOwner       string `json:"-" gorm:"type:varchar(64);not null;default:''"`
	LeaseUntil       int64  `json:"-" gorm:"not null;default:0"`
	UpdateTime       int64  `json:"-"`
}

// TossTransactionReconciliationPageCursor checkpoints provider pagination for
// one reconciliation lane. A Transaction lookup can consume most of the
// scheduler's per-source deadline, so keeping startingAfter only in memory can
// make a busy MID replay its first full page forever. Recent and historical
// lanes use separate rows and therefore cannot starve one another.
type TossTransactionReconciliationPageCursor struct {
	SourceKey     string `json:"-" gorm:"type:varchar(64);primaryKey;not null"`
	Lane          string `json:"-" gorm:"type:varchar(16);primaryKey;not null"`
	CursorTime    int64  `json:"-" gorm:"not null"`
	StartTime     int64  `json:"-" gorm:"not null"`
	EndTime       int64  `json:"-" gorm:"not null"`
	StartingAfter string `json:"-" gorm:"type:varchar(64);not null;default:''"`
	UpdateTime    int64  `json:"-" gorm:"not null"`
}

var (
	ErrTossTransactionCursorConflict     = errors.New("Toss transaction reconciliation cursor changed concurrently")
	ErrTossTransactionPageCursorConflict = errors.New("Toss transaction reconciliation page cursor changed concurrently")
	ErrTossTransactionLeaseHeld          = errors.New("Toss transaction reconciliation source lease is held")
	ErrTossTransactionLeaseLost          = errors.New("Toss transaction reconciliation source lease was lost")
)

const (
	TossTransactionPageLaneRecent     = "recent"
	TossTransactionPageLaneHistorical = "historical"
)

func validTossTransactionPageCursorIdentity(sourceKey, lane string) bool {
	sourceKey = strings.TrimSpace(sourceKey)
	lane = strings.TrimSpace(lane)
	return sourceKey != "" && len(sourceKey) <= 64 &&
		(lane == TossTransactionPageLaneRecent || lane == TossTransactionPageLaneHistorical)
}

func validateTossTransactionReconciliationPageLeaseTx(tx *gorm.DB, sourceKey, lane, leaseOwner string, cursorTime, now int64) error {
	if tx == nil || !validTossTransactionPageCursorIdentity(sourceKey, lane) || leaseOwner == "" || cursorTime <= 0 || now <= 0 {
		return ErrTossTransactionLeaseLost
	}
	var parent TossTransactionReconciliationCursor
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("source_key = ?", sourceKey).First(&parent).Error; err != nil {
		return err
	}
	persistedCursorTime := parent.CursorTime
	if lane == TossTransactionPageLaneRecent {
		persistedCursorTime = parent.RecentCursorTime
	}
	if persistedCursorTime != cursorTime {
		return ErrTossTransactionCursorConflict
	}
	if parent.LeaseOwner != leaseOwner || parent.LeaseUntil <= now {
		return ErrTossTransactionLeaseLost
	}
	return nil
}

// AcquireTossTransactionReconciliationLease serializes every provider lookup
// for one stable MID/source across application nodes. The cursor/page CAS still
// protects durable progress, while this longer source lease prevents replicas
// from issuing the same expensive Transaction and Payment GETs before either
// node reaches that CAS. A crashed owner is recoverable after leaseUntil.
func AcquireTossTransactionReconciliationLeaseWithContext(ctx context.Context, sourceKey, owner string, leaseSeconds int64) (bool, error) {
	sourceKey = strings.TrimSpace(sourceKey)
	owner = strings.TrimSpace(owner)
	if sourceKey == "" || len(sourceKey) > 64 || owner == "" || len(owner) > 64 || leaseSeconds <= 0 || leaseSeconds > 60*60 {
		return false, errors.New("invalid Toss transaction reconciliation lease")
	}
	db := dbWithContext(ctx)
	now := getDBTimestampTx(db)
	leaseUntil := now + leaseSeconds
	result := db.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ?", sourceKey).
		Where("lease_until <= ? OR lease_until IS NULL OR lease_owner = ?", now, owner).
		Updates(map[string]interface{}{
			"lease_owner": owner,
			"lease_until": leaseUntil,
			"update_time": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	// MySQL's default affected-row semantics can report zero when the same
	// owner renews within one DB-clock second and every assigned value is
	// unchanged. Reloading the guarded row distinguishes that benign no-op from
	// an active competing owner without relying on dialect-specific flags.
	var persisted TossTransactionReconciliationCursor
	if err := db.Select("source_key", "lease_owner", "lease_until").
		Where("source_key = ?", sourceKey).First(&persisted).Error; err != nil {
		return false, err
	}
	return persisted.LeaseOwner == owner && persisted.LeaseUntil > now, nil
}

func ReleaseTossTransactionReconciliationLeaseWithContext(ctx context.Context, sourceKey, owner string) error {
	sourceKey = strings.TrimSpace(sourceKey)
	owner = strings.TrimSpace(owner)
	if sourceKey == "" || len(sourceKey) > 64 || owner == "" || len(owner) > 64 {
		return errors.New("invalid Toss transaction reconciliation lease release")
	}
	db := dbWithContext(ctx)
	now := getDBTimestampTx(db)
	result := db.Model(&TossTransactionReconciliationCursor{}).
		Where("source_key = ? AND lease_owner = ?", sourceKey, owner).
		Updates(map[string]interface{}{
			"lease_owner": "",
			"lease_until": int64(0),
			"update_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var persisted TossTransactionReconciliationCursor
	if err := db.Select("source_key", "lease_owner", "lease_until").
		Where("source_key = ?", sourceKey).First(&persisted).Error; err != nil {
		return err
	}
	if persisted.LeaseOwner == "" && persisted.LeaseUntil == 0 {
		return nil
	}
	return ErrTossTransactionLeaseLost
}

// GetOrCreateTossTransactionReconciliationPageCursor starts a durable provider
// page scan or resumes the exact interval already in progress. The caller may
// offer a later end on a subsequent run (for example, when "now" moved), but
// must finish the stored closed interval before widening it.
func GetOrCreateTossTransactionReconciliationPageCursor(sourceKey, lane string, cursorTime, startTime, endTime int64) (*TossTransactionReconciliationPageCursor, error) {
	return GetOrCreateTossTransactionReconciliationPageCursorWithContext(context.Background(), sourceKey, lane, cursorTime, startTime, endTime)
}

func GetOrCreateTossTransactionReconciliationPageCursorWithContext(ctx context.Context, sourceKey, lane string, cursorTime, startTime, endTime int64) (*TossTransactionReconciliationPageCursor, error) {
	return getOrCreateTossTransactionReconciliationPageCursorWithContext(ctx, sourceKey, lane, "", cursorTime, startTime, endTime)
}

func GetOrCreateTossTransactionReconciliationPageCursorLeasedWithContext(ctx context.Context, sourceKey, lane, leaseOwner string, cursorTime, startTime, endTime int64) (*TossTransactionReconciliationPageCursor, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 64 {
		return nil, errors.New("invalid Toss transaction reconciliation lease owner")
	}
	return getOrCreateTossTransactionReconciliationPageCursorWithContext(ctx, sourceKey, lane, leaseOwner, cursorTime, startTime, endTime)
}

func getOrCreateTossTransactionReconciliationPageCursorWithContext(ctx context.Context, sourceKey, lane, leaseOwner string, cursorTime, startTime, endTime int64) (*TossTransactionReconciliationPageCursor, error) {
	sourceKey = strings.TrimSpace(sourceKey)
	lane = strings.TrimSpace(lane)
	if !validTossTransactionPageCursorIdentity(sourceKey, lane) || cursorTime <= 0 || startTime <= 0 || endTime <= startTime {
		return nil, errors.New("invalid Toss transaction reconciliation page cursor")
	}
	row := &TossTransactionReconciliationPageCursor{}
	err := dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := getDBTimestampTx(tx)
		var parent TossTransactionReconciliationCursor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_key = ?", sourceKey).
			First(&parent).Error; err != nil {
			return err
		}
		if leaseOwner != "" && (parent.LeaseOwner != leaseOwner || parent.LeaseUntil <= now) {
			return ErrTossTransactionLeaseLost
		}
		persistedCursorTime := parent.CursorTime
		if lane == TossTransactionPageLaneRecent {
			persistedCursorTime = parent.RecentCursorTime
		}
		if persistedCursorTime != cursorTime {
			return ErrTossTransactionCursorConflict
		}

		var existing TossTransactionReconciliationPageCursor
		existingResult := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_key = ? AND lane = ?", sourceKey, lane).
			First(&existing)
		if existingResult.Error != nil && !errors.Is(existingResult.Error, gorm.ErrRecordNotFound) {
			return existingResult.Error
		}
		if existingResult.Error == nil && existing.CursorTime != cursorTime {
			if err := tx.Where("source_key = ? AND lane = ? AND cursor_time = ?", sourceKey, lane, existing.CursorTime).
				Delete(&TossTransactionReconciliationPageCursor{}).Error; err != nil {
				return err
			}
		}
		candidate := &TossTransactionReconciliationPageCursor{
			SourceKey:  sourceKey,
			Lane:       lane,
			CursorTime: cursorTime,
			StartTime:  startTime,
			EndTime:    endTime,
			UpdateTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(candidate).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_key = ? AND lane = ?", sourceKey, lane).
			First(row).Error; err != nil {
			return err
		}
		if row.CursorTime != cursorTime || row.StartTime != startTime || row.EndTime <= row.StartTime || row.EndTime > endTime || len(row.StartingAfter) > 64 {
			return ErrTossTransactionPageCursorConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// AdvanceTossTransactionReconciliationPageCursor publishes startingAfter only
// after every local side effect from the full provider page succeeds. Repeating
// a page after a crash is safe; skipping an incompletely applied page is not.
func AdvanceTossTransactionReconciliationPageCursor(sourceKey, lane string, cursorTime, startTime, endTime int64, expectedStartingAfter, nextStartingAfter string) error {
	return AdvanceTossTransactionReconciliationPageCursorWithContext(
		context.Background(), sourceKey, lane, cursorTime, startTime, endTime, expectedStartingAfter, nextStartingAfter,
	)
}

func AdvanceTossTransactionReconciliationPageCursorWithContext(ctx context.Context, sourceKey, lane string, cursorTime, startTime, endTime int64, expectedStartingAfter, nextStartingAfter string) error {
	return advanceTossTransactionReconciliationPageCursorWithContext(
		ctx, sourceKey, lane, "", cursorTime, startTime, endTime, expectedStartingAfter, nextStartingAfter,
	)
}

func AdvanceTossTransactionReconciliationPageCursorLeasedWithContext(ctx context.Context, sourceKey, lane, leaseOwner string, cursorTime, startTime, endTime int64, expectedStartingAfter, nextStartingAfter string) error {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 64 {
		return errors.New("invalid Toss transaction reconciliation lease owner")
	}
	return advanceTossTransactionReconciliationPageCursorWithContext(
		ctx, sourceKey, lane, leaseOwner, cursorTime, startTime, endTime, expectedStartingAfter, nextStartingAfter,
	)
}

func advanceTossTransactionReconciliationPageCursorWithContext(ctx context.Context, sourceKey, lane, leaseOwner string, cursorTime, startTime, endTime int64, expectedStartingAfter, nextStartingAfter string) error {
	sourceKey = strings.TrimSpace(sourceKey)
	lane = strings.TrimSpace(lane)
	expectedStartingAfter = strings.TrimSpace(expectedStartingAfter)
	nextStartingAfter = strings.TrimSpace(nextStartingAfter)
	if !validTossTransactionPageCursorIdentity(sourceKey, lane) || cursorTime <= 0 || startTime <= 0 || endTime <= startTime ||
		len(expectedStartingAfter) > 64 || nextStartingAfter == "" || len(nextStartingAfter) > 64 || nextStartingAfter == expectedStartingAfter {
		return errors.New("invalid Toss transaction reconciliation page cursor advance")
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := getDBTimestampTx(tx)
		if leaseOwner != "" {
			if err := validateTossTransactionReconciliationPageLeaseTx(tx, sourceKey, lane, leaseOwner, cursorTime, now); err != nil {
				return err
			}
		}
		result := tx.Model(&TossTransactionReconciliationPageCursor{}).
			Where("source_key = ? AND lane = ? AND cursor_time = ? AND start_time = ? AND end_time = ? AND starting_after = ?",
				sourceKey, lane, cursorTime, startTime, endTime, expectedStartingAfter).
			Updates(map[string]interface{}{
				"starting_after": nextStartingAfter,
				"update_time":    now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossTransactionPageCursorConflict
		}
		return nil
	})
}

// CompleteTossTransactionReconciliationPageCursor removes the page checkpoint
// before the higher-level time cursor is advanced. A crash in between merely
// repeats an idempotent window; reversing that order could skip provider rows.
func CompleteTossTransactionReconciliationPageCursor(sourceKey, lane string, cursorTime, startTime, endTime int64, expectedStartingAfter string) error {
	return CompleteTossTransactionReconciliationPageCursorWithContext(
		context.Background(), sourceKey, lane, cursorTime, startTime, endTime, expectedStartingAfter,
	)
}

func CompleteTossTransactionReconciliationPageCursorWithContext(ctx context.Context, sourceKey, lane string, cursorTime, startTime, endTime int64, expectedStartingAfter string) error {
	return completeTossTransactionReconciliationPageCursorWithContext(
		ctx, sourceKey, lane, "", cursorTime, startTime, endTime, expectedStartingAfter,
	)
}

func CompleteTossTransactionReconciliationPageCursorLeasedWithContext(ctx context.Context, sourceKey, lane, leaseOwner string, cursorTime, startTime, endTime int64, expectedStartingAfter string) error {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 64 {
		return errors.New("invalid Toss transaction reconciliation lease owner")
	}
	return completeTossTransactionReconciliationPageCursorWithContext(
		ctx, sourceKey, lane, leaseOwner, cursorTime, startTime, endTime, expectedStartingAfter,
	)
}

func completeTossTransactionReconciliationPageCursorWithContext(ctx context.Context, sourceKey, lane, leaseOwner string, cursorTime, startTime, endTime int64, expectedStartingAfter string) error {
	sourceKey = strings.TrimSpace(sourceKey)
	lane = strings.TrimSpace(lane)
	expectedStartingAfter = strings.TrimSpace(expectedStartingAfter)
	if !validTossTransactionPageCursorIdentity(sourceKey, lane) || cursorTime <= 0 || startTime <= 0 || endTime <= startTime || len(expectedStartingAfter) > 64 {
		return errors.New("invalid Toss transaction reconciliation page cursor completion")
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := getDBTimestampTx(tx)
		if leaseOwner != "" {
			if err := validateTossTransactionReconciliationPageLeaseTx(tx, sourceKey, lane, leaseOwner, cursorTime, now); err != nil {
				return err
			}
		}
		result := tx.Where(
			"source_key = ? AND lane = ? AND cursor_time = ? AND start_time = ? AND end_time = ? AND starting_after = ?",
			sourceKey, lane, cursorTime, startTime, endTime, expectedStartingAfter,
		).Delete(&TossTransactionReconciliationPageCursor{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossTransactionPageCursorConflict
		}
		return nil
	})
}

// DeleteTossTransactionReconciliationPageCursorsBefore discards only provider
// intervals that are no longer queryable. It is used for Toss test stores,
// whose Transaction API exposes at most the most recent three days.
func DeleteTossTransactionReconciliationPageCursorsBefore(sourceKey string, earliestStartTime int64) error {
	return DeleteTossTransactionReconciliationPageCursorsBeforeWithContext(context.Background(), sourceKey, earliestStartTime)
}

func DeleteTossTransactionReconciliationPageCursorsBeforeWithContext(ctx context.Context, sourceKey string, earliestStartTime int64) error {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" || len(sourceKey) > 64 || earliestStartTime <= 0 {
		return errors.New("invalid Toss transaction reconciliation page cursor floor")
	}
	return dbWithContext(ctx).Where("source_key = ? AND start_time < ?", sourceKey, earliestStartTime).
		Delete(&TossTransactionReconciliationPageCursor{}).Error
}

// TossTransactionReconciliationCursorAlias links an older credential-scoped
// cursor to the stable MID-scoped cursor that supersedes it. InitialCursor is
// positive only for a legacy source that must be created during the bridge;
// zero means "merge this alias only when it already exists".
type TossTransactionReconciliationCursorAlias struct {
	SourceKey     string
	InitialCursor int64
}

func GetOrCreateTossTransactionReconciliationCursor(sourceKey string, initialCursor int64) (*TossTransactionReconciliationCursor, error) {
	return GetOrCreateTossTransactionReconciliationCursorWithContext(context.Background(), sourceKey, initialCursor)

}

func GetOrCreateTossTransactionReconciliationCursorWithContext(ctx context.Context, sourceKey string, initialCursor int64) (*TossTransactionReconciliationCursor, error) {
	row, _, err := GetOrCreateTossTransactionReconciliationCursorWithAliasesWithContext(ctx, sourceKey, initialCursor, nil)
	return row, err
}

func normalizeTossTransactionCursorAliases(sourceKey string, aliases []TossTransactionReconciliationCursorAlias) ([]TossTransactionReconciliationCursorAlias, error) {
	byKey := make(map[string]int64, len(aliases))
	for i := range aliases {
		aliasKey := strings.TrimSpace(aliases[i].SourceKey)
		if aliasKey == "" || len(aliasKey) > 64 || aliases[i].InitialCursor < 0 {
			return nil, errors.New("invalid Toss transaction reconciliation cursor alias")
		}
		if aliasKey == sourceKey {
			continue
		}
		initial := aliases[i].InitialCursor
		if current, exists := byKey[aliasKey]; !exists || (initial > 0 && (current <= 0 || initial < current)) {
			byKey[aliasKey] = initial
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]TossTransactionReconciliationCursorAlias, 0, len(keys))
	for _, key := range keys {
		result = append(result, TossTransactionReconciliationCursorAlias{SourceKey: key, InitialCursor: byKey[key]})
	}
	return result, nil
}

// GetOrCreateTossTransactionReconciliationCursorWithAliases performs a
// one-way, idempotent cursor bridge from pre-fingerprint secret-hash sources to
// the stable client-key/MID source. The oldest existing high-water mark wins.
// Alias rows remain and are advanced atomically with the target so an older
// application node in a rolling deployment cannot recreate an old cursor and
// make the new node repeatedly rewind.
func GetOrCreateTossTransactionReconciliationCursorWithAliases(sourceKey string, initialCursor int64, aliases []TossTransactionReconciliationCursorAlias) (*TossTransactionReconciliationCursor, []string, error) {
	return GetOrCreateTossTransactionReconciliationCursorWithAliasesWithContext(context.Background(), sourceKey, initialCursor, aliases)
}

func GetOrCreateTossTransactionReconciliationCursorWithAliasesWithContext(ctx context.Context, sourceKey string, initialCursor int64, aliases []TossTransactionReconciliationCursorAlias) (*TossTransactionReconciliationCursor, []string, error) {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" || len(sourceKey) > 64 || initialCursor <= 0 {
		return nil, nil, errors.New("invalid Toss transaction reconciliation cursor")
	}
	normalizedAliases, err := normalizeTossTransactionCursorAliases(sourceKey, aliases)
	if err != nil {
		return nil, nil, err
	}
	row := &TossTransactionReconciliationCursor{}
	activeAliases := make([]string, 0, len(normalizedAliases))
	err = dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := getDBTimestampTx(tx)
		target := &TossTransactionReconciliationCursor{
			SourceKey:  sourceKey,
			CursorTime: initialCursor,
			UpdateTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(target).Error; err != nil {
			return err
		}
		for i := range normalizedAliases {
			if normalizedAliases[i].InitialCursor <= 0 {
				continue
			}
			alias := &TossTransactionReconciliationCursor{
				SourceKey:  normalizedAliases[i].SourceKey,
				CursorTime: normalizedAliases[i].InitialCursor,
				UpdateTime: now,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(alias).Error; err != nil {
				return err
			}
		}

		keys := make([]string, 0, len(normalizedAliases)+1)
		keys = append(keys, sourceKey)
		for i := range normalizedAliases {
			keys = append(keys, normalizedAliases[i].SourceKey)
		}
		var rows []TossTransactionReconciliationCursor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_key IN ?", keys).
			Order("source_key ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		minimum := int64(0)
		latestRecent := int64(0)
		targetFound := false
		activeAliases = activeAliases[:0]
		for i := range rows {
			if rows[i].CursorTime <= 0 || rows[i].RecentCursorTime < 0 {
				return errors.New("invalid persisted Toss transaction reconciliation cursor")
			}
			if rows[i].SourceKey == sourceKey {
				targetFound = true
			} else {
				activeAliases = append(activeAliases, rows[i].SourceKey)
			}
			if minimum == 0 || rows[i].CursorTime < minimum {
				minimum = rows[i].CursorTime
			}
			if rows[i].RecentCursorTime > latestRecent {
				latestRecent = rows[i].RecentCursorTime
			}
		}
		if !targetFound || minimum <= 0 {
			return errors.New("Toss transaction reconciliation cursor was not persisted")
		}
		var targetRow TossTransactionReconciliationCursor
		for i := range rows {
			if rows[i].SourceKey == sourceKey {
				targetRow = rows[i]
				break
			}
		}
		if minimum < targetRow.CursorTime {
			result := tx.Model(&TossTransactionReconciliationCursor{}).
				Where("source_key = ? AND cursor_time = ?", sourceKey, targetRow.CursorTime).
				Updates(map[string]interface{}{"cursor_time": minimum, "update_time": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTossTransactionCursorConflict
			}
			targetRow.CursorTime = minimum
			targetRow.UpdateTime = now
		}
		// The historical backlog cursor merges toward the oldest mark so no
		// interval is skipped. The independent recent-tail cursor has the opposite
		// rule: a completed scan from any alias is valid for the same MID, so keep
		// the newest mark and copy it to every existing alias atomically.
		if latestRecent > 0 {
			for i := range rows {
				if rows[i].RecentCursorTime == latestRecent {
					continue
				}
				query := tx.Model(&TossTransactionReconciliationCursor{}).
					Where("source_key = ?", rows[i].SourceKey)
				if rows[i].RecentCursorTime == 0 {
					query = query.Where("recent_cursor_time = 0 OR recent_cursor_time IS NULL")
				} else {
					query = query.Where("recent_cursor_time = ?", rows[i].RecentCursorTime)
				}
				result := query.Updates(map[string]interface{}{
					"recent_cursor_time": latestRecent,
					"update_time":        now,
				})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrTossTransactionCursorConflict
				}
			}
			targetRow.RecentCursorTime = latestRecent
			targetRow.UpdateTime = now
		}
		*row = targetRow
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if row.CursorTime <= 0 {
		return nil, nil, errors.New("invalid persisted Toss transaction reconciliation cursor")
	}
	return row, activeAliases, nil
}

func AdvanceTossTransactionReconciliationCursor(sourceKey string, expectedCursor, nextCursor int64) error {
	return AdvanceTossTransactionReconciliationCursorWithContext(context.Background(), sourceKey, expectedCursor, nextCursor)
}

func AdvanceTossTransactionReconciliationCursorWithContext(ctx context.Context, sourceKey string, expectedCursor, nextCursor int64) error {
	return AdvanceTossTransactionReconciliationCursorWithAliasesWithContext(ctx, sourceKey, nil, expectedCursor, nextCursor)
}

// AdvanceTossTransactionReconciliationCursorWithAliases keeps every existing
// legacy alias at least as far advanced as the MID cursor in one transaction.
// An alias that appeared behind the caller's snapshot forces a retry so no
// historical interval can be skipped during a mixed-version rollout.
func AdvanceTossTransactionReconciliationCursorWithAliases(sourceKey string, aliases []string, expectedCursor, nextCursor int64) error {
	return AdvanceTossTransactionReconciliationCursorWithAliasesWithContext(context.Background(), sourceKey, aliases, expectedCursor, nextCursor)
}

func AdvanceTossTransactionReconciliationCursorWithAliasesWithContext(ctx context.Context, sourceKey string, aliases []string, expectedCursor, nextCursor int64) error {
	return advanceTossTransactionReconciliationCursorWithAliasesWithContext(ctx, sourceKey, aliases, "", expectedCursor, nextCursor)
}

// AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext is
// the provider-worker variant. Besides the exact cursor CAS, it proves that the
// caller still owns a non-expired source lease before publishing progress.
func AdvanceTossTransactionReconciliationCursorWithAliasesLeasedWithContext(ctx context.Context, sourceKey string, aliases []string, leaseOwner string, expectedCursor, nextCursor int64) error {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 64 {
		return errors.New("invalid Toss transaction reconciliation lease owner")
	}
	return advanceTossTransactionReconciliationCursorWithAliasesWithContext(ctx, sourceKey, aliases, leaseOwner, expectedCursor, nextCursor)
}

func advanceTossTransactionReconciliationCursorWithAliasesWithContext(ctx context.Context, sourceKey string, aliases []string, leaseOwner string, expectedCursor, nextCursor int64) error {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" || expectedCursor <= 0 || nextCursor <= expectedCursor {
		return errors.New("invalid Toss transaction reconciliation cursor advance")
	}
	aliasRows := make([]TossTransactionReconciliationCursorAlias, 0, len(aliases))
	for _, alias := range aliases {
		aliasRows = append(aliasRows, TossTransactionReconciliationCursorAlias{SourceKey: alias})
	}
	normalizedAliases, err := normalizeTossTransactionCursorAliases(sourceKey, aliasRows)
	if err != nil {
		return err
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		keys := make([]string, 0, len(normalizedAliases)+1)
		keys = append(keys, sourceKey)
		for i := range normalizedAliases {
			keys = append(keys, normalizedAliases[i].SourceKey)
		}
		var rows []TossTransactionReconciliationCursor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_key IN ?", keys).
			Order("source_key ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		now := getDBTimestampTx(tx)
		targetFound := false
		for i := range rows {
			if rows[i].SourceKey == sourceKey {
				targetFound = true
				if rows[i].CursorTime != expectedCursor {
					return ErrTossTransactionCursorConflict
				}
				if leaseOwner != "" && (rows[i].LeaseOwner != leaseOwner || rows[i].LeaseUntil <= now) {
					return ErrTossTransactionLeaseLost
				}
				continue
			}
			if rows[i].CursorTime < expectedCursor {
				return ErrTossTransactionCursorConflict
			}
		}
		if !targetFound {
			return ErrTossTransactionCursorConflict
		}
		targetQuery := tx.Model(&TossTransactionReconciliationCursor{}).
			Where("source_key = ? AND cursor_time = ?", sourceKey, expectedCursor)
		if leaseOwner != "" {
			targetQuery = targetQuery.Where("lease_owner = ? AND lease_until > ?", leaseOwner, now)
		}
		result := targetQuery.
			Updates(map[string]interface{}{"cursor_time": nextCursor, "update_time": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTossTransactionCursorConflict
		}
		for i := range rows {
			if rows[i].SourceKey == sourceKey || rows[i].CursorTime >= nextCursor {
				continue
			}
			result := tx.Model(&TossTransactionReconciliationCursor{}).
				Where("source_key = ? AND cursor_time = ?", rows[i].SourceKey, rows[i].CursorTime).
				Updates(map[string]interface{}{"cursor_time": nextCursor, "update_time": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTossTransactionCursorConflict
			}
		}
		return nil
	})
}

// AdvanceTossTransactionReconciliationRecentCursorWithAliases records a fully
// completed recent-tail scan independently of the older backlog cursor. The
// caller must update this mark only after every provider window through
// nextRecentCursor succeeds. A target backlog CAS mismatch or an alias with a
// newer recent mark fails closed; the next worker will safely repeat the
// idempotent scan.
func AdvanceTossTransactionReconciliationRecentCursorWithAliases(
	sourceKey string,
	aliases []string,
	expectedCursor, expectedRecentCursor, nextRecentCursor int64,
) error {
	return AdvanceTossTransactionReconciliationRecentCursorWithAliasesWithContext(
		context.Background(), sourceKey, aliases, expectedCursor, expectedRecentCursor, nextRecentCursor,
	)
}

func AdvanceTossTransactionReconciliationRecentCursorWithAliasesWithContext(
	ctx context.Context,
	sourceKey string,
	aliases []string,
	expectedCursor, expectedRecentCursor, nextRecentCursor int64,
) error {
	return advanceTossTransactionReconciliationRecentCursorWithAliasesWithContext(
		ctx, sourceKey, aliases, "", expectedCursor, expectedRecentCursor, nextRecentCursor,
	)
}

func AdvanceTossTransactionReconciliationRecentCursorWithAliasesLeasedWithContext(
	ctx context.Context,
	sourceKey string,
	aliases []string,
	leaseOwner string,
	expectedCursor, expectedRecentCursor, nextRecentCursor int64,
) error {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 64 {
		return errors.New("invalid Toss transaction reconciliation lease owner")
	}
	return advanceTossTransactionReconciliationRecentCursorWithAliasesWithContext(
		ctx, sourceKey, aliases, leaseOwner, expectedCursor, expectedRecentCursor, nextRecentCursor,
	)
}

func advanceTossTransactionReconciliationRecentCursorWithAliasesWithContext(
	ctx context.Context,
	sourceKey string,
	aliases []string,
	leaseOwner string,
	expectedCursor, expectedRecentCursor, nextRecentCursor int64,
) error {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" || expectedCursor <= 0 || expectedRecentCursor < 0 || nextRecentCursor <= expectedRecentCursor {
		return errors.New("invalid Toss transaction reconciliation recent cursor advance")
	}
	aliasRows := make([]TossTransactionReconciliationCursorAlias, 0, len(aliases))
	for _, alias := range aliases {
		aliasRows = append(aliasRows, TossTransactionReconciliationCursorAlias{SourceKey: alias})
	}
	normalizedAliases, err := normalizeTossTransactionCursorAliases(sourceKey, aliasRows)
	if err != nil {
		return err
	}
	return dbWithContext(ctx).Transaction(func(tx *gorm.DB) error {
		keys := make([]string, 0, len(normalizedAliases)+1)
		keys = append(keys, sourceKey)
		for i := range normalizedAliases {
			keys = append(keys, normalizedAliases[i].SourceKey)
		}
		var rows []TossTransactionReconciliationCursor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("source_key IN ?", keys).
			Order("source_key ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		now := getDBTimestampTx(tx)
		targetFound := false
		for i := range rows {
			if rows[i].RecentCursorTime < 0 || rows[i].RecentCursorTime > expectedRecentCursor {
				return ErrTossTransactionCursorConflict
			}
			if rows[i].SourceKey == sourceKey {
				targetFound = true
				if rows[i].CursorTime != expectedCursor || rows[i].RecentCursorTime != expectedRecentCursor {
					return ErrTossTransactionCursorConflict
				}
				if leaseOwner != "" && (rows[i].LeaseOwner != leaseOwner || rows[i].LeaseUntil <= now) {
					return ErrTossTransactionLeaseLost
				}
			}
		}
		if !targetFound {
			return ErrTossTransactionCursorConflict
		}
		for i := range rows {
			query := tx.Model(&TossTransactionReconciliationCursor{}).
				Where("source_key = ?", rows[i].SourceKey)
			if rows[i].SourceKey == sourceKey {
				query = query.Where("cursor_time = ?", expectedCursor)
				if leaseOwner != "" {
					query = query.Where("lease_owner = ? AND lease_until > ?", leaseOwner, now)
				}
			}
			if rows[i].RecentCursorTime == 0 {
				query = query.Where("recent_cursor_time = 0 OR recent_cursor_time IS NULL")
			} else {
				query = query.Where("recent_cursor_time = ?", rows[i].RecentCursorTime)
			}
			result := query.Updates(map[string]interface{}{
				"recent_cursor_time": nextRecentCursor,
				"update_time":        now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTossTransactionCursorConflict
			}
		}
		return nil
	})
}
