package store

import (
	"context"
	stdsql "database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Mxucc/StorFlow/shop/model"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) (*Store, *stdsql.DB) {
	t.Helper()

	db, err := stdsql.Open("sqlite", filepath.Join(t.TempDir(), "shop.db"))
	if err != nil {
		t.Fatalf("failed to open the test database: %v", err)
	}
	// A single connection keeps the in-file database free of lock contention.
	db.SetMaxOpenConns(1)

	st := New(db, DialectSQLite, nil)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return st, db
}

func createShop(t *testing.T, st *Store, uid int, slug string) *model.Shop {
	t.Helper()

	shop := &model.Shop{
		ID:     NewID(),
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

func TestMigrateIsIdempotent(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("a second Migrate() must succeed: %v", err)
	}
}

func TestShopLifecycle(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	shop := createShop(t, st, 7, "my-shop")

	byUID, err := st.GetShopByUID(ctx, 7)
	if err != nil {
		t.Fatalf("GetShopByUID() failed: %v", err)
	}
	if byUID.ID != shop.ID || byUID.Slug != "my-shop" {
		t.Fatalf("unexpected shop %+v", byUID)
	}

	bySlug, err := st.GetShopBySlug(ctx, "my-shop")
	if err != nil {
		t.Fatalf("GetShopBySlug() failed: %v", err)
	}
	if bySlug.UID != 7 {
		t.Fatalf("unexpected shop owner %d", bySlug.UID)
	}

	// A second shop for the same user is rejected by the unique constraint.
	dup := &model.Shop{ID: NewID(), UID: 7, Slug: "another", Name: "x", Status: model.ShopStatusActive}
	if err := st.CreateShop(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for a duplicate owner, got %v", err)
	}

	// Slug collisions are reported as conflicts as well.
	dupSlug := &model.Shop{ID: NewID(), UID: 8, Slug: "my-shop", Name: "x", Status: model.ShopStatusActive}
	if err := st.CreateShop(ctx, dupSlug); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for a duplicate slug, got %v", err)
	}

	shop.Status = model.ShopStatusDisabled
	shop.Theme = model.Theme{PrimaryColor: "#123456"}
	if err := st.UpdateShop(ctx, shop); err != nil {
		t.Fatalf("UpdateShop() failed: %v", err)
	}

	reloaded, err := st.GetShopByID(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetShopByID() failed: %v", err)
	}
	if reloaded.Status != model.ShopStatusDisabled || reloaded.Theme.PrimaryColor != "#123456" {
		t.Fatalf("the update was not persisted: %+v", reloaded)
	}

	if _, err := st.GetShopByUID(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBalanceLedgerAndWithdrawal(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 1, "seller")

	balance, err := st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 0 || balance.Frozen != 0 {
		t.Fatalf("a new shop must start empty, got %+v", balance)
	}

	if _, err := st.CreditShop(ctx, shop.ID, 10_000, model.LedgerKindSale, "order-1", "sale"); err != nil {
		t.Fatalf("CreditShop() failed: %v", err)
	}

	// Freezing more than available must fail without touching the balance.
	if _, err := st.FreezeForWithdraw(ctx, shop.ID, 20_000, "w-1", ""); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}

	balance, err = st.GetBalance(ctx, shop.ID)
	if err != nil {
		t.Fatalf("GetBalance() failed: %v", err)
	}
	if balance.Available != 10_000 {
		t.Fatalf("a rejected freeze must not move money, got %d", balance.Available)
	}

	// Freeze, then reject: the money returns to the available balance.
	if _, err := st.FreezeForWithdraw(ctx, shop.ID, 4_000, "w-2", ""); err != nil {
		t.Fatalf("FreezeForWithdraw() failed: %v", err)
	}
	balance, _ = st.GetBalance(ctx, shop.ID)
	if balance.Available != 6_000 || balance.Frozen != 4_000 {
		t.Fatalf("unexpected balance after freezing: %+v", balance)
	}

	if _, err := st.ReleaseFrozen(ctx, shop.ID, 4_000, "w-2", "rejected"); err != nil {
		t.Fatalf("ReleaseFrozen() failed: %v", err)
	}
	balance, _ = st.GetBalance(ctx, shop.ID)
	if balance.Available != 10_000 || balance.Frozen != 0 {
		t.Fatalf("unexpected balance after a rejection: %+v", balance)
	}

	// Freeze, then settle: the money leaves the wallet.
	if _, err := st.FreezeForWithdraw(ctx, shop.ID, 5_000, "w-3", ""); err != nil {
		t.Fatalf("FreezeForWithdraw() failed: %v", err)
	}
	if _, err := st.SettleFrozen(ctx, shop.ID, 5_000, "w-3", "paid"); err != nil {
		t.Fatalf("SettleFrozen() failed: %v", err)
	}
	balance, _ = st.GetBalance(ctx, shop.ID)
	if balance.Available != 5_000 || balance.Frozen != 0 || balance.TotalWithdraw != 5_000 {
		t.Fatalf("unexpected balance after settling: %+v", balance)
	}
	if balance.TotalIncome != 10_000 {
		t.Fatalf("total income must not change on payout, got %d", balance.TotalIncome)
	}

	ledger, err := st.ListLedger(ctx, shop.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListLedger() failed: %v", err)
	}
	// sale, freeze, refund, freeze, settle
	if len(ledger) != 5 {
		t.Fatalf("expected 5 ledger entries, got %d", len(ledger))
	}
	for _, entry := range ledger {
		if entry.Balance < 0 {
			t.Fatalf("a ledger entry must never report a negative balance: %+v", entry)
		}
	}
}

