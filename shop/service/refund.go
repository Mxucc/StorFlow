package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// AdminRefundOrder refunds a settled order. It is the counterpart of a payment:
//
//   - the order moves to "refunded" (a single conditional update, so it cannot
//     be refunded twice);
//   - the buyer's entitlement is revoked, which immediately closes the paywall
//     and therefore the buyer's linked folder;
//   - the seller's wallet is debited by the income it had received (its balance
//     may go negative when the money was already withdrawn: that debt is what
//     the marketplace has to collect);
//   - the referral rebate paid to the inviter is clawed back as well.
//
// The money itself is returned out of band (the epay protocol used here has no
// refund API), which is why the endpoint records the reason.
func (s *Service) AdminRefundOrder(c *gin.Context, orderNo, reason string) (*model.Order, error) {
	order, err := s.store.GetOrder(c, orderNo)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", err)
	}

	if err := s.refundOrder(c, order, reason); err != nil {
		return nil, err
	}
	return s.store.GetOrder(c, orderNo)
}

// refundOrder performs the refund itself: order state, entitlement revocation,
// wallet and referral bookkeeping, and the renaming of the buyer's folder. It is
// shared by the administrator action and by an approved buyer request.
//
// An order paid through the merchant's own channel (PayMode custom) was never
// credited to a wallet, so only the state and the entitlement are touched: the
// money is returned by the merchant, out of band.
func (s *Service) refundOrder(c context.Context, order *model.Order, reason string) error {
	if order.Status != model.OrderStatusPaid {
		return serializer.NewError(serializer.CodeConflict, "Only a paid order can be refunded", nil)
	}
	orderNo := order.OrderNo

	shop, err := s.store.GetShopByID(c, order.ShopID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
	}

	entitlement, entitlementErr := s.store.GetEntitlement(c, order.BuyerUID, order.Kind, order.ObjectID)
	if entitlementErr != nil && entitlementErr != store.ErrNotFound {
		return serializer.NewError(serializer.CodeDBError, "Failed to load the entitlement", entitlementErr)
	}

	settings := s.Settings()
	// A deposit order and an order paid through the merchant's own channel were
	// never credited to a wallet: only the state and the entitlement are touched.
	isDeposit := order.Kind == model.OrderKindDeposit
	settledByPlatform := order.PayMode.OrDefault() == model.PayModePlatform && !isDeposit
	fee := model.ApplyRate(order.Payable, settings.FeePercent)
	income := order.Payable - fee
	if !settledByPlatform {
		fee, income = 0, 0
	}

	referralRate := 0
	if settledByPlatform && order.ReferrerUID > 0 {
		referralRate = s.referralRate(c, order.ReferrerUID)
	}
	rebate := model.ApplyRate(order.Payable, referralRate)

	note := strings.TrimSpace(reason)
	if note == "" {
		note = "管理员退款"
	}

	err = s.store.Tx(c, func(tx *store.Tx) error {
		refunded, err := tx.MarkOrderRefunded(c, orderNo)
		if err != nil {
			return err
		}
		if !refunded {
			return store.ErrConflict
		}

		if entitlementErr == nil {
			if err := tx.SetEntitlementStatus(c, entitlement.ID, model.EntitlementStatusRevoked); err != nil {
				return err
			}
		}

		if income > 0 {
			// Income that is still held for this order never reached the seller:
			// take it back from the pending bucket and only the remainder from
			// the available balance.
			held, err := tx.LedgerAmountForRef(c, shop.ID, orderNo, model.LedgerKindSalePending)
			if err != nil {
				return err
			}
			if held > 0 {
				if _, err := tx.DebitPending(c, shop.ID, held, model.LedgerKindAdjust, orderNo,
					fmt.Sprintf("订单退款：扣回待解冻收入（%s）", note)); err == nil {
					income -= held
				} else if err != store.ErrInsufficientBalance {
					return err
				}
			}
		}

		if income > 0 {
			if _, err := tx.DebitShop(c, shop.ID, income, model.LedgerKindAdjust, orderNo,
				fmt.Sprintf("订单退款：扣回收入（%s）", note)); err != nil {
				return err
			}
		}

		if rebate > 0 && order.ReferrerUID != 0 && order.ReferrerUID != shop.UID {
			inviterShop, err := tx.GetShopByUID(c, order.ReferrerUID)
			if err == nil {
				if _, err := tx.DebitShop(c, inviterShop.ID, rebate, model.LedgerKindAdjust, orderNo,
					fmt.Sprintf("订单退款：扣回邀请返利（%s）", note)); err != nil {
					return err
				}
				if err := tx.AddReferralRebate(c, order.BuyerUID, -rebate); err != nil && err != store.ErrNotFound {
					return err
				}
			} else if err != store.ErrNotFound {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if err == store.ErrConflict {
			return serializer.NewError(serializer.CodeConflict, "This order was already refunded", nil)
		}
		return serializer.NewError(serializer.CodeDBError, "Failed to refund the order", err)
	}

	// A refunded deposit leaves the platform: the shop owes it again and
	// withdrawals stay blocked until it is paid back.
	if isDeposit {
		amount := order.Paid
		if amount <= 0 {
			amount = order.Payable
		}
		if err := s.store.RefundDeposit(c, shop.ID, amount); err != nil {
			s.l.Warning("Failed to release the refunded deposit of shop %s: %s", shop.ID, err)
		}
	}

	// The delivered folder is renamed so the buyer sees why it stopped working.
	if entitlementErr == nil && entitlement.FolderID > 0 {
		refreshed, err := s.store.GetEntitlementByID(c, entitlement.ID)
		if err == nil && refreshed.FolderID > 0 {
			newName := refreshed.Title + " (已退款)"
			if _, err := s.dep.DBClient().File.UpdateOneID(refreshed.FolderID).SetName(newName).Save(c); err != nil {
				s.l.Warning("Failed to rename the refunded folder %d: %s", refreshed.FolderID, err)
			}
		}
	}

	return nil
}

// AdminShopRow is one row of the admin shop table.
type AdminShopRow struct {
	Shop      *model.Shop    `json:"shop"`
	Balance   *model.Balance `json:"balance"`
	Sales     int            `json:"sales"`
	SoldCount int            `json:"sold_count"`
}

// AdminListShops lists every shop with its wallet and headline numbers.
func (s *Service) AdminListShops(c *gin.Context, limit, offset int) ([]*AdminShopRow, error) {
	shops, err := s.store.ListShops(c, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list shops", err)
	}

	res := make([]*AdminShopRow, 0, len(shops))
	for _, shop := range shops {
		row := &AdminShopRow{Shop: shop}

		if balance, err := s.store.GetBalance(c, shop.ID); err == nil {
			row.Balance = balance
		}
		if sold, err := s.store.CountEntitlementsByShop(c, shop.ID); err == nil {
			row.SoldCount = sold
		}
		if orders, err := s.store.CountPaidOrdersByShop(c, shop.ID); err == nil {
			row.Sales = orders
		}
		res = append(res, row)
	}
	return res, nil
}

// Reconcile compares every wallet with its ledger and reports the shops whose
// balance drifted. It is called by the module janitor: the ledger is the source
// of truth, the balance is a cached snapshot.
func (s *Service) Reconcile(ctx context.Context) (int, error) {
	shops, err := s.store.ListShops(ctx, 100, 0)
	if err != nil {
		return 0, err
	}

	drifted := 0
	for _, shop := range shops {
		balance, err := s.store.GetBalance(ctx, shop.ID)
		if err != nil {
			return drifted, err
		}

		total, err := s.store.LedgerTotal(ctx, shop.ID)
		if err != nil {
			return drifted, err
		}

		if balance.Available+balance.Frozen+balance.Pending != total {
			drifted++
			s.l.Error("Shop %s balance drifted: available(%d) + frozen(%d) + pending(%d) != ledger(%d)",
				shop.ID, balance.Available, balance.Frozen, balance.Pending, total)
		}
	}
	return drifted, nil
}

// RefundView is a refund request enriched with what a reviewer needs to decide.
type RefundView struct {
	*model.Refund
	// BuyerNickname is filled for the seller and for administrators.
	BuyerNickname string `json:"buyer_nickname,omitempty"`
	// Title is the title of the refunded order.
	Title string `json:"title,omitempty"`
	// OrderStatus is the current state of the underlying order.
	OrderStatus model.OrderStatus `json:"order_status,omitempty"`
	// PayMode is the payment channel of the underlying order: a refund of a
	// merchant channel order has to be documented and verified by hand.
	PayMode model.PayMode `json:"pay_mode,omitempty"`
}

// RequestRefund lets a buyer ask for their money back. The request is reviewed
// by the seller (or an administrator); the content keeps working until it is
// approved.
func (s *Service) RequestRefund(c *gin.Context, entitlementID, reason string) (*model.Refund, error) {
	buyer, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	entitlement, err := s.store.GetEntitlementByID(c, entitlementID)
	if err != nil || entitlement.UID != buyer.ID {
		return nil, serializer.NewError(serializer.CodeNotFound, "Purchase not found", err)
	}
	if entitlement.Status == model.EntitlementStatusRevoked {
		return nil, serializer.NewError(serializer.CodeConflict, "This purchase was already refunded", nil)
	}

	order, err := s.store.GetOrder(c, entitlement.OrderNo)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", err)
	}
	if order.Status != model.OrderStatusPaid {
		return nil, serializer.NewError(serializer.CodeConflict, "Only a paid order can be refunded", nil)
	}

	// Refund window: buyers may ask for their money back for a limited time.
	// Zero disables the limit (the seller decides).
	if windowDays := s.Settings().RefundWindowDays; windowDays > 0 {
		deadline := order.PaidAt + int64(windowDays)*24*3600
		if order.PaidAt > 0 && time.Now().Unix() > deadline {
			return nil, serializer.NewError(serializer.CodeParamErr,
				fmt.Sprintf("The refund window of %d day(s) has passed", windowDays), nil)
		}
	}

	note := strings.TrimSpace(reason)
	if len([]rune(note)) > 500 {
		return nil, serializer.NewError(serializer.CodeParamErr, "The reason is too long", nil)
	}

	refund := &model.Refund{
		ID:            newID(),
		EntitlementID: entitlement.ID,
		OrderNo:       order.OrderNo,
		UID:           buyer.ID,
		ShopID:        order.ShopID,
		Amount:        order.Payable,
		Reason:        note,
		Status:        model.RefundStatusPending,
	}
	if err := s.store.CreateRefund(c, refund); err != nil {
		if !store.IsConflict(err) {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to create the refund request", err)
		}

		// One request per order: a pending one blocks a second ask, but a
		// refused one can be raised again (for example with more details).
		existing, getErr := s.store.GetRefundByOrder(c, order.OrderNo)
		if getErr != nil {
			return nil, serializer.NewError(serializer.CodeConflict, "A refund request already exists for this order", nil)
		}
		if existing.Status == model.RefundStatusApproved || existing.Status == model.RefundStatusRefunded ||
			existing.Status == model.RefundStatusConfirmed {
			return nil, serializer.NewError(serializer.CodeConflict, "This order was already refunded", nil)
		}
		if existing.Status == model.RefundStatusPending {
			return nil, serializer.NewError(serializer.CodeConflict, "A refund request is already pending", nil)
		}

		reset, err := s.store.ResetRefund(c, existing.ID, note)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to reopen the refund request", err)
		}
		if !reset {
			return nil, serializer.NewError(serializer.CodeConflict, "A refund request already exists for this order", nil)
		}

		return s.store.GetRefund(c, existing.ID)
	}
	return refund, nil
}

// MyRefunds lists the refund requests of the requester.
func (s *Service) MyRefunds(c *gin.Context, limit, offset int) ([]*model.Refund, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	refunds, err := s.store.ListRefunds(c, "", user.ID, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list refunds", err)
	}
	return refunds, nil
}

// ShopRefunds lists the refund requests received by the requester's shop.
func (s *Service) ShopRefunds(c *gin.Context, limit, offset int) ([]*RefundView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	refunds, err := s.store.ListRefunds(c, shop.ID, 0, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list refunds", err)
	}
	return s.decorateRefunds(c, refunds), nil
}

// AdminRefunds lists refund requests of every shop.
func (s *Service) AdminRefunds(c *gin.Context, limit, offset int) ([]*RefundView, error) {
	refunds, err := s.store.ListRefunds(c, "", 0, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list refunds", err)
	}
	return s.decorateRefunds(c, refunds), nil
}

// ReviewRefund drives a refund request through its state machine.
//
// Approving runs the same revocation path as the administrator action: the buyer
// loses access at once and, for a platform channel order, the seller's wallet is
// debited. The money of a merchant channel order is returned by the merchant
// itself, so the seller documents the refund and an administrator verifies the
// proof before the case is closed.
func (s *Service) ReviewRefund(c *gin.Context, id string, req *ReviewRefundRequest) (*model.Refund, error) {
	operator, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	refund, err := s.store.GetRefund(c, id)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Refund request not found", err)
	}

	isAdmin := s.isAdmin(c)
	// A seller may only review the requests of their own shop; an administrator
	// may review anything.
	if !isAdmin {
		shop, err := s.store.GetShopByID(c, refund.ShopID)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
		}
		if shop.UID != operator.ID {
			return nil, serializer.NewError(serializer.CodeNoPermissionErr, "Permission denied", nil)
		}
	}

	remark := strings.TrimSpace(req.Remark)

	switch req.Action {
	case "approve":
		if refund.Status != model.RefundStatusPending {
			return nil, serializer.NewError(serializer.CodeConflict, "This request was already reviewed", nil)
		}
		order, err := s.store.GetOrder(c, refund.OrderNo)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", err)
		}
		if err := s.refundOrder(c, order, firstNonEmpty(remark, refund.Reason, "买家申请退款")); err != nil {
			return nil, err
		}
		if ok, err := s.store.ReviewRefund(c, id, model.RefundStatusApproved, operator.ID, remark); err != nil || !ok {
			s.l.Warning("Refund %s was settled but its request could not be marked approved: %v %v", id, ok, err)
		}

	case "reject":
		if refund.Status != model.RefundStatusPending {
			return nil, serializer.NewError(serializer.CodeConflict, "This request was already reviewed", nil)
		}
		if ok, err := s.store.ReviewRefund(c, id, model.RefundStatusRejected, operator.ID, remark); err != nil || !ok {
			if err != nil {
				return nil, serializer.NewError(serializer.CodeDBError, "Failed to review the refund", err)
			}
			return nil, serializer.NewError(serializer.CodeConflict, "This request was already reviewed", nil)
		}

	case "evidence":
		// The merchant refunded the buyer through its own channel and reports
		// the proof; an administrator still has to verify it.
		if refund.Status != model.RefundStatusApproved {
			return nil, serializer.NewError(serializer.CodeConflict, "This refund is not waiting for a proof", nil)
		}
		order, err := s.store.GetOrder(c, refund.OrderNo)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "Order not found", err)
		}
		if order.PayMode.OrDefault() != model.PayModeCustom {
			return nil, serializer.NewError(serializer.CodeParamErr,
				"Only a refund of a merchant payment channel needs a proof", nil)
		}
		evidence := firstNonEmpty(req.Evidence, remark)
		if length := len([]rune(evidence)); length < 4 || length > 500 {
			return nil, serializer.NewError(serializer.CodeParamErr,
				"Please describe the refund: 交易号、退款凭证等", nil)
		}
		if ok, err := s.store.SubmitRefundEvidence(c, id, evidence); err != nil || !ok {
			if err != nil {
				return nil, serializer.NewError(serializer.CodeDBError, "Failed to submit the refund proof", err)
			}
			return nil, serializer.NewError(serializer.CodeConflict, "This refund is not waiting for a proof", nil)
		}

	case "confirm", "return":
		// Only the platform verifies whether the merchant really refunded the
		// buyer: the money never passed through the platform.
		if !isAdmin {
			return nil, serializer.NewError(serializer.CodeNoPermissionErr, "Only an administrator may verify a refund", nil)
		}
		if refund.Status != model.RefundStatusRefunded {
			return nil, serializer.NewError(serializer.CodeConflict, "This refund is not waiting for verification", nil)
		}
		if req.Action == "confirm" {
			if ok, err := s.store.ConfirmRefund(c, id, operator.ID, remark); err != nil || !ok {
				if err != nil {
					return nil, serializer.NewError(serializer.CodeDBError, "Failed to confirm the refund", err)
				}
				return nil, serializer.NewError(serializer.CodeConflict, "This refund is not waiting for verification", nil)
			}
			break
		}
		if ok, err := s.store.ReturnRefund(c, id, operator.ID, firstNonEmpty(remark, "退款凭证不足，请重新提交")); err != nil || !ok {
			if err != nil {
				return nil, serializer.NewError(serializer.CodeDBError, "Failed to return the refund", err)
			}
			return nil, serializer.NewError(serializer.CodeConflict, "This refund is not waiting for verification", nil)
		}

	default:
		return nil, serializer.NewError(serializer.CodeParamErr, "Unknown review action", nil)
	}

	return s.store.GetRefund(c, id)
}

func (s *Service) decorateRefunds(c *gin.Context, refunds []*model.Refund) []*RefundView {
	nicknames := make(map[int]string, 4)
	titles := make(map[string]string, len(refunds))
	payModes := make(map[string]model.PayMode, len(refunds))

	res := make([]*RefundView, 0, len(refunds))
	for _, refund := range refunds {
		view := &RefundView{Refund: refund}

		if _, ok := nicknames[refund.UID]; !ok {
			nickname := ""
			if buyer, err := s.dep.UserClient().GetByID(c, refund.UID); err == nil {
				nickname = buyer.Nick
			}
			nicknames[refund.UID] = nickname
		}
		view.BuyerNickname = nicknames[refund.UID]

		if _, ok := titles[refund.OrderNo]; !ok {
			title := ""
			if order, err := s.store.GetOrder(c, refund.OrderNo); err == nil {
				title = order.Title
				view.OrderStatus = order.Status
				payModes[refund.OrderNo] = order.PayMode.OrDefault()
			}
			titles[refund.OrderNo] = title
		}
		view.Title = titles[refund.OrderNo]
		view.PayMode = payModes[refund.OrderNo]

		res = append(res, view)
	}
	return res
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
