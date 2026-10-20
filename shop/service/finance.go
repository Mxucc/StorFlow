package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// RequestWithdraw freezes balance and creates a payout request.
func (s *Service) RequestWithdraw(c *gin.Context, req *WithdrawRequest) (*model.Withdrawal, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}

	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	// A shop that has not paid the platform deposit cannot withdraw: the deposit
	// is the collateral the platform keeps.
	if due := s.DepositDue(shop); due > 0 {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr,
			fmt.Sprintf("Please pay the shop deposit of %s first", due), nil)
	}

	settings := s.Settings()
	amount := model.Money(req.Amount)
	if amount < settings.MinWithdraw {
		return nil, serializer.NewError(serializer.CodeParamErr,
			fmt.Sprintf("The minimum withdrawal is %s", settings.MinWithdraw), nil)
	}
	if amount > model.MaxPrice*100 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Withdrawal amount is too large", nil)
	}

	account := req.Account
	if err := account.Normalize(); err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid payout account", err)
	}

	fee := model.ApplyRate(amount, settings.WithdrawFeePercent)
	actual := amount - fee
	if actual <= 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "The payout fee exceeds the amount", nil)
	}

	withdrawal := &model.Withdrawal{
		ID:      newID(),
		ShopID:  shop.ID,
		Amount:  amount,
		Fee:     fee,
		Actual:  actual,
		Account: account,
		Status:  model.WithdrawalStatusPending,
	}

	// Freeze first: the money leaves the available balance before the request
	// exists, so a shop can never request more than it owns.
	if _, err := s.store.FreezeForWithdraw(c, shop.ID, amount, withdrawal.ID, "提现申请冻结"); err != nil {
		if err == store.ErrInsufficientBalance {
			return nil, serializer.NewError(serializer.CodeNoPermissionErr, "Insufficient balance", err)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to freeze the balance", err)
	}

	if err := s.store.CreateWithdrawal(c, withdrawal); err != nil {
		// Give the money back when the request could not be stored.
		if _, releaseErr := s.store.ReleaseFrozen(c, shop.ID, amount, withdrawal.ID, "提现申请失败回滚"); releaseErr != nil {
			s.l.Error("Failed to release frozen balance after a failed withdrawal: %s", releaseErr)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create the withdrawal", err)
	}

	return withdrawal, nil
}

// MyWithdrawals lists the payout requests of the requester's shop.
func (s *Service) MyWithdrawals(c *gin.Context, limit, offset int) ([]*model.Withdrawal, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	items, err := s.store.ListWithdrawals(c, shop.ID, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list withdrawals", err)
	}
	return items, nil
}

// AdminListWithdrawals lists payout requests of every shop.
func (s *Service) AdminListWithdrawals(c *gin.Context, limit, offset int) ([]*model.Withdrawal, error) {
	items, err := s.store.ListWithdrawals(c, "", limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list withdrawals", err)
	}
	return items, nil
}

// ReviewWithdraw settles a payout request:
//
//	reject  -> the frozen money goes back to the seller
//	approve -> the request is marked approved, money stays frozen
//	paid    -> the frozen money leaves the wallet
func (s *Service) ReviewWithdraw(c *gin.Context, id string, req *ReviewWithdrawalRequest) (*model.Withdrawal, error) {
	operator, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	withdrawal, err := s.store.GetWithdrawal(c, id)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Withdrawal not found", err)
	}
	if withdrawal.Status == model.WithdrawalStatusRejected || withdrawal.Status == model.WithdrawalStatusPaid {
		return nil, serializer.NewError(serializer.CodeConflict, "This withdrawal is already settled", nil)
	}

	action := strings.TrimSpace(req.Action)
	switch action {
	case "reject":
		ok, err := s.store.UpdateWithdrawalStatus(c, id, withdrawal.Status, model.WithdrawalStatusRejected, operator.ID, req.Remark)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to update the withdrawal", err)
		}
		if !ok {
			return nil, serializer.NewError(serializer.CodeConflict, "This withdrawal is already settled", nil)
		}
		if _, err := s.store.ReleaseFrozen(c, withdrawal.ShopID, withdrawal.Amount, id, "提现被驳回，余额退回"); err != nil {
			s.l.Error("Failed to release frozen balance of withdrawal %s: %s", id, err)
		}

	case "approve":
		ok, err := s.store.UpdateWithdrawalStatus(c, id, withdrawal.Status, model.WithdrawalStatusApproved, operator.ID, req.Remark)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to update the withdrawal", err)
		}
		if !ok {
			return nil, serializer.NewError(serializer.CodeConflict, "This withdrawal is already settled", nil)
		}

	case "paid":
		ok, err := s.store.UpdateWithdrawalStatus(c, id, withdrawal.Status, model.WithdrawalStatusPaid, operator.ID, req.Remark)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to update the withdrawal", err)
		}
		if !ok {
			return nil, serializer.NewError(serializer.CodeConflict, "This withdrawal is already settled", nil)
		}
		if _, err := s.store.SettleFrozen(c, withdrawal.ShopID, withdrawal.Amount, id, "提现已打款"); err != nil {
			s.l.Error("Failed to settle frozen balance of withdrawal %s: %s", id, err)
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to settle the balance", err)
		}

	default:
		return nil, serializer.NewError(serializer.CodeParamErr, "Unknown review action", nil)
	}

	return s.store.GetWithdrawal(c, id)
}