func TestWithdrawalReviewIsSingleShot(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 2, "payout")

	if _, err := st.CreditShop(ctx, shop.ID, 5_000, model.LedgerKindSale, "", ""); err != nil {
		t.Fatalf("CreditShop() failed: %v", err)
	}

	withdrawal := &model.Withdrawal{
		ID:      NewID(),
		ShopID:  shop.ID,
		Amount:  5_000,
		Actual:  5_000,
		Account: model.WithdrawalAccount{Channel: "alipay", Name: "seller", Account: "a@b.c"},
		Status:  model.WithdrawalStatusPending,
	}
	if _, err := st.FreezeForWithdraw(ctx, shop.ID, withdrawal.Amount, withdrawal.ID, ""); err != nil {
		t.Fatalf("FreezeForWithdraw() failed: %v", err)
	}
	if err := st.CreateWithdrawal(ctx, withdrawal); err != nil {
		t.Fatalf("CreateWithdrawal() failed: %v", err)
	}

	ok, err := st.UpdateWithdrawalStatus(ctx, withdrawal.ID, model.WithdrawalStatusPending,
		model.WithdrawalStatusPaid, 1, "done")
	if err != nil || !ok {
		t.Fatalf("the first review must succeed: %v %v", ok, err)
	}

	ok, err = st.UpdateWithdrawalStatus(ctx, withdrawal.ID, model.WithdrawalStatusPending,
		model.WithdrawalStatusPaid, 1, "again")
	if err != nil {
		t.Fatalf("a repeated review must not error: %v", err)
	}
	if ok {
		t.Fatal("a repeated review must not report a second settlement")
	}

	stored, err := st.GetWithdrawal(ctx, withdrawal.ID)
	if err != nil {
		t.Fatalf("GetWithdrawal() failed: %v", err)
	}
	if stored.Account.Channel != "alipay" || stored.Status != model.WithdrawalStatusPaid {
		t.Fatalf("unexpected withdrawal %+v", stored)
	}
}

