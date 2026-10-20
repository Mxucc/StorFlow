package store

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Mxucc/StorFlow/shop/model"
)

const shopColumns = `id, uid, slug, name, description, avatar, cover, theme, status, referrer_rate,
	pay_mode, epay_api_url, epay_pid, epay_key, deposit_paid, deposit_paid_at, created_at, updated_at`

// CreateShop inserts a shop together with its (empty) balance account.
func (s *Store) CreateShop(ctx context.Context, shop *model.Shop) error {
	theme, err := json.Marshal(shop.Theme)
	if err != nil {
		return fmt.Errorf("shop: failed to encode theme: %w", err)
	}

	now := time.Now().Unix()
	shop.CreatedAt, shop.UpdatedAt = now, now

	return s.Tx(ctx, func(tx *Tx) error {
		_, err := tx.exec(ctx, `INSERT INTO crshop_shop (`+shopColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			shop.ID, shop.UID, shop.Slug, shop.Name, shop.Description, shop.Avatar, shop.Cover,
			string(theme), string(shop.Status), shop.ReferrerRate, string(shop.PayMode.OrDefault()),
			shop.EpayAPIURL, shop.EpayPID, shop.EpayKey, shop.DepositPaid, shop.DepositPaidAt,
			shop.CreatedAt, shop.UpdatedAt)
		if err != nil {
			return wrapErr("shop: failed to create shop", err)
		}

		_, err = tx.exec(ctx, `INSERT INTO crshop_balance (shop_id, available, frozen, pending, total_income, total_withdraw, updated_at)
			VALUES (?, 0, 0, 0, 0, 0, ?)`, shop.ID, now)
		if err != nil {
			return wrapErr("shop: failed to create balance", err)
		}
		return nil
	})
}

// GetShopByUID returns the shop owned by a site user.
func (s *Store) GetShopByUID(ctx context.Context, uid int) (*model.Shop, error) {
	return scanShop(s.conn().queryRow(ctx, `SELECT `+shopColumns+` FROM crshop_shop WHERE uid = ?`, uid))
}

// GetShopBySlug returns a shop by its public slug.
func (s *Store) GetShopBySlug(ctx context.Context, slug string) (*model.Shop, error) {
	return scanShop(s.conn().queryRow(ctx, `SELECT `+shopColumns+` FROM crshop_shop WHERE slug = ?`, slug))
}

// GetShopByID returns a shop by its identifier.
func (s *Store) GetShopByID(ctx context.Context, id string) (*model.Shop, error) {
	return scanShop(s.conn().queryRow(ctx, `SELECT `+shopColumns+` FROM crshop_shop WHERE id = ?`, id))
}

