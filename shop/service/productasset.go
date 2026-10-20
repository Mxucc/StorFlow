package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/pkg/util"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/gin-gonic/gin"
)

// 商品介绍的素材（图片 / 视频 / 音频）直连卖家网盘里的文件。
//
// 为什么不用 Asset（把字节复制进模块表）：
//   - 商品介绍经常要嵌演示视频，复制既占空间又跟不上卖家的更新；
//   - 「直连」意味着卖家在网盘里替换同名文件后，介绍页立刻生效。
//
// 所以这里只签发一个 HMAC 引用：token 里只有商品 ID、文件 ID 和过期时间，
// 不含路径。服务时先按商品的介绍可见性校验访客，再以卖家的身份打开文件并
// 流式返回 —— EntitySource.Serve 原生支持 Range 与 ETag，视频才能拖动播放。
const (
	// ProductAssetPathPrefix 是素材 URL 的前缀，后面依次是 slug、product、
	// id 和 token。相对路径存储，换域名或走反向代理都不会失效。
	ProductAssetPathPrefix = "/api/v4/shop/store/"

	// productAssetTTL 是素材引用的有效期。URL 会被写进商品介绍的 Markdown，
	// 必须长期可用；token 里没有敏感信息，只是两个 ID。
	productAssetTTL = 10 * 365 * 24 * 3600
)

// productAssetMimes 是允许嵌进商品介绍的媒体类型。刻意不含 SVG：素材与站点
// 同源，直接打开一个 SVG URL 会执行其中的脚本。
var productAssetMimes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
}

