package fs

import (
	"context"
	"errors"
	"testing"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
)

type fakePolicyClient struct {
	policies map[int]*ent.StoragePolicy
}

func (f *fakePolicyClient) SetClient(*ent.Client) inventory.TxOperator { return f }
func (f *fakePolicyClient) GetClient() *ent.Client                    { return nil }

func (f *fakePolicyClient) GetByGroup(context.Context, *ent.Group) (*ent.StoragePolicy, error) {
	return nil, nil
}

func (f *fakePolicyClient) GetPolicyByID(_ context.Context, id int) (*ent.StoragePolicy, error) {
	p, ok := f.policies[id]
	if !ok {
		return nil, &ent.NotFoundError{}
	}

	return p, nil
}

func (f *fakePolicyClient) UpdateAccessKey(context.Context, *ent.StoragePolicy, string) error {
	return nil
}

func (f *fakePolicyClient) ListPolicyByType(context.Context, types.PolicyType) ([]*ent.StoragePolicy, error) {
	return nil, nil
}

func (f *fakePolicyClient) ListPolicies(context.Context, *inventory.ListPolicyParameters) (*inventory.ListPolicyResult, error) {
	return nil, nil
}

func (f *fakePolicyClient) Upsert(context.Context, *ent.StoragePolicy) (*ent.StoragePolicy, error) {
	return nil, nil
}

func (f *fakePolicyClient) Delete(context.Context, *ent.StoragePolicy) error { return nil }

func TestLoadBalanceWeight(t *testing.T) {
	if LoadBalanceWeight(0) != 1 || LoadBalanceWeight(-5) != 1 || LoadBalanceWeight(7) != 7 {
		t.Fatalf("unexpected weight normalization")
	}
}

func TestPickLoadBalanceSubPolicy(t *testing.T) {
	if PickLoadBalanceSubPolicy(nil) != nil {
		t.Fatalf("nil setting should return nil")
	}

	if PickLoadBalanceSubPolicy(&types.PolicySetting{}) != nil {
		t.Fatalf("empty sub policies should return nil")
	}

	setting := &types.PolicySetting{
		LoadBalanceSubPolicies: []types.LoadBalanceSubPolicy{
			{PolicyID: 11, Weight: 0}, // treated as 1
			{PolicyID: 22, Weight: 3},
		},
	}

	counts := map[int]int{}
	const rounds = 20000
	for i := 0; i < rounds; i++ {
		sub := PickLoadBalanceSubPolicy(setting)
		if sub == nil {
			t.Fatalf("unexpected nil sub policy")
		}
		counts[sub.PolicyID]++
	}

	if counts[11] == 0 || counts[22] == 0 {
		t.Fatalf("every sub policy should be picked at least once: %v", counts)
	}
	// weight 1 : 3 => ~25% / 75% with generous tolerance.
	ratio := float64(counts[11]) / float64(rounds)
	if ratio < 0.20 || ratio > 0.30 {
		t.Fatalf("weight distribution out of range: %v (ratio %f)", counts, ratio)
	}
}

func TestResolveLoadBalancePolicy(t *testing.T) {
	ctx := context.Background()
	client := &fakePolicyClient{policies: map[int]*ent.StoragePolicy{
		1: {ID: 1, Type: types.PolicyTypeLocal},
		2: {ID: 2, Type: types.PolicyTypeS3},
		3: {ID: 3, Type: types.PolicyTypeLoadBalance, Settings: &types.PolicySetting{
			LoadBalanceSubPolicies: []types.LoadBalanceSubPolicy{{PolicyID: 2, Weight: 1}},
		}},
		4: {ID: 4, Type: types.PolicyTypeLoadBalance, Settings: &types.PolicySetting{}},
		5: {ID: 5, Type: types.PolicyTypeLoadBalance, Settings: &types.PolicySetting{
			LoadBalanceSubPolicies: []types.LoadBalanceSubPolicy{{PolicyID: 3, Weight: 1}},
		}},
		6: {ID: 6, Type: types.PolicyTypeLoadBalance, Settings: &types.PolicySetting{
			LoadBalanceSubPolicies: []types.LoadBalanceSubPolicy{{PolicyID: 404, Weight: 1}},
		}},
	}}

	// Non load balance policies are returned as-is.
	res, err := ResolveLoadBalancePolicy(ctx, client, client.policies[1])
	if err != nil || res.ID != 1 {
		t.Fatalf("plain policy should be returned as-is: %v %v", res, err)
	}

	// load_balance resolves to its sub policy.
	res, err = ResolveLoadBalancePolicy(ctx, client, client.policies[3])
	if err != nil || res.ID != 2 {
		t.Fatalf("load balance should resolve to sub policy #2: %v %v", res, err)
	}

	// No sub policy configured.
	if _, err = ResolveLoadBalancePolicy(ctx, client, client.policies[4]); !errors.Is(err, ErrLoadBalanceEmpty) {
		t.Fatalf("expected ErrLoadBalanceEmpty, got %v", err)
	}

	// Nested load_balance is rejected.
	if _, err = ResolveLoadBalancePolicy(ctx, client, client.policies[5]); !errors.Is(err, ErrLoadBalanceNested) {
		t.Fatalf("expected ErrLoadBalanceNested, got %v", err)
	}

	// Missing sub policy is rejected.
	if _, err = ResolveLoadBalancePolicy(ctx, client, client.policies[6]); err == nil {
		t.Fatalf("expected error for missing sub policy")
	}
}
