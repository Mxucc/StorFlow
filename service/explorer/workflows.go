package explorer

import (
	"encoding/gob"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/task"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/downloader"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs/dbfs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/filemanager/workflows"
	"github.com/Mxucc/StorFlow/pkg/queue"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
	"github.com/gofrs/uuid"
	"github.com/samber/lo"
	"golang.org/x/tools/container/intsets"
)


// resolveNodeSelection 解析并校验用户显式选择的处理节点（Pro: 可用节点 / 允许选择节点）。
// 未指定节点时返回用户组的可用节点白名单，任务会按权重在白名单内分配节点。
func resolveNodeSelection(c *gin.Context, user *ent.User, nodeIDHash string) (workflows.NodeSelection, error) {
	sel := workflows.NodeSelection{}
	if user != nil && user.Edges.Group != nil && user.Edges.Group.Settings != nil {
		sel.AllowedNodes = user.Edges.Group.Settings.AllowedNodes
	}

	if nodeIDHash == "" {
		return sel, nil
	}

	if user == nil || user.Edges.Group == nil || user.Edges.Group.Settings == nil ||
		!user.Edges.Group.Settings.AllowSelectNode {
		return sel, serializer.NewError(serializer.CodeGroupNotAllowed,
			"Your group is not allowed to select the processing node", nil)
	}

	dep := dependency.FromContext(c)
	nodeID, err := dep.HashIDEncoder().Decode(nodeIDHash, hashid.NodeID)
	if err != nil {
		return sel, serializer.NewError(serializer.CodeParamErr, "Unknown node ID", err)
	}

	if len(sel.AllowedNodes) > 0 && !lo.Contains(sel.AllowedNodes, nodeID) {
		return sel, serializer.NewError(serializer.CodeGroupNotAllowed,
			"The selected node is not available for your group", nil)
	}

	sel.NodeID = nodeID
	return sel, nil
}
// NodeResponse 用户可选的可用节点（Pro: 允许选择节点）。
type NodeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ListAvailableNodesResponse struct {
	Nodes []*NodeResponse `json:"nodes"`
}

// ListAvailableNodes 返回用户组可选的可用节点列表。
// 未开启「允许选择节点」时返回空列表，前端据此隐藏节点选择器。
func ListAvailableNodes(c *gin.Context) (*ListAvailableNodesResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	res := &ListAvailableNodesResponse{Nodes: make([]*NodeResponse, 0)}

	var allowed []int
	allowSelect := false
	if user != nil && user.Edges.Group != nil && user.Edges.Group.Settings != nil {
		allowed = user.Edges.Group.Settings.AllowedNodes
		allowSelect = user.Edges.Group.Settings.AllowSelectNode
	}
	if !allowSelect {
		return res, nil
	}

	nodes, err := dep.NodeClient().ListActiveNodes(c, nil)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list nodes", err)
	}

	encoder := dep.HashIDEncoder()
	for _, node := range nodes {
		if len(allowed) > 0 && !lo.Contains(allowed, node.ID) {
			continue
		}
		res.Nodes = append(res.Nodes, &NodeResponse{
			ID:   hashid.EncodeNodeID(encoder, node.ID),
			Name: node.Name,
		})
	}

	return res, nil
}

// ItemMoveService 处理多文件/目录移动
type ItemMoveService struct {
	SrcDir string        `json:"src_dir" binding:"required,min=1,max=65535"`
	Src    ItemIDService `json:"src"`
	Dst    string        `json:"dst" binding:"required,min=1,max=65535"`
}

// ItemRenameService 处理多文件/目录重命名
type ItemRenameService struct {
	Src     ItemIDService `json:"src"`
	NewName string        `json:"new_name" binding:"required,min=1,max=255"`
}

// ItemService 处理多文件/目录相关服务
type ItemService struct {
	Items []uint `json:"items"`
	Dirs  []uint `json:"dirs"`
}

// ItemIDService 处理多文件/目录相关服务，字段值为HashID，可通过Raw()方法获取原始ID
type ItemIDService struct {
	Items      []string `json:"items"`
	Dirs       []string `json:"dirs"`
	Source     *ItemService
	Force      bool `json:"force"`
	UnlinkOnly bool `json:"unlink"`
}

// ItemDecompressService 文件解压缩任务服务
type ItemDecompressService struct {
	Src      string `json:"src"`
	Dst      string `json:"dst" binding:"required,min=1,max=65535"`
	Encoding string `json:"encoding"`
}

// ItemPropertyService 获取对象属性服务
type ItemPropertyService struct {
	ID        string `binding:"required"`
	TraceRoot bool   `form:"trace_root"`
	IsFolder  bool   `form:"is_folder"`
}

func init() {
	gob.Register(ItemIDService{})
}

