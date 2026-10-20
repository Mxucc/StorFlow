package store

import (
	"context"
	stdsql "database/sql"
	"errors"
	"time"
)

// GetSetting returns a module setting, or the empty string when unset.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.conn().queryRow(ctx, `SELECT v FROM crshop_setting WHERE k = ?`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, stdsql.ErrNoRows) || errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", wrapErr("shop: failed to query setting", err)
	}
	return value, nil
}

// AllSettings returns every module setting.
func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.conn().query(ctx, `SELECT k, v FROM crshop_setting`)
	if err != nil {
		return nil, wrapErr("shop: failed to query settings", err)
	}
	defer rows.Close()

	res := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, wrapErr("shop: failed to scan setting", err)
		}
		res[k] = v
	}
	return res, wrapErr("shop: failed to iterate settings", rows.Err())
}

// SetSetting upserts a module setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	now := time.Now().Unix()
	if _, err := s.conn().exec(ctx, `UPDATE crshop_setting SET v = ?, updated_at = ? WHERE k = ?`,
		value, now, key); err != nil {
		return wrapErr("shop: failed to update setting", err)
	}

	// UPDATE reports no error for a missing row, so upsert explicitly.
	res, err := s.conn().exec(ctx, `INSERT INTO crshop_setting (k, v, updated_at)
		SELECT ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM crshop_setting WHERE k = ?)`,
		key, value, now, key)
	if err != nil {
		return wrapErr("shop: failed to insert setting", err)
	}
	_ = res
	return nil
}
