package types

import (
	"time"
)

// UserSetting 用户其他配置
type (
	UserSetting struct {
		ProfileOff          bool                     `json:"profile_off,omitempty"`
		PreferredTheme      string                   `json:"preferred_theme,omitempty"`
		VersionRetention    bool                     `json:"version_retention,omitempty"`
		VersionRetentionExt []string                 `json:"version_retention_ext,omitempty"`
		VersionRetentionMax int                      `json:"version_retention_max,omitempty"`
		Pined               []PinedFile              `json:"pined,omitempty"`
		Language            string                   `json:"email_language,omitempty"`
		DisableViewSync     bool                     `json:"disable_view_sync,omitempty"`
		FsViewMap           map[string]ExplorerView  `json:"fs_view_map,omitempty"`
		ShareLinksInProfile ShareLinksInProfileLevel `json:"share_links_in_profile,omitempty"`
		// ExtraStorage is purchased capacity (bytes) from storage packs.
		ExtraStorage int64 `json:"extra_storage,omitempty"`
		// Points balance cache; the points ledger table is the source of truth.
		Points int64 `json:"points,omitempty"`
	}

	ShareLinksInProfileLevel string

	PinedFile struct {
		Uri  string `json:"uri"`
		Name string `json:"name,omitempty"`
		// Locked 为 true 时该置顶项来自用户组默认置顶（Pro: 默认固定分享），用户无法取消。
		Locked bool `json:"locked,omitempty"`
	}

	// GroupSetting 用户组其他配置
	GroupSetting struct {
		CompressSize          int64                  `json:"compress_size,omitempty"` // 可压缩大小
		DecompressSize        int64                  `json:"decompress_size,omitempty"`
		RemoteDownloadOptions map[string]interface{} `json:"remote_download_options,omitempty"` // 离线下载用户组配置
		SourceBatchSize       int                    `json:"source_batch,omitempty"`
		Aria2BatchSize        int                    `json:"aria2_batch,omitempty"`
		MaxWalkedFiles        int                    `json:"max_walked_files,omitempty"`
		TrashRetention        int                    `json:"trash_retention,omitempty"`
		RedirectedSource      bool                   `json:"redirected_source,omitempty"`
		// AvailablePolicies 用户组可用的存储策略 ID 列表（Pro: 可用存储策略多选）。
		// 为空表示不做限制，只使用用户组绑定的默认策略。
		AvailablePolicies []int `json:"available_policies,omitempty"`
		// DefaultPinnedShares 用户组默认置顶的分享 ID 列表（数字 ID，Pro: 默认固定分享）。
		// 组内用户登录后会在侧边栏顶部看到这些分享，且无法取消置顶。
		DefaultPinnedShares []int `json:"default_pinned_shares,omitempty"`
		// FolderDirectLink 允许组内用户为目录生成直链（Pro: 目录直链）。
		// 目录直链访问时由服务端现场打包 ZIP 返回。
		FolderDirectLink bool `json:"folder_direct_link,omitempty"`
		// AllowedNodes 用户组可用的任务处理节点 ID 列表（数字 ID，Pro: 可用节点）。
		// 为空表示不做限制；覆盖的任务范围：离线下载、压缩/解压缩、存储策略转移。
		AllowedNodes []int `json:"allowed_nodes,omitempty"`
		// AllowSelectNode 允许组内用户在创建任务前自行选择处理节点（Pro: 允许选择节点）。
		AllowSelectNode bool `json:"allow_select_node,omitempty"`
	}

	// PolicySetting 非公有的存储策略属性
	PolicySetting struct {
		// Upyun访问Token
		Token string `json:"token"`
		// 允许的文件扩展名
		FileType []string `json:"file_type"`
		// IsFileTypeDenyList Whether above list is a deny list.
		IsFileTypeDenyList bool `json:"is_file_type_deny_list,omitempty"`
		// FileRegexp 文件扩展名正则表达式
		NameRegexp string `json:"file_regexp,omitempty"`
		// IsNameRegexp Whether above regexp is a deny list.
		IsNameRegexpDenyList bool `json:"is_name_regexp_deny_list,omitempty"`
		// OauthRedirect Oauth 重定向地址
		OauthRedirect string `json:"od_redirect,omitempty"`
		// CustomProxy whether to use custom-proxy to get file content
		CustomProxy bool `json:"custom_proxy,omitempty"`
		// Yun139 (China Mobile Cloud Drive) credentials. The access token is
		// refreshed by the yun139_token_refresh cron when username/password or
		// mail cookies are configured.
		Yun139Token        string `json:"yun139_token,omitempty"`
		Yun139Username     string `json:"yun139_username,omitempty"`
		Yun139Password     string `json:"yun139_password,omitempty"`
		Yun139MailCookies  string `json:"yun139_mail_cookies,omitempty"`
		Yun139RootFolderID string `json:"yun139_root_folder_id,omitempty"`
		// LoadBalanceSubPolicies 负载均衡策略的子策略与权重（Pro: 负载均衡存储策略）。
		// 每次上传按权重随机挑选一个子策略，实际文件落在被选中的子策略上。
		LoadBalanceSubPolicies []LoadBalanceSubPolicy `json:"load_balance_sub_policies,omitempty"`
		// ProxyServer 反代地址
		ProxyServer string `json:"proxy_server,omitempty"`
		// InternalProxy whether to use StorFlow internal proxy to get file content
		InternalProxy bool `json:"internal_proxy,omitempty"`
		// OdDriver OneDrive 驱动器定位符
		OdDriver string `json:"od_driver,omitempty"`
		// Region 区域代码
		Region string `json:"region,omitempty"`
		// ServerSideEndpoint 服务端请求使用的 Endpoint，为空时使用 Policy.Server 字段
		ServerSideEndpoint string `json:"server_side_endpoint,omitempty"`
		// 分片上传的分片大小
		ChunkSize int64 `json:"chunk_size,omitempty"`
		// 每秒对存储端的 API 请求上限
		TPSLimit float64 `json:"tps_limit,omitempty"`
		// 每秒 API 请求爆发上限
		TPSLimitBurst int `json:"tps_limit_burst,omitempty"`
		// Set this to `true` to force the request to use path-style addressing,
		// i.e., `http://s3.amazonaws.com/BUCKET/KEY `
		S3ForcePathStyle bool `json:"s3_path_style"`
		// File extensions that support thumbnail generation using native policy API.
		ThumbExts []string `json:"thumb_exts,omitempty"`
		// Whether to support all file extensions for thumbnail generation.
		ThumbSupportAllExts bool `json:"thumb_support_all_exts,omitempty"`
		// ThumbMaxSize indicates the maximum allowed size of a thumbnail. 0 indicates that no limit is set.
		ThumbMaxSize int64 `json:"thumb_max_size,omitempty"`
		// Whether to upload file through server's relay.
		Relay bool `json:"relay,omitempty"`
		// Whether to pre allocate space for file before upload in physical disk.
		PreAllocate bool `json:"pre_allocate,omitempty"`
		// MediaMetaExts file extensions that support media meta generation using native policy API.
		MediaMetaExts []string `json:"media_meta_exts,omitempty"`
		// MediaMetaGeneratorProxy whether to use local proxy to generate media meta.
		MediaMetaGeneratorProxy bool `json:"media_meta_generator_proxy,omitempty"`
		// ThumbGeneratorProxy whether to use local proxy to generate thumbnail.
		ThumbGeneratorProxy bool `json:"thumb_generator_proxy,omitempty"`
		// NativeMediaProcessing whether to use native media processing API from storage provider.
		NativeMediaProcessing bool `json:"native_media_processing"`
		// S3DeleteBatchSize the number of objects to delete in each batch.
		S3DeleteBatchSize int `json:"s3_delete_batch_size,omitempty"`
		// StreamSaver whether to use stream saver to download file in Web.
		StreamSaver bool `json:"stream_saver,omitempty"`
		// UseCname whether to use CNAME for endpoint (OSS).
		UseCname bool `json:"use_cname,omitempty"`
		// CDN domain does not need to be signed.
		SourceAuth bool `json:"source_auth,omitempty"`
		// QiniuUploadCdn whether to use CDN for Qiniu upload.
		QiniuUploadCdn bool `json:"qiniu_upload_cdn,omitempty"`
		// ChunkConcurrency the number of chunks to upload concurrently.
		ChunkConcurrency int `json:"chunk_concurrency,omitempty"`
		// Whether to enable file encryption.
		Encryption bool `json:"encryption,omitempty"`
	}

	// LoadBalanceSubPolicy 是负载均衡策略的子策略条目：实际文件会落在 PolicyID 上，
	// Weight 决定被选中的相对概率（<=0 时按 1 处理）。
	LoadBalanceSubPolicy struct {
		PolicyID int `json:"policy_id"`
		Weight   int `json:"weight"`
	}

	FileType         int
	EntityType       int
	GroupPermission  int
	FilePermission   int
	DavAccountOption int
	NodeCapability   int

	NodeSetting struct {
		Provider            DownloaderProvider `json:"provider,omitempty"`
		*QBittorrentSetting `json:"qbittorrent,omitempty"`
		*Aria2Setting       `json:"aria2,omitempty"`
		// 下载监控间隔
		Interval       int  `json:"interval,omitempty"`
		WaitForSeeding bool `json:"wait_for_seeding,omitempty"`
		// URLValidation controls SSRF policy applied to user-supplied URLs
		// fetched by this node's downloader. nil means the secure default
		// (validation on, no extra allowlist) — existing nodes upgraded in
		// place stay protected without admin action.
		URLValidation *URLValidationSetting `json:"url_validation,omitempty"`
	}

	URLValidationSetting struct {
		// Disabled turns the SSRF check off entirely on this node. Only set
		// this when the downloader runs in a network segment that cannot
		// reach any internal asset (e.g. dedicated egress namespace).
		Disabled bool `json:"disabled,omitempty"`
		// AllowedHosts is a list of hostnames or IP literals that bypass all
		// checks. Exact, case-insensitive match against url.Hostname().
		AllowedHosts []string `json:"allowed_hosts,omitempty"`
		// AllowedCIDRs is a list of CIDR blocks (IPv4 or IPv6) whose IPs are
		// treated as safe even if they would otherwise be rejected (private,
		// link-local, etc.). Use this to whitelist a LAN range like
		// "192.168.10.0/24" so a local NAS can be fetched.
		AllowedCIDRs []string `json:"allowed_cidrs,omitempty"`
	}

	DownloaderProvider string

	QBittorrentSetting struct {
		Server   string         `json:"server,omitempty"`
		User     string         `json:"user,omitempty"`
		Password string         `json:"password,omitempty"`
		Options  map[string]any `json:"options,omitempty"`
		TempPath string         `json:"temp_path,omitempty"`
	}

	Aria2Setting struct {
		Server   string         `json:"server,omitempty"`
		Token    string         `json:"token,omitempty"`
		Options  map[string]any `json:"options,omitempty"`
		TempPath string         `json:"temp_path,omitempty"`
	}

	TaskPublicState struct {
		Error            string          `json:"error,omitempty"`
		ErrorHistory     []string        `json:"error_history,omitempty"`
		ExecutedDuration time.Duration   `json:"executed_duration,omitempty"`
		RetryCount       int             `json:"retry_count,omitempty"`
		ResumeTime       int64           `json:"resume_time,omitempty"`
		SlaveTaskProps   *SlaveTaskProps `json:"slave_task_props,omitempty"`
	}

	SlaveTaskProps struct {
		NodeID            int    `json:"node_id,omitempty"`
		MasterSiteURl     string `json:"master_site_u_rl,omitempty"`
		MasterSiteID      string `json:"master_site_id,omitempty"`
		MasterSiteVersion string `json:"master_site_version,omitempty"`
	}

	EntityProps struct {
		UnlinkOnly      bool             `json:"unlink_only,omitempty"`
		EncryptMetadata *EncryptMetadata `json:"encrypt_metadata,omitempty"`
	}

	Cipher string

	EncryptMetadata struct {
		Algorithm    Cipher `json:"algorithm"`
		Key          []byte `json:"key"`
		KeyPlainText []byte `json:"key_plain_text,omitempty"`
		IV           []byte `json:"iv"`
	}

	DavAccountProps struct {
	}

	PolicyType string

	FileProps struct {
		View *ExplorerView `json:"view,omitempty"`
	}

	ExplorerView struct {
		PageSize       int              `json:"page_size" binding:"min=50"`
		Order          string           `json:"order,omitempty" binding:"max=255"`
		OrderDirection string           `json:"order_direction,omitempty" binding:"eq=asc|eq=desc"`
		View           string           `json:"view,omitempty" binding:"eq=list|eq=grid|eq=gallery"`
		Thumbnail      bool             `json:"thumbnail,omitempty"`
		GalleryWidth   int              `json:"gallery_width,omitempty" binding:"min=50,max=500"`
		Columns        []ListViewColumn `json:"columns,omitempty" binding:"max=1000"`
	}

	ListViewColumn struct {
		Type  int             `json:"type" binding:"min=0"`
		Width *int            `json:"width,omitempty"`
		Props *ColumTypeProps `json:"props,omitempty"`
	}

	ColumTypeProps struct {
		MetadataKey   string `json:"metadata_key,omitempty" binding:"max=255"`
		CustomPropsID string `json:"custom_props_id,omitempty" binding:"max=255"`
	}

	ShareProps struct {
		// Whether to share view setting from owner
		ShareView bool `json:"share_view,omitempty"`
		// Whether to automatically show readme file in share view
		ShowReadMe bool `json:"show_read_me,omitempty"`
		// Permission applied to everyone visiting this share, including
		// anonymous visitors. Empty value means read-only.
		Permission SharePermission `json:"permission,omitempty"`
		// Grants overrides Permission for specific users, enabling
		// collaborative shares with per-user permission.
		Grants []ShareGrant `json:"grants,omitempty"`
		// PayEnabled marks this share as a paid share: visitors can read the
		// listing metadata but must own an entitlement issued by the shop
		// before the content can be listed, downloaded or saved.
		PayEnabled bool `json:"pay_enabled,omitempty"`
		// Price is the one-time price of the share in cents.
		Price int64 `json:"price,omitempty"`
		// PayCurrency is the currency of Price, defaults to CNY.
		PayCurrency string `json:"pay_currency,omitempty"`
		// IsDefault marks the share as a site default share. Newly registered
		// users can discover default shares on their home page.
		IsDefault bool `json:"is_default,omitempty"`
		// Internal marks a share that belongs to another subsystem (the shop
		// module) rather than to a user. Internal shares are hidden from the
		// share management UI and cannot be deleted by hand: deleting one would
		// break the deliveries of the subsystem that owns it.
		Internal bool `json:"internal,omitempty"`
	}
	OAuthClientProps struct {
		Description     string `json:"description,omitempty"`
		Icon            string `json:"icon,omitempty"`
		RefreshTokenTTL int64  `json:"refresh_token_ttl,omitempty"` // in seconds, 0 means default
	}

	FileTypeIconSetting struct {
		Exts      []string `json:"exts"`
		Icon      string   `json:"icon,omitempty"`
		Color     string   `json:"color,omitempty"`
		ColorDark string   `json:"color_dark,omitempty"`
		Img       string   `json:"img,omitempty"`
	}
)