func TestOrderAndEntitlement(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 3, "orders")

	order := &model.Order{
		OrderNo:  "20240101000000ABCDEF",
		Kind:     model.OrderKindProduct,
		ShopID:   shop.ID,
		ObjectID: "product-1",
		BuyerUID: 42,
		Title:    "Folder",
		Amount:   1_000,
		Payable:  1_000,
		Status:   model.OrderStatusPending,
		Channel:  model.ChannelAlipay,
		ExpireAt: 100,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	pending, err := st.GetPendingOrder(ctx, 42, model.OrderKindProduct, "product-1")
	if err != nil {
		t.Fatalf("GetPendingOrder() failed: %v", err)
	}
	if pending.OrderNo != order.OrderNo {
		t.Fatalf("unexpected pending order %+v", pending)
	}

	var entitlement *model.Entitlement
	if err := st.Tx(ctx, func(tx *Tx) error {
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-1", 1_000, 0, "")
		if err != nil {
			return err
		}
		if !settled {
			t.Fatal("the first settlement must win")
		}

		entitlement = &model.Entitlement{
			ID:       NewID(),
			UID:      42,
			Kind:     model.OrderKindProduct,
			ObjectID: "product-1",
			OrderNo:  order.OrderNo,
			ShopID:   shop.ID,
			Title:    "Folder",
			Status:   model.EntitlementStatusPending,
		}
		return tx.CreateEntitlement(ctx, entitlement)
	}); err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	// A duplicated notification must not settle twice.
	if err := st.Tx(ctx, func(tx *Tx) error {
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-1", 1_000, 0, "")
		if err != nil {
			return err
		}
		if settled {
			t.Fatal("a duplicated notification must not settle the order again")
		}
		return nil
	}); err != nil {
		t.Fatalf("repeated settlement failed: %v", err)
	}

	// The unique key (uid, kind, object) makes the grant idempotent.
	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.CreateEntitlement(ctx, &model.Entitlement{
			ID: NewID(), UID: 42, Kind: model.OrderKindProduct, ObjectID: "product-1",
			OrderNo: order.OrderNo, Status: model.EntitlementStatusPending,
		})
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict when granting twice, got %v", err)
	}

	stored, err := st.GetEntitlement(ctx, 42, model.OrderKindProduct, "product-1")
	if err != nil {
		t.Fatalf("GetEntitlement() failed: %v", err)
	}
	if stored.OrderNo != order.OrderNo {
		t.Fatalf("unexpected entitlement %+v", stored)
	}

	stored.Status = model.EntitlementStatusGranted
	stored.FolderID = 1234
	if err := st.UpdateEntitlement(ctx, stored); err != nil {
		t.Fatalf("UpdateEntitlement() failed: %v", err)
	}
	if refreshed, _ := st.GetEntitlementByID(ctx, stored.ID); refreshed.FolderID != 1234 {
		t.Fatalf("the delivery state was not persisted: %+v", refreshed)
	}

	if items, err := st.ListEntitlements(ctx, 42, 10, 0); err != nil || len(items) != 1 {
		t.Fatalf("ListEntitlements() = %v, %v", items, err)
	}

	if orders, err := st.ListOrdersByBuyer(ctx, 42, 10, 0); err != nil || len(orders) != 1 {
		t.Fatalf("ListOrdersByBuyer() = %v, %v", orders, err)
	}
}

func TestCloseExpiredOrders(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 4, "expiry")

	order := &model.Order{
		OrderNo:  "expired-order",
		Kind:     model.OrderKindProduct,
		ShopID:   shop.ID,
		ObjectID: "p",
		BuyerUID: 5,
		Payable:  100,
		Status:   model.OrderStatusPending,
		ExpireAt: 10,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	closed, err := st.CloseExpiredOrders(ctx, 100, 10)
	if err != nil {
		t.Fatalf("CloseExpiredOrders() failed: %v", err)
	}
	if closed != 1 {
		t.Fatalf("expected one closed order, got %d", closed)
	}

	reloaded, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil {
		t.Fatalf("GetOrder() failed: %v", err)
	}
	if reloaded.Status != model.OrderStatusClosed {
		t.Fatalf("unexpected status %q", reloaded.Status)
	}

	// Closing again is a no-op.
	if closed, err := st.CloseExpiredOrders(ctx, 100, 10); err != nil || closed != 0 {
		t.Fatalf("a second sweep closed %d orders (%v)", closed, err)
	}
}

