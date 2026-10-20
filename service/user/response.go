package user

import (
	"context"
	"fmt"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/user"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/auth"
	"github.com/Mxucc/StorFlow/pkg/boolset"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/samber/lo"
	"github.com/ua-parser/uap-go/uaparser"
)

type PreparePasskeyLoginResponse struct {
	Options   *protocol.CredentialAssertion `json:"options"`
	SessionID string                        `json:"session_id"`
}

type UserSettings struct {
	VersionRetentionEnabled bool         `json:"version_retention_enabled"`
	VersionRetentionExt     []string     `json:"version_retention_ext,omitempty"`
	VersionRetentionMax     int          `json:"version_retention_max,omitempty"`
	Paswordless             bool         `json:"passwordless"`
	TwoFAEnabled            bool         `json:"two_fa_enabled"`
	Passkeys                []Passkey    `json:"passkeys,omitempty"`
	DisableViewSync         bool         `json:"disable_view_sync"`
	ShareLinksInProfile     string       `json:"share_links_in_profile"`
	OAuthGrants             []OauthGrant `json:"oauth_grants,omitempty"`
}

func BuildUserSettings(u *ent.User, passkeys []*ent.Passkey, parser *uaparser.Parser, grants []*ent.OAuthGrant) *UserSettings {
	return &UserSettings{
		VersionRetentionEnabled: u.Settings.VersionRetention,
		VersionRetentionExt:     u.Settings.VersionRetentionExt,
		VersionRetentionMax:     u.Settings.VersionRetentionMax,
		TwoFAEnabled:            u.TwoFactorSecret != "",
		Paswordless:             u.Password == "",
		Passkeys: lo.Map(passkeys, func(item *ent.Passkey, index int) Passkey {
			return BuildPasskey(item)
		}),
		DisableViewSync:     u.Settings.DisableViewSync,
		ShareLinksInProfile: string(u.Settings.ShareLinksInProfile),
		OAuthGrants: lo.Map(grants, func(item *ent.OAuthGrant, index int) OauthGrant {
			return BuildOauthGrant(item)
		}),
	}
}

type Passkey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func BuildPasskey(passkey *ent.Passkey) Passkey {
	return Passkey{
		ID:        passkey.CredentialID,
		Name:      passkey.Name,
		UsedAt:    passkey.UsedAt,
		CreatedAt: passkey.CreatedAt,
	}
}

// Node option for handling workflows.
type Node struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	Capabilities *boolset.BooleanSet `json:"capabilities"`
}

// BuildNodes serialize a list of nodes.
func BuildNodes(nodes []*ent.Node, idEncoder hashid.Encoder) []*Node {
	res := make([]*Node, 0, len(nodes))
	for _, v := range nodes {
		res = append(res, BuildNode(v, idEncoder))
	}

	return res
}

// BuildNode serialize a node.
func BuildNode(node *ent.Node, idEncoder hashid.Encoder) *Node {
	return &Node{
		ID:           hashid.EncodeNodeID(idEncoder, node.ID),
		Name:         node.Name,
		Type:         string(node.Type),
		Capabilities: node.Capabilities,
	}
}

// BuiltinLoginResponse response for a successful login for builtin auth provider.
type BuiltinLoginResponse struct {
	User  User       `json:"user"`
	Token auth.Token `json:"token"`
}

// User 用户序列化器
type User struct {
	ID                  string                         `json:"id"`
	Email               string                         `json:"email,omitempty"`
	Nickname            string                         `json:"nickname"`
	Status              user.Status                    `json:"status,omitempty"`
	Avatar              string                         `json:"avatar,omitempty"`
	CreatedAt           time.Time                      `json:"created_at"`
	PreferredTheme      string                         `json:"preferred_theme,omitempty"`
	Anonymous           bool                           `json:"anonymous,omitempty"`
	Group               *Group                         `json:"group,omitempty"`
	Pined               []types.PinedFile              `json:"pined,omitempty"`
	Language            string                         `json:"language,omitempty"`
	DisableViewSync     bool                           `json:"disable_view_sync,omitempty"`
	ShareLinksInProfile types.ShareLinksInProfileLevel `json:"share_links_in_profile,omitempty"`
}

type Group struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	Permission          *boolset.BooleanSet `json:"permission,omitempty"`
	DirectLinkBatchSize int                 `json:"direct_link_batch_size,omitempty"`
	TrashRetention      int                 `json:"trash_retention,omitempty"`
	// FolderDirectLink 用户组是否允许为目录生成直链（Pro: 目录直链）。
	FolderDirectLink bool `json:"folder_direct_link,omitempty"`
	// AllowSelectNode 是否允许组内用户为任务自选处理节点（Pro: 允许选择节点）。
	AllowSelectNode bool `json:"allow_select_node,omitempty"`
	// AllowedNodes 用户组可用的处理节点 ID（数字 ID，Pro: 可用节点），为空表示不限。
	AllowedNodes []int `json:"allowed_nodes,omitempty"`
}

