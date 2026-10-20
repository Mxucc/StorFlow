package store

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Mxucc/StorFlow/shop/model"
)

const withdrawalColumns = `id, shop_id, amount, fee, actual, account, status, remark, operator, created_at, updated_at`

// CreateWithdrawal inserts a payout request.
func (s *Store) CreateWithdrawal(ctx context.Context, w *model.Withdrawal) error {
	account, err := json.Marshal(w.Account)
	if err != nil {
		return wrapErr("shop: failed to encode withdrawal account", err)
	}

	now := time.Now().Unix()
	w.CreatedAt, w.UpdatedAt = now, now

	_, err = s.conn().exec(ctx, `INSERT INTO crshop_withdrawal (`+withdrawalColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.ShopID, w.Amount, w.Fee, w.Actual, string(account), string(w.Status),
		w.Remark, w.Operator, w.CreatedAt, w.UpdatedAt)
	return wrapErr("shop: failed to create withdrawal", err)
}

// GetWithdrawal returns a payout request by id.
func (s *Store) GetWithdrawal(ctx context.Context, id string) (*model.Withdrawal, error) {
	return scanWithdrawal(s.conn().queryRow(ctx, `SELECT `+withdrawalColumns+` FROM crshop_withdrawal WHERE id = ?`, id))
}

