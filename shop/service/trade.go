package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/pkg/sharegate"
	"github.com/Mxucc/StorFlow/service/share"
	"github.com/Mxucc/StorFlow/shop/gateway/epay"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// OrderView is the projection of an order.
type OrderView struct {
	*model.Order
	// PayURL is the cashier link, present while the order is pending.
	PayURL string `json:"pay_url,omitempty"`
	// EntitlementID is set once the content was delivered.
	EntitlementID string `json:"entitlement_id,omitempty"`
	// BuyerNickname is filled for the seller and for administrators.
	BuyerNickname string `json:"buyer_nickname,omitempty"`
}

// Checkout creates a pending order and returns the cashier URL.
func (s *Service) Checkout(c *gin.Context, req *CreateOrderRequest) (*OrderView, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}

	buyer, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	kind := model.OrderKind(req.Kind)
	if !kind.Valid() {
		return nil, serializer.NewError(serializer.CodeParamErr, "Unknown order kind", nil)
	}

	var (
		title    string
		price    model.Money
		shopID   string
		shopUID  int
		objectID string
		shop     *model.Shop
	)

	switch kind {
	case model.OrderKindProduct:
		product, err := s.store.GetProductByID(c, req.ObjectID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", err)
		}
		shop, err = s.store.GetShopByID(c, product.ShopID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
		}
		if shop.Status != model.ShopStatusActive {
			return nil, serializer.NewError(serializer.CodeNotFound, "The shop is closed", nil)
		}
		if !product.Status.Purchasable() {
			return nil, serializer.NewError(serializer.CodeNotFound, "Product is not on sale", nil)
		}
		if entitlement, err := s.store.GetEntitlement(c, buyer.ID, model.OrderKindProduct, product.ID); err == nil &&
			entitlement.Status != model.EntitlementStatusRevoked {
			return nil, serializer.NewError(serializer.CodeConflict, "You already own this product", nil)
		}

		objectID = product.ID
		title = product.Name
		price = product.EffectivePrice(time.Now().Unix())
		shopID = shop.ID
		shopUID = shop.UID

	case model.OrderKindShare:
		shareID, err := s.Hasher().Decode(req.ObjectID, hashid.ShareID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid share id", err)
		}

		ctx := context.WithValue(c, inventory.LoadShareFile{}, true)
		ctx = context.WithValue(ctx, inventory.LoadShareUser{}, true)
		entity, err := s.dep.ShareClient().GetByID(ctx, shareID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Share not found", err)
		}
		if !sharegate.Payable(entity) {
			return nil, serializer.NewError(serializer.CodeParamErr, "This share is not for sale", nil)
		}
		if entity.Password != "" && req.SharePassword != entity.Password {
			return nil, serializer.NewError(serializer.CodeNoPermissionErr, "Incorrect share password", nil)
		}
		if entity.Edges.User == nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Share owner is missing", nil)
		}
		if entity.Edges.User.ID == buyer.ID {
			return nil, serializer.NewError(serializer.CodeParamErr, "You own this share", nil)
		}
		if entitlement, err := s.store.GetEntitlement(c, buyer.ID, model.OrderKindShare, req.ObjectID); err == nil &&
			entitlement.Status != model.EntitlementStatusRevoked {
			return nil, serializer.NewError(serializer.CodeConflict, "You already own this share", nil)
		}

		shop, err = s.store.GetShopByUID(c, entity.Edges.User.ID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeInternalSetting,
				"The share owner has not opened a shop yet", err)
		}
		if shop.Status != model.ShopStatusActive {
			return nil, serializer.NewError(serializer.CodeNotFound, "The shop is closed", nil)
		}

		objectID = req.ObjectID
		title = entity.Edges.File.Name
		if title == "" {
			title = "Paid share"
		}
		price = model.Money(sharegate.Price(entity))
		shopID = shop.ID
		shopUID = shop.UID
	}
	// Fail before the order exists when the shop cannot accept payments: a
	// merchant channel without credentials, or a platform gateway the
	// administrator has not configured yet.
	if _, _, err := s.gatewayFor(shop); err != nil {
		return nil, err
	}

	// Reuse an open order for the same object: without this a buyer who clicks
	// "buy" twice would own two payable orders for one product and could pay
	// for it twice.
	if pending, err := s.store.GetPendingOrder(c, buyer.ID, kind, objectID); err == nil {
		if pending.ExpireAt == 0 || pending.ExpireAt > time.Now().Unix() {
			return s.orderView(c, pending)
		}
		// The previous attempt expired: close it and let the buyer start over.
		if _, err := s.store.CloseExpiredOrders(c, time.Now().Unix(), 10); err != nil {
			s.l.Warning("Failed to close expired orders: %s", err)
		}
	} else if err != store.ErrNotFound {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to check open orders", err)
	}

	discount := model.Money(0)
	couponID := ""

	// Coupon: shop scoped, applied on the promoted price.
	if code := strings.TrimSpace(req.Coupon); code != "" {
		coupon, err := s.store.GetCouponByCode(c, shopID, strings.ToUpper(code))
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Coupon not found", err)
		}
		if !coupon.Available(time.Now().Unix()) {
			return nil, serializer.NewError(serializer.CodeParamErr, "Coupon is not available", nil)
		}
		if coupon.ProductID != "" && coupon.ProductID != objectID {
			return nil, serializer.NewError(serializer.CodeParamErr, "Coupon does not apply to this product", nil)
		}
		if price < coupon.MinAmount {
			return nil, serializer.NewError(serializer.CodeParamErr, "Order amount is below the coupon minimum", nil)
		}
		if coupon.PerUser > 0 {
			used, err := s.store.CountCouponUses(c, coupon.ID, buyer.ID)
			if err != nil {
				return nil, serializer.NewError(serializer.CodeDBError, "Failed to validate the coupon", err)
			}
			if used >= coupon.PerUser {
				return nil, serializer.NewError(serializer.CodeConflict, "Coupon already used", nil)
			}
		}

		discount = s.couponDiscount(coupon, price)
		couponID = coupon.ID
	}

	payable := model.ClampPayable(price - discount)

	referrerUID := 0
	if referral, err := s.store.GetReferralByInvitee(c, buyer.ID); err == nil {
		referrerUID = referral.InviterUID
	} else if err == store.ErrNotFound && req.Ref != "" {
		// The visitor registered through a resource page but the binding was
		// not written yet (for example the shop module was enabled later).
		if inviter, err := s.Hasher().Decode(req.Ref, hashid.UserID); err == nil && inviter != buyer.ID {
			if err := s.bindReferral(c, inviter, buyer.ID, "checkout", objectID); err == nil {
				referrerUID = inviter
			}
		}
	}

	order := &model.Order{
		OrderNo:     orderNo(),
		Kind:        kind,
		ShopID:      shopID,
		ObjectID:    objectID,
		BuyerUID:    buyer.ID,
		Title:       title,
		Amount:      price,
		Discount:    discount,
		Payable:     payable,
		Status:      model.OrderStatusPending,
		CouponID:    couponID,
		ReferrerUID: referrerUID,
		ClientIP:    c.ClientIP(),
		ExpireAt:    time.Now().Unix() + int64(s.Settings().OrderTTL),
		// The payment mode is snapshotted: the order keeps being verified and
		// settled the way it was created, whatever the shop does later.
		PayMode: shop.PayMode.OrDefault(),
	}

	channel := model.PayChannel(strings.TrimSpace(req.Channel))
	if !channel.Valid() {
		channel = s.defaultChannel()
	}
	order.Channel = channel

	// The coupon is reserved together with the order: a single-use code cannot
	// be spread over several discounted orders, and an abandoned order gives the
	// reservation back when it expires.
	couponReservation := (*model.Coupon)(nil)
	if couponID != "" {
		couponReservation, err = s.store.GetCouponByID(c, couponID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to load the coupon", err)
		}
	}

	if err := s.store.Tx(c, func(tx *store.Tx) error {
		if couponReservation != nil {
			if err := tx.ReserveCoupon(c, couponReservation.ID, buyer.ID, order.OrderNo,
				couponReservation.MaxUses, couponReservation.PerUser); err != nil {
				return err
			}
		}
		return tx.CreateOrder(c, order)
	}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, serializer.NewError(serializer.CodeConflict, "Coupon already used", nil)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create the order", err)
	}

	view, err := s.orderView(c, order)
	if err != nil {
		return nil, err
	}
	_ = shopUID
	return view, nil
}