func TestCouponUsageLimits(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 5, "coupons")

	coupon := &model.Coupon{
		ID:      NewID(),
		ShopID:  shop.ID,
		Code:    "WELCOME",
		Kind:    model.CouponKindPercent,
		Value:   10,
		MaxUses: 1,
		PerUser: 1,
		Status:  model.CouponStatusActive,
	}
	if err := st.CreateCoupon(ctx, coupon); err != nil {
		t.Fatalf("CreateCoupon() failed: %v", err)
	}

	if _, err := st.GetCouponByCode(ctx, shop.ID, "WELCOME"); err != nil {
		t.Fatalf("GetCouponByCode() failed: %v", err)
	}

	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.RedeemCoupon(ctx, coupon.ID, 1, "order-1", coupon.MaxUses)
	}); err != nil {
		t.Fatalf("the first redemption must succeed: %v", err)
	}

	// The total usage limit is enforced inside the transaction.
	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.RedeemCoupon(ctx, coupon.ID, 2, "order-2", coupon.MaxUses)
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for an exhausted coupon, got %v", err)
	}

	if used, err := st.CountCouponUses(ctx, coupon.ID, 1); err != nil || used != 1 {
		t.Fatalf("CountCouponUses() = %d, %v", used, err)
	}

	// A duplicate code inside one shop is rejected.
	dup := &model.Coupon{ID: NewID(), ShopID: shop.ID, Code: "WELCOME", Kind: model.CouponKindFixed, Value: 100}
	if err := st.CreateCoupon(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for a duplicate code, got %v", err)
	}
}

func TestReferralIsBoundOnce(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	referral := &model.Referral{ID: NewID(), InviterUID: 1, InviteeUID: 2, Rate: 1000}
	if err := st.CreateReferral(ctx, referral); err != nil {
		t.Fatalf("CreateReferral() failed: %v", err)
	}

	if err := st.CreateReferral(ctx, &model.Referral{ID: NewID(), InviterUID: 3, InviteeUID: 2}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for a second inviter, got %v", err)
	}

	if err := st.Tx(ctx, func(tx *Tx) error { return tx.AddReferralRebate(ctx, 2, 500) }); err != nil {
		t.Fatalf("AddReferralRebate() failed: %v", err)
	}

	stored, err := st.GetReferralByInvitee(ctx, 2)
	if err != nil {
		t.Fatalf("GetReferralByInvitee() failed: %v", err)
	}
	if stored.TotalRebate != 500 {
		t.Fatalf("unexpected rebate total %d", stored.TotalRebate)
	}

	items, err := st.ListReferrals(ctx, 1, 10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListReferrals() = %v, %v", items, err)
	}
}