// AllowedStoragePolicies 返回用户组允许使用的存储策略 ID 列表。
// defaultPolicyID 是用户组绑定的默认策略；当 available_policies 未配置时，
// 退回为「只允许默认策略」的旧行为。返回空列表表示不做限制。
func AllowedStoragePolicies(setting *GroupSetting, defaultPolicyID int) []int {
	if setting != nil && len(setting.AvailablePolicies) > 0 {
		return setting.AvailablePolicies
	}

	if defaultPolicyID > 0 {
		return []int{defaultPolicyID}
	}

	return nil
}

// SharePermission is the collaboration level of a share link.
type SharePermission string

// ShareGrant overrides the share-wide permission for one user. UserID is the
// hash encoded user ID.
type ShareGrant struct {
	UserID     string          `json:"user_id"`
	Permission SharePermission `json:"permission"`
}

// SharePermission levels of a collaborative share.
const (
	// SharePermissionRead only allows reading the shared tree.
	SharePermissionRead SharePermission = "read"
	// SharePermissionUpload allows reading and uploading new files.
	SharePermissionUpload SharePermission = "upload"
	// SharePermissionEdit allows full collaboration: upload, rename, delete.
	SharePermissionEdit SharePermission = "edit"
)

// NormalizeSharePermission falls back to read for unknown values.
func NormalizeSharePermission(p SharePermission) SharePermission {
	switch p {
	case SharePermissionUpload, SharePermissionEdit:
		return p
	default:
		return SharePermissionRead
	}
}

