package share

import (
	"context"
	"strings"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/cluster/routes"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/service/explorer"
	"github.com/gin-gonic/gin"
)

type (
	ShortLinkRedirectService struct {
		ID       string `uri:"id" binding:"required"`
		Password string `uri:"password"`
	}
	ShortLinkRedirectParamCtx struct{}
)

func (s *ShortLinkRedirectService) RedirectTo(c *gin.Context) string {
	shareLongUrl := routes.MasterShareLongUrl(s.ID, s.Password)

	shortLinkQuery := c.Request.URL.Query() // Query in ShortLink, adapt to the V3 short link format
	shareLongUrlQuery := shareLongUrl.Query()

	userSpecifiedPath := shortLinkQuery.Get("path")
	if userSpecifiedPath != "" {
		masterPath := shareLongUrlQuery.Get("path")
		masterPath += "/" + strings.TrimPrefix(userSpecifiedPath, "/")

		shareLongUrlQuery.Set("path", masterPath)
	}

	shortLinkQuery.Del("path") // 防止用户指定的 Path 就是空字符串
	for k, vals := range shortLinkQuery {
		shareLongUrlQuery[k] = append(shareLongUrlQuery[k], vals...)
	}

	shareLongUrl.RawQuery = shareLongUrlQuery.Encode()
	return shareLongUrl.String()
}

type (
	ShareInfoService struct {
		Password      string `form:"password"`
		CountViews    bool   `form:"count_views"`
		OwnerExtended bool   `form:"owner_extended"`
	}
	ShareInfoParamCtx struct{}
)

func (s *ShareInfoService) Get(c *gin.Context) (*explorer.Share, error) {
	dep := dependency.FromContext(c)
	u := inventory.UserFromContext(c)
	shareClient := dep.ShareClient()

	ctx := context.WithValue(c, inventory.LoadShareUser{}, true)
	ctx = context.WithValue(ctx, inventory.LoadShareFile{}, true)
	share, err := shareClient.GetByID(ctx, hashid.FromContext(c))
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, serializer.NewError(serializer.CodeNotFound, "Share not found", nil)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to get share", err)
	}

	if err := inventory.IsValidShare(share); err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Share link expired", err)
	}

	if s.CountViews {
		_ = shareClient.Viewed(c, share)
		explorer.WriteAudit(c, eventtype.ShareLinkViewed, "share", share.Edges.File.Name,
			map[string]interface{}{"share_id": share.ID})
	}

	unlocked := true
	// Share requires password
	if share.Password != "" && s.Password != share.Password && share.Edges.User.ID != u.ID {
		unlocked = false
	}

	base := dep.SettingProvider().SiteURL(c)
	res := explorer.BuildShare(c, share, base, dep.HashIDEncoder(), u, share.Edges.User, share.Edges.File.Name,
		types.FileType(share.Edges.File.Type), unlocked, false)

	if s.OwnerExtended && share.Edges.User.ID == u.ID {
		// Add more information about the shared file
		m := manager.NewFileManager(dep, u)
		defer m.Recycle()

		shareUri, err := fs.NewUriFromString(fs.NewShareUri(res.ID, s.Password))
		if err != nil {
			return nil, serializer.NewError(serializer.CodeInternalSetting, "Invalid share url", err)
		}

		root, err := m.Get(c, shareUri)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeNotFound, "File not found", err)
		}

		res.SourceUri = root.Uri(true).String()
	}

	return res, nil

}

type (
	ListShareService struct {
		PageSize       int    `form:"page_size" binding:"required,min=10,max=100"`
		OrderBy        string `uri:"order_by" form:"order_by" json:"order_by"`
		OrderDirection string `uri:"order_direction" form:"order_direction" json:"order_direction"`
		NextPageToken  string `form:"next_page_token"`
	}
	ListShareParamCtx struct{}

	// DiscoverService lists public, valid shares across all users
	// (ZXFile Android discover page).
	DiscoverService struct {
		PageSize       int    `form:"page_size" binding:"omitempty,min=10,max=100"`
		OrderBy        string `form:"order_by"`
		OrderDirection string `form:"order_direction"`
		NextPageToken  string `form:"next_page_token"`
	}
	DiscoverParamCtx struct{}
)