// UpdateShop persists the mutable fields of a shop.
func (s *Store) UpdateShop(ctx context.Context, shop *model.Shop) error {
	theme, err := json.Marshal(shop.Theme)
	if err != nil {
		return fmt.Errorf("shop: failed to encode theme: %w", err)
	}

	shop.UpdatedAt = time.Now().Unix()
	res, err := s.conn().exec(ctx, `UPDATE crshop_shop SET
			slug = ?, name = ?, description = ?, avatar = ?, cover = ?, theme = ?, status = ?, referrer_rate = ?,
			pay_mode = ?, epay_api_url = ?, epay_pid = ?, epay_key = ?, updated_at = ?
		WHERE id = ?`,
		shop.Slug, shop.Name, shop.Description, shop.Avatar, shop.Cover, string(theme),
		string(shop.Status), shop.ReferrerRate, string(shop.PayMode.OrDefault()),
		shop.EpayAPIURL, shop.EpayPID, shop.EpayKey, shop.UpdatedAt, shop.ID)
	if err != nil {
		return wrapErr("shop: failed to update shop", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountShopsByTimeRange counts shops created within the range.
func (s *Store) CountShopsByTimeRange(ctx context.Context, start, end int64) (int, error) {
	var total int
	err := s.conn().queryRow(ctx,
		`SELECT COUNT(*) FROM crshop_shop WHERE created_at >= ? AND created_at < ?`, start, end).Scan(&total)
	if err != nil {
		return 0, wrapErr("shop: failed to count shops", err)
	}
	return total, nil
}

func scanShop(row rowScanner) (*model.Shop, error) {
	var (
		shop model.Shop
		// The text columns are nullable in the schema: a row written before a
		// column existed (or by another tool) must not make the shop unreadable.
		description stdsql.NullString
		avatar      stdsql.NullString
		cover       stdsql.NullString
		theme       stdsql.NullString
	)
	err := row.Scan(&shop.ID, &shop.UID, &shop.Slug, &shop.Name, &description, &avatar, &cover,
		&theme, &shop.Status, &shop.ReferrerRate, &shop.PayMode, &shop.EpayAPIURL, &shop.EpayPID, &shop.EpayKey,
		&shop.DepositPaid, &shop.DepositPaidAt, &shop.CreatedAt, &shop.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query shop", err)
	}

	shop.Description, shop.Avatar, shop.Cover = description.String, avatar.String, cover.String
	if theme.String != "" {
		if err := json.Unmarshal([]byte(theme.String), &shop.Theme); err != nil {
			return nil, fmt.Errorf("shop: failed to decode theme of shop %s: %w", shop.ID, err)
		}
	}
	return &shop, nil
}

// rowScanner is implemented by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// GetShopByID returns a shop by its identifier, reading through the
// transaction handle. Inside a transaction the module must never touch the
// connection pool again: with a single connection (SQLite) that would deadlock.
func (t *Tx) GetShopByID(ctx context.Context, id string) (*model.Shop, error) {
	return scanShop(t.queryRow(ctx, `SELECT `+shopColumns+` FROM crshop_shop WHERE id = ?`, id))
}

// GetShopByUID returns the shop of a user through the transaction handle.
func (t *Tx) GetShopByUID(ctx context.Context, uid int) (*model.Shop, error) {
	return scanShop(t.queryRow(ctx, `SELECT `+shopColumns+` FROM crshop_shop WHERE uid = ?`, uid))
}

// GetBalance returns the balance account of a shop. A missing account is
// created on the fly so that a shop can never be left without a wallet.
func (s *Store) GetBalance(ctx context.Context, shopID string) (*model.Balance, error) {
	balance, err := s.scanBalance(ctx, s.conn(), shopID)
	if err == nil {
		return balance, nil
	}
	if err != ErrNotFound {
		return nil, err
	}

	if _, err := s.conn().exec(ctx, `INSERT INTO crshop_balance (shop_id, available, frozen, pending, total_income, total_withdraw, updated_at)
		VALUES (?, 0, 0, 0, 0, 0, ?)`, shopID, time.Now().Unix()); err != nil && !isUniqueViolation(err) {
		return nil, wrapErr("shop: failed to create balance", err)
	}
	return s.scanBalance(ctx, s.conn(), shopID)
}

func (s *Store) scanBalance(ctx context.Context, c *conn, shopID string) (*model.Balance, error) {
	var b model.Balance
	err := c.queryRow(ctx, `SELECT shop_id, available, frozen, pending, total_income, total_withdraw, updated_at
		FROM crshop_balance WHERE shop_id = ?`, shopID).
		Scan(&b.ShopID, &b.Available, &b.Frozen, &b.Pending, &b.TotalIncome, &b.TotalWithdraw, &b.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query balance", err)
	}
	return &b, nil
}

// CreditShop adds income to a shop wallet and appends a ledger entry in the
// same transaction.
func (s *Store) CreditShop(ctx context.Context, shopID string, amount model.Money, kind model.LedgerKind, refID, remark string) (*model.Balance, error) {
	var res *model.Balance
	err := s.Tx(ctx, func(tx *Tx) error {
		var err error
		res, err = tx.CreditShop(ctx, shopID, amount, kind, refID, remark)
		return err
	})
	return res, err
}

// CreditShop adds amount to the available balance inside an existing tx.
func (t *Tx) CreditShop(ctx context.Context, shopID string, amount model.Money, kind model.LedgerKind, refID, remark string) (*model.Balance, error) {
	if amount == 0 {
		return nil, fmt.Errorf("shop: refusing to record a zero amount")
	}

	if _, err := t.exec(ctx, `UPDATE crshop_balance SET available = available + ?, total_income = total_income + ?, updated_at = ?
		WHERE shop_id = ?`, amount, maxMoney(amount, 0), time.Now().Unix(), shopID); err != nil {
		return nil, wrapErr("shop: failed to credit balance", err)
	}

	balance, err := t.getBalanceForUpdate(ctx, shopID)
	if err != nil {
		return nil, err
	}

	if err := t.appendLedger(ctx, shopID, kind, amount, balance.Available, refID, remark); err != nil {
		return nil, err
	}
	return balance, nil
}

// DebitShop takes money back from a shop wallet (a refund of an order it was
// paid for). The balance is allowed to go negative when the seller already
// withdrew the money: that debt is what the marketplace has to collect, and
// hiding it would make the ledger lie.
func (t *Tx) DebitShop(ctx context.Context, shopID string, amount model.Money, kind model.LedgerKind, refID, remark string) (*model.Balance, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("shop: refusing to debit a non positive amount")
	}

	if _, err := t.exec(ctx, `UPDATE crshop_balance SET available = available - ?, total_income = total_income - ?, updated_at = ?
		WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
		return nil, wrapErr("shop: failed to debit balance", err)
	}

	balance, err := t.getBalanceForUpdate(ctx, shopID)
	if err != nil {
		return nil, err
	}

	if err := t.appendLedger(ctx, shopID, kind, -amount, balance.Available, refID, remark); err != nil {
		return nil, err
	}
	return balance, nil
}

// LedgerTotal returns the sum of every ledger entry of a shop.
func (s *Store) LedgerTotal(ctx context.Context, shopID string) (model.Money, error) {
	var total model.Money
	err := s.conn().queryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM crshop_ledger WHERE shop_id = ?`, shopID).
		Scan(&total)
	if err != nil {
		return 0, wrapErr("shop: failed to sum the ledger", err)
	}
	return total, nil
}

// ListShops returns every shop, newest first, for the admin panel.
func (s *Store) ListShops(ctx context.Context, limit, offset int) ([]*model.Shop, error) {
	rows, err := s.conn().query(ctx, `SELECT `+shopColumns+` FROM crshop_shop ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		capLimit(limit), max(offset, 0))
	if err != nil {
		return nil, wrapErr("shop: failed to list shops", err)
	}
	defer rows.Close()

	res := make([]*model.Shop, 0)
	for rows.Next() {
		shop, err := scanShop(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, shop)
	}
	return res, wrapErr("shop: failed to iterate shops", rows.Err())
}

// FreezeForWithdraw moves money from available to frozen.
func (s *Store) FreezeForWithdraw(ctx context.Context, shopID string, amount model.Money, refID, remark string) (*model.Balance, error) {
	var res *model.Balance
	err := s.Tx(ctx, func(tx *Tx) error {
		balance, err := tx.getBalanceForUpdate(ctx, shopID)
		if err != nil {
			return err
		}
		if balance.Available < amount {
			return ErrInsufficientBalance
		}

		if _, err := tx.exec(ctx, `UPDATE crshop_balance SET available = available - ?, frozen = frozen + ?, updated_at = ?
			WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
			return wrapErr("shop: failed to freeze balance", err)
		}

		balance.Available -= amount
		balance.Frozen += amount
		if err := tx.appendLedger(ctx, shopID, model.LedgerKindWithdrawFreeze, 0, balance.Available, refID, remark); err != nil {
			return err
		}
		res = balance
		return nil
	})
	return res, err
}

