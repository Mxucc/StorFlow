package store

import (
	"context"
	stdsql "database/sql"
	"path/filepath"
	"testing"

	"github.com/Mxucc/StorFlow/shop/model"
	_ "modernc.org/sqlite"
)

// newOrder inserts a pending order and returns it.
func newOrder(t *testing.T, st *Store, shop *model.Shop, orderNo string, kind model.OrderKind, payable model.Money) *model.Order {
	t.Helper()

	order := &model.Order{
		OrderNo:  orderNo,
		Kind:     kind,
		ShopID:   shop.ID,
		ObjectID: "object-" + orderNo,
		BuyerUID: shop.UID + 100,
		Title:    "Order " + orderNo,
		Amount:   payable,
		Payable:  payable,
		Status:   model.OrderStatusPending,
		Channel:  model.ChannelAlipay,
		PayMode:  model.PayModePlatform,
		ExpireAt: 3600,
	}
	if err := st.CreateOrder(context.Background(), order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}
	return order
}

// TestHeldIncomeLifecycle covers the wallet buckets behind "订单完成 + N 小时解冻":
// a platform sale is credited to pending first, released later, and only then
// becomes withdrawable.
func TestHeldIncomeLifecycle(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 11, "held")
	order := newOrder(t, st, shop, "20240101000000HELD", model.OrderKindProduct, 1_000)

	// The order is paid and its income is held for two hours.
	if err := st.Tx(ctx, func(tx *Tx) error {
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-1", 1_000, 2_000, model.SettleStateHeld)
		if err != nil {
			return err
		}
		if !settled {
			t.Fatal("the first settlement must win")
		}
		_, err = tx.CreditShopPending(ctx, shop.ID, 970, model.LedgerKindSalePending, order.OrderNo, "sale")
		return err
	}); err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Pending != 970 || balance.Available != 0 || balance.TotalIncome != 970 {
		t.Fatalf("unexpected held balance %+v", balance)
	}

	// The ledger stays the source of truth of the three buckets.
	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if total != balance.Available+balance.Frozen+balance.Pending {
		t.Fatalf("ledger total %d does not match the buckets of %+v", total, balance)
	}

	paid, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if paid.SettleState != model.SettleStateHeld || paid.SettleAt != 2_000 {
		t.Fatalf("unexpected settle snapshot %+v", paid)
	}

	// Only a matured order is released.
	if matured, err := st.ListMaturedSales(ctx, 1_999, 10); err != nil || len(matured) != 0 {
		t.Fatalf("an order that did not mature yet must not be listed: %v %d", err, len(matured))
	}
	matured, err := st.ListMaturedSales(ctx, 2_000, 10)
	if err != nil {
		t.Fatalf("ListMaturedSales() failed: %v", err)
	}
	if len(matured) != 1 || matured[0].OrderNo != order.OrderNo {
		t.Fatalf("unexpected matured sales %+v", matured)
	}

	held, err := st.LedgerAmountForRef(ctx, shop.ID, order.OrderNo, model.LedgerKindSalePending)
	if err != nil {
		t.Fatalf("LedgerAmountForRef() failed: %v", err)
	}
	if held != 970 {
		t.Fatalf("unexpected held amount %d", held)
	}

	if err := st.Tx(ctx, func(tx *Tx) error {
		released, err := tx.MarkOrderSettled(ctx, order.OrderNo)
		if err != nil {
			return err
		}
		if !released {
			t.Fatal("the first release must win")
		}
		_, err = tx.ReleasePending(ctx, shop.ID, held, order.OrderNo, "release")
		return err
	}); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	balance, err = st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Pending != 0 || balance.Available != 970 {
		t.Fatalf("unexpected released balance %+v", balance)
	}

	// A second release is refused by the state machine.
	if err := st.Tx(ctx, func(tx *Tx) error {
		released, err := tx.MarkOrderSettled(ctx, order.OrderNo)
		if err != nil {
			return err
		}
		if released {
			t.Fatal("an order must be released once")
		}
		return nil
	}); err != nil {
		t.Fatalf("repeated release failed: %v", err)
	}

	// The money is withdrawable now.
	if _, err := st.FreezeForWithdraw(ctx, shop.ID, 970, "w-1", ""); err != nil {
		t.Fatalf("FreezeForWithdraw() failed: %v", err)
	}
}

