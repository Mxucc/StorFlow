package service

import (
	"context"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/gin-gonic/gin"
)

const (
	// AssetPathPrefix is the public path an asset is served from. The path is
	// relative on purpose: a storefront is rendered on the same origin, so the
	// stored URL keeps working behind any reverse proxy or site URL change.
	AssetPathPrefix = "/api/v4/shop/asset/"

	// MaxAssetSize caps the size of one stored image. It is small on purpose:
	// the bytes live in the database, they are decoration, not a file host.
	MaxAssetSize = 4 << 20

	// assetListLimit caps the size of the asset gallery of one user.
	assetListLimit = 200

	// assetRetention is how long an unused asset is kept before the janitor
	// removes it. Long enough that a half finished editing session survives.
	assetRetention = 7 * 24 * 3600
)

// assetImageMimes is the whitelist of accepted image types. SVG is missing on
// purpose: it can carry script, and assets are served from the site origin.
var assetImageMimes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

// assetImageExts maps a drive file extension to the mime used to double check
// the payload of a file picked from the drive.
var assetImageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// AssetView is the API projection of a stored image.
type AssetView struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Mime      string `json:"mime"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	CreatedAt int64  `json:"created_at"`
}

// AdoptAssetRequest picks an existing file of the owner's drive as an asset.
type AdoptAssetRequest struct {
	Uri string `json:"uri" binding:"required"`
}

func (s *Service) assetView(a *model.Asset) *AssetView {
	return &AssetView{
		ID:        a.ID,
		Name:      a.Name,
		Mime:      a.Mime,
		Size:      a.Size,
		URL:       AssetPathPrefix + a.ID,
		CreatedAt: a.CreatedAt,
	}
}

// UploadAsset stores an uploaded image. The file is copied into the module
// table, so the published shop never depends on the seller's drive layout.
func (s *Service) UploadAsset(c *gin.Context) (*AssetView, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	header, err := c.FormFile("file")
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "No image was uploaded", err)
	}
	if header.Size > MaxAssetSize {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"The image is too large, the limit is "+strconv.Itoa(MaxAssetSize>>20)+" MB", nil)
	}

	reader, err := header.Open()
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Failed to read the upload", err)
	}
	defer reader.Close()

	data, err := readAssetBytes(reader)
	if err != nil {
		return nil, err
	}

	detected := http.DetectContentType(data)
	if _, ok := assetImageMimes[detected]; !ok {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"Only PNG, JPEG, GIF, WebP and BMP images are supported", nil)
	}

	return s.storeAsset(c, user.ID, header.Filename, detected, data)
}

// AdoptAsset copies an image of the owner's drive into the module. It is how a
// seller reuses pictures they already uploaded to the drive: the picker hands
// over a storflow:// URI and the module snapshots it.
func (s *Service) AdoptAsset(c *gin.Context, req *AdoptAssetRequest) (*AssetView, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	uri, err := fs.NewUriFromString(strings.TrimSpace(req.Uri))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Unknown file", err)
	}

	m := manager.NewFileManager(s.dep, user)
	defer m.Recycle()

	file, err := m.Get(c, uri)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "File not found", err)
	}
	if file.Type() != types.FileTypeFile {
		return nil, serializer.NewError(serializer.CodeParamErr, "Please pick an image file, not a folder", nil)
	}
	if file.OwnerID() != user.ID {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You can only pick your own files", nil)
	}

	ext := strings.ToLower(path.Ext(file.Name()))
	declared, ok := assetImageExts[ext]
	if !ok {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"Only PNG, JPEG, GIF, WebP and BMP images can be picked", nil)
	}
	if file.Size() > MaxAssetSize {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"The picked image is too large, the limit is "+strconv.Itoa(MaxAssetSize>>20)+" MB", nil)
	}

	source, err := m.GetEntitySource(c, file.PrimaryEntityID())
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Failed to read the picked file", err)
	}
	defer source.Close()

	data, err := readAssetBytes(source)
	if err != nil {
		return nil, err
	}

	detected := http.DetectContentType(data)
	if detected != declared {
		return nil, serializer.NewError(serializer.CodeParamErr, "The picked file is not a valid image", nil)
	}

	return s.storeAsset(c, user.ID, file.Name(), detected, data)
}

// ListAssets returns the asset gallery of the requester, newest first.
func (s *Service) ListAssets(c *gin.Context) ([]*AssetView, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	assets, err := s.store.ListAssetsByUID(c, user.ID, assetListLimit)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load images", err)
	}

	out := make([]*AssetView, 0, len(assets))
	for _, asset := range assets {
		out = append(out, s.assetView(asset))
	}
	return out, nil
}

// ServeAsset streams a stored image. It is anonymous by design: the identifier
// is 128 bits of randomness and an asset is only created when a seller
// publishes the image.
func (s *Service) ServeAsset(c *gin.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 {
		return serializer.NewError(serializer.CodeNotFound, "Image not found", nil)
	}

	asset, err := s.store.GetAsset(c, id)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Image not found", err)
	}

	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, asset.Mime, asset.Data)
	return nil
}

// PurgeAssets removes images nobody references any more. It is called by the
// module janitor so an abandoned upload does not stay in the database forever.
func (s *Service) PurgeAssets(ctx context.Context, limit int) (int, error) {
	stale, err := s.store.ListStaleAssets(ctx, time.Now().Unix()-assetRetention, limit)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, asset := range stale {
		refs, err := s.store.CountAssetReferences(ctx, asset.ID)
		if err != nil {
			continue
		}
		if refs > 0 {
			continue
		}
		if err := s.store.DeleteAsset(ctx, asset.ID); err != nil {
			continue
		}
		removed++
	}
	return removed, nil
}

// storeAsset persists the payload and returns its public projection.
func (s *Service) storeAsset(ctx context.Context, uid int, name, mimeType string, data []byte) (*AssetView, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}

	asset := &model.Asset{
		ID:        newToken(16),
		UID:       uid,
		Name:      name,
		Mime:      mimeType,
		Size:      int64(len(data)),
		Data:      data,
		CreatedAt: time.Now().Unix(),
	}
	if err := s.store.CreateAsset(ctx, asset); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to save the image", err)
	}
	return s.assetView(asset), nil
}

// readAssetBytes reads at most MaxAssetSize bytes and rejects anything larger.
func readAssetBytes(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxAssetSize+1))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Failed to read the image", err)
	}
	if len(data) > MaxAssetSize {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"The image is too large, the limit is "+strconv.Itoa(MaxAssetSize>>20)+" MB", nil)
	}
	if len(data) == 0 {
		return nil, serializer.NewError(serializer.CodeParamErr, "The image is empty", nil)
	}
	return data, nil
}