// Order returns the state of an order. Buyers only see their own orders.
func (s *Service) Order(c *gin.Context, orderNo string) (*OrderView, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	order, err := s.store.GetOrder(c, orderNo)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", err)
	}
	if order.BuyerUID != user.ID && !s.isAdmin(c) {
		return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", nil)
	}

	// Lazily close an expired order so the buyer sees the real state.
	if order.Status == model.OrderStatusPending && order.ExpireAt > 0 && order.ExpireAt < time.Now().Unix() {
		if _, err := s.store.CloseExpiredOrders(c, time.Now().Unix(), 10); err != nil {
			s.l.Warning("Failed to close expired orders: %s", err)
		}
		if refreshed, err := s.store.GetOrder(c, orderNo); err == nil {
			order = refreshed
		}
	}

	return s.orderView(c, order)
}

// ReconcilePendingOrders asks the payment gateway about pending orders whose
// asynchronous notification may have been lost, and settles the ones that were
// actually paid. It is the safety net of the payment flow: a buyer must never
// lose access because a callback was dropped.
// grace is the minimum age of an order before the gateway is asked about it:
// a fresh order is still being paid, and querying it would waste a request.
func (s *Service) ReconcilePendingOrders(ctx context.Context, limit, grace int) (int, error) {

	now := time.Now().Unix()
	if grace < 0 {
		grace = 0
	}
	orders, err := s.store.ListPendingOrdersForReconcile(ctx, now-int64(grace), now, limit)
	if err != nil {
		return 0, err
	}
	if len(orders) == 0 {
		return 0, nil
	}

	// Settlement uses the host file manager, which expects the dependency in
	// the context: a background job has no gin context to carry it.
	ctx = context.WithValue(ctx, dependency.DepCtx{}, s.dep)

	settled := 0
	for _, order := range orders {
		// Every order is queried through the gateway it was created with: an
		// order of a merchant channel is verified with the credentials of its
		// own shop.
		cfg, err := s.gatewayForOrder(ctx, order)
		if err != nil {
			s.l.Warning("Failed to resolve the gateway of order %s: %s", order.OrderNo, err)
			continue
		}
		if cfg.APIURL == "" || cfg.Key == "" {
			continue
		}

		res, err := cfg.Query(ctx, order.OrderNo)
		if err != nil {
			s.l.Debug("Gateway query for order %s failed: %s", order.OrderNo, err)
			continue
		}
		if !res.Paid() {
			continue
		}

		paid, err := epay.ParseYuan(res.Money)
		if err != nil {
			s.l.Warning("Gateway returned an unparsable amount for order %s: %s", order.OrderNo, err)
			continue
		}
		if model.Money(paid) != order.Payable {
			s.l.Warning("Gateway reports %d cents for order %s, the order expects %d: please review.",
				paid, order.OrderNo, order.Payable)
			continue
		}

		if err := s.settle(ctx, order, res.TradeNo, model.Money(paid)); err != nil {
			s.l.Warning("Failed to settle order %s from the gateway query: %s", order.OrderNo, err)
			continue
		}

		settled++
		s.l.Info("Settled order %s from the gateway query, the notification was lost.", order.OrderNo)
	}
	return settled, nil
}

