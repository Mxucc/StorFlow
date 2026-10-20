package user

import (
	"context"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/hashid"
)

// resolvePinShares 把分享数字 ID 解析为侧边栏置顶项。已删除或不存在的分享会被跳过；
// locked 表示该项由管理员固定、用户无法取消（用户组默认置顶）。
func resolvePinShares(ctx context.Context, dep dependency.Dep, shareIDs []int, locked bool) []types.PinedFile {
	if dep == nil || len(shareIDs) == 0 {
		return nil
	}

	// 置顶项展示的是被分享文件的名字，需要一并加载文件边。
	ctx = context.WithValue(ctx, inventory.LoadShareFile{}, true)

	shareClient := dep.ShareClient()
	hasher := dep.HashIDEncoder()
	res := make([]types.PinedFile, 0, len(shareIDs))
	seen := make(map[string]struct{}, len(shareIDs))

	for _, shareID := range shareIDs {
		if shareID <= 0 {
			continue
		}

		share, err := shareClient.GetByID(ctx, shareID)
		if err != nil || share == nil {
			continue
		}

		uri := fs.NewShareUri(hashid.EncodeShareID(hasher, share.ID), "")
		if _, ok := seen[uri]; ok {
			continue
		}

		name := ""
		if share.Edges.File != nil {
			name = share.Edges.File.Name
		}
		if name == "" {
			name = hashid.EncodeShareID(hasher, share.ID)
		}

		seen[uri] = struct{}{}
		res = append(res, types.PinedFile{Uri: uri, Name: name, Locked: locked})
	}

	return res
}

// ResolveGroupDefaultPins 把用户组配置的默认置顶分享（数字 ID）解析为侧边栏置顶项。
// 结果带 Locked 标记，用户无法取消置顶。
func ResolveGroupDefaultPins(ctx context.Context, dep dependency.Dep, u *ent.User) []types.PinedFile {
	if dep == nil || u == nil {
		return nil
	}

	group := u.Edges.Group
	if group == nil || group.Settings == nil || len(group.Settings.DefaultPinnedShares) == 0 {
		return nil
	}

	return resolvePinShares(ctx, dep, group.Settings.DefaultPinnedShares, true)
}

// ResolveSiteDefaultPins 把站点级「初始分享快捷方式」解析为新用户的置顶项。
// 这些快捷方式是注册时的初始值，用户可以自行取消，因此不加锁。
func ResolveSiteDefaultPins(ctx context.Context, dep dependency.Dep, shareIDs []int) []types.PinedFile {
	return resolvePinShares(ctx, dep, shareIDs, false)
}

// MergePins 把用户组默认置顶项合并到用户置顶列表：组默认项固定排在最前，
// 与用户自己置顶的同一项不重复。
func MergePins(userPins []types.PinedFile, groupPins []types.PinedFile) []types.PinedFile {
	if len(groupPins) == 0 {
		return userPins
	}

	seen := make(map[string]struct{}, len(groupPins))
	merged := make([]types.PinedFile, 0, len(groupPins)+len(userPins))
	for _, p := range groupPins {
		seen[p.Uri] = struct{}{}
		merged = append(merged, p)
	}

	for _, p := range userPins {
		if _, ok := seen[p.Uri]; ok {
			continue
		}
		merged = append(merged, p)
	}

	return merged
}
