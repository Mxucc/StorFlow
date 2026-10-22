package service

import (
	"context"
	stdsql "database/sql"
	"path/filepath"
	"testing"

	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	_ "modernc.org/sqlite"
)

// newPaymentTestService builds a service over a throwaway database. The host
// dependency is not needed by the paths under test, so it stays nil on purpose.
func newPaymentTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()

	db, err := stdsql.Open("sqlite", filepath.Join(t.TempDir(), "shop.db"))
	if err != nil {
		t.Fatalf("failed to open the test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	st := store.New(db, store.DialectSQLite, nil)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}

	svc := &Service{
		store: st,
		l:     logging.NewConsoleLogger(logging.LevelWarning),
		settings: Settings{
			Enabled: true,
			ChannelConfigs: []model.PayChannelConfig{
				{Channel: model.ChannelAlipay, Enabled: true},
				{Channel: model.ChannelWxpay, Enabled: true},
			},
			FeePercent:       300,
			SettleDelayHours: 24,
			OrderTTL:         900,
		},
	}
	return svc, st
}

func createPaymentTestShop(t *testing.T, st *store.Store, uid int, slug string) *model.Shop {
	t.Helper()

	shop := &model.Shop{
		ID:     store.NewID(),
		UID:    uid,
		Slug:   slug,
		Name:   "Shop " + slug,
		Status: model.ShopStatusActive,
	}
	if err := st.CreateShop(context.Background(), shop); err != nil {
		t.Fatalf("CreateShop() failed: %v", err)
	}
	return shop
}

// TestDepositDueAndGatewayResolution covers the two payment modes and the deposit
// bookkeeping of a shop.
func TestDepositDueAndGatewayResolution(t *testing.T) {
	svc, _ := newPaymentTestService(t)

	// Without a configured deposit nothing is owed, even when the shop paid
	// nothing at all.
	if due := svc.DepositDue(&model.Shop{}); due != 0 {
		t.Fatalf("no deposit is required, got %d", due)
	}

	svc.settings.DepositAmount = 5_000
	shop := &model.Shop{DepositPaid: 2_000}
	if due := svc.DepositDue(shop); due != 3_000 {
		t.Fatalf("expected a 3000 cents top up, got %d", due)
	}
	shop.DepositPaid = 5_000
	if due := svc.DepositDue(shop); due != 0 {
		t.Fatalf("a paid deposit must not be due, got %d", due)
	}
	// Raising the requirement later makes the difference due again.
	svc.settings.DepositAmount = 6_000
	if due := svc.DepositDue(shop); due != 1_000 {
		t.Fatalf("expected a 1000 cents top up, got %d", due)
	}
	// Paying more than required never produces a negative debt.
	svc.settings.DepositAmount = 1_000
	if due := svc.DepositDue(shop); due != 0 {
		t.Fatalf("an overpaid deposit must not be due, got %d", due)
	}
	svc.settings.DepositAmount = 0

	// A platform shop without platform credentials cannot take payments.
	platformShop := &model.Shop{PayMode: model.PayModePlatform}
	if _, mode, err := svc.gatewayFor(platformShop, model.ChannelAlipay); err == nil {
		t.Fatal("an unconfigured platform gateway must be refused")
	} else if mode != model.PayModePlatform {
		t.Fatalf("unexpected mode %q", mode)
	}

	// 每个通道各有一套凭据：支付宝与微信可以指向不同的服务商。
	svc.settings.ChannelConfigs = []model.PayChannelConfig{
		{Channel: model.ChannelAlipay, Enabled: true, APIURL: "https://pay.alipay.test/", PID: "1000", Key: "alipay-key"},
		{Channel: model.ChannelWxpay, Enabled: true, APIURL: "https://pay.wxpay.test/", PID: "2000", Key: "wxpay-key"},
	}
	cfg, mode, err := svc.gatewayFor(platformShop, model.ChannelAlipay)
	if err != nil {
		t.Fatalf("gatewayFor(alipay) failed: %v", err)
	}
	if mode != model.PayModePlatform || cfg.Key != "alipay-key" || cfg.APIURL != "https://pay.alipay.test" {
		t.Fatalf("unexpected alipay gateway %+v (%q)", cfg, mode)
	}
	cfg, _, err = svc.gatewayFor(platformShop, model.ChannelWxpay)
	if err != nil {
		t.Fatalf("gatewayFor(wxpay) failed: %v", err)
	}
	if cfg.Key != "wxpay-key" || cfg.APIURL != "https://pay.wxpay.test" {
		t.Fatalf("unexpected wxpay gateway %+v", cfg)
	}
	// A channel that is not part of the configuration must be refused instead of
	// silently falling back to another provider.
	if _, _, err := svc.gatewayFor(platformShop, model.ChannelQqpay); err == nil {
		t.Fatal("a channel without configuration must be refused")
	}

	// A merchant channel needs the credentials of the shop itself.
	custom := &model.Shop{PayMode: model.PayModeCustom}
	if _, _, err := svc.gatewayFor(custom, model.ChannelAlipay); err == nil {
		t.Fatal("an incomplete merchant channel must be refused")
	}
	custom.EpayAPIURL = "https://pay.merchant.test/"
	custom.EpayPID = "3000"
	custom.EpayKey = "merchant-key"
	cfg, mode, err = svc.gatewayFor(custom, model.ChannelAlipay)
	if err != nil {
		t.Fatalf("gatewayFor() failed: %v", err)
	}
	if mode != model.PayModeCustom || cfg.Key != "merchant-key" || cfg.APIURL != "https://pay.merchant.test" {
		t.Fatalf("unexpected merchant gateway %+v (%q)", cfg, mode)
	}

	// 只有微信通道单独配了另一套凭据（支付宝留空）时：微信走自己的服务商，
	// 支付宝沿用店铺的共享凭据；没被列出的通道视为未开通。
	custom.EpayChannels = []model.PayChannelConfig{
		{Channel: model.ChannelAlipay, Enabled: true},
		{Channel: model.ChannelWxpay, Enabled: true, APIURL: "https://pay.wx-merchant.test/", PID: "9000", Key: "wx-merchant-key"},
	}
	cfg, _, err = svc.gatewayFor(custom, model.ChannelWxpay)
	if err != nil {
		t.Fatalf("gatewayFor(wxpay) failed: %v", err)
	}
	if cfg.Key != "wx-merchant-key" || cfg.APIURL != "https://pay.wx-merchant.test" {
		t.Fatalf("the per-channel credentials must win, got %+v", cfg)
	}
	cfg, _, err = svc.gatewayFor(custom, model.ChannelAlipay)
	if err != nil {
		t.Fatalf("gatewayFor(alipay) failed: %v", err)
	}
	if cfg.Key != "merchant-key" {
		t.Fatalf("alipay must inherit the shared credentials, got %+v", cfg)
	}
	if _, _, err := svc.gatewayFor(custom, model.ChannelQqpay); err == nil {
		t.Fatal("a channel missing from the shop configuration must be refused")
	}

	// The default channel never panics on a broken configuration.
	svc.settings.ChannelConfigs = nil
	if got := svc.defaultChannel(); got != model.ChannelAlipay {
		t.Fatalf("expected alipay as the fallback channel, got %q", got)
	}
	svc.settings.ChannelConfigs = []model.PayChannelConfig{
		{Channel: model.ChannelAlipay, Enabled: false, APIURL: "https://a.test/", PID: "1", Key: "k"},
		{Channel: model.ChannelQqpay, Enabled: true, APIURL: "https://q.test/", PID: "1", Key: "k"},
	}
	if got := svc.defaultChannel(); got != model.ChannelQqpay {
		t.Fatalf("expected the first usable channel, got %q", got)
	}
}

// TestDepositOrderSettlementRecordsTheDeposit checks that paying the deposit
// flips the order, accumulates it on the shop and leaves the wallet alone.
func TestDepositOrderSettlementRecordsTheDeposit(t *testing.T) {
	svc, st := newPaymentTestService(t)
	ctx := context.Background()
	shop := createPaymentTestShop(t, st, 31, "deposit-order")

	order := &model.Order{
		OrderNo:  "20240101000000DEPOSIT",
		Kind:     model.OrderKindDeposit,
		ShopID:   shop.ID,
		ObjectID: shop.ID,
		BuyerUID: shop.UID,
		Title:    "开店保证金",
		Amount:   5_000,
		Payable:  5_000,
		Status:   model.OrderStatusPending,
		Channel:  model.ChannelAlipay,
		PayMode:  model.PayModePlatform,
		ExpireAt: 3_600,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	if err := svc.settleDeposit(ctx, order, "EP-DEPOSIT", 5_000); err != nil {
		t.Fatalf("settleDeposit() failed: %v", err)
	}

	refreshed, err := st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if refreshed.DepositPaid != 5_000 || refreshed.DepositPaidAt == 0 {
		t.Fatalf("unexpected deposit state %+v", refreshed)
	}
	if due := svc.DepositDue(refreshed); due != 0 {
		t.Fatalf("the deposit must be settled, %d left", due)
	}

	paid, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if paid.Status != model.OrderStatusPaid || paid.SettleState != "" {
		t.Fatalf("unexpected order state %+v", paid)
	}

	// The deposit is held by the platform: it is neither wallet money nor a
	// ledger movement.
	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 0 || balance.Pending != 0 || balance.TotalIncome != 0 {
		t.Fatalf("the deposit must not reach the wallet: %+v", balance)
	}
	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("the deposit must not enter the ledger, got %d", total)
	}

	// A duplicated notification must not add the deposit twice.
	if err := svc.settleDeposit(ctx, order, "EP-DEPOSIT", 5_000); err != nil {
		t.Fatalf("settleDeposit() failed: %v", err)
	}
	refreshed, err = st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if refreshed.DepositPaid != 5_000 {
		t.Fatalf("a repeated notification credited the deposit twice: %d", refreshed.DepositPaid)
	}
}

// TestCustomChannelRefundLeavesTheWalletAlone checks the "the platform only
// records the order" rule for a refund of a merchant channel order.
func TestCustomChannelRefundLeavesTheWalletAlone(t *testing.T) {
	svc, st := newPaymentTestService(t)
	ctx := context.Background()
	shop := createPaymentTestShop(t, st, 32, "custom-refund")

	order := &model.Order{
		OrderNo:  "20240101000000CUSTOM",
		Kind:     model.OrderKindProduct,
		ShopID:   shop.ID,
		ObjectID: "product-custom",
		BuyerUID: 77,
		Title:    "Folder",
		Amount:   10_000,
		Payable:  10_000,
		Status:   model.OrderStatusPending,
		Channel:  model.ChannelAlipay,
		PayMode:  model.PayModeCustom,
		ExpireAt: 3_600,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	// The merchant collected the money on its own gateway, so the platform only
	// flips the order and grants the entitlement.
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "MERCHANT-1", 10_000, 0, "")
		if err != nil {
			return err
		}
		if !settled {
			t.Fatal("the first settlement must win")
		}
		return tx.CreateEntitlement(ctx, &model.Entitlement{
			ID:       store.NewID(),
			UID:      order.BuyerUID,
			Kind:     order.Kind,
			ObjectID: order.ObjectID,
			OrderNo:  order.OrderNo,
			ShopID:   shop.ID,
			Title:    order.Title,
			Status:   model.EntitlementStatusGranted,
		})
	}); err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	paid, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if err := svc.refundOrder(ctx, paid, "商家已线下退款"); err != nil {
		t.Fatalf("refundOrder() failed: %v", err)
	}

	refunded, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if refunded.Status != model.OrderStatusRefunded {
		t.Fatalf("unexpected order state %+v", refunded)
	}

	entitlement, err := st.GetEntitlement(ctx, order.BuyerUID, order.Kind, order.ObjectID)
	if err != nil {
		t.Fatalf("GetEntitlement() failed: %v", err)
	}
	if entitlement.Status != model.EntitlementStatusRevoked {
		t.Fatalf("the refund must revoke the entitlement, got %q", entitlement.Status)
	}

	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 0 || balance.Frozen != 0 || balance.Pending != 0 || balance.TotalIncome != 0 {
		t.Fatalf("a merchant channel refund must not move money: %+v", balance)
	}
	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("a merchant channel order must not write ledger entries, got %d", total)
	}
}

