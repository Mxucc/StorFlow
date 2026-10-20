package workflows

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/cluster"
	"github.com/Mxucc/StorFlow/pkg/queue"
	"github.com/Mxucc/StorFlow/pkg/util"
)

const (
	TaskTempPath                 = "fm_workflows"
	slaveProgressRefreshInterval = 5 * time.Second
)

type NodeState struct {
	NodeID int `json:"node_id"`
	// AllowedNodes 该任务允许使用的节点白名单（来自用户组「可用节点」设置，Pro）。
	// 为空表示不做限制。
	AllowedNodes []int `json:"allowed_nodes,omitempty"`

	progress queue.Progresses
}

// NodeSelection 任务的节点选择参数（Pro: 可用节点 / 允许选择节点）。
type NodeSelection struct {
	// NodeID 用户显式选择的处理节点 ID，0 表示由系统按权重分配。
	NodeID int
	// AllowedNodes 用户组允许的节点白名单，为空表示不做限制。
	AllowedNodes []int
}

// applyTo 把节点选择参数写入任务状态，供 allocateNode 使用。
func (s NodeSelection) applyTo(state *NodeState) {
	state.NodeID = s.NodeID
	state.AllowedNodes = s.AllowedNodes
}

// allocateNode allocates a node for the task.
func allocateNode(ctx context.Context, dep dependency.Dep, state *NodeState, capability types.NodeCapability) (cluster.Node, error) {
	np, err := dep.NodePool(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get node pool: %w", err)
	}

	node, err := np.Get(ctx, capability, state.NodeID, state.AllowedNodes)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	state.NodeID = node.ID()
	return node, nil
}

// prepareSlaveTaskCtx prepares the context for the slave task.
func prepareSlaveTaskCtx(ctx context.Context, props *types.SlaveTaskProps) context.Context {
	ctx = context.WithValue(ctx, cluster.SlaveNodeIDCtx{}, strconv.Itoa(props.NodeID))
	ctx = context.WithValue(ctx, cluster.MasterSiteUrlCtx{}, props.MasterSiteURl)
	ctx = context.WithValue(ctx, cluster.MasterSiteVersionCtx{}, props.MasterSiteVersion)
	ctx = context.WithValue(ctx, cluster.MasterSiteIDCtx{}, props.MasterSiteID)
	return ctx
}

func prepareTempFolder(ctx context.Context, dep dependency.Dep, t queue.Task) (string, error) {
	settings := dep.SettingProvider()
	tempPath := util.DataPath(path.Join(settings.TempPath(ctx), TaskTempPath, strconv.Itoa(t.ID())))
	if err := util.CreatNestedFolder(tempPath); err != nil {
		return "", fmt.Errorf("failed to create temp folder: %w", err)
	}

	dep.Logger().Info("Temp folder created: %s", tempPath)
	return tempPath, nil
}
