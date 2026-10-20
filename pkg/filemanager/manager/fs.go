package manager

import (
	"context"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/cluster"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/cos"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/ks3"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/local"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/obs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/onedrive"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/oss"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/qiniu"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/remote"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/s3"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/upyun"
	"github.com/Mxucc/StorFlow/pkg/filemanager/driver/yun139"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/serializer"
)

func (m *manager) LocalDriver(policy *ent.StoragePolicy) driver.Handler {
	if policy == nil {
		policy = &ent.StoragePolicy{Type: types.PolicyTypeLocal, Settings: &types.PolicySetting{}}
	}
	return local.New(policy, m.l, m.config)
}

func (m *manager) CastStoragePolicyOnSlave(ctx context.Context, policy *ent.StoragePolicy) *ent.StoragePolicy {
	if !m.stateless {
		return policy
	}

	nodeId := cluster.NodeIdFromContext(ctx)
	if policy.Type == types.PolicyTypeRemote {
		if nodeId != policy.NodeID {
			return policy
		}

		policyCopy := *policy
		policyCopy.Type = types.PolicyTypeLocal
		return &policyCopy
	} else if policy.Type == types.PolicyTypeLocal {
		policyCopy := *policy
		policyCopy.NodeID = nodeId
		policyCopy.Type = types.PolicyTypeRemote
		policyCopy.SetNode(&ent.Node{
			ID:       nodeId,
			Server:   cluster.MasterSiteUrlFromContext(ctx),
			SlaveKey: m.config.Slave().Secret,
		})
		return &policyCopy
	} else if policy.Type == types.PolicyTypeOss {
		policyCopy := *policy
		if policyCopy.Settings != nil {
			policyCopy.Settings.ServerSideEndpoint = ""
		}
	}

	return policy
}

func (m *manager) GetStorageDriver(ctx context.Context, policy *ent.StoragePolicy) (driver.Handler, error) {
	switch policy.Type {
	case types.PolicyTypeLocal:
		return local.New(policy, m.l, m.config), nil
	case types.PolicyTypeRemote:
		return remote.New(ctx, policy, m.settings, m.config, m.l)
	case types.PolicyTypeOss:
		return oss.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeCos:
		return cos.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeS3:
		return s3.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeKs3:
		return ks3.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeObs:
		return obs.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeQiniu:
		return qiniu.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeUpyun:
		return upyun.New(ctx, policy, m.settings, m.config, m.l, m.dep.MimeDetector(ctx))
	case types.PolicyTypeOd:
		return onedrive.New(ctx, policy, m.settings, m.config, m.l, m.dep.CredManager())
	case types.PolicyTypeYun139:
		return yun139.New(ctx, policy, m.config, m.l)
	case types.PolicyTypeLoadBalance:
		// 负载均衡策略是复合策略，没有自己的 driver：实际文件总是落在被选中的子策略
		// 上（见 fs.ResolveLoadBalancePolicy），因此这里出现 load_balance 说明调用方
		// 漏做了解析。返回明确错误，避免继续按未知类型处理。
		return nil, ErrLoadBalanceNotResolved
	default:
		return nil, ErrUnknownPolicyType
	}
}

func (m *manager) getEntityPolicyDriver(cxt context.Context, e fs.Entity, policyOverwrite *ent.StoragePolicy) (*ent.StoragePolicy, driver.Handler, error) {
	policyID := e.PolicyID()
	var (
		policy *ent.StoragePolicy
		err    error
	)
	if policyID == 0 {
		policy = &ent.StoragePolicy{Type: types.PolicyTypeLocal, Settings: &types.PolicySetting{}}
	} else {
		if policyOverwrite != nil && policyOverwrite.ID == policyID {
			policy = policyOverwrite
		} else {
			policy, err = m.policyClient.GetPolicyByID(cxt, e.PolicyID())
			if err != nil {
				return nil, nil, serializer.NewError(serializer.CodeDBError, "failed to get policy", err)
			}
		}
	}

	d, err := m.GetStorageDriver(cxt, policy)
	if err != nil {
		return nil, nil, err
	}

	return policy, d, nil
}
