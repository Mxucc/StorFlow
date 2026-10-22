package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ddl holds the dialect specific column type names used by the module DDL.
type ddl struct {
	// key is the type of a primary key column holding an application generated
	// identifier.
	key string
	// text is an unbounded text column.
	text string
	// blob is an unbounded binary column, used by the stored images.
	blob string
	// num is a 64 bit integer column.
	num string
	// str returns a bounded varchar column type.
	str func(n int) string
}

func (s *Store) ddl() ddl {
	switch s.dialect {
	case DialectMySQL:
		return ddl{
			key:  "VARCHAR(64)",
			text: "LONGTEXT",
			blob: "LONGBLOB",
			num:  "BIGINT",
			str:  func(n int) string { return fmt.Sprintf("VARCHAR(%d)", n) },
		}
	case DialectPostgres:
		return ddl{
			key:  "VARCHAR(64)",
			text: "TEXT",
			blob: "BYTEA",
			num:  "BIGINT",
			str:  func(n int) string { return fmt.Sprintf("VARCHAR(%d)", n) },
		}
	default:
		return ddl{
			key:  "TEXT",
			text: "TEXT",
			blob: "BLOB",
			num:  "INTEGER",
			str:  func(n int) string { return "TEXT" },
		}
	}
}

// Migrate creates every table owned by the shop module. It is idempotent and
// never touches tables owned by the host application.
func (s *Store) Migrate(ctx context.Context) error {
	t := s.ddl()
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_setting (
			k %s NOT NULL PRIMARY KEY,
			v %s NOT NULL,
			updated_at %s NOT NULL
		)`, t.str(64), t.text, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_shop (
			id %s NOT NULL PRIMARY KEY,
			uid %s NOT NULL UNIQUE,
			slug %s NOT NULL UNIQUE,
			name %s NOT NULL,
			description %s,
			avatar %s,
			cover %s,
			theme %s,
			status %s NOT NULL,
			referrer_rate %s NOT NULL,
			pay_mode %s NOT NULL,
			epay_api_url %s NOT NULL,
			epay_pid %s NOT NULL,
			epay_key %s NOT NULL,
			epay_channels %s NOT NULL,
			deposit_paid %s NOT NULL,
			deposit_paid_at %s NOT NULL,
			created_at %s NOT NULL,
			updated_at %s NOT NULL
		)`, t.key, t.num, t.str(64), t.str(100), t.text, t.str(1024), t.str(1024), t.text, t.str(16), t.num, t.str(16), t.str(1024), t.str(64), t.str(128), t.str(2048), t.num, t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_balance (
			shop_id %s NOT NULL PRIMARY KEY,
			available %s NOT NULL,
			frozen %s NOT NULL,
			pending %s NOT NULL,
			total_income %s NOT NULL,
			total_withdraw %s NOT NULL,
			updated_at %s NOT NULL
		)`, t.key, t.num, t.num, t.num, t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_ledger (
			id %s NOT NULL PRIMARY KEY,
			shop_id %s NOT NULL,
			kind %s NOT NULL,
			amount %s NOT NULL,
			balance %s NOT NULL,
			ref_id %s,
			remark %s,
			created_at %s NOT NULL
		)`, t.key, t.key, t.str(24), t.num, t.num, t.str(64), t.text, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_product (
			id %s NOT NULL PRIMARY KEY,
			shop_id %s NOT NULL,
			file_id %s NOT NULL,
			share_id %s NOT NULL,
			share_password %s,
			name %s NOT NULL,
			description %s,
			cover %s,
			price %s NOT NULL,
			discount %s NOT NULL,
			discount_end %s NOT NULL,
			status %s NOT NULL,
			sales %s NOT NULL,
			views %s NOT NULL,
			props %s,
			created_at %s NOT NULL,
			updated_at %s NOT NULL,
			UNIQUE (shop_id, file_id)
		)`, t.key, t.key, t.num, t.num, t.str(64), t.str(100), t.text, t.str(1024), t.num, t.num, t.num, t.str(16), t.num, t.num, t.text, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_order (
			order_no %s NOT NULL PRIMARY KEY,
			kind %s NOT NULL,
			shop_id %s,
			object_id %s NOT NULL,
			buyer_uid %s NOT NULL,
			title %s,
			amount %s NOT NULL,
			discount %s NOT NULL,
			payable %s NOT NULL,
			paid %s NOT NULL,
			status %s NOT NULL,
			channel %s,
			pay_mode %s,
			settle_at %s NOT NULL,
			settle_state %s,
			coupon_id %s,
			referrer_uid %s NOT NULL,
			transfer_state %s,
			trade_no %s,
			client_ip %s,
			created_at %s NOT NULL,
			updated_at %s NOT NULL,
			paid_at %s NOT NULL,
			expire_at %s NOT NULL
		)`, t.key, t.str(16), t.key, t.str(64), t.num, t.str(200), t.num, t.num, t.num, t.num, t.str(16), t.str(16), t.str(16), t.num, t.str(16), t.key, t.num, t.str(16), t.str(128), t.str(64), t.num, t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_entitlement (
			id %s NOT NULL PRIMARY KEY,
			uid %s NOT NULL,
			kind %s NOT NULL,
			object_id %s NOT NULL,
			order_no %s NOT NULL,
			shop_id %s,
			title %s,
			status %s NOT NULL,
			folder_id %s NOT NULL,
			created_at %s NOT NULL,
			updated_at %s NOT NULL,
			UNIQUE (uid, kind, object_id)
		)`, t.key, t.num, t.str(16), t.str(64), t.key, t.key, t.str(200), t.str(16), t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_withdrawal (
			id %s NOT NULL PRIMARY KEY,
			shop_id %s NOT NULL,
			amount %s NOT NULL,
			fee %s NOT NULL,
			actual %s NOT NULL,
			account %s NOT NULL,
			status %s NOT NULL,
			remark %s,
			operator %s NOT NULL,
			created_at %s NOT NULL,
			updated_at %s NOT NULL
		)`, t.key, t.key, t.num, t.num, t.num, t.text, t.str(16), t.text, t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_refund (
			id %s NOT NULL PRIMARY KEY,
			entitlement_id %s NOT NULL,
			order_no %s NOT NULL UNIQUE,
			uid %s NOT NULL,
			shop_id %s,
			amount %s NOT NULL,
			reason %s,
			status %s NOT NULL,
			operator %s NOT NULL,
			remark %s,
			evidence %s NOT NULL,
			evidence_at %s NOT NULL,
			verifier %s NOT NULL,
			verified_at %s NOT NULL,
			created_at %s NOT NULL,
			updated_at %s NOT NULL
		)`, t.key, t.key, t.key, t.num, t.key, t.num, t.text, t.str(16), t.num, t.text, t.str(512), t.num, t.num, t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_coupon (
			id %s NOT NULL PRIMARY KEY,
			shop_id %s NOT NULL,
			code %s NOT NULL,
			kind %s NOT NULL,
			value %s NOT NULL,
			min_amount %s NOT NULL,
			product_id %s,
			max_uses %s NOT NULL,
			used %s NOT NULL,
			per_user %s NOT NULL,
			status %s NOT NULL,
			start_at %s NOT NULL,
			end_at %s NOT NULL,
			created_at %s NOT NULL,
			UNIQUE (shop_id, code)
		)`, t.key, t.key, t.str(32), t.str(16), t.num, t.num, t.key, t.num, t.num, t.num, t.str(16), t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_coupon_use (
			id %s NOT NULL PRIMARY KEY,
			coupon_id %s NOT NULL,
			uid %s NOT NULL,
			order_no %s NOT NULL,
			created_at %s NOT NULL
		)`, t.key, t.key, t.num, t.key, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_referral (
			id %s NOT NULL PRIMARY KEY,
			inviter_uid %s NOT NULL,
			invitee_uid %s NOT NULL UNIQUE,
			source %s,
			object_id %s,
			rate %s NOT NULL,
			total_rebate %s NOT NULL,
			created_at %s NOT NULL,
			updated_at %s NOT NULL
		)`, t.key, t.num, t.num, t.str(64), t.str(64), t.num, t.num, t.num, t.num),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS crshop_asset (
			id %s NOT NULL PRIMARY KEY,
			uid %s NOT NULL,
			name %s,
			mime %s NOT NULL,
			size %s NOT NULL,
			data %s NOT NULL,
			created_at %s NOT NULL
		)`, t.key, t.num, t.str(255), t.str(128), t.num, t.blob, t.num),
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("shop: failed to create schema: %w", err)
		}
	}

	// Secondary indexes are best effort: every dialect has a different (and
	// sometimes missing) IF NOT EXISTS support for CREATE INDEX.
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_crshop_product_shop ON crshop_product (shop_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_order_buyer ON crshop_order (buyer_uid, status)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_order_shop ON crshop_order (shop_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_asset_uid ON crshop_asset (uid, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_order_created ON crshop_order (created_at)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_entitlement_uid ON crshop_entitlement (uid, kind)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_ledger_shop ON crshop_ledger (shop_id, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_withdrawal_shop ON crshop_withdrawal (shop_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_coupon_use ON crshop_coupon_use (coupon_id, uid)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_refund_shop ON crshop_refund (shop_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_refund_uid ON crshop_refund (uid, status)",
		"CREATE INDEX IF NOT EXISTS idx_crshop_order_settle ON crshop_order (settle_state, settle_at)",
	}
	for _, stmt := range indexes {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !isIndexExists(err) {
			s.l.Warning("shop: failed to create index: %s", err)
		}
	}

	// Columns added after the first release: CREATE TABLE IF NOT EXISTS does not
	// touch an existing table, so every later column has to be added explicitly.
	// The statement is expected to fail when the column is already there.
	added := []struct{ table, column, ddl string }{
		{"crshop_shop", "pay_mode", t.str(16) + " NOT NULL DEFAULT ''"},
		{"crshop_shop", "epay_api_url", t.str(1024) + " NOT NULL DEFAULT ''"},
		{"crshop_shop", "epay_pid", t.str(64) + " NOT NULL DEFAULT ''"},
		{"crshop_shop", "epay_key", t.str(128) + " NOT NULL DEFAULT ''"},
		// 逐通道的易支付配置（JSON）；为空表示所有通道共用上面的 epay_api_url/pid/key。
		{"crshop_shop", "epay_channels", t.str(2048) + " NOT NULL DEFAULT ''"},
		{"crshop_shop", "deposit_paid", t.num + " NOT NULL DEFAULT 0"},
		{"crshop_shop", "deposit_paid_at", t.num + " NOT NULL DEFAULT 0"},
		{"crshop_balance", "pending", t.num + " NOT NULL DEFAULT 0"},
		{"crshop_order", "pay_mode", t.str(16) + " NOT NULL DEFAULT ''"},
		{"crshop_order", "settle_at", t.num + " NOT NULL DEFAULT 0"},
		{"crshop_order", "settle_state", t.str(16) + " NOT NULL DEFAULT ''"},
		// MySQL refuses a DEFAULT on a LONGTEXT column, so the proof is stored as
		// a bounded string: the service validates its length anyway.
		{"crshop_refund", "evidence", t.str(512) + " NOT NULL DEFAULT ''"},
		{"crshop_refund", "evidence_at", t.num + " NOT NULL DEFAULT 0"},
		{"crshop_refund", "verifier", t.num + " NOT NULL DEFAULT 0"},
		{"crshop_refund", "verified_at", t.num + " NOT NULL DEFAULT 0"},
	}
	for _, col := range added {
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", col.table, col.column, col.ddl)
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !isColumnExists(err) {
			return fmt.Errorf("shop: failed to add column %s.%s: %w", col.table, col.column, err)
		}
	}

	return nil
}

// isIndexExists reports whether err says the index already exists. MySQL has no
// IF NOT EXISTS for CREATE INDEX, so the statement is expected to fail there.
func isIndexExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"already exists", "duplicate key name", "exists"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// isColumnExists reports whether err says the column is already there, which is
// the expected outcome of a repeated ALTER TABLE ... ADD COLUMN.
func isColumnExists(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"duplicate column", "already exists", "duplicate_column"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// ErrInsufficientBalance is returned when a balance mutation would go negative.
var ErrInsufficientBalance = errors.New("shop: insufficient balance")
