package vas

import (
	"context"
	"errors"
	"fmt"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/walletbalance"
	"github.com/Mxucc/StorFlow/ent/walletledger"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// Reasons recorded in the wallet ledger.
const (
	// WalletReasonRecharge is a card (卡密) redemption.
	WalletReasonRecharge = "recharge"
	// WalletReasonPurchase is a purchase of a VAS product (subscription etc.).
	WalletReasonPurchase = "purchase"
	// WalletReasonShopPay is a purchase in a cloud drive shop.
	WalletReasonShopPay = "shop_pay"
	// WalletReasonRefund returns money of a refunded order to the buyer.
	WalletReasonRefund = "refund"
	// WalletReasonManual is an administrator adjustment.
	WalletReasonManual = "manual"
)

// ErrInsufficientWallet means the wallet cannot cover the requested debit.
var ErrInsufficientWallet = errors.New("钱包余额不足")

// GetWalletBalance returns the wallet balance of a user, in cents.
func GetWalletBalance(ctx context.Context, db *ent.Client, userID int) int64 {
	if db == nil || userID <= 0 {
		return 0
	}
	row, err := db.WalletBalance.Query().Where(walletbalance.UserIDEQ(userID)).First(ctx)
	if err != nil {
		return 0
	}
	return row.Balance
}

// CreditWallet adds amount (cents) to a user's wallet. It is a no-op for a non
// positive amount, so callers can pass a computed value without guarding.
func CreditWallet(ctx context.Context, db *ent.Client, userID int, amount int64, reason, note, ref string) (int64, error) {
	if amount <= 0 {
		return GetWalletBalance(ctx, db, userID), nil
	}
	var balance int64
	err := withWalletTx(ctx, db, func(tx *ent.Tx) error {
		var err error
		balance, err = CreditWalletTx(ctx, tx, userID, amount, reason, note, ref)
		return err
	})
	return balance, err
}

// DebitWallet takes amount (cents) from a user's wallet. It returns
// ErrInsufficientWallet when the wallet cannot cover it; the conditional update
// behind it makes that safe under concurrency.
func DebitWallet(ctx context.Context, db *ent.Client, userID int, amount int64, reason, note, ref string) (int64, error) {
	if amount <= 0 {
		return GetWalletBalance(ctx, db, userID), nil
	}
	var balance int64
	err := withWalletTx(ctx, db, func(tx *ent.Tx) error {
		var err error
		balance, err = DebitWalletTx(ctx, tx, userID, amount, reason, note, ref)
		return err
	})
	return balance, err
}

// CreditWalletTx credits the wallet inside an existing transaction.
func CreditWalletTx(ctx context.Context, tx *ent.Tx, userID int, amount int64, reason, note, ref string) (int64, error) {
	if amount <= 0 {
		return 0, nil
	}
	return walletChange(ctx, tx, userID, amount, reason, note, ref)
}

// DebitWalletTx debits the wallet inside an existing transaction.
func DebitWalletTx(ctx context.Context, tx *ent.Tx, userID int, amount int64, reason, note, ref string) (int64, error) {
	if amount <= 0 {
		return 0, nil
	}
	return walletChange(ctx, tx, userID, -amount, reason, note, ref)
}

// walletChange applies one movement and appends its ledger row.
//
// The snapshot row is changed by a conditional UPDATE (balance >= -delta for a
// debit), which is atomic on every supported database: two concurrent checkouts
// can never spend the same money twice.
func walletChange(ctx context.Context, tx *ent.Tx, userID int, delta int64, reason, note, ref string) (int64, error) {
	if userID <= 0 {
		return 0, fmt.Errorf("wallet: missing user")
	}
	if delta == 0 {
		return 0, nil
	}
	if err := ensureWalletRow(ctx, tx, userID); err != nil {
		return 0, err
	}

	upd := tx.WalletBalance.Update().Where(walletbalance.UserIDEQ(userID))
	if delta < 0 {
		upd = upd.Where(walletbalance.BalanceGTE(-delta))
	}
	affected, err := upd.AddBalance(delta).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("wallet: failed to update the balance: %w", err)
	}
	if affected == 0 {
		return 0, ErrInsufficientWallet
	}

	row, err := tx.WalletBalance.Query().Where(walletbalance.UserIDEQ(userID)).First(ctx)
	if err != nil {
		return 0, fmt.Errorf("wallet: failed to read the balance: %w", err)
	}

	creator := tx.WalletLedger.Create().
		SetUserID(userID).
		SetDelta(delta).
		SetBalanceAfter(row.Balance).
		SetReason(reason)
	if note != "" {
		creator = creator.SetNote(note)
	}
	if ref != "" {
		creator = creator.SetRef(ref)
	}
	if _, err := creator.Save(ctx); err != nil {
		return 0, fmt.Errorf("wallet: failed to record the movement: %w", err)
	}
	return row.Balance, nil
}

// ensureWalletRow creates the wallet row of a user on first use.
func ensureWalletRow(ctx context.Context, tx *ent.Tx, userID int) error {
	exists, err := tx.WalletBalance.Query().Where(walletbalance.UserIDEQ(userID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("wallet: failed to query the balance: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := tx.WalletBalance.Create().SetUserID(userID).SetBalance(0).Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			// Another request created it first: that is exactly what we want.
			return nil
		}
		return fmt.Errorf("wallet: failed to create the balance: %w", err)
	}
	return nil
}

// withWalletTx runs fn in a transaction and commits it.
func withWalletTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return fmt.Errorf("wallet: failed to start a transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("wallet: failed to commit: %w", err)
	}
	return nil
}

// WalletEntry is one movement as the console shows it.
type WalletEntry struct {
	Delta        int64  `json:"delta"`
	BalanceAfter int64  `json:"balance_after"`
	Reason       string `json:"reason"`
	Note         string `json:"note,omitempty"`
	Ref          string `json:"ref,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

// WalletView is the user-facing wallet state.
type WalletView struct {
	// Balance is the spendable amount, in cents.
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
	// History is the most recent movement, newest first.
	History []WalletEntry `json:"history"`
}

// WalletSummary returns the current user's balance and recent movements.
func WalletSummary(c *gin.Context) serializer.Response {
	db := dependency.FromContext(c).DBClient()
	user := currentUser(c)
	if user == nil || user.ID == 0 {
		return serializer.Err(c, serializer.NewError(serializer.CodeNoPermissionErr, "Login required", nil))
	}

	rows, err := db.WalletLedger.Query().
		Where(walletledger.UserIDEQ(user.ID)).
		Order(ent.Desc(walletledger.FieldCreatedAt), ent.Desc(walletledger.FieldID)).
		Limit(50).
		All(c)
	if err != nil {
		return serializer.DBErr(c, "Failed to load the wallet history", err)
	}

	history := make([]WalletEntry, 0, len(rows))
	for _, row := range rows {
		history = append(history, WalletEntry{
			Delta:        row.Delta,
			BalanceAfter: row.BalanceAfter,
			Reason:       row.Reason,
			Note:         row.Note,
			Ref:          row.Ref,
			CreatedAt:    row.CreatedAt.Unix(),
		})
	}

	return serializer.Response{Data: WalletView{
		Balance:  GetWalletBalance(c, db, user.ID),
		Currency: "CNY",
		History:  history,
	}}
}