func TestProductUpsertAndListing(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 6, "catalog")

	product := &model.Product{
		ID:      NewID(),
		ShopID:  shop.ID,
		FileID:  100,
		ShareID: 200,
		Name:    "Bundle",
		Price:   2_000,
		Status:  model.ProductStatusOnSale,
		Props:   model.ProductProps{Summary: "nice"},
	}
	if err := st.CreateProduct(ctx, product); err != nil {
		t.Fatalf("CreateProduct() failed: %v", err)
	}

	// One folder can only be listed once per shop.
	if err := st.CreateProduct(ctx, &model.Product{ID: NewID(), ShopID: shop.ID, FileID: 100, Name: "dup"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for a duplicated folder, got %v", err)
	}

	product.Price = 1_500
	product.Discount = 10
	product.Props.Tags = []string{"a"}
	if err := st.UpdateProduct(ctx, product); err != nil {
		t.Fatalf("UpdateProduct() failed: %v", err)
	}

	reloaded, err := st.GetProductByID(ctx, product.ID)
	if err != nil {
		t.Fatalf("GetProductByID() failed: %v", err)
	}
	if reloaded.Price != 1_500 || reloaded.Discount != 10 || len(reloaded.Props.Tags) != 1 {
		t.Fatalf("the update was not persisted: %+v", reloaded)
	}
	if reloaded.SharePassword != product.SharePassword {
		t.Fatalf("the internal share password must survive an update")
	}

	if _, err := st.GetProductByFileID(ctx, shop.ID, 100); err != nil {
		t.Fatalf("GetProductByFileID() failed: %v", err)
	}

	if err := st.IncreaseProductSales(ctx, product.ID, 2); err != nil {
		t.Fatalf("IncreaseProductSales() failed: %v", err)
	}

	public, err := st.ListProductsByShop(ctx, shop.ID, true, 10, 0)
	if err != nil || len(public) != 1 {
		t.Fatalf("ListProductsByShop() = %v, %v", public, err)
	}
	if public[0].Sales != 2 {
		t.Fatalf("unexpected sales counter %d", public[0].Sales)
	}

	// Off-sale products disappear from the storefront but stay in the shop.
	product.Status = model.ProductStatusOffSale
	if err := st.UpdateProduct(ctx, product); err != nil {
		t.Fatalf("UpdateProduct() failed: %v", err)
	}
	if public, _ := st.ListProductsByShop(ctx, shop.ID, true, 10, 0); len(public) != 0 {
		t.Fatalf("an off-sale product must not be public, got %v", public)
	}
	if all, _ := st.ListProductsByShop(ctx, shop.ID, false, 10, 0); len(all) != 1 {
		t.Fatalf("the seller must still see the product, got %v", all)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	if value, err := st.GetSetting(ctx, "missing"); err != nil || value != "" {
		t.Fatalf("GetSetting(missing) = %q, %v", value, err)
	}

	if err := st.SetSetting(ctx, "epay_pid", "1001"); err != nil {
		t.Fatalf("SetSetting() failed: %v", err)
	}
	if err := st.SetSetting(ctx, "epay_pid", "1002"); err != nil {
		t.Fatalf("SetSetting() must upsert: %v", err)
	}

	if value, err := st.GetSetting(ctx, "epay_pid"); err != nil || value != "1002" {
		t.Fatalf("GetSetting() = %q, %v", value, err)
	}

	all, err := st.AllSettings(ctx)
	if err != nil || all["epay_pid"] != "1002" {
		t.Fatalf("AllSettings() = %v, %v", all, err)
	}
}

func TestGetProductByShareID(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 9, "share-lookup")

	product := &model.Product{
		ID: NewID(), ShopID: shop.ID, FileID: 7, ShareID: 4242, Name: "Bundle",
		Price: 100, Status: model.ProductStatusOnSale,
	}
	if err := st.CreateProduct(ctx, product); err != nil {
		t.Fatalf("CreateProduct() failed: %v", err)
	}

	found, err := st.GetProductByShareID(ctx, 4242)
	if err != nil {
		t.Fatalf("GetProductByShareID() failed: %v", err)
	}
	if found.ID != product.ID {
		t.Fatalf("unexpected product %+v", found)
	}

	// A share that belongs to no product must be reported as missing, not as an
	// empty product: the paywall relies on this to decide access.
	if _, err := st.GetProductByShareID(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListEndpointsReturnEmptySlices(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 10, "empty-lists")

	if items, err := st.ListEntitlements(ctx, 123, 10, 0); err != nil || items == nil {
		t.Fatalf("ListEntitlements() must return an empty slice, got %v %v", items, err)
	}
	if items, err := st.ListOrdersByBuyer(ctx, 123, 10, 0); err != nil || items == nil {
		t.Fatalf("ListOrdersByBuyer() must return an empty slice, got %v %v", items, err)
	}
	if items, err := st.ListProductsByShop(ctx, shop.ID, true, 10, 0); err != nil || items == nil {
		t.Fatalf("ListProductsByShop() must return an empty slice, got %v %v", items, err)
	}
	if items, err := st.ListLedger(ctx, shop.ID, 10, 0); err != nil || items == nil {
		t.Fatalf("ListLedger() must return an empty slice, got %v %v", items, err)
	}
	if items, err := st.ListWithdrawals(ctx, shop.ID, 10, 0); err != nil || items == nil {
		t.Fatalf("ListWithdrawals() must return an empty slice, got %v %v", items, err)
	}
	if items, err := st.ListCoupons(ctx, shop.ID, 10, 0); err != nil || items == nil {
		t.Fatalf("ListCoupons() must return an empty slice, got %v %v", items, err)
	}
	if items, err := st.ListReferrals(ctx, 123, 10, 0); err != nil || items == nil {
		t.Fatalf("ListReferrals() must return an empty slice, got %v %v", items, err)
	}
}

func TestCouponReservationIsExclusive(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 11, "reserve")

	coupon := &model.Coupon{
		ID: NewID(), ShopID: shop.ID, Code: "ONCE", Kind: model.CouponKindFixed,
		Value: 100, MaxUses: 5, PerUser: 1, Status: model.CouponStatusActive,
	}
	if err := st.CreateCoupon(ctx, coupon); err != nil {
		t.Fatalf("CreateCoupon() failed: %v", err)
	}

	// The first reservation of a buyer succeeds...
	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.ReserveCoupon(ctx, coupon.ID, 5, "order-1", coupon.MaxUses, coupon.PerUser)
	}); err != nil {
		t.Fatalf("the first reservation must succeed: %v", err)
	}

	// ...a second one for the same buyer does not, even before paying.
	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.ReserveCoupon(ctx, coupon.ID, 5, "order-2", coupon.MaxUses, coupon.PerUser)
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for the second reservation, got %v", err)
	}

	// Another buyer is not blocked by the per user limit.
	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.ReserveCoupon(ctx, coupon.ID, 6, "order-3", coupon.MaxUses, coupon.PerUser)
	}); err != nil {
		t.Fatalf("another buyer must be able to reserve: %v", err)
	}

	// Expiring the first order releases its reservation.
	if err := st.CreateOrder(ctx, &model.Order{
		OrderNo: "order-1", Kind: model.OrderKindProduct, ShopID: shop.ID, ObjectID: "p",
		BuyerUID: 5, Payable: 100, Status: model.OrderStatusPending, ExpireAt: 1,
	}); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}
	if closed, err := st.CloseExpiredOrders(ctx, 100, 10); err != nil || closed != 1 {
		t.Fatalf("CloseExpiredOrders() = %d, %v", closed, err)
	}

	stored, err := st.GetCouponByID(ctx, coupon.ID)
	if err != nil {
		t.Fatalf("GetCouponByID() failed: %v", err)
	}
	if stored.Used != 1 {
		t.Fatalf("the released reservation must lower the counter, used = %d", stored.Used)
	}

	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.ReserveCoupon(ctx, coupon.ID, 5, "order-4", coupon.MaxUses, coupon.PerUser)
	}); err != nil {
		t.Fatalf("the buyer must be able to use the coupon again after the release: %v", err)
	}
}

