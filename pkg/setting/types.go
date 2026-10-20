package setting

import (
	"time"
)

type PWASetting struct {
	SmallIcon       string
	MediumIcon      string
	LargeIcon       string
	Display         string
	ThemeColor      string
	BackgroundColor string
}

type SiteBasic struct {
	Name         string
	Title        string
	ID           string
	Description  string
	Script       string
	Announcement string
}

type CaptchaType string

const (
	CaptchaNormal    = CaptchaType("normal")
	CaptchaReCaptcha = CaptchaType("recaptcha")
	CaptchaTcaptcha  = CaptchaType("tcaptcha")
	CaptchaTurnstile = CaptchaType("turnstile")
	CaptchaCap       = CaptchaType("cap")
)

type ReCaptcha struct {
	Key    string
	Secret string
}

type TcCaptcha struct {
	AppID        string
	AppSecretKey string
	SecretID     string
	SecretKey    string
}

type Turnstile struct {
	Key    string
	Secret string
}

type Cap struct {
	InstanceURL string
	SiteKey     string
	SecretKey   string
	AssetServer string
}

type SMTP struct {
	FromName        string
	From            string
	Host            string
	ReplyTo         string
	User            string
	Password        string
	ForceEncryption bool
	Port            int
	Keepalive       int
}

type TokenAuth struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type DBFS struct {
	UseCursorPagination        bool
	MaxPageSize                int
	MaxRecursiveSearchedFolder int
	UseSSEForSearch            bool
}

type (
	QueueType    string
	QueueSetting struct {
		WorkerNum          int
		MaxExecution       time.Duration
		BackoffFactor      float64
		BackoffMaxDuration time.Duration
		MaxRetry           int
		RetryDelay         time.Duration
	}
)

type ThumbEncode struct {
	Quality int
	Format  string
}

var (
	QueueTypeMediaMeta      = QueueType("media_meta")
	QueueTypeIOIntense      = QueueType("io_intense")
	QueueTypeThumb          = QueueType("thumb")
	QueueTypeEntityRecycle  = QueueType("recycle")
	QueueTypeSlave          = QueueType("slave")
	QueueTypeRemoteDownload = QueueType("remote_download")
)

type CronType string

var (
	CronTypeEntityCollect    = CronType("entity_collect")
	CronTypeTrashBinCollect  = CronType("trash_bin_collect")
	CronTypeOauthCredRefresh = CronType("oauth_cred_refresh")
	CronTypeGroupExpire      = CronType("group_expire")
	// CronTypeYun139TokenRefresh refreshes the access token of yun139 policies.
	CronTypeYun139TokenRefresh = CronType("yun139_token_refresh")
)

type Theme struct {
	Themes       string
	DefaultTheme string
}

type Logo struct {
	Normal string
	Light  string
}

type LegalDocuments struct {
	PrivacyPolicy  string
	TermsOfService string
}

type CaptchaMode int

const (
	CaptchaModeNumber = CaptchaMode(iota)
	CaptchaModeAlphabet
	CaptchaModeArithmetic
	CaptchaModeNumberAlphabet
)

type Captcha struct {
	Height             int
	Width              int
	Mode               CaptchaMode
	ComplexOfNoiseText int
	ComplexOfNoiseDot  int
	IsShowHollowLine   bool
	IsShowNoiseDot     bool
	IsShowNoiseText    bool
	IsShowSlimeLine    bool
	IsShowSineLine     bool
	Length             int
}

type ExplorerFrontendSettings struct {
	Icons string
}

type MapProvider string

const (
	MapProviderOpenStreetMap = MapProvider("openstreetmap")
	MapProviderGoogle        = MapProvider("google")
	MapProviderMapbox        = MapProvider("mapbox")
)

type MapGoogleTileType string

const (
	MapGoogleTileTypeRegular   = MapGoogleTileType("regular")
	MapGoogleTileTypeSatellite = MapGoogleTileType("satellite")
	MapGoogleTileTypeTerrain   = MapGoogleTileType("terrain")
)

type MapSetting struct {
	Provider       MapProvider
	GoogleTileType MapGoogleTileType
	MapboxAK       string
}

// Viewer related

type (
	SearchCategory string
)

const (
	CategoryUnknown  = SearchCategory("unknown")
	CategoryImage    = SearchCategory("image")
	CategoryVideo    = SearchCategory("video")
	CategoryAudio    = SearchCategory("audio")
	CategoryDocument = SearchCategory("document")
)

