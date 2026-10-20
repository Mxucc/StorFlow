package store

import (
	"context"
	"time"

	"github.com/Mxucc/StorFlow/shop/model"
)

const orderColumns = `order_no, kind, shop_id, object_id, buyer_uid, title, amount, discount, payable, paid,
	status, channel, pay_mode, settle_at, settle_state, coupon_id, referrer_uid, transfer_state, trade_no, client_ip,
	created_at, updated_at, paid_at, expire_at`

// CreateOrder inserts a pending order.
func (s *Store) CreateOrder(ctx context.Context, o *model.Order) error {
	return s.conn().createOrder(ctx, o)
}

// CreateOrder inserts an order through the transaction handle.
func (t *Tx) CreateOrder(ctx context.Context, o *model.Order) error {
	return t.conn.createOrder(ctx, o)
}

func (c *conn) createOrder(ctx context.Context, o *model.Order) error {
	now := time.Now().Unix()
	o.CreatedAt, o.UpdatedAt = now, now

	_, err := c.exec(ctx, `INSERT INTO crshop_order (`+orderColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.OrderNo, string(o.Kind), o.ShopID, o.ObjectID, o.BuyerUID, o.Title, o.Amount, o.Discount,
		o.Payable, o.Paid, string(o.Status), string(o.Channel), string(o.PayMode.OrDefault()), o.SettleAt,
		o.SettleState, o.CouponID, o.ReferrerUID, o.TransferState, o.TradeNo, o.ClientIP,
		o.CreatedAt, o.UpdatedAt, o.PaidAt, o.ExpireAt)
	return wrapErr("shop: failed to create order", err)
}

// GetOrder returns an order by its order number.
func (s *Store) GetOrder(ctx context.Context, orderNo string) (*model.Order, error) {
	return scanOrder(s.conn().queryRow(ctx, `SELECT `+orderColumns+` FROM crshop_order WHERE order_no = ?`, orderNo))
}

// GetPendingOrder returns the pending order of a buyer for one object, if any.
// It is used to avoid creating duplicate unpaid orders.
func (s *Store) GetPendingOrder(ctx context.Context, buyerUID int, kind model.OrderKind, objectID string) (*model.Order, error) {
	return scanOrder(s.conn().queryRow(ctx, `SELECT `+orderColumns+`
		FROM crshop_order WHERE buyer_uid = ? AND kind = ? AND object_id = ? AND status = ?
		ORDER BY created_at DESC LIMIT 1`,
		buyerUID, string(kind), objectID, string(model.OrderStatusPending)))
}