const (
	GroupPermissionIsAdmin = GroupPermission(iota)
	GroupPermissionIsAnonymous
	GroupPermissionShare
	GroupPermissionWebDAV
	GroupPermissionArchiveDownload
	GroupPermissionArchiveTask
	GroupPermissionWebDAVProxy
	GroupPermissionShareDownload
	// GroupPermissionShareFree lets members open any paid share without buying
	// it (Pro: 无需购买分享链接). Keep the numeric layout stable: the value is
	// persisted in the group permission bit set and mirrored by the frontend
	// enum `GroupPermission` in assets/src/api/user.ts.
	GroupPermissionShareFree
	GroupPermissionRemoteDownload
	// GroupPermissionMigratePolicy lets members create storage policy migration
	// tasks for their own files (Pro: 存储策略转移).
	GroupPermissionMigratePolicy
	GroupPermissionRedirectedSource // not used
	GroupPermissionAdvanceDelete
	// GroupPermissionEscalateAnonymity lets members grant more than read-only
	// permission to anonymous share visitors (Pro: 提升匿名用户权限).
	GroupPermissionEscalateAnonymity
	GroupPermission_CommunityPlaceholder4
	GroupPermissionSetExplicitUser_placeholder
	GroupPermissionIgnoreFileOwnership // not used
	GroupPermissionUniqueRedirectDirectLink
)