// ProductAssetView is the API projection of an embedded product asset.
type ProductAssetView struct {
	// URL is the site relative address to put inside the Markdown.
	URL  string `json:"url"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}

// ProductAssetRequest picks a file of the owner's drive for a product page.
type ProductAssetRequest struct {
	Uri string `json:"uri" binding:"required"`
}

// productAssetClaim is the signed payload of an asset token. Field names are
// kept short because the token ends up inside the Markdown source.
type productAssetClaim struct {
	Product string `json:"p"`
	FileID  int    `json:"f"`
	Expires int64  `json:"e"`
}

// assetKind maps a media mime to the coarse kind the editor uses to decide
// between an image and a video embed.
func assetKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	default:
		return "file"
	}
}

func (s *Service) signProductAsset(c *gin.Context, claim productAssetClaim) (string, error) {
	payload, err := json.Marshal(claim)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, []byte(s.dep.SettingProvider().SecretKey(c)))
	mac.Write(payload)
	signature := mac.Sum(nil)

	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature[:16]), nil
}

func (s *Service) verifyProductAsset(c *gin.Context, token string) (*productAssetClaim, error) {
	invalid := serializer.NewError(serializer.CodeNotFound, "Asset not found", nil)

	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payloadPart == "" || signaturePart == "" {
		return nil, invalid
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return nil, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return nil, invalid
	}

	mac := hmac.New(sha256.New, []byte(s.dep.SettingProvider().SecretKey(c)))
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)[:16]) {
		return nil, invalid
	}

	claim := &productAssetClaim{}
	if err := json.Unmarshal(payload, claim); err != nil {
		return nil, invalid
	}
	if claim.Product == "" || claim.FileID == 0 {
		return nil, invalid
	}
	if claim.Expires > 0 && claim.Expires < time.Now().Unix() {
		return nil, invalid
	}
	return claim, nil
}

// SignProductAsset turns one drive file of the seller into a stable URL that
// can be embedded in the product page. The file is not copied: the token only
// carries the product and file identifiers, and serving re-checks that the
// file still belongs to the shop owner.
func (s *Service) SignProductAsset(c *gin.Context, productID string, req *ProductAssetRequest) (*ProductAssetView, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	product, err := s.store.GetProductByID(c, strings.TrimSpace(productID))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", err)
	}

	shop, err := s.store.GetShopByID(c, product.ShopID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
	}
	if shop.UID != user.ID {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You can only edit your own products", nil)
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
		return nil, serializer.NewError(serializer.CodeParamErr, "Please pick a file, not a folder", nil)
	}
	if file.OwnerID() != user.ID {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You can only pick your own files", nil)
	}

	ext := strings.ToLower(path.Ext(file.Name()))
	mimeType, ok := productAssetMimes[ext]
	if !ok {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"Only images, videos and audio can be embedded in a product page", nil)
	}

	token, err := s.signProductAsset(c, productAssetClaim{
		Product: product.ID,
		FileID:  file.ID(),
		Expires: time.Now().Unix() + productAssetTTL,
	})
	if err != nil {
		return nil, serializer.NewError(serializer.CodeInternalSetting, "Failed to sign the asset", err)
	}

	return &ProductAssetView{
		URL: ProductAssetPathPrefix + url.PathEscape(shop.Slug) + "/product/" +
			url.PathEscape(product.ID) + "/asset/" + token,
		Name: file.Name(),
		Mime: mimeType,
		Kind: assetKind(mimeType),
		Size: file.Size(),
	}, nil
}

// ServeProductAsset streams one embedded asset. Visibility comes from the
// product payload: a public page may embed public material, while a tutorial
// can be restricted to logged in users or to the buyers of that product.
func (s *Service) ServeProductAsset(c *gin.Context, slug, productID, token string) error {
	claim, err := s.verifyProductAsset(c, token)
	if err != nil {
		return err
	}
	if claim.Product != productID {
		return serializer.NewError(serializer.CodeNotFound, "Asset not found", nil)
	}

	product, shop, err := s.publicProduct(c, slug, productID)
	if err != nil {
		return err
	}

	user := CurrentUser(c)
	loggedIn := user != nil && user.ID != 0

	switch product.Props.EffectiveDescriptionVisibility() {
	case model.DescriptionVisibilityLogin:
		if !loggedIn {
			return serializer.NewError(serializer.CodeNoPermissionErr, "Please sign in to view this content", nil)
		}
	case model.DescriptionVisibilityPurchased:
		if !loggedIn {
			return serializer.NewError(serializer.CodeNoPermissionErr, "Please sign in to view this content", nil)
		}
		// The shop owner previews their own page without owning the product.
		if user.ID != shop.UID {
			entitlement, err := s.store.GetEntitlement(c, user.ID, model.OrderKindProduct, product.ID)
			if err != nil || entitlement.Status == model.EntitlementStatusRevoked {
				return serializer.NewError(serializer.CodeNoPermissionErr,
					"Purchase this product to view its content", nil)
			}
		}
	}

	// Read as the seller: the file lives in their drive and the visitor only
	// ever sees bytes, never the drive layout or a path.
	owner, err := s.dep.UserClient().GetByID(c, shop.UID)
	if err != nil {
		return serializer.NewError(serializer.CodeDBError, "Shop owner not found", err)
	}
	util.WithValue(c, inventory.UserCtx{}, owner)

	m := manager.NewFileManager(s.dep, owner)
	defer m.Recycle()

	file, err := m.TraverseFile(c, claim.FileID)
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Asset not found", err)
	}
	if file.OwnerID() != shop.UID {
		return serializer.NewError(serializer.CodeNotFound, "Asset not found", nil)
	}
	if file.Type() != types.FileTypeFile {
		return serializer.NewError(serializer.CodeNotFound, "Asset not found", nil)
	}

	source, err := m.GetEntitySource(c, file.PrimaryEntityID())
	if err != nil {
		return serializer.NewError(serializer.CodeNotFound, "Asset not found", err)
	}
	defer source.Close()

	// A public asset may be cached by any proxy in between; the restricted
	// ones must stay private, otherwise a CDN would leak them to visitors.
	if product.Props.EffectiveDescriptionVisibility() == model.DescriptionVisibilityPublic {
		c.Header("Cache-Control", "public, max-age=3600")
	} else {
		c.Header("Cache-Control", "private, max-age=60")
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")

	// 用扩展名给出 Content-Type：EntitySource 只在响应头没设置时才嗅探内容，
	// 而 mov / mkv 这类容器靠嗅探并不可靠，视频会因此播不出来。
	if ext := strings.ToLower(path.Ext(file.Name())); ext != "" {
		if mimeType, ok := productAssetMimes[ext]; ok {
			c.Header("Content-Type", mimeType)
		}
	}

	source.Serve(c.Writer, c.Request)
	return nil
}
