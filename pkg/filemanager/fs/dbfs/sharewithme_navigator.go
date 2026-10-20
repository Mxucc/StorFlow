package dbfs

import (
	"context"
	"errors"
	"fmt"

	"github.com/Mxucc/StorFlow/application/constants"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/boolset"
	"github.com/Mxucc/StorFlow/pkg/cache"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/Mxucc/StorFlow/pkg/setting"
)

var sharedWithMeNavigatorCapability = &boolset.BooleanSet{}

// NewSharedWithMeNavigator creates a navigator for user's "shared with me" file system.
func NewSharedWithMeNavigator(u *ent.User, fileClient inventory.FileClient, l logging.Logger,
	config *setting.DBFS, hasher hashid.Encoder) Navigator {
	n := &sharedWithMeNavigator{
		user:       u,
		l:          l,
		fileClient: fileClient,
		config:     config,
		hasher:     hasher,
	}
	n.baseNavigator = newBaseNavigator(fileClient, defaultFilter, u, hasher, config)
	return n
}

type sharedWithMeNavigator struct {
	l          logging.Logger
	user       *ent.User
	fileClient inventory.FileClient
	config     *setting.DBFS
	hasher     hashid.Encoder

	root *File
	*baseNavigator
}

func (t *sharedWithMeNavigator) Recycle() {

}

func (n *sharedWithMeNavigator) PersistState(kv cache.Driver, key string) {
}

func (n *sharedWithMeNavigator) RestoreState(s State) error {
	return nil
}

func (t *sharedWithMeNavigator) To(ctx context.Context, path *fs.URI) (*File, error) {
	// Anonymous user does not have a trash folder.
	if inventory.IsAnonymousUser(t.user) {
		return nil, ErrLoginRequired
	}

	elements := path.Elements()
	if len(elements) > 1 {
		// Shared with me folder is a flatten tree, only root and one level of
		// shortcuts can be accessed.
		return nil, fs.ErrPathNotExist.WithError(fmt.Errorf("invalid Path %q", path))
	}

	if t.root == nil {
		rootFile, err := t.fileClient.Root(ctx, t.user)
		if err != nil {
			t.l.Info("User's root folder not found: %s, will initialize it.", err)
			return nil, ErrFsNotInitialized
		}

		t.root = newFile(nil, rootFile)
		rootPath := newSharedWithMeUri("")
		t.root.Path[pathIndexRoot], t.root.Path[pathIndexUser] = rootPath, rootPath
		t.root.OwnerModel = t.user
		t.root.IsUserRoot = true
		t.root.CapabilitiesBs = t.Capabilities(false).Capability
	}

	if len(elements) == 0 {
		return t.root, nil
	}

	// A single element addresses one shortcut. It is resolved into the symbolic
	// file itself, whose redirect metadata points at the shared content; read
	// paths follow that link (see DBFS.linkedFolderTarget).
	fileID, err := t.hasher.Decode(elements[0], hashid.FileID)
	if err != nil {
		return nil, fs.ErrPathNotExist.WithError(fmt.Errorf("invalid shortcut %q", elements[0]))
	}

	model, err := t.fileClient.GetByID(ctx, fileID)
	if err != nil {
		return nil, fs.ErrPathNotExist.WithError(err)
	}
	if model.OwnerID != t.user.ID || !model.IsSymbolic {
		return nil, fs.ErrPathNotExist.WithError(fmt.Errorf("shortcut %q does not belong to the user", elements[0]))
	}

	file := newFile(t.root, model)
	file.Path[pathIndexUser] = path.Root()
	return file, nil
}

func (t *sharedWithMeNavigator) Children(ctx context.Context, parent *File, args *ListArgs) (*ListResult, error) {
	args.SharedWithMe = true
	res, err := t.baseNavigator.children(ctx, nil, args)
	if err != nil {
		return nil, err
	}

	// Adding user uri for each file.
	for i := 0; i < len(res.Files); i++ {
		res.Files[i].Path[pathIndexUser] = newSharedWithMeUri(hashid.EncodeFileID(t.hasher, res.Files[i].Model.ID))
	}

	return res, nil
}

func (t *sharedWithMeNavigator) Capabilities(isSearching bool) *fs.NavigatorProps {
	res := &fs.NavigatorProps{
		Capability:            sharedWithMeNavigatorCapability,
		OrderDirectionOptions: fullOrderDirectionOption,
		OrderByOptions:        fullOrderByOption,
		MaxPageSize:           t.config.MaxPageSize,
	}

	if isSearching {
		res.OrderByOptions = searchLimitedOrderByOption
	}

	return res
}

func (t *sharedWithMeNavigator) Walk(ctx context.Context, levelFiles []*File, limit, depth int, f WalkFunc) error {
	return errors.New("not implemented")
}

func (n *sharedWithMeNavigator) FollowTx(ctx context.Context) (func(), error) {
	if _, ok := ctx.Value(inventory.TxCtx{}).(*inventory.Tx); !ok {
		return nil, fmt.Errorf("navigator: no inherited transaction found in context")
	}
	newFileClient, _, _, err := inventory.WithTx(ctx, n.fileClient)
	if err != nil {
		return nil, err
	}

	oldFileClient := n.fileClient
	revert := func() {
		n.fileClient = oldFileClient
		n.baseNavigator.fileClient = oldFileClient
	}

	n.fileClient = newFileClient
	n.baseNavigator.fileClient = newFileClient
	return revert, nil
}

func (n *sharedWithMeNavigator) ExecuteHook(ctx context.Context, hookType fs.HookType, file *File) error {
	return nil
}

func (n *sharedWithMeNavigator) GetView(ctx context.Context, file *File) *types.ExplorerView {
	if view, ok := n.user.Settings.FsViewMap[string(constants.FileSystemSharedWithMe)]; ok {
		return &view
	}
	return getDefaultView()
}
