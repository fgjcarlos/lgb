package historian

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/fgjcarlos/lgb/internal/config"
)

// Retention sources reported by RetentionPolicy.Effective (#85).
const (
	RetentionSourceTag    = "tag"
	RetentionSourcePLC    = "plc"
	RetentionSourceGlobal = "global"
)

// RetentionPolicy resolves how long each (PLC, tag) series is kept (#85): a
// per-tag override beats the per-PLC default, which beats DefaultDays. Values
// <= 0 inherit the next level; DefaultDays <= 0 keeps inheriting series forever.
type RetentionPolicy struct {
	DefaultDays int
	PLCDays     map[string]int            // PLC name -> default days
	TagDays     map[string]map[string]int // PLC name -> tag name -> override days
}

// NewRetentionPolicy builds the policy for the configured PLCs, keeping only
// the overrides that are set (> 0).
func NewRetentionPolicy(defaultDays int, plcs []config.PLC) RetentionPolicy {
	p := RetentionPolicy{DefaultDays: defaultDays, PLCDays: map[string]int{}, TagDays: map[string]map[string]int{}}
	for _, plc := range plcs {
		if plc.RetentionDays > 0 {
			p.PLCDays[plc.Name] = plc.RetentionDays
		}
		for _, tag := range plc.Tags {
			if tag.RetentionDays <= 0 {
				continue
			}
			if p.TagDays[plc.Name] == nil {
				p.TagDays[plc.Name] = map[string]int{}
			}
			p.TagDays[plc.Name][tag.Name] = tag.RetentionDays
		}
	}
	return p
}

// Effective returns the retention days applied to (plcName, tagName) and the
// level that set them (RetentionSourceTag, RetentionSourcePLC or
// RetentionSourceGlobal).
func (p RetentionPolicy) Effective(plcName, tagName string) (int, string) {
	if days := p.TagDays[plcName][tagName]; days > 0 {
		return days, RetentionSourceTag
	}
	if days := p.PLCDays[plcName]; days > 0 {
		return days, RetentionSourcePLC
	}
	return p.DefaultDays, RetentionSourceGlobal
}

// EnforceRetention deletes, in one transaction, every sample older than the
// effective retention of its series at now. Series without a PLC or tag
// policy (including ones no longer configured) follow DefaultDays.
//
// shortcut: each override is one bound parameter, so the global DELETE is
// limited to SQLite's 32766 variables (~16k tag overrides); batch it if a
// deployment ever gets near that.
func (s *Store) EnforceRetention(ctx context.Context, now time.Time, policy RetentionPolicy) error {
	cutoff := func(days int) int64 {
		return now.UTC().Add(-time.Duration(days) * 24 * time.Hour).UnixNano()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Sorted iteration keeps the statement order deterministic.
	var overridden [][2]string // (plc, tag) pairs with their own override
	for _, plcName := range sortedKeys(policy.TagDays) {
		for _, tagName := range sortedKeys(policy.TagDays[plcName]) {
			days := policy.TagDays[plcName][tagName]
			if days <= 0 {
				continue
			}
			overridden = append(overridden, [2]string{plcName, tagName})
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM tag_samples WHERE plc_name = ? AND tag_name = ? AND ts_unix_nano < ?`,
				plcName, tagName, cutoff(days)); err != nil {
				return err
			}
		}
	}

	var plcDefaults []string
	for _, plcName := range sortedKeys(policy.PLCDays) {
		days := policy.PLCDays[plcName]
		if days <= 0 {
			continue
		}
		plcDefaults = append(plcDefaults, plcName)
		query := `DELETE FROM tag_samples WHERE plc_name = ? AND ts_unix_nano < ?`
		args := []any{plcName, cutoff(days)}
		var own []any
		for _, pair := range overridden {
			if pair[0] == plcName {
				own = append(own, pair[1])
			}
		}
		if len(own) > 0 {
			query += ` AND tag_name NOT IN (` + placeholders(len(own)) + `)`
			args = append(args, own...)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}

	if policy.DefaultDays > 0 {
		query := `DELETE FROM tag_samples WHERE ts_unix_nano < ?`
		args := []any{cutoff(policy.DefaultDays)}
		if len(plcDefaults) > 0 {
			query += ` AND plc_name NOT IN (` + placeholders(len(plcDefaults)) + `)`
			for _, name := range plcDefaults {
				args = append(args, name)
			}
		}
		if len(overridden) > 0 {
			query += ` AND (plc_name, tag_name) NOT IN (VALUES ` + strings.TrimSuffix(strings.Repeat("(?, ?), ", len(overridden)), ", ") + `)`
			for _, pair := range overridden {
				args = append(args, pair[0], pair[1])
			}
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