type (
	DownloadWorkflowService struct {
		Src     []string `json:"src"`
		SrcFile string   `json:"src_file"`
		Dst     string   `json:"dst" binding:"required"`
		// NodeID 用户显式选择的处理节点（hash id，Pro: 允许选择节点）。
		NodeID string `json:"node_id"`
	}
	CreateDownloadParamCtx struct{}
)

func (service *DownloadWorkflowService) CreateDownloadTask(c *gin.Context) ([]*TaskResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	if !user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionRemoteDownload)) {
		return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Group not allowed to download files", nil)
	}

	// Src must be set
	if service.SrcFile == "" && len(service.Src) == 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "No source files", nil)
	}

	// Only one of src and src_file can be set
	if service.SrcFile != "" && len(service.Src) > 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid source files", nil)
	}

	dst, err := fs.NewUriFromString(service.Dst)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid destination", err)
	}

	// Validate dst
	_, err = m.Get(c, dst, dbfs.WithRequiredCapabilities(dbfs.NavigatorCapabilityCreateFile))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid destination", err)
	}

	// 检查批量任务数量
	limit := user.Edges.Group.Settings.Aria2BatchSize
	if limit > 0 && len(service.Src) > limit {
		return nil, serializer.NewError(serializer.CodeBatchAria2Size, "", nil)
	}

	// Validate src file
	if service.SrcFile != "" {
		src, err := fs.NewUriFromString(service.SrcFile)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid source file uri", err)
		}

		_, err = m.Get(c, src, dbfs.WithRequiredCapabilities(dbfs.NavigatorCapabilityDownloadFile))
		if err != nil {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid source file", err)
		}
	}

	// 处理节点选择（Pro: 可用节点 / 允许选择节点）
	sel, err := resolveNodeSelection(c, user, service.NodeID)
	if err != nil {
		return nil, err
	}
	// batch creating tasks
	ae := serializer.NewAggregateError()
	tasks := make([]queue.Task, 0, len(service.Src))
	for _, src := range service.Src {
		if src == "" {
			continue
		}

		t, err := workflows.NewRemoteDownloadTask(c, src, service.SrcFile, service.Dst, sel)
		if err != nil {
			ae.Add(src, err)
			continue
		}

		if err := dep.RemoteDownloadQueue(c).QueueTask(c, t); err != nil {
			ae.Add(src, err)
		}

		tasks = append(tasks, t)
	}

	if service.SrcFile != "" {
		t, err := workflows.NewRemoteDownloadTask(c, "", service.SrcFile, service.Dst, sel)
		if err != nil {
			ae.Add(service.SrcFile, err)
		}

		if err := dep.RemoteDownloadQueue(c).QueueTask(c, t); err != nil {
			ae.Add(service.SrcFile, err)
		}

		tasks = append(tasks, t)
	}

	return lo.Map(tasks, func(item queue.Task, index int) *TaskResponse {
		return BuildTaskResponse(item, nil, hasher)
	}), ae.Aggregate()
}

type (
	ArchiveWorkflowService struct {
		Src      []string `json:"src" binding:"required"`
		Dst      string   `json:"dst" binding:"required"`
		Encoding string   `json:"encoding"`
		Password string   `json:"password"`
		FileMask []string `json:"file_mask"`
		// NodeID 用户显式选择的处理节点（hash id，Pro: 允许选择节点）。
		NodeID string `json:"node_id"`
	}
	CreateArchiveParamCtx struct{}
)

func (service *ArchiveWorkflowService) CreateExtractTask(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	if !user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionArchiveTask)) {
		return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Group not allowed to compress files", nil)
	}

	dst, err := fs.NewUriFromString(service.Dst)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid destination", err)
	}

	if len(service.Src) == 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "No source files", nil)
	}

	// Validate destination
	if _, err := m.Get(c, dst, dbfs.WithRequiredCapabilities(dbfs.NavigatorCapabilityCreateFile)); err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid destination", err)
	}

	// Create task
	sel, err := resolveNodeSelection(c, user, service.NodeID)
	if err != nil {
		return nil, err
	}

	t, err := workflows.NewExtractArchiveTask(c, service.Src[0], service.Dst, service.Encoding, service.Password, service.FileMask, sel)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to create task", err)
	}

	if err := dep.IoIntenseQueue(c).QueueTask(c, t); err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to queue task", err)
	}

	return BuildTaskResponse(t, nil, hasher), nil
}