type storage struct {
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

// WebAuthnCredentials 外部验证器凭证
type WebAuthnCredentials struct {
	ID          []byte `json:"id"`
	FingerPrint string `json:"fingerprint"`
}

type PrepareLoginResponse struct {
	WebAuthnEnabled bool `json:"webauthn_enabled"`
	PasswordEnabled bool `json:"password_enabled"`
	// SSOEnabled 反映 oidc_enabled，供登录页决定是否展示 SSO 入口。
	SSOEnabled bool `json:"sso_enabled"`
}

// BuildWebAuthnList 构建设置页面凭证列表
func BuildWebAuthnList(credentials []webauthn.Credential) []WebAuthnCredentials {
	res := make([]WebAuthnCredentials, 0, len(credentials))
	for _, v := range credentials {
		credential := WebAuthnCredentials{
			ID:          v.ID,
			FingerPrint: fmt.Sprintf("% X", v.Authenticator.AAGUID),
		}
		res = append(res, credential)
	}

	return res
}

// BuildUser 序列化用户
func BuildUser(user *ent.User, idEncoder hashid.Encoder) User {
	return BuildUserWithPins(user, idEncoder, nil)
}

// BuildUserWithPins 序列化用户，并把额外的置顶项（用户组默认置顶分享）合并进侧边栏列表。
func BuildUserWithPins(user *ent.User, idEncoder hashid.Encoder, extraPins []types.PinedFile) User {
	return User{
		ID:                  hashid.EncodeUserID(idEncoder, user.ID),
		Email:               user.Email,
		Nickname:            user.Nick,
		Status:              user.Status,
		Avatar:              user.Avatar,
		CreatedAt:           user.CreatedAt,
		PreferredTheme:      user.Settings.PreferredTheme,
		Anonymous:           user.ID == 0,
		Group:               BuildGroup(user.Edges.Group, idEncoder),
		Pined:               MergePins(user.Settings.Pined, extraPins),
		Language:            user.Settings.Language,
		DisableViewSync:     user.Settings.DisableViewSync,
		ShareLinksInProfile: user.Settings.ShareLinksInProfile,
	}
}

// BuildUserWithGroupPins 序列化用户，并带上用户组配置的默认置顶分享（Pro: 默认固定分享）。
func BuildUserWithGroupPins(ctx context.Context, dep dependency.Dep, user *ent.User, idEncoder hashid.Encoder) User {
	return BuildUserWithPins(user, idEncoder, ResolveGroupDefaultPins(ctx, dep, user))
}

type OauthGrant struct {
	ClientID   string     `json:"client_id"`
	ClientName string     `json:"client_name"`
	ClientLogo string     `json:"client_logo"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func BuildOauthGrant(grant *ent.OAuthGrant) OauthGrant {
	res := OauthGrant{
		Scopes:     grant.Scopes,
		LastUsedAt: grant.LastUsedAt,
	}

	if grant.Edges.Client != nil {
		res.ClientID = grant.Edges.Client.GUID
		res.ClientName = grant.Edges.Client.Name
		if grant.Edges.Client.Props != nil {
			res.ClientLogo = grant.Edges.Client.Props.Icon
		}
	}

	return res
}

func BuildGroup(group *ent.Group, idEncoder hashid.Encoder) *Group {
	if group == nil {
		return nil
	}

	// 用户组设置可能为 NULL（例如刚建组或历史数据），这里兜底避免序列化用户时 panic。
	settings := group.Settings
	if settings == nil {
		settings = &types.GroupSetting{}
	}

	return &Group{
		ID:                  hashid.EncodeGroupID(idEncoder, group.ID),
		Name:                group.Name,
		Permission:          group.Permissions,
		DirectLinkBatchSize: settings.SourceBatchSize,
		TrashRetention:      settings.TrashRetention,
		FolderDirectLink:    settings.FolderDirectLink,
		AllowSelectNode:     settings.AllowSelectNode,
		AllowedNodes:        settings.AllowedNodes,
	}
}

const sensitiveTag = "redacted"

const (
	RedactLevelAnonymous = iota
	RedactLevelUser
)

// BuildUserRedacted Serialize a user without sensitive information.
// The email is only exposed when the requester is a signed-in user (level >=
// RedactLevelUser) and the `expose_user_email` setting is enabled.
func BuildUserRedacted(ctx context.Context, u *ent.User, level int, idEncoder hashid.Encoder) User {
	userRaw := BuildUser(u, idEncoder)

	user := User{
		ID:                  userRaw.ID,
		Nickname:            userRaw.Nickname,
		Avatar:              userRaw.Avatar,
		CreatedAt:           userRaw.CreatedAt,
		ShareLinksInProfile: userRaw.ShareLinksInProfile,
	}

	if userRaw.Group != nil {
		user.Group = RedactedGroup(userRaw.Group)
	}

	if level == RedactLevelUser && dependency.FromContext(ctx).SettingProvider().ExposeUserEmail(ctx) {
		user.Email = userRaw.Email
	}

	return user
}

// BuildGroupRedacted Serialize a group without sensitive information.
func RedactedGroup(g *Group) *Group {
	if g == nil {
		return nil
	}

	return &Group{
		ID:   g.ID,
		Name: g.Name,
	}
}