const (
	NodeCapabilityNone NodeCapability = iota
	NodeCapabilityCreateArchive
	NodeCapabilityExtractArchive
	NodeCapabilityRemoteDownload
	NodeCapability_CommunityPlaceholder
)

const (
	FileTypeFile FileType = iota
	FileTypeFolder
)

const (
	EntityTypeVersion EntityType = iota
	EntityTypeThumbnail
	EntityTypeLivePhoto
)

func FileTypeFromString(s string) FileType {
	switch s {
	case "file":
		return FileTypeFile
	case "folder":
		return FileTypeFolder
	}
	return -1
}

const (
	DavAccountReadOnly DavAccountOption = iota
	DavAccountProxy
	DavAccountDisableSysFiles
)

const (
	PolicyTypeLocal  = "local"
	PolicyTypeQiniu  = "qiniu"
	PolicyTypeUpyun  = "upyun"
	PolicyTypeOss    = "oss"
	PolicyTypeCos    = "cos"
	PolicyTypeS3     = "s3"
	PolicyTypeKs3    = "ks3"
	PolicyTypeOd     = "onedrive"
	PolicyTypeRemote = "remote"
	PolicyTypeObs    = "obs"
	// PolicyTypeYun139 is the China Mobile Cloud Drive (139) policy.
	PolicyTypeYun139 = "yun139"
	// PolicyTypeLoadBalance is a composite policy that picks one of its sub
	// policies for each upload (Pro: 负载均衡存储策略).
	PolicyTypeLoadBalance = "load_balance"
)