// CreateCompressTask Create task to create an archive file
func (service *ArchiveWorkflowService) CreateCompressTask(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	if !user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionArchiveTask)) {
		return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Group not allowed to compress files", nil)
	}

	dst, err := fs.NewUriFromString(service.Dst)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid destination", err)
	}

	// Create a placeholder file then delete it to validate the destination
	session, err := m.PrepareUpload(c, &fs.UploadRequest{
		Props: &fs.UploadProps{
			Uri:             dst,
			Size:            0,
			UploadSessionID: uuid.Must(uuid.NewV4()).String(),
			ExpireAt:        time.Now().Add(time.Second * 3600),
		},
	})
	if err != nil {
		return nil, err
	}
	m.OnUploadFailed(c, session)

	// Create task
	sel, err := resolveNodeSelection(c, user, service.NodeID)
	if err != nil {
		return nil, err
	}

	t, err := workflows.NewCreateArchiveTask(c, service.Src, service.Dst, sel)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to create task", err)
	}

	if err := dep.IoIntenseQueue(c).QueueTask(c, t); err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to queue task", err)
	}

	return BuildTaskResponse(t, nil, hasher), nil
}

type (
	ImportWorkflowService struct {
		Src              string `json:"src" binding:"required"`
		Dst              string `json:"dst" binding:"required"`
		ExtractMediaMeta bool   `json:"extract_media_meta"`
		UserID           string `json:"user_id" binding:"required"`
		Recursive        bool   `json:"recursive"`
		PolicyID         int    `json:"policy_id" binding:"required"`
	}
	CreateImportParamCtx struct{}
)

func (service *ImportWorkflowService) CreateImportTask(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	if !user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Only admin can import files", nil)
	}

	userId, err := hasher.Decode(service.UserID, hashid.UserID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid user id", err)
	}

	owner, err := dep.UserClient().GetLoginUserByID(c, userId)
	if err != nil || owner.ID == 0 {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to get user", err)
	}

	dst, err := fs.NewUriFromString(fs.NewMyUri(service.UserID))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid destination", err)
	}

	// Create task
	t, err := workflows.NewImportTask(c, owner, service.Src, service.Recursive, dst.Join(service.Dst).String(), service.PolicyID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to create task", err)
	}

	if err := dep.IoIntenseQueue(c).QueueTask(c, t); err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to queue task", err)
	}

	return BuildTaskResponse(t, nil, hasher), nil
}

type (
	RelocateWorkflowService struct {
		// Src / FileIDs 二选一；gin 的 required_without + min 组合会误伤（空字段仍触发 min），
		// 因此去掉 binding，改为服务内手工校验
		Src         []string `json:"src"`
		FileIDs     []int    `json:"file_ids"`
		DstPolicyID int      `json:"dst_policy_id" binding:"required"`
	}
	CreateRelocateParamCtx struct{}
)

func (service *RelocateWorkflowService) CreateRelocateTask(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	// Relocating files is either an administrator action or a permission that a
	// group may grant to its members (Pro: 存储策略转移). The task itself runs as
	// the requesting user, so a member can only move their own files.
	group := user.Edges.Group
	if group == nil {
		return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Group permission denied", nil)
	}
	if !group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		if !group.Permissions.Enabled(int(types.GroupPermissionMigratePolicy)) {
			return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Group permission denied", nil)
		}
		// 迁移目的地必须落在用户组的可用存储策略范围内：既不能绕过用户组的存储
		// 策略限制，也不会因为组配置了多个可用策略而互相冲突。
		allowed := types.AllowedStoragePolicies(group.Settings, group.StoragePolicyID)
		if len(allowed) > 0 && !lo.Contains(allowed, service.DstPolicyID) {
			return nil, serializer.NewError(serializer.CodeGroupNotAllowed,
				"You can only relocate files to the storage policies available for your group", nil)
		}
	}

	if len(service.Src) == 0 && len(service.FileIDs) == 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Src 或 FileIDs 至少提供一个", nil)
	}

	t, err := workflows.NewRelocateTask(c, user, service.Src, service.FileIDs, service.DstPolicyID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to create task", err)
	}

	if err := dep.IoIntenseQueue(c).QueueTask(c, t); err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to queue task", err)
	}

	return BuildTaskResponse(t, nil, hasher), nil
}

type (
	ListTaskService struct {
		PageSize      int    `form:"page_size" binding:"required,min=10,max=100"`
		Category      string `form:"category" binding:"required,eq=general|eq=downloading|eq=downloaded"`
		NextPageToken string `form:"next_page_token"`
	}
	ListTaskParamCtx struct{}
)