// UpdateWithdrawalStatus moves a payout request into a new state. It returns
// false when the request was already handled, so that reviewers cannot settle
// the same request twice.
func (s *Store) UpdateWithdrawalStatus(ctx context.Context, id string, from, to model.WithdrawalStatus, operator int, remark string) (bool, error) {
	res, err := s.conn().exec(ctx, `UPDATE crshop_withdrawal SET status = ?, operator = ?, remark = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		string(to), operator, remark, time.Now().Unix(), id, string(from))
	if err != nil {
		return false, wrapErr("shop: failed to update withdrawal", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

// ListWithdrawals returns payout requests, optionally scoped to one shop.
func (s *Store) ListWithdrawals(ctx context.Context, shopID string, limit, offset int) ([]*model.Withdrawal, error) {
	query := `SELECT ` + withdrawalColumns + ` FROM crshop_withdrawal`
	args := []any{}
	if shopID != "" {
		query += ` WHERE shop_id = ?`
		args = append(args, shopID)
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, capLimit(limit), max(offset, 0))

	rows, err := s.conn().query(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("shop: failed to list withdrawals", err)
	}
	defer rows.Close()

	res := make([]*model.Withdrawal, 0)
	for rows.Next() {
		w, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, w)
	}
	return res, wrapErr("shop: failed to iterate withdrawals", rows.Err())
}

func scanWithdrawal(row rowScanner) (*model.Withdrawal, error) {
	var (
		w       model.Withdrawal
		account string
		remark  string
	)
	err := row.Scan(&w.ID, &w.ShopID, &w.Amount, &w.Fee, &w.Actual, &account, &w.Status, &remark,
		&w.Operator, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query withdrawal", err)
	}

	w.Remark = remark
	if account != "" {
		if err := json.Unmarshal([]byte(account), &w.Account); err != nil {
			return nil, wrapErr("shop: failed to decode withdrawal account", err)
		}
	}
	return &w, nil
}

const couponColumns = `id, shop_id, code, kind, value, min_amount, product_id, max_uses, used, per_user,
	status, start_at, end_at, created_at`

// CreateCoupon inserts a discount code.
func (s *Store) CreateCoupon(ctx context.Context, c *model.Coupon) error {
	c.CreatedAt = time.Now().Unix()
	_, err := s.conn().exec(ctx, `INSERT INTO crshop_coupon (`+couponColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ShopID, c.Code, string(c.Kind), c.Value, c.MinAmount, c.ProductID, c.MaxUses,
		c.Used, c.PerUser, string(c.Status), c.StartAt, c.EndAt, c.CreatedAt)
	return wrapErr("shop: failed to create coupon", err)
}

// UpdateCoupon persists the mutable fields of a coupon.
func (s *Store) UpdateCoupon(ctx context.Context, c *model.Coupon) error {
	res, err := s.conn().exec(ctx, `UPDATE crshop_coupon SET code = ?, kind = ?, value = ?, min_amount = ?,
			product_id = ?, max_uses = ?, per_user = ?, status = ?, start_at = ?, end_at = ?
		WHERE id = ?`,
		c.Code, string(c.Kind), c.Value, c.MinAmount, c.ProductID, c.MaxUses, c.PerUser,
		string(c.Status), c.StartAt, c.EndAt, c.ID)
	if err != nil {
		return wrapErr("shop: failed to update coupon", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetCouponByCode returns an active coupon of a shop by its code.
func (s *Store) GetCouponByCode(ctx context.Context, shopID, code string) (*model.Coupon, error) {
	return scanCoupon(s.conn().queryRow(ctx, `SELECT `+couponColumns+`
		FROM crshop_coupon WHERE shop_id = ? AND code = ?`, shopID, code))
}

// GetCouponByID returns a coupon by its identifier.
func (s *Store) GetCouponByID(ctx context.Context, id string) (*model.Coupon, error) {
	return scanCoupon(s.conn().queryRow(ctx, `SELECT `+couponColumns+` FROM crshop_coupon WHERE id = ?`, id))
}

// GetCouponByID reads a coupon through the transaction handle.
func (t *Tx) GetCouponByID(ctx context.Context, id string) (*model.Coupon, error) {
	return scanCoupon(t.queryRow(ctx, `SELECT `+couponColumns+` FROM crshop_coupon WHERE id = ?`, id))
}

// ListCoupons returns the coupons of a shop.
func (s *Store) ListCoupons(ctx context.Context, shopID string, limit, offset int) ([]*model.Coupon, error) {
	rows, err := s.conn().query(ctx, `SELECT `+couponColumns+`
		FROM crshop_coupon WHERE shop_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		shopID, capLimit(limit), max(offset, 0))
	if err != nil {
		return nil, wrapErr("shop: failed to list coupons", err)
	}
	defer rows.Close()

	res := make([]*model.Coupon, 0)
	for rows.Next() {
		c, err := scanCoupon(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, c)
	}
	return res, wrapErr("shop: failed to iterate coupons", rows.Err())
}

// CountCouponUses counts how often a user already used a coupon, reading
// through the transaction handle.
func (t *Tx) CountCouponUses(ctx context.Context, couponID string, uid int) (int, error) {
	var total int
	err := t.queryRow(ctx, `SELECT COUNT(*) FROM crshop_coupon_use WHERE coupon_id = ? AND uid = ?`,
		couponID, uid).Scan(&total)
	return total, wrapErr("shop: failed to count coupon uses", err)
}

// CountCouponUses counts how often a user already used a coupon.
func (s *Store) CountCouponUses(ctx context.Context, couponID string, uid int) (int, error) {
	var total int
	err := s.conn().queryRow(ctx, `SELECT COUNT(*) FROM crshop_coupon_use WHERE coupon_id = ? AND uid = ?`,
		couponID, uid).Scan(&total)
	return total, wrapErr("shop: failed to count coupon uses", err)
}

// ReserveCoupon claims one usage of a coupon for an order. It is the checkout
// time reservation: the discount is only granted when the usage could be
// claimed, which is what stops a buyer from opening several discounted orders
// with a single-use code, or from spending a coupon that is globally exhausted.
func (t *Tx) ReserveCoupon(ctx context.Context, couponID string, uid int, orderNo string, maxUses, perUser int) error {
	if perUser > 0 {
		used, err := t.CountCouponUses(ctx, couponID, uid)
		if err != nil {
			return err
		}
		if used >= perUser {
			return ErrConflict
		}
	}

	return t.RedeemCoupon(ctx, couponID, uid, orderNo, maxUses)
}

// ReleaseCoupon returns a reservation when an order never gets paid.
func (t *Tx) ReleaseCoupon(ctx context.Context, orderNo string) error {
	var couponID string
	err := t.queryRow(ctx, `SELECT coupon_id FROM crshop_coupon_use WHERE order_no = ?`, orderNo).Scan(&couponID)
	if err != nil {
		if errors.Is(err, stdsql.ErrNoRows) || errors.Is(err, ErrNotFound) {
			// Nothing was reserved for this order.
			return nil
		}
		return wrapErr("shop: failed to load the coupon reservation", err)
	}

	if _, err := t.exec(ctx, `DELETE FROM crshop_coupon_use WHERE order_no = ?`, orderNo); err != nil {
		return wrapErr("shop: failed to release coupon usage", err)
	}

	if _, err := t.exec(ctx, `UPDATE crshop_coupon SET used = CASE WHEN used > 0 THEN used - 1 ELSE 0 END
		WHERE id = ?`, couponID); err != nil {
		return wrapErr("shop: failed to release coupon counter", err)
	}
	return nil
}

// RedeemCoupon increases the used counter and records the usage. Both writes
// happen inside one transaction so that the coupon can never be over-redeemed
// beyond its total limit.
func (t *Tx) RedeemCoupon(ctx context.Context, couponID string, uid int, orderNo string, maxUses int) error {
	query := `UPDATE crshop_coupon SET used = used + ? WHERE id = ?`
	args := []any{1, couponID}
	if maxUses > 0 {
		query += ` AND used + 1 <= ?`
		args = append(args, maxUses)
	}

	res, err := t.exec(ctx, query, args...)
	if err != nil {
		return wrapErr("shop: failed to redeem coupon", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}

	_, err = t.exec(ctx, `INSERT INTO crshop_coupon_use (id, coupon_id, uid, order_no, created_at)
		VALUES (?, ?, ?, ?, ?)`, newID(), couponID, uid, orderNo, time.Now().Unix())
	return wrapErr("shop: failed to record coupon usage", err)
}

func scanCoupon(row rowScanner) (*model.Coupon, error) {
	var c model.Coupon
	err := row.Scan(&c.ID, &c.ShopID, &c.Code, &c.Kind, &c.Value, &c.MinAmount, &c.ProductID,
		&c.MaxUses, &c.Used, &c.PerUser, &c.Status, &c.StartAt, &c.EndAt, &c.CreatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query coupon", err)
	}
	return &c, nil
}

const referralColumns = `id, inviter_uid, invitee_uid, source, object_id, rate, total_rebate, created_at, updated_at`

// CreateReferral binds an invitee to an inviter. A user can only be bound once,
// which the unique key on invitee_uid enforces.
func (s *Store) CreateReferral(ctx context.Context, r *model.Referral) error {
	now := time.Now().Unix()
	r.CreatedAt, r.UpdatedAt = now, now

	_, err := s.conn().exec(ctx, `INSERT INTO crshop_referral (`+referralColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.InviterUID, r.InviteeUID, r.Source, r.ObjectID, r.Rate, r.TotalRebate, r.CreatedAt, r.UpdatedAt)
	return wrapErr("shop: failed to create referral", err)
}

// GetReferralByInvitee returns the inviter of a user.
func (s *Store) GetReferralByInvitee(ctx context.Context, uid int) (*model.Referral, error) {
	return scanReferral(s.conn().queryRow(ctx, `SELECT `+referralColumns+` FROM crshop_referral WHERE invitee_uid = ?`, uid))
}

// AddReferralRebate accumulates the rebate paid to the inviter inside a tx.
func (t *Tx) AddReferralRebate(ctx context.Context, inviteeUID int, amount model.Money) error {
	if amount == 0 {
		return nil
	}

	res, err := t.exec(ctx, `UPDATE crshop_referral SET total_rebate = total_rebate + ?, updated_at = ?
		WHERE invitee_uid = ?`, amount, time.Now().Unix(), inviteeUID)
	if err != nil {
		return wrapErr("shop: failed to update referral rebate", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListReferrals returns the invitees of a shop owner.
func (s *Store) ListReferrals(ctx context.Context, inviterUID, limit, offset int) ([]*model.Referral, error) {
	rows, err := s.conn().query(ctx, `SELECT `+referralColumns+`
		FROM crshop_referral WHERE inviter_uid = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		inviterUID, capLimit(limit), max(offset, 0))
	if err != nil {
		return nil, wrapErr("shop: failed to list referrals", err)
	}
	defer rows.Close()

	res := make([]*model.Referral, 0)
	for rows.Next() {
		r, err := scanReferral(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, wrapErr("shop: failed to iterate referrals", rows.Err())
}

func scanReferral(row rowScanner) (*model.Referral, error) {
	var (
		r      model.Referral
		source string
		object string
	)
	err := row.Scan(&r.ID, &r.InviterUID, &r.InviteeUID, &source, &object, &r.Rate, &r.TotalRebate,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query referral", err)
	}
	r.Source, r.ObjectID = source, object
	return &r, nil
}