const (
	DownloaderProviderAria2       = DownloaderProvider("aria2")
	DownloaderProviderQBittorrent = DownloaderProvider("qbittorrent")
)

type (
	ViewerAction string
	ViewerType   string
)

const (
	ViewerActionView = "view"
	ViewerActionEdit = "edit"

	ViewerTypeBuiltin = "builtin"
	ViewerTypeWopi    = "wopi"
	ViewerTypeCustom  = "custom"
)

type (
	Viewer struct {
		ID                      string                             `json:"id"`
		Type                    ViewerType                         `json:"type"`
		DisplayName             string                             `json:"display_name"`
		Exts                    []string                           `json:"exts"`
		Url                     string                             `json:"url,omitempty"`
		Icon                    string                             `json:"icon,omitempty"`
		WopiActions             map[string]map[ViewerAction]string `json:"wopi_actions,omitempty"`
		Props                   map[string]string                  `json:"props,omitempty"`
		MaxSize                 int64                              `json:"max_size,omitempty"`
		Disabled                bool                               `json:"disabled,omitempty"`
		Templates               []NewFileTemplate                  `json:"templates,omitempty"`
		Platform                string                             `json:"platform,omitempty"`
		RequiredGroupPermission []GroupPermission                  `json:"required_group_permission,omitempty"`
	}
	ViewerGroup struct {
		Viewers []Viewer `json:"viewers"`
	}

	DefaultViewerMapping map[string]string

	NewFileTemplate struct {
		Ext         string `json:"ext"`
		DisplayName string `json:"display_name"`
	}
)