func TestCouponGlobalLimitIsEnforced(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 12, "limit")

	coupon := &model.Coupon{
		ID: NewID(), ShopID: shop.ID, Code: "LAST", Kind: model.CouponKindPercent,
		Value: 10, MaxUses: 1, Status: model.CouponStatusActive,
	}
	if err := st.CreateCoupon(ctx, coupon); err != nil {
		t.Fatalf("CreateCoupon() failed: %v", err)
	}

	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.ReserveCoupon(ctx, coupon.ID, 1, "order-a", coupon.MaxUses, coupon.PerUser)
	}); err != nil {
		t.Fatalf("the first reservation must succeed: %v", err)
	}

	// The global limit stops every later reservation, whoever asks.
	if err := st.Tx(ctx, func(tx *Tx) error {
		return tx.ReserveCoupon(ctx, coupon.ID, 2, "order-b", coupon.MaxUses, coupon.PerUser)
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict once the coupon is exhausted, got %v", err)
	}
}

func TestRefundPrimitivesAndReconciliation(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 20, "refund")

	// A settled order: 5000 payable, 3% fee -> 4850 income.
	order := &model.Order{
		OrderNo: "refund-me", Kind: model.OrderKindProduct, ShopID: shop.ID, ObjectID: "p",
		BuyerUID: 21, Amount: 5000, Payable: 5000, Status: model.OrderStatusPending, Channel: model.ChannelAlipay,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	var entitlement *model.Entitlement
	if err := st.Tx(ctx, func(tx *Tx) error {
		if _, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-1", 5000, 0, ""); err != nil {
			return err
		}
		if _, err := tx.CreditShop(ctx, shop.ID, 4850, model.LedgerKindSale, order.OrderNo, "sale"); err != nil {
			return err
		}
		entitlement = &model.Entitlement{
			ID: NewID(), UID: 21, Kind: model.OrderKindProduct, ObjectID: "p",
			OrderNo: order.OrderNo, ShopID: shop.ID, Status: model.EntitlementStatusGranted,
		}
		return tx.CreateEntitlement(ctx, entitlement)
	}); err != nil {
		t.Fatalf("settlement failed: %v", err)
	}

	// The ledger must agree with the wallet snapshot.
	balance, _ := st.GetBalance(ctx, shop.ID)
	total, err := st.LedgerTotal(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LedgerTotal() failed: %v", err)
	}
	if balance.Available+balance.Frozen != total {
		t.Fatalf("balance %d/%d does not match the ledger %d", balance.Available, balance.Frozen, total)
	}

	// Refund: order -> refunded, entitlement revoked, wallet debited.
	if err := st.Tx(ctx, func(tx *Tx) error {
		refunded, err := tx.MarkOrderRefunded(ctx, order.OrderNo)
		if err != nil || !refunded {
			return errors.New("the first refund must succeed")
		}
		if err := tx.SetEntitlementStatus(ctx, entitlement.ID, model.EntitlementStatusRevoked); err != nil {
			return err
		}
		_, err = tx.DebitShop(ctx, shop.ID, 4850, model.LedgerKindAdjust, order.OrderNo, "refund")
		return err
	}); err != nil {
		t.Fatalf("refund failed: %v", err)
	}

	if err := st.Tx(ctx, func(tx *Tx) error {
		refunded, err := tx.MarkOrderRefunded(ctx, order.OrderNo)
		if err != nil {
			return err
		}
		if refunded {
			t.Fatal("a second refund must not be possible")
		}
		return nil
	}); err != nil {
		t.Fatalf("repeated refund failed: %v", err)
	}

	stored, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil || stored.Status != model.OrderStatusRefunded {
		t.Fatalf("unexpected order %+v (%v)", stored, err)
	}

	refreshed, err := st.GetEntitlementByID(ctx, entitlement.ID)
	if err != nil || refreshed.Status != model.EntitlementStatusRevoked {
		t.Fatalf("unexpected entitlement %+v (%v)", refreshed, err)
	}

	balance, _ = st.GetBalance(ctx, shop.ID)
	total, _ = st.LedgerTotal(ctx, shop.ID)
	if balance.Available != 0 {
		t.Fatalf("the refund must empty the wallet, got %d", balance.Available)
	}
	if balance.Available+balance.Frozen != total {
		t.Fatalf("after the refund the ledger drifted: %d vs %d", balance.Available+balance.Frozen, total)
	}
}