// TestPlatformRefundTakesHeldIncomeBack checks that a refund of an order whose
// income is still held by the platform empties the pending bucket instead of
// pushing the available balance negative.
func TestPlatformRefundTakesHeldIncomeBack(t *testing.T) {
	svc, st := newPaymentTestService(t)
	ctx := context.Background()
	shop := createPaymentTestShop(t, st, 33, "platform-refund")

	order := &model.Order{
		OrderNo:  "20240101000000PLATFORM",
		Kind:     model.OrderKindProduct,
		ShopID:   shop.ID,
		ObjectID: "product-platform",
		BuyerUID: 78,
		Title:    "Folder",
		Amount:   10_000,
		Payable:  10_000,
		Status:   model.OrderStatusPending,
		Channel:  model.ChannelAlipay,
		PayMode:  model.PayModePlatform,
		ExpireAt: 3_600,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	// Paid through the platform gateway: the income is held for the unfreeze
	// window (3% commission).
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-2", 10_000, 7_200, model.SettleStateHeld)
		if err != nil {
			return err
		}
		if !settled {
			t.Fatal("the first settlement must win")
		}
		if _, err := tx.CreditShopPending(ctx, shop.ID, 9_700, model.LedgerKindSalePending, order.OrderNo, "sale"); err != nil {
			return err
		}
		return tx.CreateEntitlement(ctx, &model.Entitlement{
			ID:       store.NewID(),
			UID:      order.BuyerUID,
			Kind:     order.Kind,
			ObjectID: order.ObjectID,
			OrderNo:  order.OrderNo,
			ShopID:   shop.ID,
			Title:    order.Title,
			Status:   model.EntitlementStatusGranted,
		})
	}); err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	paid, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if err := svc.refundOrder(ctx, paid, "买家申请退款"); err != nil {
		t.Fatalf("refundOrder() failed: %v", err)
	}

	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Pending != 0 || balance.Available != 0 || balance.TotalIncome != 0 {
		t.Fatalf("the held income must be taken back from pending: %+v", balance)
	}
	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("the ledger must be balanced after the refund, got %d", total)
	}
}