// ReleaseFrozen returns frozen money to the available balance (withdrawal
// rejected).
func (s *Store) ReleaseFrozen(ctx context.Context, shopID string, amount model.Money, refID, remark string) (*model.Balance, error) {
	var res *model.Balance
	err := s.Tx(ctx, func(tx *Tx) error {
		balance, err := tx.getBalanceForUpdate(ctx, shopID)
		if err != nil {
			return err
		}
		if balance.Frozen < amount {
			return ErrInsufficientBalance
		}

		if _, err := tx.exec(ctx, `UPDATE crshop_balance SET available = available + ?, frozen = frozen - ?, updated_at = ?
			WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
			return wrapErr("shop: failed to release balance", err)
		}

		balance.Available += amount
		balance.Frozen -= amount
		if err := tx.appendLedger(ctx, shopID, model.LedgerKindWithdrawRefund, 0, balance.Available, refID, remark); err != nil {
			return err
		}
		res = balance
		return nil
	})
	return res, err
}

// SettleFrozen removes frozen money from the wallet (withdrawal paid).
func (s *Store) SettleFrozen(ctx context.Context, shopID string, amount model.Money, refID, remark string) (*model.Balance, error) {
	var res *model.Balance
	err := s.Tx(ctx, func(tx *Tx) error {
		balance, err := tx.getBalanceForUpdate(ctx, shopID)
		if err != nil {
			return err
		}
		if balance.Frozen < amount {
			return ErrInsufficientBalance
		}

		if _, err := tx.exec(ctx, `UPDATE crshop_balance SET frozen = frozen - ?, total_withdraw = total_withdraw + ?, updated_at = ?
			WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
			return wrapErr("shop: failed to settle balance", err)
		}

		balance.Frozen -= amount
		balance.TotalWithdraw += amount
		if err := tx.appendLedger(ctx, shopID, model.LedgerKindWithdrawSettle, -amount, balance.Available, refID, remark); err != nil {
			return err
		}
		res = balance
		return nil
	})
	return res, err
}