func (service *ListTaskService) ListTasks(c *gin.Context) (*TaskListResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	taskClient := dep.TaskClient()

	args := &inventory.ListTaskArgs{
		PaginationArgs: &inventory.PaginationArgs{
			UseCursorPagination: true,
			PageToken:           service.NextPageToken,
			PageSize:            service.PageSize,
		},
		Types:  []string{queue.CreateArchiveTaskType, queue.ExtractArchiveTaskType, queue.RelocateTaskType, queue.ImportTaskType},
		UserID: user.ID,
	}

	if service.Category != "general" {
		args.Types = []string{queue.RemoteDownloadTaskType}
		if service.Category == "downloading" {
			args.PageSize = intsets.MaxInt
			args.Status = []task.Status{task.StatusSuspending, task.StatusProcessing, task.StatusQueued}
		} else if service.Category == "downloaded" {
			args.Status = []task.Status{task.StatusCanceled, task.StatusError, task.StatusCompleted}
		}
	}

	// Get tasks
	res, err := taskClient.List(c, args)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to query tasks", err)
	}

	tasks := make([]queue.Task, 0, len(res.Tasks))
	nodeMap := make(map[int]*ent.Node)
	for _, t := range res.Tasks {
		task, err := queue.NewTaskFromModel(t)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to parse task", err)
		}

		summary := task.Summarize(hasher)
		if summary != nil && summary.NodeID > 0 {
			if _, ok := nodeMap[summary.NodeID]; !ok {
				nodeMap[summary.NodeID] = nil
			}
		}
		tasks = append(tasks, task)
	}

	// Get nodes
	nodes, err := dep.NodeClient().ListActiveNodes(c, lo.Keys(nodeMap))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to query nodes", err)
	}
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	// Build response
	return BuildTaskListResponse(tasks, res, nodeMap, hasher), nil
}

func TaskPhaseProgress(c *gin.Context, taskID int) (queue.Progresses, error) {
	dep := dependency.FromContext(c)
	u := inventory.UserFromContext(c)
	r := dep.TaskRegistry()
	t, found := r.Get(taskID)
	if !found || (t.Owner().ID != u.ID && !u.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin))) {
		return queue.Progresses{}, nil
	}

	return t.Progress(c), nil
}

func CancelDownloadTask(c *gin.Context, taskID int) error {
	dep := dependency.FromContext(c)
	u := inventory.UserFromContext(c)
	r := dep.TaskRegistry()
	t, found := r.Get(taskID)
	if !found || t.Owner().ID != u.ID {
		return serializer.NewError(serializer.CodeNotFound, "Task not found", nil)
	}

	if downloadTask, ok := t.(*workflows.RemoteDownloadTask); ok {
		if err := downloadTask.CancelDownload(c); err != nil {
			return serializer.NewError(serializer.CodeInternalSetting, "Failed to cancel download task", err)
		}
	}

	return nil
}

type (
	SetDownloadFilesService struct {
		Files []*downloader.SetFileToDownloadArgs `json:"files" binding:"required"`
	}
	SetDownloadFilesParamCtx struct{}
)

func (service *SetDownloadFilesService) SetDownloadFiles(c *gin.Context, taskID int) error {
	dep := dependency.FromContext(c)
	u := inventory.UserFromContext(c)
	r := dep.TaskRegistry()

	t, found := r.Get(taskID)
	if !found || t.Owner().ID != u.ID {
		return serializer.NewError(serializer.CodeNotFound, "Task not found", nil)
	}

	status := t.Status()
	summary := t.Summarize(dep.HashIDEncoder())
	// Task must be in processing state
	if status != task.StatusSuspending && status != task.StatusProcessing {
		return serializer.NewError(serializer.CodeNotFound, "Task not in processing state", nil)
	}

	// Task must in monitoring loop
	if summary.Phase != workflows.RemoteDownloadTaskPhaseMonitor {
		return serializer.NewError(serializer.CodeNotFound, "Task not in monitoring loop", nil)
	}

	if downloadTask, ok := t.(*workflows.RemoteDownloadTask); ok {
		if err := downloadTask.SetDownloadTarget(c, service.Files...); err != nil {
			return serializer.NewError(serializer.CodeInternalSetting, "Failed to set download files", err)
		}
	}

	return nil
}

type (
	RebuildFTSIndexWorkflowService struct {
		FilteredStoragePolicy []int `json:"filtered_storage_policy"`
	}
	CreateRebuildFTSIndexParamCtx struct{}
)

func (service *RebuildFTSIndexWorkflowService) CreateRebuildFTSIndexTask(c *gin.Context) (*TaskResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	if !user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		return nil, serializer.NewError(serializer.CodeGroupNotAllowed, "Only admin can import files", nil)
	}

	// Create task
	t, err := workflows.NewRebuildIndexTask(c, user, service.FilteredStoragePolicy)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to create task", err)
	}

	if err := dep.MediaMetaQueue(c).QueueTask(c, t); err != nil {
		return nil, serializer.NewError(serializer.CodeCreateTaskError, "Failed to queue task", err)
	}

	return BuildTaskResponse(t, nil, hasher), nil
}
