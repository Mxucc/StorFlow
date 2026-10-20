package explorer

import (
	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
)

type (
	PatchMetadataService struct {
		Uris    []string           `json:"uris" binding:"required"`
		Patches []fs.MetadataPatch `json:"patches" binding:"required,dive"`
	}

	PatchMetadataParameterCtx struct{}
)

func (s *PatchMetadataService) GetUris() []string {
	return s.Uris
}

func (s *PatchMetadataService) Patch(c *gin.Context) error {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	uris, err := fs.NewUriFromStrings(s.Uris...)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "unknown uri", err)
	}

	return m.PatchMedata(c, uris, s.Patches...)
}
