package share

import (
	"context"
	"fmt"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	entfile "github.com/Mxucc/StorFlow/ent/file"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs/dbfs"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
)

// shareOwnerMetadataKey mirrors the metadata key validated by the file manager.
const shareOwnerMetadataKey = dbfs.MetadataSysPrefix + "shared_owner"

// SaveShortcut saves a share as a symbolic entry in the user's "shared with me"
// list. The entry holds no content of its own: it only remembers the share
// address (sys:shared_redirect), so the owner's later changes are visible
// immediately and no copy is stored twice.
//
// The function is idempotent: saving the same share twice returns the existing
// entry.
func SaveShortcut(ctx context.Context, dep dependency.Dep, user *ent.User, share *ent.Share) (int, error) {
	if user == nil || user.ID == 0 {
		return 0, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}
	if share == nil || share.Edges.File == nil {
		return 0, serializer.NewError(serializer.CodeNotFound, "Share not found", nil)
	}
	if share.Edges.File.ID == 0 {
		return 0, serializer.NewError(serializer.CodeNotFound, "Share source is missing", nil)
	}

	db := dep.DBClient()
	hasher := dep.HashIDEncoder()

	existed, err := db.File.Query().
		Where(
			entfile.IsSymbolic(true),
			entfile.OwnerIDEQ(user.ID),
			entfile.FileChildrenEQ(share.Edges.File.ID),
		).
		First(ctx)
	if err == nil {
		return existed.ID, nil
	}
	if !ent.IsNotFound(err) {
		return 0, serializer.NewError(serializer.CodeDBError, "Failed to check existing shortcuts", err)
	}

	created, err := db.File.Create().
		SetName(share.Edges.File.Name).
		SetType(share.Edges.File.Type).
		SetIsSymbolic(true).
		SetOwnerID(user.ID).
		SetFileChildren(share.Edges.File.ID).
		Save(ctx)
	if err != nil {
		return 0, serializer.NewError(serializer.CodeDBError, "Failed to save the shortcut", err)
	}

	owner := share.Edges.User
	ownerHash := ""
	if owner != nil {
		ownerHash = hashid.EncodeUserID(hasher, owner.ID)
	}

	metadata := map[string]string{
		dbfs.MetadataSharedRedirect: fs.NewShareUri(hashid.EncodeShareID(hasher, share.ID), share.Password),
		shareOwnerMetadataKey:       ownerHash,
	}
	for key, value := range metadata {
		if value == "" {
			continue
		}
		if _, err := db.Metadata.Create().
			SetName(key).
			SetValue(value).
			SetFile(created).
			SetIsPublic(false).
			Save(ctx); err != nil {
			return created.ID, fmt.Errorf("failed to store metadata %s: %w", key, err)
		}
	}

	return created.ID, nil
}

type (
	// RedeemService saves a share (by id + password) as a shortcut in the
	// current user's "shared with me" list.
	RedeemService struct {
		ID       string `json:"id" binding:"required"`
		Password string `json:"password"`
	}
	RedeemParamCtx struct{}
)

// Redeem validates the share and saves the shortcut.
func (service *RedeemService) Redeem(c *gin.Context) error {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	if user == nil || user.ID == 0 {
		return serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}

	shareID, err := dep.HashIDEncoder().Decode(service.ID, hashid.ShareID)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Invalid share id", err)
	}

	ctx := context.WithValue(c, inventory.LoadShareFile{}, true)
	ctx = context.WithValue(ctx, inventory.LoadShareUser{}, true)
	entity, err := dep.ShareClient().GetByID(ctx, shareID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Share not found", err)
	}

	if entity.Password != "" && entity.Password != service.Password {
		if owner, err := entity.Edges.UserOrErr(); err != nil || owner.ID != user.ID {
			return serializer.NewError(serializer.CodeNoPermissionErr, "Incorrect share password", nil)
		}
	}

	if _, err := SaveShortcut(c, dep, user, entity); err != nil {
		return err
	}
	return nil
}
