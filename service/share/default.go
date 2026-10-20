package share

import (
	"context"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/service/explorer"
	"github.com/gin-gonic/gin"
)

type (
	// DefaultShareService lists the site default shares (Pro: 默认分享). Newly
	// registered users see them right after signing in.
	DefaultShareService struct {
		Limit int `form:"limit"`
	}
	DefaultShareParamCtx struct{}
)

// maxDefaultShareScan bounds how many public shares are examined while looking
// for default ones. The default flag lives inside the share props JSON, so it
// cannot be filtered in SQL portably; the scan keeps the query cheap and the
// behaviour predictable.
const maxDefaultShareScan = 300

// List returns every valid default share of the instance.
func (s *DefaultShareService) List(c *gin.Context) (*ListShareResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	shareClient := dep.ShareClient()
	hasher := dep.HashIDEncoder()

	limit := s.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	ctx := context.WithValue(c, inventory.LoadShareUser{}, true)
	ctx = context.WithValue(ctx, inventory.LoadShareFile{}, true)

	var (
		matched []*ent.Share
		token   string
		scanned int
	)
	for len(matched) < limit && scanned < maxDefaultShareScan {
		pageSize := 50
		if remaining := maxDefaultShareScan - scanned; remaining < pageSize {
			pageSize = remaining
		}

		res, err := shareClient.List(ctx, &inventory.ListShareArgs{
			PaginationArgs: &inventory.PaginationArgs{
				UseCursorPagination: true,
				PageToken:           token,
				PageSize:            pageSize,
				Order:               inventory.OrderDirectionDesc,
			},
			PublicOnly: true,
		})
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to list default shares", err)
		}

		for _, share := range res.Shares {
			scanned++
			if share.Props == nil || !share.Props.IsDefault {
				continue
			}
			if inventory.IsValidShare(share) != nil {
				continue
			}
			matched = append(matched, share)
			if len(matched) >= limit {
				break
			}
		}

		if res.PaginationResults == nil || res.PaginationResults.NextPageToken == "" {
			break
		}
		token = res.PaginationResults.NextPageToken
	}

	base := dep.SettingProvider().SiteURL(ctx)
	infos := make([]explorer.Share, 0, len(matched))
	for _, share := range matched {
		infos = append(infos, *explorer.BuildShare(ctx, share, base, hasher, user, share.Edges.User,
			share.Edges.File.Name, types.FileType(share.Edges.File.Type), true, false))
	}

	return &ListShareResponse{
		Shares: infos,
		Pagination: &inventory.PaginationResults{
			PageSize:   limit,
			TotalItems: len(matched),
		},
	}, nil
}