// CreditShopPending records income the platform holds: the money is credited to
// the pending bucket of the wallet and only released once the unfreeze window of
// the order has passed. It is the counterpart of CreditShop for orders paid
// through the platform gateway.
func (t *Tx) CreditShopPending(ctx context.Context, shopID string, amount model.Money, kind model.LedgerKind, refID, remark string) (*model.Balance, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("shop: refusing to hold a non positive amount")
	}

	if _, err := t.exec(ctx, `UPDATE crshop_balance SET pending = pending + ?, total_income = total_income + ?, updated_at = ?
		WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
		return nil, wrapErr("shop: failed to credit the pending balance", err)
	}

	balance, err := t.getBalanceForUpdate(ctx, shopID)
	if err != nil {
		return nil, err
	}

	if err := t.appendLedger(ctx, shopID, kind, amount, balance.Available, refID, remark); err != nil {
		return nil, err
	}
	return balance, nil
}

// ReleasePending moves held income into the available balance. The ledger entry
// carries a zero amount on purpose: the money only changes bucket, so the total
// of the ledger (available + frozen + pending) stays the same.
func (t *Tx) ReleasePending(ctx context.Context, shopID string, amount model.Money, refID, remark string) (*model.Balance, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("shop: refusing to release a non positive amount")
	}

	balance, err := t.getBalanceForUpdate(ctx, shopID)
	if err != nil {
		return nil, err
	}
	if balance.Pending < amount {
		return nil, ErrInsufficientBalance
	}

	if _, err := t.exec(ctx, `UPDATE crshop_balance SET pending = pending - ?, available = available + ?, updated_at = ?
		WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
		return nil, wrapErr("shop: failed to release the pending balance", err)
	}

	balance.Pending -= amount
	balance.Available += amount
	if err := t.appendLedger(ctx, shopID, model.LedgerKindSaleRelease, 0, balance.Available, refID, remark); err != nil {
		return nil, err
	}
	return balance, nil
}

// DebitPending takes held income back, for a refund of an order whose income was
// never released to the seller.
func (t *Tx) DebitPending(ctx context.Context, shopID string, amount model.Money, kind model.LedgerKind, refID, remark string) (*model.Balance, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("shop: refusing to debit a non positive amount")
	}

	balance, err := t.getBalanceForUpdate(ctx, shopID)
	if err != nil {
		return nil, err
	}
	if balance.Pending < amount {
		return nil, ErrInsufficientBalance
	}

	if _, err := t.exec(ctx, `UPDATE crshop_balance SET pending = pending - ?, total_income = total_income - ?, updated_at = ?
		WHERE shop_id = ?`, amount, amount, time.Now().Unix(), shopID); err != nil {
		return nil, wrapErr("shop: failed to debit the pending balance", err)
	}

	balance.Pending -= amount
	balance.TotalIncome -= amount
	if err := t.appendLedger(ctx, shopID, kind, -amount, balance.Available, refID, remark); err != nil {
		return nil, err
	}
	return balance, nil
}