// MyOrders lists the orders of the requester.
func (s *Service) MyOrders(c *gin.Context, limit, offset int) ([]*model.Order, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	orders, err := s.store.ListOrdersByBuyer(c, user.ID, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list orders", err)
	}
	return orders, nil
}

// ShopOrders lists the orders received by the requester's shop. The buyer is
// identified by nickname only: a seller needs to be able to answer a support
// request, but has no business seeing the buyer's email address.
func (s *Service) ShopOrders(c *gin.Context, limit, offset int) ([]*OrderView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	orders, err := s.store.ListOrdersByShop(c, shop.ID, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list orders", err)
	}

	nicknames := make(map[int]string, 4)
	res := make([]*OrderView, 0, len(orders))
	for _, order := range orders {
		view := &OrderView{Order: order}
		if _, ok := nicknames[order.BuyerUID]; !ok {
			nickname := ""
			if buyer, err := s.dep.UserClient().GetByID(c, order.BuyerUID); err == nil {
				nickname = buyer.Nick
			}
			nicknames[order.BuyerUID] = nickname
		}
		view.BuyerNickname = nicknames[order.BuyerUID]
		res = append(res, view)
	}
	return res, nil
}

// Notify processes an asynchronous epay notification and settles the order.
//
// It is intentionally idempotent: the state transition happens in a single
// conditional UPDATE, so a duplicated notification cannot credit the seller
// twice.
func (s *Service) Notify(c *gin.Context, params map[string]string) error {
	cfg := s.epayConfig()

	orderNo := params["out_trade_no"]
	if orderNo == "" {
		return fmt.Errorf("missing out_trade_no")
	}

	order, err := s.store.GetOrder(c, orderNo)
	if err != nil {
		return fmt.Errorf("unknown order %s: %w", orderNo, err)
	}

	// The signature is verified for every notification, including the ones for
	// orders that are already settled: an unsigned request must never be
	// acknowledged, otherwise the endpoint would accept forged traffic.
	notify, err := cfg.ParseNotify(params, int64(order.Payable))
	if err != nil {
		return err
	}

	paid, err := notify.AmountCents()
	if err != nil {
		return err
	}

	return s.settle(c, order, notify.TradeNo, model.Money(paid))
}