// MarkOrderPaid flips an open order to paid. It returns false when the order was
// already settled, which makes the epay notification idempotent.
//
// A closed order is accepted on purpose: if the buyer paid after the order
// expired (gateways do not always respect the merchant deadline), refusing the
// money would leave the buyer charged and empty handed.
// MarkOrderPaid flips an open order to paid. It returns false when the order was
// already settled, which makes the epay notification idempotent.
//
// A closed order is accepted on purpose: if the buyer paid after the order
// expired (gateways do not always respect the merchant deadline), refusing the
// money would leave the buyer charged and empty handed.
//
// settleAt and settleState describe whether the platform holds the income of the
// order back for a while; both are zero/empty when nothing is held.
func (t *Tx) MarkOrderPaid(ctx context.Context, orderNo, tradeNo string, paid model.Money, settleAt int64, settleState string) (bool, error) {
	now := time.Now().Unix()
	res, err := t.exec(ctx, `UPDATE crshop_order SET status = ?, paid = ?, trade_no = ?, paid_at = ?,
			settle_at = ?, settle_state = ?, updated_at = ?
		WHERE order_no = ? AND status IN (?, ?)`,
		string(model.OrderStatusPaid), paid, tradeNo, now, settleAt, settleState, now, orderNo,
		string(model.OrderStatusPending), string(model.OrderStatusClosed))
	if err != nil {
		return false, wrapErr("shop: failed to mark order paid", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

// MarkOrderRefunded flips a paid order to refunded. It reports false when the
// order is not in the paid state, so a refund cannot be applied twice.
func (t *Tx) MarkOrderRefunded(ctx context.Context, orderNo string) (bool, error) {
	now := time.Now().Unix()
	res, err := t.exec(ctx, `UPDATE crshop_order SET status = ?, updated_at = ? WHERE order_no = ? AND status = ?`,
		string(model.OrderStatusRefunded), now, orderNo, string(model.OrderStatusPaid))
	if err != nil {
		return false, wrapErr("shop: failed to mark order refunded", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}

// SetOrderChannel records the payment channel chosen by the buyer.
func (s *Store) SetOrderChannel(ctx context.Context, orderNo string, channel model.PayChannel) error {
	_, err := s.conn().exec(ctx, `UPDATE crshop_order SET channel = ?, updated_at = ? WHERE order_no = ?`,
		string(channel), time.Now().Unix(), orderNo)
	return wrapErr("shop: failed to set order channel", err)
}

// SetOrderTransferState records the delivery state of a paid order.
func (s *Store) SetOrderTransferState(ctx context.Context, orderNo, state string) error {
	_, err := s.conn().exec(ctx, `UPDATE crshop_order SET transfer_state = ?, updated_at = ? WHERE order_no = ?`,
		state, time.Now().Unix(), orderNo)
	return wrapErr("shop: failed to set transfer state", err)
}

// CloseExpiredOrders marks stale pending orders as closed and returns how many
// rows were updated.
func (s *Store) CloseExpiredOrders(ctx context.Context, now int64, limit int) (int, error) {
	rows, err := s.conn().query(ctx, `SELECT order_no FROM crshop_order
		WHERE status = ? AND expire_at > 0 AND expire_at < ? ORDER BY created_at LIMIT ?`,
		string(model.OrderStatusPending), now, capLimit(limit))
	if err != nil {
		return 0, wrapErr("shop: failed to query expired orders", err)
	}

	var orderNos []string
	for rows.Next() {
		var orderNo string
		if err := rows.Scan(&orderNo); err != nil {
			rows.Close()
			return 0, wrapErr("shop: failed to scan expired order", err)
		}
		orderNos = append(orderNos, orderNo)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, wrapErr("shop: failed to iterate expired orders", err)
	}

	closed := 0
	for _, orderNo := range orderNos {
		var didClose bool
		err := s.Tx(ctx, func(tx *Tx) error {
			res, err := tx.exec(ctx, `UPDATE crshop_order SET status = ?, updated_at = ?
				WHERE order_no = ? AND status = ?`,
				string(model.OrderStatusClosed), now, orderNo, string(model.OrderStatusPending))
			if err != nil {
				return wrapErr("shop: failed to close expired order", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return wrapErr("shop: failed to read affected rows", err)
			}
			if n == 0 {
				return nil
			}

			// Give the reserved coupon back, the buyer never paid for it.
			didClose = true
			return tx.ReleaseCoupon(ctx, orderNo)
		})
		if err != nil {
			return closed, err
		}
		if didClose {
			closed++
		}
	}
	return closed, nil
}

// ListPendingOrdersForReconcile returns pending orders that are old enough to be
// worth querying at the gateway but not expired yet. It is used to settle
// payments whose asynchronous notification was lost.
func (s *Store) ListPendingOrdersForReconcile(ctx context.Context, olderThan, now int64, limit int) ([]*model.Order, error) {
	rows, err := s.conn().query(ctx, `SELECT `+orderColumns+` FROM crshop_order
		WHERE status = ? AND created_at < ? AND (expire_at = 0 OR expire_at > ?)
		ORDER BY created_at LIMIT ?`,
		string(model.OrderStatusPending), olderThan, now, capLimit(limit))
	if err != nil {
		return nil, wrapErr("shop: failed to list pending orders", err)
	}
	defer rows.Close()

	res := make([]*model.Order, 0)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, order)
	}
	return res, wrapErr("shop: failed to iterate pending orders", rows.Err())
}

// ListOrdersByBuyer returns the orders of a buyer, newest first.
func (s *Store) ListOrdersByBuyer(ctx context.Context, uid, limit, offset int) ([]*model.Order, error) {
	return s.listOrders(ctx, `buyer_uid = ?`, []any{uid}, limit, offset)
}

// ListOrdersByShop returns the orders received by a shop.
func (s *Store) ListOrdersByShop(ctx context.Context, shopID string, limit, offset int) ([]*model.Order, error) {
	return s.listOrders(ctx, `shop_id = ?`, []any{shopID}, limit, offset)
}

func (s *Store) listOrders(ctx context.Context, where string, args []any, limit, offset int) ([]*model.Order, error) {
	query := `SELECT ` + orderColumns + ` FROM crshop_order WHERE ` + where +
		` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, capLimit(limit), max(offset, 0))

	rows, err := s.conn().query(ctx, query, args...)
	if err != nil {
		return nil, wrapErr("shop: failed to list orders", err)
	}
	defer rows.Close()

	res := make([]*model.Order, 0)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, o)
	}
	return res, wrapErr("shop: failed to iterate orders", rows.Err())
}

// GetEntitlement reads an entitlement through the transaction handle.
func (t *Tx) GetEntitlement(ctx context.Context, uid int, kind model.OrderKind, objectID string) (*model.Entitlement, error) {
	return scanEntitlement(t.queryRow(ctx, `SELECT `+entitlementColumns+`
		FROM crshop_entitlement WHERE uid = ? AND kind = ? AND object_id = ?`,
		uid, string(kind), objectID))
}

// SetEntitlementStatus updates only the status column of an entitlement.
func (t *Tx) SetEntitlementStatus(ctx context.Context, id string, status model.EntitlementStatus) error {
	res, err := t.exec(ctx, `UPDATE crshop_entitlement SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now().Unix(), id)
	if err != nil {
		return wrapErr("shop: failed to update entitlement status", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountPaidOrdersByShop counts settled orders, used by the seller dashboard.
func (s *Store) CountPaidOrdersByShop(ctx context.Context, shopID string) (int, error) {
	var total int
	err := s.conn().queryRow(ctx, `SELECT COUNT(*) FROM crshop_order WHERE shop_id = ? AND status = ?`,
		shopID, string(model.OrderStatusPaid)).Scan(&total)
	return total, wrapErr("shop: failed to count orders", err)
}

func scanOrder(row rowScanner) (*model.Order, error) {
	var o model.Order
	err := row.Scan(&o.OrderNo, &o.Kind, &o.ShopID, &o.ObjectID, &o.BuyerUID, &o.Title, &o.Amount,
		&o.Discount, &o.Payable, &o.Paid, &o.Status, &o.Channel, &o.PayMode, &o.SettleAt, &o.SettleState,
		&o.CouponID, &o.ReferrerUID, &o.TransferState, &o.TradeNo, &o.ClientIP,
		&o.CreatedAt, &o.UpdatedAt, &o.PaidAt, &o.ExpireAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query order", err)
	}
	return &o, nil
}

const entitlementColumns = `id, uid, kind, object_id, order_no, shop_id, title, status, folder_id, created_at, updated_at`

// CreateEntitlement grants a buyer access to a sold object. It is idempotent:
// the unique key (uid, kind, object_id) turns a repeated grant into a conflict
// error that callers translate into "already owned".
func (t *Tx) CreateEntitlement(ctx context.Context, e *model.Entitlement) error {
	now := time.Now().Unix()
	e.CreatedAt, e.UpdatedAt = now, now

	_, err := t.exec(ctx, `INSERT INTO crshop_entitlement (`+entitlementColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.UID, string(e.Kind), e.ObjectID, e.OrderNo, e.ShopID, e.Title,
		string(e.Status), e.FolderID, e.CreatedAt, e.UpdatedAt)
	return wrapErr("shop: failed to create entitlement", err)
}

// GetEntitlement returns the entitlement of a user for one object.
func (s *Store) GetEntitlement(ctx context.Context, uid int, kind model.OrderKind, objectID string) (*model.Entitlement, error) {
	return scanEntitlement(s.conn().queryRow(ctx, `SELECT `+entitlementColumns+`
		FROM crshop_entitlement WHERE uid = ? AND kind = ? AND object_id = ?`,
		uid, string(kind), objectID))
}

// GetEntitlementByID returns an entitlement by its identifier.
func (s *Store) GetEntitlementByID(ctx context.Context, id string) (*model.Entitlement, error) {
	return scanEntitlement(s.conn().queryRow(ctx, `SELECT `+entitlementColumns+`
		FROM crshop_entitlement WHERE id = ?`, id))
}

// ListEntitlements returns every purchase of a user, newest first.
func (s *Store) ListEntitlements(ctx context.Context, uid, limit, offset int) ([]*model.Entitlement, error) {
	rows, err := s.conn().query(ctx, `SELECT `+entitlementColumns+`
		FROM crshop_entitlement WHERE uid = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		uid, capLimit(limit), max(offset, 0))
	if err != nil {
		return nil, wrapErr("shop: failed to list entitlements", err)
	}
	defer rows.Close()

	res := make([]*model.Entitlement, 0)
	for rows.Next() {
		e, err := scanEntitlement(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, e)
	}
	return res, wrapErr("shop: failed to iterate entitlements", rows.Err())
}

// UpdateEntitlement records the delivery result of an entitlement.
func (s *Store) UpdateEntitlement(ctx context.Context, e *model.Entitlement) error {
	e.UpdatedAt = time.Now().Unix()
	res, err := s.conn().exec(ctx, `UPDATE crshop_entitlement SET status = ?, folder_id = ?, title = ?, updated_at = ?
		WHERE id = ?`, string(e.Status), e.FolderID, e.Title, e.UpdatedAt, e.ID)
	if err != nil {
		return wrapErr("shop: failed to update entitlement", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListEntitledObjectIDs returns the ids of every object of one kind the user
// owns, so a listing can resolve ownership in a single query.
func (s *Store) ListEntitledObjectIDs(ctx context.Context, uid int, kind model.OrderKind) (map[string]struct{}, error) {
	rows, err := s.conn().query(ctx, `SELECT object_id FROM crshop_entitlement
		WHERE uid = ? AND kind = ? AND status <> ?`, uid, string(kind), string(model.EntitlementStatusRevoked))
	if err != nil {
		return nil, wrapErr("shop: failed to list entitlements", err)
	}
	defer rows.Close()

	res := make(map[string]struct{}, 8)
	for rows.Next() {
		var objectID string
		if err := rows.Scan(&objectID); err != nil {
			return nil, wrapErr("shop: failed to scan entitlement", err)
		}
		res[objectID] = struct{}{}
	}
	return res, wrapErr("shop: failed to iterate entitlements", rows.Err())
}

// FirstEntitlementAt returns the time the first entitlement for an object was
// granted, or 0 when nobody owns it yet.
func (s *Store) FirstEntitlementAt(ctx context.Context, kind model.OrderKind, objectID string) (int64, error) {
	var first int64
	err := s.conn().queryRow(ctx, `SELECT COALESCE(MIN(created_at), 0) FROM crshop_entitlement
		WHERE kind = ? AND object_id = ?`, string(kind), objectID).Scan(&first)
	if err != nil {
		return 0, wrapErr("shop: failed to query the first entitlement", err)
	}
	return first, nil
}

// CountEntitlementsByShop counts sold items of a shop.
func (s *Store) CountEntitlementsByShop(ctx context.Context, shopID string) (int, error) {
	var total int
	err := s.conn().queryRow(ctx, `SELECT COUNT(*) FROM crshop_entitlement WHERE shop_id = ?`, shopID).Scan(&total)
	return total, wrapErr("shop: failed to count entitlements", err)
}

func scanEntitlement(row rowScanner) (*model.Entitlement, error) {
	var e model.Entitlement
	err := row.Scan(&e.ID, &e.UID, &e.Kind, &e.ObjectID, &e.OrderNo, &e.ShopID, &e.Title,
		&e.Status, &e.FolderID, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, wrapErr("shop: failed to query entitlement", err)
	}
	return &e, nil
}

// ListMaturedSales returns paid platform orders whose held income may be
// released to the seller now. Custom channel orders are never held, so they are
// not part of this set.
func (s *Store) ListMaturedSales(ctx context.Context, now int64, limit int) ([]*model.Order, error) {
	rows, err := s.conn().query(ctx, `SELECT `+orderColumns+` FROM crshop_order
		WHERE status = ? AND settle_state = ? AND settle_at > 0 AND settle_at <= ?
		ORDER BY settle_at LIMIT ?`,
		string(model.OrderStatusPaid), model.SettleStateHeld, now, capLimit(limit))
	if err != nil {
		return nil, wrapErr("shop: failed to list matured sales", err)
	}
	defer rows.Close()

	res := make([]*model.Order, 0)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, order)
	}
	return res, wrapErr("shop: failed to iterate matured sales", rows.Err())
}

// MarkOrderSettled flips a held order to released. It reports false when the
// order is not held any more, which keeps the release sweep idempotent.
func (t *Tx) MarkOrderSettled(ctx context.Context, orderNo string) (bool, error) {
	res, err := t.exec(ctx, `UPDATE crshop_order SET settle_state = ?, updated_at = ?
		WHERE order_no = ? AND settle_state = ?`,
		model.SettleStateReleased, time.Now().Unix(), orderNo, model.SettleStateHeld)
	if err != nil {
		return false, wrapErr("shop: failed to mark the order settled", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, wrapErr("shop: failed to read affected rows", err)
	}
	return n > 0, nil
}