// TestDebitPendingTakesHeldIncomeBack covers the refund of an order whose income
// was never released to the seller.
func TestDebitPendingTakesHeldIncomeBack(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 12, "debit-held")

	if err := st.Tx(ctx, func(tx *Tx) error {
		_, err := tx.CreditShopPending(ctx, shop.ID, 500, model.LedgerKindSalePending, "order-1", "sale")
		return err
	}); err != nil {
		t.Fatalf("CreditShopPending() failed: %v", err)
	}

	if err := st.Tx(ctx, func(tx *Tx) error {
		_, err := tx.DebitPending(ctx, shop.ID, 500, model.LedgerKindAdjust, "order-1", "refund")
		return err
	}); err != nil {
		t.Fatalf("DebitPending() failed: %v", err)
	}

	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Pending != 0 || balance.Available != 0 || balance.TotalIncome != 0 {
		t.Fatalf("unexpected balance after the refund %+v", balance)
	}

	// Taking back more than what is held is refused instead of going negative.
	if err := st.Tx(ctx, func(tx *Tx) error {
		_, err := tx.DebitPending(ctx, shop.ID, 1, model.LedgerKindAdjust, "order-1", "refund")
		return err
	}); err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

// TestDepositIsRecordedOutsideTheWallet checks that the platform deposit (保证金)
// accumulates on the shop and never enters the wallet or the ledger.
func TestDepositIsRecordedOutsideTheWallet(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 13, "deposit")

	if err := st.MarkDepositPaid(ctx, shop.ID, 3_000); err != nil {
		t.Fatalf("MarkDepositPaid() failed: %v", err)
	}
	if err := st.MarkDepositPaid(ctx, shop.ID, 2_000); err != nil {
		t.Fatalf("MarkDepositPaid() failed: %v", err)
	}

	refreshed, err := st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if refreshed.DepositPaid != 5_000 || refreshed.DepositPaidAt == 0 {
		t.Fatalf("unexpected deposit state %+v", refreshed)
	}

	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 0 || balance.Frozen != 0 || balance.Pending != 0 {
		t.Fatalf("the deposit must not reach the wallet: %+v", balance)
	}

	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("the deposit must not enter the ledger, got %d", total)
	}

	if err := st.MarkDepositPaid(ctx, "missing-shop", 100); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for an unknown shop, got %v", err)
	}
}

// TestShopPaymentConfigurationRoundTrip stores the merchant channel credentials
// of a shop.
func TestShopPaymentConfigurationRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 14, "merchant-channel")

	if shop.PayMode.OrDefault() != model.PayModePlatform {
		t.Fatalf("a fresh shop must default to the platform channel, got %q", shop.PayMode)
	}

	shop.PayMode = model.PayModeCustom
	shop.EpayAPIURL = "https://pay.merchant.test"
	shop.EpayPID = "1001"
	shop.EpayKey = "secret-key"
	if err := st.UpdateShop(ctx, shop); err != nil {
		t.Fatalf("UpdateShop() failed: %v", err)
	}

	loaded, err := st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if loaded.PayMode != model.PayModeCustom || loaded.EpayAPIURL != "https://pay.merchant.test" ||
		loaded.EpayPID != "1001" || loaded.EpayKey != "secret-key" {
		t.Fatalf("unexpected payment configuration %+v", loaded)
	}

	// Updating the profile must not clear the channel or the deposit.
	if err := st.MarkDepositPaid(ctx, shop.ID, 1_500); err != nil {
		t.Fatalf("MarkDepositPaid() failed: %v", err)
	}
	loaded.Name = "Renamed"
	if err := st.UpdateShop(ctx, loaded); err != nil {
		t.Fatalf("UpdateShop() failed: %v", err)
	}
	loaded, err = st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if loaded.EpayKey != "secret-key" || loaded.DepositPaid != 1_500 {
		t.Fatalf("a profile update lost data: %+v", loaded)
	}
}