func TestLatePaymentOfAClosedOrderIsHonoured(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 30, "late")

	order := &model.Order{
		OrderNo: "late-order", Kind: model.OrderKindProduct, ShopID: shop.ID, ObjectID: "p",
		BuyerUID: 31, Payable: 500, Status: model.OrderStatusPending, ExpireAt: 10,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}

	if closed, err := st.CloseExpiredOrders(ctx, 100, 10); err != nil || closed != 1 {
		t.Fatalf("CloseExpiredOrders() = %d, %v", closed, err)
	}

	// The buyer paid after the deadline: the settlement must still go through.
	if err := st.Tx(ctx, func(tx *Tx) error {
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-LATE", 500, 0, "")
		if err != nil {
			return err
		}
		if !settled {
			t.Fatal("a late payment must settle the closed order")
		}
		return nil
	}); err != nil {
		t.Fatalf("late settlement failed: %v", err)
	}

	stored, err := st.GetOrder(ctx, order.OrderNo)
	if err != nil || stored.Status != model.OrderStatusPaid || stored.Paid != 500 {
		t.Fatalf("unexpected order %+v (%v)", stored, err)
	}

	// A refunded order must never be re-settled by a duplicated notification.
	if err := st.Tx(ctx, func(tx *Tx) error {
		refunded, err := tx.MarkOrderRefunded(ctx, order.OrderNo)
		if err != nil || !refunded {
			t.Fatalf("refund failed: %v", err)
		}
		settled, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-LATE", 500, 0, "")
		if err != nil {
			return err
		}
		if settled {
			t.Fatal("a refunded order must not be settled again")
		}
		return nil
	}); err != nil {
		t.Fatalf("refund/duplicate settlement failed: %v", err)
	}
}

