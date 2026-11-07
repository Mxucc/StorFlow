package vas

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect/sql"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/walletledger"
	"github.com/Mxucc/StorFlow/pkg/boolset"

	// Registers the sqlite3 driver used by the ent client below.
	_ "github.com/Mxucc/StorFlow/inventory"
)

// newTestClient opens a throwaway database with the full ent schema.
func newTestClient(t *testing.T) *ent.Client {
	t.Helper()

	driver, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "vas.db"))
	if err != nil {
		t.Fatalf("failed to open the test database: %v", err)
	}
	client := ent.NewClient(ent.Driver(driver))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("failed to create the test schema: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newTestUser creates a group and a user, because the wallet rows reference a
// real user through a foreign key.
func newTestUser(t *testing.T, client *ent.Client, email string) *ent.User {
	t.Helper()
	ctx := context.Background()

	group, err := client.Group.Create().
		SetName("test-" + email).
		SetPermissions(&boolset.BooleanSet{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create the test group: %v", err)
	}
	user, err := client.User.Create().
		SetEmail(email).
		SetNick("wallet").
		SetGroupUsers(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create the test user: %v", err)
	}
	return user
}

// TestWalletCreditDebitAndLedger covers the wallet primitives: the snapshot row,
// the ledger trail behind it and the refusal to overdraw.
func TestWalletCreditDebitAndLedger(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	user := newTestUser(t, client, "wallet@example.com")

	if balance, err := CreditWallet(ctx, client, user.ID, 10_000, WalletReasonRecharge, "card A", "CARD-A"); err != nil {
		t.Fatalf("CreditWallet() failed: %v", err)
	} else if balance != 10_000 {
		t.Fatalf("expected a balance of 10000, got %d", balance)
	}

	if balance, err := DebitWallet(ctx, client, user.ID, 3_000, WalletReasonPurchase, "subscription", "ZX-1"); err != nil {
		t.Fatalf("DebitWallet() failed: %v", err)
	} else if balance != 7_000 {
		t.Fatalf("expected a balance of 7000, got %d", balance)
	}

	// Spending more than the wallet holds is refused and changes nothing.
	if _, err := DebitWallet(ctx, client, user.ID, 7_001, WalletReasonPurchase, "too much", "ZX-2"); !errors.Is(err, ErrInsufficientWallet) {
		t.Fatalf("expected ErrInsufficientWallet, got %v", err)
	}
	if got := GetWalletBalance(ctx, client, user.ID); got != 7_000 {
		t.Fatalf("a refused debit must not move money, balance is %d", got)
	}

	// The ledger explains every movement and sums up to the balance.
	rows, err := client.WalletLedger.Query().
		Where(walletledger.UserIDEQ(user.ID)).
		Order(ent.Asc(walletledger.FieldID)).
		All(ctx)
	if err != nil {
		t.Fatalf("failed to query the wallet ledger: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 ledger rows, got %d", len(rows))
	}
	var sum int64
	for _, row := range rows {
		sum += row.Delta
	}
	if sum != GetWalletBalance(ctx, client, user.ID) {
		t.Fatalf("the ledger (%d) does not match the balance (%d)", sum, GetWalletBalance(ctx, client, user.ID))
	}
	if rows[0].BalanceAfter != 10_000 || rows[1].BalanceAfter != 7_000 {
		t.Fatalf("unexpected balance snapshots: %d, %d", rows[0].BalanceAfter, rows[1].BalanceAfter)
	}
	if rows[1].Reason != WalletReasonPurchase || rows[1].Ref != "ZX-1" {
		t.Fatalf("unexpected ledger entry %+v", rows[1])
	}
}

// TestWalletExactBalanceCanBeSpent makes sure the boundary (balance == amount) is
// allowed: an off-by-one guard would make the last cent unspendable.
func TestWalletExactBalanceCanBeSpent(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	user := newTestUser(t, client, "exact@example.com")

	if _, err := CreditWallet(ctx, client, user.ID, 500, WalletReasonRecharge, "", ""); err != nil {
		t.Fatalf("CreditWallet() failed: %v", err)
	}
	if balance, err := DebitWallet(ctx, client, user.ID, 500, WalletReasonShopPay, "product", "ORDER-1"); err != nil {
		t.Fatalf("spending the exact balance must work: %v", err)
	} else if balance != 0 {
		t.Fatalf("expected an empty wallet, got %d", balance)
	}
	if _, err := DebitWallet(ctx, client, user.ID, 1, WalletReasonShopPay, "product", "ORDER-2"); !errors.Is(err, ErrInsufficientWallet) {
		t.Fatalf("an empty wallet must refuse a debit, got %v", err)
	}
}

// TestWalletKeepsOneRowPerUser checks that repeated movements reuse the snapshot
// row instead of creating a second wallet.
func TestWalletKeepsOneRowPerUser(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	user := newTestUser(t, client, "single@example.com")

	for i := 0; i < 3; i++ {
		if _, err := CreditWallet(ctx, client, user.ID, 100, WalletReasonRecharge, "", ""); err != nil {
			t.Fatalf("CreditWallet() failed: %v", err)
		}
	}

	rows, err := client.WalletBalance.Query().All(ctx)
	if err != nil {
		t.Fatalf("failed to query the wallets: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one wallet row, got %d", len(rows))
	}
	if rows[0].Balance != 300 {
		t.Fatalf("expected a balance of 300, got %d", rows[0].Balance)
	}
}

// TestBalanceGiftCodeCreditsTheWallet covers the card path: a "balance" card
// credits its face value and leaves a recharge entry in the ledger.
func TestBalanceGiftCodeCreditsTheWallet(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	user := newTestUser(t, client, "card@example.com")

	code, err := client.GiftCode.Create().
		SetCode("CARD-2500").
		SetProductType(ProductTypeBalance).
		SetNum(0).
		SetAmount(2_500).
		SetName("¥25 充值卡").
		Save(ctx)
	if err != nil {
		t.Fatalf("failed to create the gift code: %v", err)
	}

	if err := withWalletTx(ctx, client, func(tx *ent.Tx) error {
		return applyGiftCodeReward(ctx, tx, user, code)
	}); err != nil {
		t.Fatalf("applyGiftCodeReward() failed: %v", err)
	}

	if got := GetWalletBalance(ctx, client, user.ID); got != 2_500 {
		t.Fatalf("expected a balance of 2500, got %d", got)
	}
	rows, err := client.WalletLedger.Query().Where(walletledger.UserIDEQ(user.ID)).All(ctx)
	if err != nil {
		t.Fatalf("failed to query the wallet ledger: %v", err)
	}
	if len(rows) != 1 || rows[0].Reason != WalletReasonRecharge || rows[0].Ref != "CARD-2500" {
		t.Fatalf("unexpected ledger after the redemption: %+v", rows)
	}

	// An unknown card type must not credit anything.
	broken := &ent.GiftCode{ProductType: "bogus", Code: "X"}
	if err := withWalletTx(ctx, client, func(tx *ent.Tx) error {
		return applyGiftCodeReward(ctx, tx, user, broken)
	}); err == nil {
		t.Fatal("an unknown card type must be refused")
	}
	if got := GetWalletBalance(ctx, client, user.ID); got != 2_500 {
		t.Fatalf("a failed redemption must not move money, balance is %d", got)
	}
}

// TestGiftCodeExpiry covers the validity window of a card.
func TestGiftCodeExpiry(t *testing.T) {
	if giftCodeExpired(nil, 100) {
		t.Fatal("a missing card is not expired, it simply does not exist")
	}
	// Zero means "never expires".
	if giftCodeExpired(&ent.GiftCode{ExpiresAt: 0}, 1<<40) {
		t.Fatal("a card without an expiry must never expire")
	}
	card := &ent.GiftCode{ExpiresAt: 1_000}
	if giftCodeExpired(card, 999) {
		t.Fatal("a card must stay redeemable before its expiry")
	}
	if giftCodeExpired(card, 1_000) {
		t.Fatal("a card must stay redeemable at the exact expiry second")
	}
	if !giftCodeExpired(card, 1_001) {
		t.Fatal("a card must expire after its expiry second")
	}
}
