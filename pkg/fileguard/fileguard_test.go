package fileguard

import (
	"context"
	"testing"

	"github.com/Mxucc/StorFlow/ent"
)

func TestNoGuardAllowsEverything(t *testing.T) {
	Register(nil)

	file := &ent.File{ID: 1}
	if err := CheckDeletion(context.Background(), file, true, nil); err != nil {
		t.Fatalf("without a guard every deletion must be allowed: %v", err)
	}
	if err := CheckDeletion(context.Background(), nil, true, nil); err != nil {
		t.Fatalf("a nil file must be allowed: %v", err)
	}
}

func TestGuardDecides(t *testing.T) {
	Register(func(_ context.Context, file *ent.File, hard bool, ancestors []*ent.File) *Decision {
		if file.ID == 1 {
			return &Decision{Allowed: false, Reason: "protected"}
		}
		return &Decision{Allowed: true}
	})
	defer Register(nil)

	if err := CheckDeletion(context.Background(), &ent.File{ID: 1}, true, nil); err == nil {
		t.Fatal("the guard must be able to refuse a deletion")
	} else if err.Error() != "protected" {
		t.Fatalf("the reason must reach the user, got %q", err.Error())
	}

	if err := CheckDeletion(context.Background(), &ent.File{ID: 2}, true, nil); err != nil {
		t.Fatalf("an allowed deletion must pass: %v", err)
	}
}

func TestGuardReceivesContext(t *testing.T) {
	type ctxKey struct{}
	var (
		gotHard      bool
		gotAncestors int
	)

	Register(func(ctx context.Context, _ *ent.File, hard bool, ancestors []*ent.File) *Decision {
		if ctx.Value(ctxKey{}) == nil {
			t.Fatal("the guard must receive the request context")
		}
		gotHard = hard
		gotAncestors = len(ancestors)
		return &Decision{Allowed: true}
	})
	defer Register(nil)

	ctx := context.WithValue(context.Background(), ctxKey{}, true)
	ancestors := []*ent.File{{ID: 7}, {ID: 8}}
	if err := CheckDeletion(ctx, &ent.File{ID: 9}, true, ancestors); err != nil {
		t.Fatalf("CheckDeletion() failed: %v", err)
	}
	if !gotHard || gotAncestors != 2 {
		t.Fatalf("unexpected guard arguments: hard=%v ancestors=%d", gotHard, gotAncestors)
	}
}

func TestNilDecisionMeansAllowed(t *testing.T) {
	Register(func(context.Context, *ent.File, bool, []*ent.File) *Decision { return nil })
	defer Register(nil)

	if err := CheckDeletion(context.Background(), &ent.File{ID: 1}, false, nil); err != nil {
		t.Fatalf("a nil decision must be treated as allowed: %v", err)
	}
}
