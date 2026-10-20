package explorer

import (
	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type (
	PinFileService struct {
		Uri  string `json:"uri" binding:"required"`
		Name string `json:"name"`
	}
	PinFileParameterCtx struct{}
)

// PinFileService pins new uri to sidebar
func (service *PinFileService) PinFile(c *gin.Context) error {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	userClient := dep.UserClient()

	uri, err := fs.NewUriFromString(service.Uri)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "unknown uri", err)
	}

	uriStr := uri.String()
	for _, pin := range user.Settings.Pined {
		if pin.Uri == uriStr {
			if pin.Name != service.Name {
				return serializer.NewError(serializer.CodeObjectExist, "uri already pinned with different name", nil)
			}

			return nil
		}
	}

	user.Settings.Pined = append(user.Settings.Pined, types.PinedFile{
		Uri:  uriStr,
		Name: service.Name,
	})
	if err := userClient.SaveSettings(c, user); err != nil {
		return serializer.NewError(serializer.CodeDBError, "failed to save settings", err)
	}

	return nil
}

// UnpinFile removes uri from sidebar
func (service *PinFileService) UnpinFile(c *gin.Context) error {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	userClient := dep.UserClient()

	uri, err := fs.NewUriFromString(service.Uri)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "unknown uri", err)
	}

	uriStr := uri.String()

	// 用户组默认置顶的分享（Pro: 默认固定分享）不允许用户取消。
	if dep := dependency.FromContext(c); dep != nil && user.Edges.Group != nil && user.Edges.Group.Settings != nil {
		hasher := dep.HashIDEncoder()
		for _, shareID := range user.Edges.Group.Settings.DefaultPinnedShares {
			if shareID > 0 && fs.NewShareUri(hashid.EncodeShareID(hasher, shareID), "") == uriStr {
				return serializer.NewError(serializer.CodeGroupNotAllowed,
					"This pin is managed by your user group and cannot be removed", nil)
			}
		}
	}

	user.Settings.Pined = lo.Filter(user.Settings.Pined, func(pin types.PinedFile, index int) bool {
		return pin.Uri != uriStr
	})

	if err := userClient.SaveSettings(c, user); err != nil {
		return serializer.NewError(serializer.CodeDBError, "failed to save settings", err)
	}

	return nil
}