func TestRefundRequests(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 40, "refunds")

	refund := &model.Refund{
		ID: NewID(), EntitlementID: "ent-1", OrderNo: "order-1", UID: 41,
		ShopID: shop.ID, Amount: 1000, Reason: "changed my mind", Status: model.RefundStatusPending,
	}
	if err := st.CreateRefund(ctx, refund); err != nil {
		t.Fatalf("CreateRefund() failed: %v", err)
	}

	// One request per order.
	if err := st.CreateRefund(ctx, &model.Refund{
		ID: NewID(), EntitlementID: "ent-1", OrderNo: "order-1", UID: 41, Status: model.RefundStatusPending,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected a conflict for a second request, got %v", err)
	}

	stored, err := st.GetRefundByOrder(ctx, "order-1")
	if err != nil || stored.Amount != 1000 || stored.Reason != "changed my mind" {
		t.Fatalf("unexpected refund %+v (%v)", stored, err)
	}

	// A request is reviewed exactly once.
	ok, err := st.ReviewRefund(ctx, refund.ID, model.RefundStatusApproved, 7, "ok")
	if err != nil || !ok {
		t.Fatalf("the first review must succeed: %v %v", ok, err)
	}
	ok, err = st.ReviewRefund(ctx, refund.ID, model.RefundStatusRejected, 8, "again")
	if err != nil {
		t.Fatalf("a repeated review must not error: %v", err)
	}
	if ok {
		t.Fatal("a repeated review must not report a second settlement")
	}

	if list, err := st.ListRefunds(ctx, shop.ID, 0, 10, 0); err != nil || len(list) != 1 {
		t.Fatalf("ListRefunds() = %v, %v", list, err)
	}
	if list, err := st.ListRefunds(ctx, "", 41, 10, 0); err != nil || len(list) != 1 {
		t.Fatalf("ListRefunds(by user) = %v, %v", list, err)
	}
	if list, err := st.ListRefunds(ctx, "", 999, 10, 0); err != nil || len(list) != 0 {
		t.Fatalf("ListRefunds(other user) = %v, %v", list, err)
	}
}

func TestFirstSaleAtAndSoldProducts(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	shop := createShop(t, st, 42, "first-sale")

	product := &model.Product{
		ID: NewID(), ShopID: shop.ID, FileID: 77, ShareID: 88, Name: "Bundle",
		Price: 100, Status: model.ProductStatusOnSale, Sales: 0,
	}
	if err := st.CreateProduct(ctx, product); err != nil {
		t.Fatalf("CreateProduct() failed: %v", err)
	}

	if at, err := st.FirstSaleAt(ctx, product.ID); err != nil || at != 0 {
		t.Fatalf("FirstSaleAt() = %d, %v", at, err)
	}
	if sold, err := st.ListProductsWithSales(ctx, 10); err != nil || len(sold) != 0 {
		t.Fatalf("ListProductsWithSales() = %v, %v", sold, err)
	}

	order := &model.Order{
		OrderNo: "sold-1", Kind: model.OrderKindProduct, ShopID: shop.ID, ObjectID: product.ID,
		BuyerUID: 43, Payable: 100, Status: model.OrderStatusPending,
	}
	if err := st.CreateOrder(ctx, order); err != nil {
		t.Fatalf("CreateOrder() failed: %v", err)
	}
	if err := st.Tx(ctx, func(tx *Tx) error {
		_, err := tx.MarkOrderPaid(ctx, order.OrderNo, "EP-1", 100, 0, "")
		return err
	}); err != nil {
		t.Fatalf("settlement failed: %v", err)
	}
	if err := st.IncreaseProductSales(ctx, product.ID, 1); err != nil {
		t.Fatalf("IncreaseProductSales() failed: %v", err)
	}

	if at, err := st.FirstSaleAt(ctx, product.ID); err != nil || at == 0 {
		t.Fatalf("FirstSaleAt() after the sale = %d, %v", at, err)
	}
	if sold, err := st.ListProductsWithSales(ctx, 10); err != nil || len(sold) != 1 {
		t.Fatalf("ListProductsWithSales() = %v, %v", sold, err)
	}
	if found, err := st.GetProductByFileIDAnyShop(ctx, 77); err != nil || found.ID != product.ID {
		t.Fatalf("GetProductByFileIDAnyShop() = %+v, %v", found, err)
	}
}
