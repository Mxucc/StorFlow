package fs

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/serializer"
)

var (
	// ErrLoadBalanceEmpty is returned when a load_balance policy has no usable
	// sub storage policy configured.
	ErrLoadBalanceEmpty = serializer.NewError(serializer.CodePolicyNotExist,
		"Load balance policy has no sub storage policy configured", nil)
	// ErrLoadBalanceNested is returned when a load_balance policy references
	// another load_balance policy as its sub policy.
	ErrLoadBalanceNested = serializer.NewError(serializer.CodeInternalSetting,
		"Load balance policy cannot reference another load balance policy", nil)
)

// LoadBalanceWeight normalizes a load_balance sub policy weight: values <= 0 are
// treated as 1 so that a malformed entry can never break the selection.
func LoadBalanceWeight(weight int) int {
	if weight <= 0 {
		return 1
	}

	return weight
}

// PickLoadBalanceSubPolicy randomly selects one sub policy of the given
// load_balance policy settings, using the configured weight as relative
// probability. It returns nil if no sub policy is configured.
func PickLoadBalanceSubPolicy(setting *types.PolicySetting) *types.LoadBalanceSubPolicy {
	if setting == nil || len(setting.LoadBalanceSubPolicies) == 0 {
		return nil
	}

	subPolicies := setting.LoadBalanceSubPolicies
	total := 0
	for _, sub := range subPolicies {
		total += LoadBalanceWeight(sub.Weight)
	}

	// total is always >= len(subPolicies) >= 1, the fallback below only exists
	// for defensive completeness.
	if total <= 0 {
		return &subPolicies[0]
	}

	target := rand.IntN(total)
	acc := 0
	for i := range subPolicies {
		acc += LoadBalanceWeight(subPolicies[i].Weight)
		if target < acc {
			return &subPolicies[i]
		}
	}

	return &subPolicies[len(subPolicies)-1]
}

// ResolveLoadBalancePolicy resolves a load_balance composite policy into one of
// its sub policies (picked by weight). Any other policy type is returned as-is.
//
// Only the resolved sub policy is written into file entities
// (Entity.StoragePolicyID), so download, delete, relocate and driver
// initialization naturally operate on a concrete policy and never need to know
// about load_balance. A sub policy is not allowed to be another load_balance
// policy (enforced on save, re-checked here defensively).
//
// Whitelist tradeoff: when a user explicitly picks a storage policy, the group
// whitelist (types.AllowedStoragePolicies) is validated against the load_balance
// policy itself, see dbfs.validatePreferredPolicy. Its sub policies are
// configured by the administrator and trusted, so they are intentionally NOT
// re-validated against the requesting user's group: a single load_balance policy
// can be shared by groups with different whitelists, and group configuration can
// change after the policy was saved, so a static check is not possible.
func ResolveLoadBalancePolicy(ctx context.Context, policyClient inventory.StoragePolicyClient,
	policy *ent.StoragePolicy) (*ent.StoragePolicy, error) {
	if policy == nil || policy.Type != types.PolicyTypeLoadBalance {
		return policy, nil
	}

	sub := PickLoadBalanceSubPolicy(policy.Settings)
	if sub == nil {
		return nil, ErrLoadBalanceEmpty
	}

	subPolicy, err := policyClient.GetPolicyByID(ctx, sub.PolicyID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodePolicyNotExist,
			fmt.Sprintf("Load balance sub policy #%d not found", sub.PolicyID), err)
	}

	if subPolicy.Type == types.PolicyTypeLoadBalance {
		return nil, ErrLoadBalanceNested
	}

	return subPolicy, nil
}
