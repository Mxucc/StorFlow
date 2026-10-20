package store

import (
	"context"
	"time"

	"github.com/Mxucc/StorFlow/shop/model"
)

const refundColumns = `id, entitlement_id, order_no, uid, shop_id, amount, reason, status, operator, remark,
	evidence, evidence_at, verifier, verified_at, created_at, updated_at`

// CreateRefund records a refund request. One request per order is allowed, which
// the unique key on order_no enforces.
func (s *Store) CreateRefund(ctx context.Context, r *model.Refund) error {
	now := time.Now().Unix()
	r.CreatedAt, r.UpdatedAt = now, now

	_, err := s.conn().exec(ctx, `INSERT INTO crshop_refund (`+refundColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.EntitlementID, r.OrderNo, r.UID, r.ShopID, r.Amount, r.Reason,
		string(r.Status), r.Operator, r.Remark, r.Evidence, r.EvidenceAt, r.Verifier, r.VerifiedAt,
		r.CreatedAt, r.UpdatedAt)
	return wrapErr("shop: failed to create refund", err)
}

// GetRefund returns a refund request by id.
func (s *Store) GetRefund(ctx context.Context, id string) (*model.Refund, error) {
	return scanRefund(s.conn().queryRow(ctx, `SELECT `+refundColumns+` FROM crshop_refund WHERE id = ?`, id))
}

// GetRefundByOrder returns the refund request of an order, if any.
func (s *Store) GetRefundByOrder(ctx context.Context, orderNo string) (*model.Refund, error) {
	return scanRefund(s.conn().queryRow(ctx, `SELECT `+refundColumns+` FROM crshop_refund WHERE order_no = ?`, orderNo))
}

// ListRefunds returns refund requests, optionally scoped to one shop or one user.
func (s *Store) ListRefunds(ctx context.Context, shopID string, uid int, limit, offset int) ([]*model.Refund, error) {
	query := `SELECT ` + refundColumns + ` FROM crshop_refund WHERE 1 = 1`
	args := []any{}
	if shopID != "" {
		query += ` AND shop_id = ?`
		args = append(args, shopID)
	}
	if uid > 0 {
		query += ` AND uid = ?`
		args = append(args, uid)
	}
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, capLimit(limit), max(offset, 0))

	rows, err := s.conn().query(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("shop: failed to list refunds", err)
	}
	defer rows.Close()

	res := make([]*model.Refund, 0)
	for rows.Next() {
		refund, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, refund)
	}
	return res, wrapErr("shop: failed to iterate refunds", rows.Err())
}

// ResetRefund turns a rejected request back into a pending one, so a buyer may
// ask again after a refusal.
func (s *Store) ResetRefund(ctx context.Context, id, reason string) (bool, error) {
	res, err := s.conn().exec(ctx, `UPDATE crshop_refund SET status = ?, reason = ?, remark = '', operator = 0, updated_at = ?
		WHERE id = ? AND status = ?`,
		string(model.RefundStatusPending), reason, time.Now().Unix(), id, string(model.RefundStatusRejected))
	if err != nil {
		return false, wrapErr("shop: failed to reset refund", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

// ReviewRefund moves a pending request to its final state. It reports false when
// the request was already reviewed, so two reviewers cannot settle it twice.
func (s *Store) ReviewRefund(ctx context.Context, id string, to model.RefundStatus, operator int, remark string) (bool, error) {
	res, err := s.conn().exec(ctx, `UPDATE crshop_refund SET status = ?, operator = ?, remark = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		string(to), operator, remark, time.Now().Unix(), id, string(model.RefundStatusPending))
	if err != nil {
		return false, wrapErr("shop: failed to review refund", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

func scanRefund(row rowScanner) (*model.Refund, error) {
	var (
		r        model.Refund
		reason   string
		remark   string
		evidence string
	)
	err := row.Scan(&r.ID, &r.EntitlementID, &r.OrderNo, &r.UID, &r.ShopID, &r.Amount, &reason,
		&r.Status, &r.Operator, &remark, &evidence, &r.EvidenceAt, &r.Verifier, &r.VerifiedAt,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query refund", err)
	}
	r.Reason, r.Remark, r.Evidence = reason, remark, evidence
	return &r, nil
}

// SubmitRefundEvidence records the proof of a refund the merchant carried out
// through their own payment channel and moves the request to "refunded": the
// money is with the buyer, an administrator still has to verify the proof.
func (s *Store) SubmitRefundEvidence(ctx context.Context, id, evidence string) (bool, error) {
	now := time.Now().Unix()
	res, err := s.conn().exec(ctx, `UPDATE crshop_refund SET status = ?, evidence = ?, evidence_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		string(model.RefundStatusRefunded), evidence, now, now, id, string(model.RefundStatusApproved))
	if err != nil {
		return false, wrapErr("shop: failed to submit the refund evidence", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

// ConfirmRefund closes a refund whose proof an administrator verified.
func (s *Store) ConfirmRefund(ctx context.Context, id string, operator int, remark string) (bool, error) {
	now := time.Now().Unix()
	res, err := s.conn().exec(ctx, `UPDATE crshop_refund SET status = ?, verifier = ?, verified_at = ?, remark = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		string(model.RefundStatusConfirmed), operator, now, remark, now, id, string(model.RefundStatusRefunded))
	if err != nil {
		return false, wrapErr("shop: failed to confirm the refund", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

// ReturnRefund sends a refund back to the seller: the submitted proof did not
// convince the administrator, so the seller has to document the refund again.
func (s *Store) ReturnRefund(ctx context.Context, id string, operator int, remark string) (bool, error) {
	res, err := s.conn().exec(ctx, `UPDATE crshop_refund SET status = ?, operator = ?, remark = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		string(model.RefundStatusApproved), operator, remark, time.Now().Unix(), id, string(model.RefundStatusRefunded))
	if err != nil {
		return false, wrapErr("shop: failed to return the refund", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}