// CreateCoupon adds a discount code to the requester's shop.
func (s *Service) CreateCoupon(c *gin.Context, req *CouponRequest) (*model.Coupon, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	coupon, err := s.buildCoupon(c, shop.ID, nil, req)
	if err != nil {
		return nil, err
	}

	if err := s.store.CreateCoupon(c, coupon); err != nil {
		if store.IsConflict(err) {
			return nil, serializer.NewError(serializer.CodeConflict, "Coupon code already exists", err)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create the coupon", err)
	}
	return coupon, nil
}

// UpdateCoupon patches a coupon of the requester's shop.
func (s *Service) UpdateCoupon(c *gin.Context, id string, req *CouponRequest) (*model.Coupon, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	existing, err := s.store.GetCouponByID(c, id)
	if err != nil || existing.ShopID != shop.ID {
		return nil, serializer.NewError(serializer.CodeNotFound, "Coupon not found", err)
	}

	coupon, err := s.buildCoupon(c, shop.ID, existing, req)
	if err != nil {
		return nil, err
	}

	if err := s.store.UpdateCoupon(c, coupon); err != nil {
		if store.IsConflict(err) {
			return nil, serializer.NewError(serializer.CodeConflict, "Coupon code already exists", err)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update the coupon", err)
	}
	return coupon, nil
}

// ListCoupons lists the coupons of the requester's shop.
func (s *Service) ListCoupons(c *gin.Context, limit, offset int) ([]*model.Coupon, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	items, err := s.store.ListCoupons(c, shop.ID, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list coupons", err)
	}
	return items, nil
}

// buildCoupon validates a coupon request and merges it into existing.
func (s *Service) buildCoupon(c *gin.Context, shopID string, existing *model.Coupon, req *CouponRequest) (*model.Coupon, error) {
	coupon := existing
	if coupon == nil {
		coupon = &model.Coupon{
			ID:     newID(),
			ShopID: shopID,
			Code:   strings.ToUpper(strings.TrimSpace(req.Code)),
			Status: model.CouponStatusActive,
		}
		if coupon.Code == "" {
			coupon.Code = strings.ToUpper(newToken(4))
		}
	}

	if req.Code != "" {
		code := strings.ToUpper(strings.TrimSpace(req.Code))
		if len(code) > 32 || len(code) < 3 {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid coupon code", nil)
		}
		coupon.Code = code
	}

	kind := model.CouponKind(req.Kind)
	if !kind.Valid() {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid coupon kind", nil)
	}
	coupon.Kind = kind

	switch kind {
	case model.CouponKindPercent:
		if req.Value <= 0 || req.Value >= 100 {
			return nil, serializer.NewError(serializer.CodeParamErr, "A percentage coupon must be within 1%-99%", nil)
		}
	case model.CouponKindFixed:
		if req.Value < 1 || req.Value > int64(model.MaxPrice) {
			return nil, serializer.NewError(serializer.CodeParamErr, "A fixed coupon amount is out of range", nil)
		}
	}
	coupon.Value = req.Value

	if req.MinAmount < 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid minimum amount", nil)
	}
	coupon.MinAmount = model.Money(req.MinAmount)

	if req.ProductID != "" {
		product, err := s.store.GetProductByID(c, req.ProductID)
		if err != nil || product.ShopID != shopID {
			return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", nil)
		}
		coupon.ProductID = req.ProductID
	} else {
		coupon.ProductID = ""
	}

	if req.MaxUses < 0 || req.PerUser < 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid usage limit", nil)
	}
	coupon.MaxUses, coupon.PerUser = req.MaxUses, req.PerUser

	if req.StartAt < 0 || req.EndAt < 0 || (req.EndAt > 0 && req.StartAt > req.EndAt) {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid coupon validity window", nil)
	}
	coupon.StartAt, coupon.EndAt = req.StartAt, req.EndAt

	if req.Status != "" {
		switch model.CouponStatus(req.Status) {
		case model.CouponStatusActive, model.CouponStatusStopped:
			coupon.Status = model.CouponStatus(req.Status)
		default:
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid coupon status", nil)
		}
	}

	if coupon.EndAt > 0 && coupon.EndAt < time.Now().Unix() && coupon.Status == model.CouponStatusActive {
		return nil, serializer.NewError(serializer.CodeParamErr, "The coupon already expired", nil)
	}

	return coupon, nil
}