// settle records a paid order: it flips the order, credits the seller, pays the
// referral rebate, grants the entitlement and delivers the content. It is
// idempotent, so both the asynchronous notification and the gateway
// reconciliation can call it for the same order.
//
// The money follows the payment mode snapshot of the order:
//
//	platform  the platform collected it, so the seller is credited (held for the
//	          configured unfreeze window) and the commission is deducted;
//	custom    the merchant collected it directly, so nothing is credited, no
//	          commission is charged and no referral rebate is paid.
func (s *Service) settle(c context.Context, order *model.Order, tradeNo string, paid model.Money) error {
	if order.Status == model.OrderStatusPaid {
		// Already settled: nothing to do.
		return nil
	}
	// A closed order may still be paid late: honour the payment instead of
	// keeping the money without delivering anything.
	if order.Status != model.OrderStatusPending && order.Status != model.OrderStatusClosed {
		return fmt.Errorf("order %s is %s", order.OrderNo, order.Status)
	}

	// A deposit order only records the payment of the shop deposit: no content
	// is delivered and the wallet is not involved.
	if order.Kind == model.OrderKindDeposit {
		return s.settleDeposit(c, order, tradeNo, paid)
	}

	settings := s.Settings()
	settledByPlatform := order.PayMode.OrDefault() == model.PayModePlatform

	// Income of a platform channel order is held back for the configured
	// unfreeze window: the seller sees the money, but cannot withdraw it before
	// it matured.
	settleAt, settleState := int64(0), ""
	if settledByPlatform && settings.SettleDelayHours > 0 {
		settleAt = time.Now().Unix() + int64(settings.SettleDelayHours)*3600
		settleState = model.SettleStateHeld
	}

	orderNo := order.OrderNo

	// Resolve the referral rate before opening the transaction: reading through
	// the pool while a transaction holds the connection would deadlock on
	// single-connection databases (SQLite).
	referralRate := 0
	if settledByPlatform && order.ReferrerUID > 0 {
		referralRate = s.referralRate(c, order.ReferrerUID)
	}

	var (
		entitlement *model.Entitlement
		product     *model.Product
		shop        *model.Shop
	)

	err := s.store.Tx(c, func(tx *store.Tx) error {
		settled, err := tx.MarkOrderPaid(c, orderNo, tradeNo, paid, settleAt, settleState)
		if err != nil {
			return err
		}
		if !settled {
			// Lost the race against a concurrent notification.
			return nil
		}

		shop, err = tx.GetShopByID(c, order.ShopID)
		if err != nil {
			return err
		}

		if settledByPlatform {
			fee := model.ApplyRate(order.Payable, settings.FeePercent)
			income := order.Payable - fee
			if income > 0 {
				if settleState == model.SettleStateHeld {
					if _, err := tx.CreditShopPending(c, shop.ID, income, model.LedgerKindSalePending, orderNo,
						fmt.Sprintf("订单 %s 收入待解冻 %d 小时（含手续费 %s）", orderNo, settings.SettleDelayHours, fee)); err != nil {
						return err
					}
				} else if _, err := tx.CreditShop(c, shop.ID, income, model.LedgerKindSale, orderNo,
					fmt.Sprintf("订单 %s 收入（含手续费 %s）", orderNo, fee)); err != nil {
					return err
				}
			}

			// Referral rebate to the inviter of the buyer.
			if order.ReferrerUID > 0 && order.ReferrerUID != shop.UID && settings.ReferralEnabled {
				rebate := model.ApplyRate(order.Payable, referralRate)
				if rebate > 0 {
					inviterShop, err := tx.GetShopByUID(c, order.ReferrerUID)
					if err == nil {
						if _, err := tx.CreditShop(c, inviterShop.ID, rebate, model.LedgerKindReferral, orderNo,
							fmt.Sprintf("邀请返利（订单 %s）", orderNo)); err != nil {
							return err
						}
						if err := tx.AddReferralRebate(c, order.BuyerUID, rebate); err != nil && err != store.ErrNotFound {
							return err
						}
					}
				}
			}
		}

		// Grant the entitlement inside the same transaction.
		entitlement = &model.Entitlement{
			ID:       newID(),
			UID:      order.BuyerUID,
			Kind:     order.Kind,
			ObjectID: order.ObjectID,
			OrderNo:  orderNo,
			ShopID:   order.ShopID,
			Title:    order.Title,
			Status:   model.EntitlementStatusPending,
		}
		if err := tx.CreateEntitlement(c, entitlement); err != nil {
			if errors.Is(err, store.ErrConflict) {
				// Already delivered by an earlier notification.
				entitlement = nil
				return nil
			}
			return err
		}

		// The coupon was reserved when the order was created; the payment only
		// settles the money, so nothing to do here.

		if order.Kind == model.OrderKindProduct {
			product, err = tx.GetProductByID(c, order.ObjectID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if entitlement == nil {
		return nil
	}

	// Delivery runs outside the transaction: a failure keeps the entitlement in
	// the pending state and the buyer can retry it from "my purchases".
	if err := s.deliver(c, order, entitlement, product, shop); err != nil {
		s.l.Warning("Failed to deliver order %s: %s", orderNo, err)
		_ = s.store.SetOrderTransferState(c, orderNo, "failed")
		return nil
	}
	_ = s.store.SetOrderTransferState(c, orderNo, "done")
	return nil
}

// settleDeposit records the payment of a shop deposit (保证金). The deposit is
// held by the platform and is deliberately kept out of the wallet: the order is
// flipped and the shop accumulates what it paid, which is what unlocks
// withdrawals. No content is delivered and no commission is charged.
func (s *Service) settleDeposit(c context.Context, order *model.Order, tradeNo string, paid model.Money) error {
	settled := false
	err := s.store.Tx(c, func(tx *store.Tx) error {
		ok, err := tx.MarkOrderPaid(c, order.OrderNo, tradeNo, paid, 0, "")
		if err != nil {
			return err
		}
		settled = ok
		return nil
	})
	if err != nil {
		return err
	}
	if !settled {
		// Lost the race against a concurrent notification.
		return nil
	}

	if err := s.store.MarkDepositPaid(c, order.ShopID, paid); err != nil {
		return fmt.Errorf("failed to record the deposit of shop %s: %w", order.ShopID, err)
	}
	s.l.Info("Shop %s paid %d cents of deposit.", order.ShopID, int64(paid))
	return nil
}

// ReleaseMaturedSales moves the income of every matured platform order from the
// pending bucket to the available balance. It implements the "订单完成 + N 小时
// 解冻" rule: until then the money is visible but not withdrawable.
func (s *Service) ReleaseMaturedSales(ctx context.Context, limit int) (int, error) {
	orders, err := s.store.ListMaturedSales(ctx, time.Now().Unix(), limit)
	if err != nil {
		return 0, err
	}

	released := 0
	for _, order := range orders {
		var (
			amount  model.Money
			changed bool
		)
		err := s.store.Tx(ctx, func(tx *store.Tx) error {
			ok, err := tx.MarkOrderSettled(ctx, order.OrderNo)
			if err != nil || !ok {
				return err
			}
			changed = true

			// The exact amount is read back from the ledger: the commission
			// rate may have changed since the order was paid.
			held, err := tx.LedgerAmountForRef(ctx, order.ShopID, order.OrderNo, model.LedgerKindSalePending)
			if err != nil {
				return err
			}
			amount = held
			if held <= 0 {
				return nil
			}
			_, err = tx.ReleasePending(ctx, order.ShopID, held, order.OrderNo,
				fmt.Sprintf("订单 %s 收入解冻", order.OrderNo))
			return err
		})
		if err != nil {
			s.l.Warning("Failed to release the income of order %s: %s", order.OrderNo, err)
			continue
		}
		if changed {
			released++
			s.l.Debug("Released %d cents of order %s.", int64(amount), order.OrderNo)
		}
	}
	return released, nil
}

// deliver creates the symbolic folder of a settled order and marks the
// entitlement as granted.
func (s *Service) deliver(ctx context.Context, order *model.Order, entitlement *model.Entitlement,
	product *model.Product, shop *model.Shop) error {
	buyer, err := s.dep.UserClient().GetByID(ctx, order.BuyerUID)
	if err != nil {
		return fmt.Errorf("failed to load the buyer: %w", err)
	}

	var folderID int
	switch order.Kind {
	case model.OrderKindProduct:
		if product == nil {
			if product, err = s.store.GetProductByID(ctx, order.ObjectID); err != nil {
				return err
			}
		}
		folderID, err = s.deliverProduct(ctx, buyer, product.ShopID, product, order.OrderNo)
	case model.OrderKindShare:
		shareID, decodeErr := s.Hasher().Decode(order.ObjectID, hashid.ShareID)
		if decodeErr != nil {
			return decodeErr
		}
		loadCtx := context.WithValue(ctx, inventory.LoadShareFile{}, true)
		entity, shareErr := s.dep.ShareClient().GetByID(loadCtx, shareID)
		if shareErr != nil {
			return shareErr
		}
		folderID, err = s.deliverShare(ctx, buyer, entity, order.OrderNo)
	default:
		return fmt.Errorf("unknown order kind %q", order.Kind)
	}
	if err != nil {
		return err
	}

	entitlement.FolderID = folderID
	entitlement.Status = model.EntitlementStatusGranted
	if err := s.store.UpdateEntitlement(ctx, entitlement); err != nil {
		return err
	}

	if product != nil {
		_ = s.store.IncreaseProductSales(ctx, product.ID, 1)
	}
	return nil
}

// deliverShare saves a paid share as a shortcut in the buyer's drive.
func (s *Service) deliverShare(ctx context.Context, buyer *ent.User, entity *ent.Share, orderNo string) (int, error) {
	folderID, err := share.SaveShortcut(ctx, s.dep, buyer, entity)
	if err != nil {
		return 0, err
	}
	_ = orderNo
	return folderID, nil
}

// MyPurchases lists the entitlements of the requester.
func (s *Service) MyPurchases(c *gin.Context, limit, offset int) ([]*model.Entitlement, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	items, err := s.store.ListEntitlements(c, user.ID, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list purchases", err)
	}
	return items, nil
}

// RetryDelivery re-runs the delivery of a purchase whose symbolic folder was
// never created.
func (s *Service) RetryDelivery(c *gin.Context, id string) (*model.Entitlement, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	entitlement, err := s.store.GetEntitlementByID(c, id)
	if err != nil || entitlement.UID != user.ID {
		return nil, serializer.NewError(serializer.CodeNotFound, "Purchase not found", err)
	}
	if entitlement.Status == model.EntitlementStatusGranted {
		return entitlement, nil
	}
	if entitlement.Status == model.EntitlementStatusRevoked {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "This purchase was revoked", nil)
	}

	order, err := s.store.GetOrder(c, entitlement.OrderNo)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", err)
	}
	if order.Status != model.OrderStatusPaid {
		return nil, serializer.NewError(serializer.CodeParamErr, "The order is not paid yet", nil)
	}

	var product *model.Product
	if order.Kind == model.OrderKindProduct {
		if product, err = s.store.GetProductByID(c, order.ObjectID); err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", err)
		}
	}

	if err := s.deliver(c, order, entitlement, product, nil); err != nil {
		return nil, serializer.NewError(serializer.CodeInternalSetting, "Failed to save the content", err)
	}
	return entitlement, nil
}

// hasShareFree reports whether the requester belongs to a group whose members
// may open paid shares without buying them (Pro: 无需购买分享链接).
func (s *Service) hasShareFree(ctx context.Context, user *ent.User) bool {
	if user == nil {
		return false
	}

	g := user.Edges.Group
	if g == nil && user.GroupUsers > 0 {
		resolved, err := s.dep.DBClient().Group.Get(ctx, user.GroupUsers)
		if err != nil {
			return false
		}
		g = resolved
	}
	return g != nil && g.Permissions.Enabled(int(types.GroupPermissionShareFree))
}

// CheckShareAccess implements the share paywall used by the core file manager:
// a paid share is readable only by its owner or by a buyer holding an
// entitlement for it.
func (s *Service) CheckShareAccess(ctx context.Context, entity *ent.Share, requester *ent.User) error {
	if entity == nil || requester == nil {
		return sharegate.ErrPaymentRequired
	}
	// Members of a group with the share_free permission skip the paywall
	// entirely; the owner of the share is handled by the core gate.
	if s.hasShareFree(ctx, requester) {
		return nil
	}

	// Buying a share link grants the share entitlement.
	objectID := hashid.EncodeShareID(s.Hasher(), entity.ID)
	entitlement, err := s.store.GetEntitlement(ctx, requester.ID, model.OrderKindShare, objectID)
	if err == nil && entitlement.Status != model.EntitlementStatusRevoked {
		return nil
	}
	if err != nil && err != store.ErrNotFound {
		s.l.Warning("Failed to check the share entitlement: %s", err)
	}

	// Buying a product grants access to the internal share of that product, so
	// a buyer keeps working even when they only know the folder address.
	if product, err := s.store.GetProductByShareID(ctx, entity.ID); err == nil {
		entitlement, err := s.store.GetEntitlement(ctx, requester.ID, model.OrderKindProduct, product.ID)
		if err == nil && entitlement.Status != model.EntitlementStatusRevoked {
			return nil
		}
		if err != nil && err != store.ErrNotFound {
			s.l.Warning("Failed to check the product entitlement: %s", err)
		}
	} else if err != store.ErrNotFound {
		s.l.Warning("Failed to resolve the product of share %d: %s", entity.ID, err)
	}

	return sharegate.ErrPaymentRequired
}

// orderView renders an order for the API.
func (s *Service) orderView(c *gin.Context, order *model.Order) (*OrderView, error) {
	view := &OrderView{Order: order}

	if order.Status == model.OrderStatusPending {
		if url, err := s.payURL(c, order); err != nil {
			s.l.Warning("Failed to build the cashier URL for %s: %s", order.OrderNo, err)
		} else {
			view.PayURL = url
		}
	}

	if entitlement, err := s.store.GetEntitlement(c, order.BuyerUID, order.Kind, order.ObjectID); err == nil {
		view.EntitlementID = entitlement.ID
	}
	return view, nil
}

// payURL builds the epay cashier URL of a pending order through the gateway the
// order was created with.
func (s *Service) payURL(c *gin.Context, order *model.Order) (string, error) {
	cfg, err := s.gatewayForOrder(c, order)
	if err != nil {
		return "", err
	}
	if cfg.APIURL == "" || cfg.Key == "" {
		return "", epay.ErrNotConfigured
	}

	base := s.BaseURL(c)

	return cfg.PayURL(epay.PayRequest{
		TradeNo:   order.OrderNo,
		Type:      string(order.Channel),
		Name:      order.Title,
		Money:     order.Payable.Yuan(),
		NotifyURL: base + "/api/v4/shop/epay/notify",
		ReturnURL: base + "/shop/order/" + order.OrderNo,
		Param:     string(order.Kind),
		ClientIP:  order.ClientIP,
	})
}

// epayConfig returns the normalized gateway configuration.
func (s *Service) epayConfig() epay.Config {
	settings := s.Settings()
	cfg := epay.Config{
		APIURL: settings.EpayAPIURL,
		PID:    settings.EpayPID,
		Key:    settings.EpayKey,
	}
	normalized, err := cfg.Normalize()
	if err != nil {
		return cfg
	}
	return normalized
}

// couponDiscount computes the discount of a coupon on a given amount.
func (s *Service) couponDiscount(coupon *model.Coupon, amount model.Money) model.Money {
	switch coupon.Kind {
	case model.CouponKindPercent:
		return amount - model.PercentOff(amount, int(coupon.Value))
	case model.CouponKindFixed:
		return amount - model.FixedOff(amount, model.Money(coupon.Value))
	default:
		return 0
	}
}

// isAdmin reports whether the requester is an administrator.
func (s *Service) isAdmin(c *gin.Context) bool {
	user := CurrentUser(c)
	if user == nil || user.Edges.Group == nil {
		return false
	}
	return user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin))
}