type (
	CustomPropsType string
	CustomProps     struct {
		ID      string          `json:"id"`
		Name    string          `json:"name"`
		Type    CustomPropsType `json:"type"`
		Max     int             `json:"max,omitempty"`
		Min     int             `json:"min,omitempty"`
		Default string          `json:"default,omitempty"`
		Options []string        `json:"options,omitempty"`
		Icon    string          `json:"icon,omitempty"`
	}
)

const (
	CustomPropsTypeText        = "text"
	CustomPropsTypeNumber      = "number"
	CustomPropsTypeBoolean     = "boolean"
	CustomPropsTypeSelect      = "select"
	CustomPropsTypeMultiSelect = "multi_select"
	CustomPropsTypeLink        = "link"
	CustomPropsTypeRating      = "rating"
	// CustomPropsTypeUser stores the hash id of a site user.
	CustomPropsTypeUser = "user"
)

const (
	ProfilePublicShareOnly = ShareLinksInProfileLevel("")
	ProfileAllShare        = ShareLinksInProfileLevel("all_share")
	ProfileHideShare       = ShareLinksInProfileLevel("hide_share")
)

const (
	CipherAES256CTR Cipher = "aes-256-ctr"
)

const (
	ScopeProfile               = "profile"
	ScopeEmail                 = "email"
	ScopeOpenID                = "openid"
	ScopeOfflineAccess         = "offline_access"
	ScopeUserInfoRead          = "UserInfo.Read"
	ScopeUserInfoWrite         = "UserInfo.Write"
	ScopeUserSecurityInfoRead  = "UserSecurityInfo.Read"
	ScopeUserSecurityInfoWrite = "UserSecurityInfo.Write"
	ScopeWorkflowRead          = "Workflow.Read"
	ScopeWorkflowWrite         = "Workflow.Write"
	ScopeAdminRead             = "Admin.Read"
	ScopeAdminWrite            = "Admin.Write"
	ScopeVASRead               = "VAS.Read"
	ScopeVASWrite              = "VAS.Write"
	ScopeFilesRead             = "Files.Read"
	ScopeFilesWrite            = "Files.Write"
	ScopeSharesRead            = "Shares.Read"
	ScopeSharesWrite           = "Shares.Write"
	ScopeFinanceRead           = "Finance.Read"
	ScopeFinanceWrite          = "Finance.Write"
	ScopeDavAccountRead        = "DavAccount.Read"
	ScopeDavAccountWrite       = "DavAccount.Write"
)
