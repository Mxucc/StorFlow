// Package shoplink is the extension point between the host registration flow
// and the shop module.
//
// The host must not import the shop module: the shop module depends on the
// share and explorer services, which depend on the user service, so a direct
// import would close a dependency cycle. The registration flow therefore only
// knows about this tiny interface and the shop module installs the real
// implementation at start-up.
package shoplink

import (
	"context"
	"sync"
)

// ReferralBinder records that inviteeUID was invited through the referral code
// ref. Implementations must never block or fail registration: a broken
// referral link is not a reason to reject a sign-up.
type ReferralBinder func(ctx context.Context, ref string, inviteeUID int)

var (
	mu     sync.RWMutex
	binder ReferralBinder
)

// Register installs the referral binder. Passing nil removes it.
func Register(b ReferralBinder) {
	mu.Lock()
	defer mu.Unlock()
	binder = b
}

// BindReferral forwards the invitation to the registered binder, if any.
func BindReferral(ctx context.Context, ref string, inviteeUID int) {
	mu.RLock()
	b := binder
	mu.RUnlock()

	if b == nil || ref == "" || inviteeUID <= 0 {
		return
	}
	b(ctx, ref, inviteeUID)
}