// LedgerAmountForRef sums the ledger entries of one kind for one order inside an
// existing transaction.
func (t *Tx) LedgerAmountForRef(ctx context.Context, shopID, refID string, kind model.LedgerKind) (model.Money, error) {
	var total model.Money
	err := t.queryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM crshop_ledger
		WHERE shop_id = ? AND ref_id = ? AND kind = ?`, shopID, refID, string(kind)).Scan(&total)
	if err != nil {
		return 0, wrapErr("shop: failed to sum the ledger of a reference", err)
	}
	return total, nil
}

func (t *Tx) getBalanceForUpdate(ctx context.Context, shopID string) (*model.Balance, error) {
	var b model.Balance
	err := t.queryRow(ctx, `SELECT shop_id, available, frozen, pending, total_income, total_withdraw, updated_at
		FROM crshop_balance WHERE shop_id = ?`, shopID).
		Scan(&b.ShopID, &b.Available, &b.Frozen, &b.Pending, &b.TotalIncome, &b.TotalWithdraw, &b.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query balance", err)
	}
	return &b, nil
}

func (t *Tx) appendLedger(ctx context.Context, shopID string, kind model.LedgerKind, amount, balance model.Money, refID, remark string) error {
	_, err := t.exec(ctx, `INSERT INTO crshop_ledger (id, shop_id, kind, amount, balance, ref_id, remark, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		newID(), shopID, string(kind), amount, balance, refID, remark, time.Now().Unix())
	if err != nil {
		return wrapErr("shop: failed to append ledger", err)
	}
	return nil
}

// ListLedger returns the most recent ledger entries of a shop.
func (s *Store) ListLedger(ctx context.Context, shopID string, limit, offset int) ([]*model.Ledger, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.conn().query(ctx, `SELECT id, shop_id, kind, amount, balance, ref_id, remark, created_at
		FROM crshop_ledger WHERE shop_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, shopID, limit, offset)
	if err != nil {
		return nil, wrapErr("shop: failed to list ledger", err)
	}
	defer rows.Close()

	res := make([]*model.Ledger, 0)
	for rows.Next() {
		var (
			l     model.Ledger
			refID string
			note  string
		)
		if err := rows.Scan(&l.ID, &l.ShopID, &l.Kind, &l.Amount, &l.Balance, &refID, &note, &l.CreatedAt); err != nil {
			return nil, wrapErr("shop: failed to scan ledger", err)
		}
		l.RefID, l.Remark = refID, note
		res = append(res, &l)
	}
	return res, wrapErr("shop: failed to iterate ledger", rows.Err())
}

func maxMoney(a, b model.Money) model.Money {
	if a > b {
		return a
	}
	return b
}

// MarkDepositPaid records a deposit payment (保证金) of a shop. The deposit is
// held by the platform and is deliberately kept out of the wallet: it is not
// withdrawable, so it never enters the ledger.
func (s *Store) MarkDepositPaid(ctx context.Context, shopID string, amount model.Money) error {
	if amount <= 0 {
		return fmt.Errorf("shop: refusing to record a non positive deposit")
	}

	now := time.Now().Unix()
	res, err := s.conn().exec(ctx, `UPDATE crshop_shop SET deposit_paid = deposit_paid + ?, deposit_paid_at = ?, updated_at = ?
		WHERE id = ?`, amount, now, now, shopID)
	if err != nil {
		return wrapErr("shop: failed to record the deposit", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RefundDeposit takes a refunded deposit off the shop, so the shop owes it again.
// The money never entered the wallet, so only the deposit counter moves.
func (s *Store) RefundDeposit(ctx context.Context, shopID string, amount model.Money) error {
	if amount <= 0 {
		return fmt.Errorf("shop: refusing to refund a non positive deposit")
	}

	res, err := s.conn().exec(ctx, `UPDATE crshop_shop
		SET deposit_paid = CASE WHEN deposit_paid >= ? THEN deposit_paid - ? ELSE 0 END, updated_at = ?
		WHERE id = ?`, amount, amount, time.Now().Unix(), shopID)
	if err != nil {
		return wrapErr("shop: failed to refund the deposit", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LedgerAmountForRef sums the ledger entries of one kind for one reference (an
// order number). It is how the module recovers the exact amount that is held for
// an order, instead of recomputing it with a fee rate that may have changed.
func (s *Store) LedgerAmountForRef(ctx context.Context, shopID, refID string, kind model.LedgerKind) (model.Money, error) {
	var total model.Money
	err := s.conn().queryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM crshop_ledger
		WHERE shop_id = ? AND ref_id = ? AND kind = ?`, shopID, refID, string(kind)).Scan(&total)
	if err != nil {
		return 0, wrapErr("shop: failed to sum the ledger of a reference", err)
	}
	return total, nil
}
