// Package fileguard is the extension point that lets a module protect files
// from deletion.
//
// The shop module uses it to keep the promise it makes to buyers: once a folder
// was sold, the seller can no longer make the content disappear. Without a
// registered guard every file stays freely deletable, so the core keeps working
// standalone.
package fileguard

import (
	"context"
	"sync"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/pkg/serializer"
)

// Decision is the verdict of a guard.
type Decision struct {
	// Allowed reports whether the deletion may proceed.
	Allowed bool
	// Reason explains the refusal to the user; it is required when Allowed is
	// false.
	Reason string
}

// Guard inspects one deletion request:
//
//   - file is the item being deleted;
//   - hard is true for a permanent delete (and for emptying the trash), false
//     for a "move to trash";
//   - ancestors is the ancestor chain of file, nearest last, so a guard does not
//     have to walk the tree itself.
//
// A guard must be fast and must never mutate anything.
type Guard func(ctx context.Context, file *ent.File, hard bool, ancestors []*ent.File) *Decision

var (
	mu    sync.RWMutex
	guard Guard
)

// Register installs the process wide guard. Passing nil removes it.
func Register(g Guard) {
	mu.Lock()
	defer mu.Unlock()
	guard = g
}

// CheckDeletion asks the registered guard about a deletion. It returns nil when
// the deletion is allowed, and a user facing error otherwise.
func CheckDeletion(ctx context.Context, file *ent.File, hard bool, ancestors []*ent.File) error {
	mu.RLock()
	g := guard
	mu.RUnlock()

	if g == nil || file == nil {
		return nil
	}

	decision := g(ctx, file, hard, ancestors)
	if decision == nil || decision.Allowed {
		return nil
	}

	reason := decision.Reason
	if reason == "" {
		reason = "This item is protected and cannot be deleted"
	}
	return serializer.NewError(serializer.CodeNoPermissionErr, reason, nil)
}
