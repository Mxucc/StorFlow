package share

import (
	"context"
	"fmt"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/service/explorer"
	"github.com/gin-gonic/gin"
)

type (
	// ShareCreateService 创建新分享服务
	ShareCreateService struct {
		Uri             string             `json:"uri" binding:"required"`
		IsPrivate       bool               `json:"is_private"`
		Password        string             `json:"password" binding:"omitempty,max=32,alphanum"`
		RemainDownloads int                `json:"downloads"`
		Expire          int                `json:"expire"`
		ShareView       bool               `json:"share_view"`
		ShowReadMe      bool               `json:"show_readme"`
		Permission      string             `json:"permission" binding:"omitempty,eq=read|eq=upload|eq=edit"`
		Grants          []types.ShareGrant `json:"grants"`
		// PayEnabled turns the share into a paid share priced in cents. The
		// content stays gated until a buyer owns an entitlement.
		PayEnabled bool  `json:"pay_enabled"`
		Price      int64 `json:"price" binding:"omitempty,min=1,max=100000000"`
		// IsDefault publishes the share on the site default share list.
		IsDefault bool `json:"is_default"`
	}
	ShareCreateParamCtx struct{}

	BatchDeleteShareService struct {
		ShareIDs []string `json:"ids" binding:"required"`
	}
	BatchDeleteParamCtx struct{}
)

func (service *BatchDeleteShareService) Delete(c *gin.Context) error {
	dep := dependency.FromContext(c)
	uid := inventory.UserIDFromContext(c)
	shareClient := dep.ShareClient()

	var ids []int

	for _, v := range service.ShareIDs {
		id, err := dep.HashIDEncoder().Decode(v, hashid.ShareID)
		if err != nil {
			return fmt.Errorf("failed to decode hash id %q: %w", v, err)
		}

		ids = append(ids, id)
	}

	// Internal shares belong to another subsystem (the shop module). Deleting
	// one by hand would silently break every delivery that depends on it, so
	// they are removed through the owning subsystem instead.
	internal, err := internalShareIDs(c, ids)
	if err != nil {
		return err
	}
	if len(internal) > 0 {
		return serializer.NewError(serializer.CodeParamErr,
			"Some of the selected links belong to shop products and cannot be deleted here", nil)
	}

	if err := shareClient.DeleteBatchByUserID(c, uid, ids); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete shares", err)
	}

	return nil
}

// Upsert 创建或更新分享
func (service *ShareCreateService) Upsert(c *gin.Context, existed int) (string, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	// Check group permission for creating share link
	if !user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionShare)) {
		return "", serializer.NewError(serializer.CodeGroupNotAllowed, "Group permission denied", nil)
	}

	// Anonymous visitors may only read a share unless the owner's group is
	// allowed to escalate their permission (Pro: 提升匿名用户权限). Grants to
	// explicit users are not affected by this clamp.
	permission := types.SharePermission(service.Permission)
	if permission != types.SharePermissionRead &&
		!user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionEscalateAnonymity)) {
		permission = types.SharePermissionRead
	}
	uri, err := fs.NewUriFromString(service.Uri)
	if err != nil {
		return "", serializer.NewError(serializer.CodeParamErr, "unknown uri", err)
	}

	var expires *time.Time
	if service.Expire > 0 {
		expires = new(time.Time)
		*expires = time.Now().Add(time.Duration(service.Expire) * time.Second)
	}

	share, err := m.CreateOrUpdateShare(c, uri, &manager.CreateShareArgs{
		IsPrivate:       service.IsPrivate,
		Password:        service.Password,
		RemainDownloads: service.RemainDownloads,
		Expire:          expires,
		ExistedShareID:  existed,
		ShareView:       service.ShareView,
		ShowReadMe:      service.ShowReadMe,
		Permission:      permission,
		Grants:          service.Grants,
		PayEnabled:      service.PayEnabled,
		Price:           service.Price,
		IsDefault:       service.IsDefault,
	})
	if err != nil {
		return "", err
	}

	base := dep.SettingProvider().SiteURL(c)
	shareName := ""
	if share.Edges.File != nil {
		shareName = share.Edges.File.Name
	}
	explorer.WriteAudit(c, eventtype.Share, "share", shareName, nil)
	return explorer.BuildShareLink(share, dep.HashIDEncoder(), base, true), nil
}

// internalShareIDs returns the subset of ids that belong to an internal share.
func internalShareIDs(c *gin.Context, ids []int) ([]int, error) {
	dep := dependency.FromContext(c)

	ctx := context.WithValue(c, inventory.LoadShareFile{}, true)
	shares, err := dep.ShareClient().GetByIDs(ctx, ids)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load shares", err)
	}

	var res []int
	for _, share := range shares {
		if share.Props != nil && share.Props.Internal {
			res = append(res, share.ID)
		}
	}
	return res, nil
}

func DeleteShare(c *gin.Context, shareId int) error {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	shareClient := dep.ShareClient()

	ctx := context.WithValue(c, inventory.LoadShareFile{}, true)
	var (
		share *ent.Share
		err   error
	)
	if user.Edges.Group.Permissions.Enabled(int(types.GroupPermissionIsAdmin)) {
		share, err = shareClient.GetByID(ctx, shareId)
	} else {
		share, err = shareClient.GetByIDUser(ctx, shareId, user.ID)
	}
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "share not found", err)
	}

	if share.Props != nil && share.Props.Internal {
		return serializer.NewError(serializer.CodeParamErr,
			"This link belongs to a shop product and cannot be deleted here", nil)
	}

	if err := shareClient.Delete(c, share.ID); err != nil {
		return serializer.NewError(serializer.CodeDBError, "Failed to delete share", err)
	}

	shareName := ""
	if share.Edges.File != nil {
		shareName = share.Edges.File.Name
	}
	explorer.WriteAudit(c, eventtype.DeleteShare, "share", shareName, nil)
	return nil
}
