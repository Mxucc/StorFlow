package sharegate

import (
	"context"
	"errors"
	"testing"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory/types"
)

func paidShare(id int, price int64, owner *ent.User) *ent.Share {
	s := &ent.Share{ID: id, Props: &types.ShareProps{PayEnabled: true, Price: price}}
	if owner != nil {
		s.Edges.User = owner
	}
	return s
}

func TestPayableAndPrice(t *testing.T) {
	if Payable(nil) || Payable(&ent.Share{}) {
		t.Fatal("a share without props must be free")
	}
	if Payable(&ent.Share{Props: &types.ShareProps{PayEnabled: true}}) {
		t.Fatal("a paid share without a price must be treated as free")
	}

	share := paidShare(1, 1000, nil)
	if !Payable(share) || Price(share) != 1000 {
		t.Fatalf("unexpected paywall state: payable=%v price=%d", Payable(share), Price(share))
	}

	// A disabled paywall must not expose the price.
	share.Props.PayEnabled = false
	if Price(share) != 0 {
		t.Fatal("the price of a free share must be zero")
	}
}

func TestFreeShareIsAlwaysReadable(t *testing.T) {
	// Even with a checker installed, free shares must not consult it.
	Register(func(context.Context, *ent.Share, *ent.User) error {
		t.Fatal("the checker must not be called for a free share")
		return nil
	})
	defer Register(nil)

	share := &ent.Share{ID: 1, Props: &types.ShareProps{}}
	if err := CheckAccess(context.Background(), share, nil); err != nil {
		t.Fatalf("CheckAccess() on a free share failed: %v", err)
	}
}

func TestOwnerAlwaysKeepsAccess(t *testing.T) {
	owner := &ent.User{ID: 7}
	Register(func(context.Context, *ent.Share, *ent.User) error {
		return ErrPaymentRequired
	})
	defer Register(nil)

	share := paidShare(1, 500, owner)
	if err := CheckAccess(context.Background(), share, owner); err != nil {
		t.Fatalf("the owner must keep access: %v", err)
	}
}

func TestAnonymousBuyerIsGated(t *testing.T) {
	Register(func(context.Context, *ent.Share, *ent.User) error {
		t.Fatal("the checker must not be consulted for an anonymous requester")
		return nil
	})
	defer Register(nil)

	share := paidShare(1, 500, &ent.User{ID: 7})
	if err := CheckAccess(context.Background(), share, nil); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("expected ErrPaymentRequired, got %v", err)
	}
	if err := CheckAccess(context.Background(), share, &ent.User{ID: 0}); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("expected ErrPaymentRequired for a zero id, got %v", err)
	}
}

func TestCheckerDecidesForOtherUsers(t *testing.T) {
	entitled := &ent.User{ID: 42}

	Register(func(_ context.Context, _ *ent.Share, requester *ent.User) error {
		if requester.ID == entitled.ID {
			return nil
		}
		return ErrPaymentRequired
	})
	defer Register(nil)

	share := paidShare(1, 500, &ent.User{ID: 7})
	if err := CheckAccess(context.Background(), share, entitled); err != nil {
		t.Fatalf("an entitled buyer must pass: %v", err)
	}
	if err := CheckAccess(context.Background(), share, &ent.User{ID: 99}); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("a stranger must be gated, got %v", err)
	}

	if !HasAccess(context.Background(), share, entitled) {
		t.Fatal("HasAccess() must report the entitlement")
	}
	if HasAccess(context.Background(), share, &ent.User{ID: 99}) {
		t.Fatal("HasAccess() must not report access for a stranger")
	}
}

func TestWithoutCheckerPaidShareStaysReadable(t *testing.T) {
	// The core must keep working when the shop module is not installed: locking
	// every paid share out would break the instance.
	Register(nil)

	share := paidShare(1, 500, &ent.User{ID: 7})
	if err := CheckAccess(context.Background(), share, &ent.User{ID: 99}); err != nil {
		t.Fatalf("without a checker the share must stay readable: %v", err)
	}
}

func TestPreviewAllowanceIsScopedToTheShare(t *testing.T) {
	Register(func(context.Context, *ent.Share, *ent.User) error { return ErrPaymentRequired })
	defer Register(nil)

	share := paidShare(10, 500, &ent.User{ID: 7})
	other := paidShare(11, 500, &ent.User{ID: 7})

	ctx := WithPreviewAllowance(context.Background(), 10)
	if err := CheckAccess(ctx, share, nil); err != nil {
		t.Fatalf("the marked share must be readable for a preview: %v", err)
	}
	if err := CheckAccess(ctx, other, nil); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("the allowance must not leak to another share: %v", err)
	}
	if err := CheckAccess(context.Background(), share, nil); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("without the allowance the share must be gated: %v", err)
	}
}

func TestPermissionResolution(t *testing.T) {
	owner := &ent.User{ID: 7}
	share := &ent.Share{ID: 1, Props: &types.ShareProps{Permission: types.SharePermissionUpload}, Edges: ent.ShareEdges{User: owner}}

	if got := Permission(share, owner); got != types.SharePermissionEdit {
		t.Fatalf("the owner must resolve to edit, got %q", got)
	}
	if got := Permission(share, &ent.User{ID: 42}); got != types.SharePermissionUpload {
		t.Fatalf("unexpected permission %q", got)
	}
	if got := Permission(&ent.Share{}, nil); got != types.SharePermissionRead {
		t.Fatalf("a share without props must default to read, got %q", got)
	}

	share.Props.Grants = []types.ShareGrant{
		{UserID: "abc", Permission: types.SharePermissionEdit},
		{UserID: "def", Permission: types.SharePermissionUpload},
	}
	match := func(raw string) (int, bool) {
		switch raw {
		case "abc":
			return 42, true
		case "def":
			return 43, true
		}
		return 0, false
	}

	if got, ok := GrantPermission(share, 42, match); !ok || got != types.SharePermissionEdit {
		t.Fatalf("grant resolution failed: %q %v", got, ok)
	}
	if got, ok := GrantPermission(share, 43, match); !ok || got != types.SharePermissionUpload {
		t.Fatalf("grant resolution failed: %q %v", got, ok)
	}
	if _, ok := GrantPermission(share, 44, match); ok {
		t.Fatal("an unrelated user must not match a grant")
	}
	if _, ok := GrantPermission(share, 42, nil); ok {
		t.Fatal("without a decoder no grant can match")
	}
}
