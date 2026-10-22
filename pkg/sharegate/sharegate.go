// Package sharegate is the extension point that lets paid share links (a Pro
// feature) be enforced inside the core file manager without making the core
// depend on the shop module.
//
// The shop module registers a Checker at start-up; the share navigator calls
// CheckAccess before exposing any content of a share whose Props.PayEnabled is
// set. When no checker is registered every shares stays accessible, so the
// core keeps working standalone.
package sharegate

import (
	"context"
	"errors"
	"sync"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/serializer"
)

// ErrPaymentRequired is returned when the requester has no entitlement for a
// paid share. Callers may map it to a user facing error.
var ErrPaymentRequired = serializer.NewError(serializer.CodeNoPermissionErr, "此分享为付费分享，请先购买", nil)

// Checker decides whether requester may access share. It must return nil when
// access is granted, ErrPaymentRequired when the content is gated, and any
// other error for unexpected failures.
type Checker func(ctx context.Context, share *ent.Share, requester *ent.User) error

var (
	mu      sync.RWMutex
	checker Checker
)

// ChannelProvider reports the payment channels a paid share may be bought with.
// The shop module installs it at start-up; without it the caller falls back to
// every channel of the epay protocol.
type ChannelProvider func() []string

var (
	channelMu       sync.RWMutex
	channelProvider ChannelProvider
)

// Register installs the process wide checker. Passing nil removes it.
func Register(c Checker) {
	mu.Lock()
	defer mu.Unlock()
	checker = c
}

// RegisterChannels installs the channel provider. Passing nil removes it.
func RegisterChannels(p ChannelProvider) {
	channelMu.Lock()
	defer channelMu.Unlock()
	channelProvider = p
}

// PayChannels returns the channels a buyer may pick for a paid share. An empty
// result means \"no provider installed\": the caller then offers them all.
func PayChannels() []string {
	channelMu.RLock()
	p := channelProvider
	channelMu.RUnlock()
	if p == nil {
		return nil
	}
	return p()
}

// Payable reports whether share is a paid share.
func Payable(share *ent.Share) bool {
	if share == nil || share.Props == nil {
		return false
	}
	return share.Props.PayEnabled && share.Props.Price > 0
}

// Price returns the price of the share in cents, or 0 for free shares.
func Price(share *ent.Share) int64 {
	if share == nil || share.Props == nil || !share.Props.PayEnabled {
		return 0
	}
	return share.Props.Price
}

// shareOwner returns the owner of a share. The ent accessor only works when the
// edge was eagerly loaded, so the already materialized edge is preferred; a
// missing owner must never be mistaken for "not the owner".
func shareOwner(share *ent.Share) *ent.User {
	if share == nil {
		return nil
	}
	if share.Edges.User != nil {
		return share.Edges.User
	}
	if owner, err := share.Edges.UserOrErr(); err == nil {
		return owner
	}
	return nil
}

// CheckAccess enforces the paywall of share for requester. Free shares are
// always allowed. The share owner always keeps access.
func CheckAccess(ctx context.Context, share *ent.Share, requester *ent.User) error {
	if !Payable(share) {
		return nil
	}

	// Metadata preview of a storefront page.
	if previewAllowed(ctx, share.ID) {
		return nil
	}

	if owner := shareOwner(share); owner != nil && requester != nil && requester.ID != 0 && owner.ID == requester.ID {
		return nil
	}

	mu.RLock()
	c := checker
	mu.RUnlock()
	if c == nil {
		// No shop module configured: keep the share readable instead of
		// locking the instance out of its own shares.
		return nil
	}

	if requester == nil || requester.ID == 0 {
		return ErrPaymentRequired
	}

	if err := c(ctx, share, requester); err != nil {
		if errors.Is(err, ErrPaymentRequired) {
			return ErrPaymentRequired
		}
		return err
	}
	return nil
}

// Permission resolves the effective collaboration permission of share for
// requester: per-user grants override the share wide default and the owner
// always gets full access.
func Permission(share *ent.Share, requester *ent.User) types.SharePermission {
	if share == nil || share.Props == nil {
		return types.SharePermissionRead
	}

	if requester != nil && requester.ID != 0 {
		if owner := shareOwner(share); owner != nil && owner.ID == requester.ID {
			return types.SharePermissionEdit
		}
	}

	return types.NormalizeSharePermission(share.Props.Permission)
}

// GrantPermission resolves the permission for requester taking per-user grants
// into account. grantMatch reports whether a matching grant was found.
func GrantPermission(share *ent.Share, requesterID int, match func(raw string) (int, bool)) (types.SharePermission, bool) {
	if share == nil || share.Props == nil || requesterID == 0 || match == nil {
		return types.SharePermissionRead, false
	}

	for _, g := range share.Props.Grants {
		if g.UserID == "" {
			continue
		}
		uid, ok := match(g.UserID)
		if !ok || uid != requesterID {
			continue
		}
		return types.NormalizeSharePermission(g.Permission), true
	}

	return types.SharePermissionRead, false
}

// HasAccess reports whether requester may read the share: free shares and
// shares owned by the requester always do, paid shares require an entitlement
// resolved by the registered checker.
func HasAccess(ctx context.Context, share *ent.Share, requester *ent.User) bool {
	if !Payable(share) {
		return true
	}
	return CheckAccess(ctx, share, requester) == nil
}

// previewCtxKey marks a request as a metadata-only preview performed by the
// shop storefront.
type previewCtxKey struct{}

// WithPreviewAllowance marks ctx so that the paywall lets the given share be
// read for a metadata preview. Only server side code (the shop storefront
// endpoint) sets this, and it is scoped to a single share and a single
// request, so a leaked share address still cannot be read by a visitor.
func WithPreviewAllowance(ctx context.Context, shareID int) context.Context {
	return context.WithValue(ctx, previewCtxKey{}, shareID)
}

func previewAllowed(ctx context.Context, shareID int) bool {
	marked, ok := ctx.Value(previewCtxKey{}).(int)
	return ok && marked != 0 && marked == shareID
}