// TestRefundedDepositLeavesTheWalletAlone checks that refunding a deposit order
// never touches the wallet and makes the shop owe the deposit again.
func TestRefundedDepositLeavesTheWalletAlone(t *testing.T) {
	svc, st := newPaymentTestService(t)
	ctx := context.Background()
	shop := createPaymentTestShop(t, st, 34, "deposit-refund")

	order := &model.Order{
		OrderNo:  "20240101000000DEPREF",
		Kind:     model.OrderKindDeposit,
		ShopID:   shop.ID,
		ObjectID: shop.ID,
		BuyerUID: shop.UID,
		Title:    "开店保证金",
		Amount:   5_000,
		Payable:  5_000,
		Status:   model.OrderStatusPending,
		Channel:  model.ChannelAlipay,
		PayMode:  model.PayModePlatform,
		ExpireAt: 3_600,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}
	if err := svc.settleDeposit(ctx, order, "EP-DEPREF", 5_000); err != nil {
		t.Fatalf("settleDeposit() failed: %v", err)
	}

	paid, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if err := svc.refundOrder(ctx, paid, "关店退还保证金"); err != nil {
		t.Fatalf("refundOrder() failed: %v", err)
	}

	refreshed, err := st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if refreshed.DepositPaid != 0 {
		t.Fatalf("a refunded deposit must be owed again, got %d", refreshed.DepositPaid)
	}
	if due := svc.DepositDue(refreshed); svc.settings.DepositAmount != 0 && due != svc.settings.DepositAmount {
		t.Fatalf("unexpected outstanding deposit %d", due)
	}

	refunded, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if refunded.Status != model.OrderStatusRefunded {
		t.Fatalf("unexpected order state %+v", refunded)
	}

	// The deposit never reached the wallet, so the refund must not debit it.
	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 0 || balance.Frozen != 0 || balance.Pending != 0 || balance.TotalIncome != 0 {
		t.Fatalf("refunding a deposit must not move money: %+v", balance)
	}
	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("the ledger must stay empty, got %d", total)
	}
}