// TestRefundEvidenceTransitions covers the merchant channel refund flow: the
// seller documents the refund, the platform verifies it.
func TestRefundEvidenceTransitions(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 15, "evidence")

	refund := &model.Refund{
		ID:            NewID(),
		EntitlementID: "ent-1",
		OrderNo:       "20240101000000EVIDENCE",
		UID:           42,
		ShopID:        shop.ID,
		Amount:        1_000,
		Reason:        "not needed",
		Status:        model.RefundStatusPending,
	}
	if err := st.CreateRefund(ctx, refund); err != nil {
		t.Fatalf("CreateRefund() failed: %v", err)
	}

	if ok, err := st.ReviewRefund(ctx, refund.ID, model.RefundStatusApproved, 1, "ok"); err != nil || !ok {
		t.Fatalf("ReviewRefund() failed: %v %v", ok, err)
	}

	// A proof cannot be submitted twice.
	if ok, err := st.SubmitRefundEvidence(ctx, refund.ID, "支付宝交易号 20240101"); err != nil || !ok {
		t.Fatalf("SubmitRefundEvidence() failed: %v %v", ok, err)
	}
	if ok, err := st.SubmitRefundEvidence(ctx, refund.ID, "again"); err != nil || ok {
		t.Fatalf("a second proof must be refused: %v %v", ok, err)
	}

	stored, err := st.GetRefund(ctx, refund.ID)
	if err != nil {
		t.Fatalf("GetRefund() failed: %v", err)
	}
	if stored.Status != model.RefundStatusRefunded || stored.Evidence == "" || stored.EvidenceAt == 0 {
		t.Fatalf("unexpected refund %+v", stored)
	}

	// The administrator sends it back, then the seller submits a better proof.
	if ok, err := st.ReturnRefund(ctx, refund.ID, 1, "凭证不清晰"); err != nil || !ok {
		t.Fatalf("ReturnRefund() failed: %v %v", ok, err)
	}
	if ok, err := st.SubmitRefundEvidence(ctx, refund.ID, "微信支付单号 4200001"); err != nil || !ok {
		t.Fatalf("SubmitRefundEvidence() failed: %v %v", ok, err)
	}
	if ok, err := st.ConfirmRefund(ctx, refund.ID, 1, "已核实"); err != nil || !ok {
		t.Fatalf("ConfirmRefund() failed: %v %v", ok, err)
	}
	if ok, err := st.ConfirmRefund(ctx, refund.ID, 1, "again"); err != nil || ok {
		t.Fatalf("a second verification must be refused: %v %v", ok, err)
	}

	stored, err = st.GetRefund(ctx, refund.ID)
	if err != nil {
		t.Fatalf("GetRefund() failed: %v", err)
	}
	if stored.Status != model.RefundStatusConfirmed || stored.Verifier != 1 || stored.VerifiedAt == 0 {
		t.Fatalf("unexpected refund %+v", stored)
	}
}

// TestMigrateAddsColumnsToExistingTables makes sure an installation that was
// created before the payment modes existed is upgraded in place.
func TestMigrateAddsColumnsToExistingTables(t *testing.T) {
	ctx := context.Background()
	db, err := stdsql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("failed to open the test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	// The shop and wallet tables as they looked before the payment modes.
	legacy := []string{
		`CREATE TABLE crshop_shop (
			id TEXT NOT NULL PRIMARY KEY,
			uid INTEGER NOT NULL UNIQUE,
			slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT,
			avatar TEXT,
			cover TEXT,
			theme TEXT,
			status TEXT NOT NULL,
			referrer_rate INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE crshop_balance (
			shop_id TEXT NOT NULL PRIMARY KEY,
			available INTEGER NOT NULL,
			frozen INTEGER NOT NULL,
			total_income INTEGER NOT NULL,
			total_withdraw INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`INSERT INTO crshop_shop (id, uid, slug, name, status, referrer_rate, created_at, updated_at)
			VALUES ('legacy-shop', 21, 'legacy', 'Legacy', 'active', 0, 1, 1)`,
		`INSERT INTO crshop_balance (shop_id, available, frozen, total_income, total_withdraw, updated_at)
			VALUES ('legacy-shop', 700, 0, 700, 0, 1)`,
	}
	for _, stmt := range legacy {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("failed to create the legacy schema: %v", err)
		}
	}

	st := New(db, DialectSQLite, nil)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}
	// Running it again must be a no-op.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("a second Migrate() must succeed: %v", err)
	}

	shop, err := st.GetShopByID(ctx, "legacy-shop")
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if shop.PayMode != "" || shop.DepositPaid != 0 || shop.EpayKey != "" {
		t.Fatalf("unexpected upgraded shop %+v", shop)
	}

	balance, err := st.GetBalance(ctx, "legacy-shop")
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 700 || balance.Pending != 0 {
		t.Fatalf("unexpected upgraded balance %+v", balance)
	}
}