type AppSetting struct {
	Promotion        bool
	DesktopPromotion bool
	// Feedback / Forum 为客户端设置页展示的链接，空字符串表示不展示。
	Feedback string
	Forum    string
}

type EmailTemplate struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Language string `json:"language"`
}

type Avatar struct {
	Gravatar string `json:"gravatar"`
	Path     string `json:"path"`
}

type AvatarProcess struct {
	Path        string `json:"path"`
	MaxFileSize int64  `json:"max_file_size"`
	MaxWidth    int    `json:"max_width"`
}

type CustomNavItem struct {
	Icon string `json:"icon"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type CustomHTML struct {
	HeadlessFooter string `json:"headless_footer,omitempty"`
	HeadlessBody   string `json:"headless_bottom,omitempty"`
	SidebarBottom  string `json:"sidebar_bottom,omitempty"`
}

type FTSIndexType string

const (
	FTSIndexTypeNone        = FTSIndexType("")
	FTSIndexTypeMeilisearch = FTSIndexType("meilisearch")
)

type FTSExtractorType string

const (
	FTSExtractorTypeNone = FTSExtractorType("")
	FTSExtractorTypeTika = FTSExtractorType("tika")
)

type FTSIndexMeilisearchSetting struct {
	Endpoint         string
	APIKey           string
	PageSize         int
	EmbeddingEnbaled bool
	EmbeddingSetting string
}

type FTSTikaExtractorSetting struct {
	Endpoint    string
	Exts        []string
	MaxFileSize int64
}

type MasterEncryptKeyVaultType string

const (
	MasterEncryptKeyVaultTypeSetting = MasterEncryptKeyVaultType("setting")
	MasterEncryptKeyVaultTypeEnv     = MasterEncryptKeyVaultType("env")
	MasterEncryptKeyVaultTypeFile    = MasterEncryptKeyVaultType("file")
)

// EmailProviderFilter controls how the email domain filter is applied when a new
// account is registered (Pro: 邮箱域过滤).
type EmailProviderFilter string

const (
	EmailProviderFilterDisabled  = EmailProviderFilter("disabled")
	EmailProviderFilterWhitelist = EmailProviderFilter("whitelist")
	EmailProviderFilterBlacklist = EmailProviderFilter("blacklist")
)

// OIDC holds the OIDC SSO (G6) settings.
type OIDC struct {
	// Enabled is true when OIDC SSO login is turned on.
	Enabled bool
	// Endpoint is the IdP issuer used for the discovery document.
	Endpoint string
	// ClientID / ClientSecret are the OIDC client credentials.
	ClientID     string
	ClientSecret string
	// Scopes is the list of requested scopes.
	Scopes []string
	// DisplayName is the label of the login button, an i18n key or a literal.
	DisplayName string
	// AutoRedirect is true when the login page should jump to the IdP right away.
	AutoRedirect bool
	// PasswordLoginDisabled is true when the password login form should be hidden.
	PasswordLoginDisabled bool
	// AutoRegister is true when a new local account may be created on first login.
	AutoRegister bool
	// AutoLinkEmail is true when an existing account with the same email may be bound.
	AutoLinkEmail bool
	// RequireEmailVerified is true when the email_verified claim must be true.
	RequireEmailVerified bool
	// SubjectClaim / EmailClaim / NameClaim are the claim names used to read
	// the unique subject, the email address and the display name.
	SubjectClaim string
	EmailClaim   string
	NameClaim    string
}

// QQConfig holds the QQ Connect login settings. It reuses the OIDC binding
// table (issuer "qq") and the same frontend callback contract as OIDC.
type QQConfig struct {
	// Enabled is true when QQ Connect login is turned on.
	Enabled bool
	// AppID / AppSecret are the credentials of the QQ Connect application.
	AppID     string
	AppSecret string
	// DirectSignIn is true when the login page should jump to QQ right away
	// (the "direct_sign_in" field of qq_login_config).
	DirectSignIn bool
	// AutoRegister is true when a new local account may be created on first login.
	AutoRegister bool
	// DisplayName is the label of the login button, an i18n key or a literal.
	DisplayName string
	// APIBase is the base URL of the QQ OAuth2 endpoints
	// (authorize / token / me). Hidden advanced setting.
	APIBase string
	// GraphBase is the base URL of the QQ graph API (user/get_user_info).
	// Hidden advanced setting.
	GraphBase string
}