// Discover lists public shares from all users, ordered by creation
// time descending. SQL-level filters: no password, not expired,
// remaining downloads. Owner/source-file validity is filtered after
// the query via IsValidShare.
func (s *DiscoverService) List(c *gin.Context) (*ListShareResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	if inventory.IsAnonymousUser(user) {
		return nil, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}
	hasher := dep.HashIDEncoder()
	shareClient := dep.ShareClient()

	args := &inventory.ListShareArgs{
		PaginationArgs: &inventory.PaginationArgs{
			UseCursorPagination: true,
			PageToken:           s.NextPageToken,
			PageSize:            s.PageSize,
			Order:               inventory.OrderDirection(s.OrderDirection),
			OrderBy:             s.OrderBy,
		},
		PublicOnly: true,
		OnlyValid:  true,
	}

	ctx := context.WithValue(c, inventory.LoadShareUser{}, true)
	ctx = context.WithValue(ctx, inventory.LoadShareFile{}, true)
	ctx = context.WithValue(ctx, inventory.LoadFileMetadata{}, true)
	res, err := shareClient.List(ctx, args)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list shares", err)
	}

	// Drop shares whose owner is inactive or source file deleted.
	validShares := make([]*ent.Share, 0, len(res.Shares))
	for _, sh := range res.Shares {
		if err := inventory.IsValidShare(sh); err == nil {
			validShares = append(validShares, sh)
		}
	}
	res.Shares = validShares

	base := dep.SettingProvider().SiteURL(ctx)
	return BuildListShareResponse(ctx, res, hasher, base, user, true), nil
}

func (s *ListShareService) List(c *gin.Context) (*ListShareResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	shareClient := dep.ShareClient()

	args := &inventory.ListShareArgs{
		PaginationArgs: &inventory.PaginationArgs{
			UseCursorPagination: true,
			PageToken:           s.NextPageToken,
			PageSize:            s.PageSize,
			Order:               inventory.OrderDirection(s.OrderDirection),
			OrderBy:             s.OrderBy,
		},
		UserID: user.ID,
	}

	ctx := context.WithValue(c, inventory.LoadShareUser{}, true)
	ctx = context.WithValue(ctx, inventory.LoadShareFile{}, true)
	ctx = context.WithValue(ctx, inventory.LoadFileMetadata{}, true)
	res, err := shareClient.List(ctx, args)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list shares", err)
	}

	// Internal shares (created by the shop module for the folders on sale) are
	// an implementation detail: they are not manageable links.
	res.Shares = filterInternalShares(res.Shares)

	base := dep.SettingProvider().SiteURL(ctx)
	return BuildListShareResponse(ctx, res, hasher, base, user, true), nil
}

// filterInternalShares drops the shares owned by another subsystem.
func filterInternalShares(shares []*ent.Share) []*ent.Share {
	res := make([]*ent.Share, 0, len(shares))
	for _, share := range shares {
		if share.Props != nil && share.Props.Internal {
			continue
		}
		res = append(res, share)
	}
	return res
}

func (s *ListShareService) ListInUserProfile(c *gin.Context, uid int) (*ListShareResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	hasher := dep.HashIDEncoder()
	shareClient := dep.ShareClient()

	targetUser, err := dep.UserClient().GetActiveByID(c, uid)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to get user", err)
	}

	if targetUser.Settings != nil && targetUser.Settings.ShareLinksInProfile == types.ProfileHideShare {
		return nil, serializer.NewError(serializer.CodeParamErr, "User has disabled share links in profile", nil)
	}

	publicOnly := targetUser.Settings == nil || targetUser.Settings.ShareLinksInProfile == types.ProfilePublicShareOnly
	args := &inventory.ListShareArgs{
		PaginationArgs: &inventory.PaginationArgs{
			UseCursorPagination: true,
			PageToken:           s.NextPageToken,
			PageSize:            s.PageSize,
			Order:               inventory.OrderDirection(s.OrderDirection),
			OrderBy:             s.OrderBy,
		},
		UserID:     uid,
		PublicOnly: publicOnly,
	}

	ctx := context.WithValue(c, inventory.LoadShareUser{}, true)
	ctx = context.WithValue(ctx, inventory.LoadShareFile{}, true)
	ctx = context.WithValue(ctx, inventory.LoadFileMetadata{}, true)
	res, err := shareClient.List(ctx, args)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list shares", err)
	}

	res.Shares = filterInternalShares(res.Shares)

	base := dep.SettingProvider().SiteURL(ctx)
	return BuildListShareResponse(ctx, res, hasher, base, user, false), nil
}
