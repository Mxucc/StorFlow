package setting

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/auth/requestinfo"
	"github.com/Mxucc/StorFlow/pkg/boolset"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
)

type (
	// Provider provides strong type setting access.
	Provider interface {
		// Site basic information
		SiteBasic(ctx context.Context) *SiteBasic
		// PWA related settings
		PWA(ctx context.Context) *PWASetting
		// RegisterEnabled returns true if public sign-up is enabled.
		RegisterEnabled(ctx context.Context) bool
		// FilterEmailProvider returns the email domain filter mode applied at registration.
		FilterEmailProvider(ctx context.Context) EmailProviderFilter
		// FilterEmailProviderRule returns the email domains used by the registration filter.
		FilterEmailProviderRule(ctx context.Context) []string
		// DisableSubAddressEmail returns true if email addresses containing "+" are
		// rejected at registration.
		DisableSubAddressEmail(ctx context.Context) bool
		// OIDC returns the OIDC SSO (G6) settings.
		OIDC(ctx context.Context) *OIDC
		// QQLogin returns true when the QQ Connect login is turned on.
		QQLogin(ctx context.Context) bool
		// QQConfig returns the QQ Connect login settings (credentials, hidden
		// advanced API base URLs, ...).
		QQConfig(ctx context.Context) *QQConfig
		// AuthnEnabled returns true if Webauthn is enabled.
		AuthnEnabled(ctx context.Context) bool
		// RegCaptchaEnabled returns true if registration captcha is enabled.
		RegCaptchaEnabled(ctx context.Context) bool
		// LoginCaptchaEnabled returns true if login captcha is enabled.
		LoginCaptchaEnabled(ctx context.Context) bool
		// ForgotPasswordCaptchaEnabled returns true if forgot password captcha is enabled.
		ForgotPasswordCaptchaEnabled(ctx context.Context) bool
		// CaptchaType returns the type of captcha used.
		CaptchaType(ctx context.Context) CaptchaType
		// ReCaptcha returns the Google reCaptcha settings.
		ReCaptcha(ctx context.Context) *ReCaptcha
		// TcCaptcha returns the Tencent Cloud Captcha settings.
		TcCaptcha(ctx context.Context) *TcCaptcha
		// TurnstileCaptcha returns the Cloudflare Turnstile settings.
		TurnstileCaptcha(ctx context.Context) *Turnstile
		// CapCaptcha returns the Cap settings.
		CapCaptcha(ctx context.Context) *Cap
		// EmailActivationEnabled returns true if email activation is required.
		EmailActivationEnabled(ctx context.Context) bool
		// DefaultGroup returns the default group ID for new users.
		DefaultGroup(ctx context.Context) int
		// SMTP returns the SMTP settings.
		SMTP(ctx context.Context) *SMTP
		// SiteURL returns the basic URL.
		SiteURL(ctx context.Context) *url.URL
		// SecretKey returns the secret key for general signature.
		SecretKey(ctx context.Context) string
		// ActivationEmailTemplate returns the email template for activation.
		ActivationEmailTemplate(ctx context.Context) []EmailTemplate
		// ResetEmailTemplate returns the email template for reset password.
		ResetEmailTemplate(ctx context.Context) []EmailTemplate
		// ReceiptEmailTemplate returns the email template for payment receipts.
		ReceiptEmailTemplate(ctx context.Context) []EmailTemplate
		// ExceedQuotaEmailTemplate returns the email template for storage quota exceed notices.
		ExceedQuotaEmailTemplate(ctx context.Context) []EmailTemplate
		// DefaultSymbolics returns the site-wide pinned share IDs applied to new users.
		DefaultSymbolics(ctx context.Context) []int
		// TokenAuth returns token based auth related settings.
		TokenAuth(ctx context.Context) *TokenAuth
		// HashIDSalt returns the salt used for hash ID generation.
		HashIDSalt(ctx context.Context) string
		// DBFS returns the DBFS related settings.
		DBFS(ctx context.Context) *DBFS
		// MaxBatchedFile returns the maximum number of files in a batch operation.
		MaxBatchedFile(ctx context.Context) int
		// UploadSessionTTL returns the TTL of upload session.
		UploadSessionTTL(ctx context.Context) time.Duration
		// MaxOnlineEditSize returns the maximum size of online editing.
		MaxOnlineEditSize(ctx context.Context) int64
		// SlaveRequestSignTTL returns the TTL of slave request signature.
		SlaveRequestSignTTL(ctx context.Context) int
		// ChunkRetryLimit returns the maximum number of chunk retries.
		ChunkRetryLimit(ctx context.Context) int
		// UseChunkBuffer returns true if chunk buffer is enabled.
		UseChunkBuffer(ctx context.Context) bool
		// Queue returns the queue settings.
		Queue(ctx context.Context, queueType QueueType) *QueueSetting
		// EntityUrlCacheMargin returns the safe margin of entity URL cache. URL cache will
		// expire in (EntityUrlValidDuration - EntityUrlCacheMargin).
		EntityUrlCacheMargin(ctx context.Context) int
		// EntityUrlValidDuration returns the valid duration of entity URL.
		EntityUrlValidDuration(ctx context.Context) time.Duration
		// PublicResourceMaxAge returns the max age of public resources.
		PublicResourceMaxAge(ctx context.Context) int
		// MediaMetaEnabled returns true if media meta is enabled.
		MediaMetaEnabled(ctx context.Context) bool
		// MediaMetaExifEnabled returns true if media meta exif is enabled.
		MediaMetaExifEnabled(ctx context.Context) bool
		// MediaMetaExifSizeLimit returns the size limit of media meta exif. first return value is for local sources;
		// second return value is for remote sources.
		MediaMetaExifSizeLimit(ctx context.Context) (int64, int64)
		// MediaMetaExifBruteForce returns true if media meta exif brute force search is enabled.
		MediaMetaExifBruteForce(ctx context.Context) bool
		// MediaMetaMusicEnabled returns true if media meta audio is enabled.
		MediaMetaMusicEnabled(ctx context.Context) bool
		// MediaMetaMusicSizeLimit returns the size limit of media meta audio. first return value is for local sources;
		MediaMetaMusicSizeLimit(ctx context.Context) (int64, int64)
		// MediaMetaFFProbeEnabled returns true if media meta ffprobe is enabled.
		MediaMetaFFProbeEnabled(ctx context.Context) bool
		// MediaMetaFFProbeSizeLimit returns the size limit of media meta ffprobe. first return value is for local sources;
		MediaMetaFFProbeSizeLimit(ctx context.Context) (int64, int64)
		// MediaMetaFFProbePath returns the path of ffprobe executable.
		MediaMetaFFProbePath(ctx context.Context) string
		// MediaMetaGeocodingEnabled returns true if media meta geocoding is enabled.
		MediaMetaGeocodingEnabled(ctx context.Context) bool
		// MediaMetaGeocodingMapboxAK returns the Mapbox access token.
		MediaMetaGeocodingMapboxAK(ctx context.Context) string
		// ThumbSize returns the size limit of thumbnails.
		ThumbSize(ctx context.Context) (int, int)
		// ThumbEncode returns the thumbnail encoding settings.
		ThumbEncode(ctx context.Context) *ThumbEncode
		// BuiltinThumbGeneratorEnabled returns true if builtin thumb generator is enabled.
		BuiltinThumbGeneratorEnabled(ctx context.Context) bool
		// BuiltinThumbMaxSize returns the maximum size of builtin thumb generator.
		BuiltinThumbMaxSize(ctx context.Context) int64
		// TempPath returns the path of temporary directory.
		TempPath(ctx context.Context) string
		// ThumbEntitySuffix returns the suffix of entity thumbnails.
		ThumbEntitySuffix(ctx context.Context) string
		// ThumbSlaveSidecarSuffix returns the suffix of slave sidecar thumbnails.
		ThumbSlaveSidecarSuffix(ctx context.Context) string
		// ThumbGCAfterGen returns true if force GC is invoked after thumb generation.
		ThumbGCAfterGen(ctx context.Context) bool
		// FFMpegPath returns the path of ffmpeg executable.
		FFMpegPath(ctx context.Context) string
		// FFMpegThumbGeneratorEnabled returns true if ffmpeg thumb generator is enabled.
		FFMpegThumbGeneratorEnabled(ctx context.Context) bool
		// FFMpegThumbExts returns the supported extensions of ffmpeg thumb generator.
		FFMpegThumbExts(ctx context.Context) []string
		// FFMpegThumbSeek returns the seek time of ffmpeg thumb generator.
		FFMpegThumbSeek(ctx context.Context) string
		// FFMpegThumbMaxSize returns the maximum size of ffmpeg thumb generator.
		FFMpegThumbMaxSize(ctx context.Context) int64
		// VipsThumbGeneratorEnabled returns true if vips thumb generator is enabled.
		VipsThumbGeneratorEnabled(ctx context.Context) bool
		// VipsThumbExts returns the supported extensions of vips thumb generator.
		VipsThumbExts(ctx context.Context) []string
		// VipsThumbMaxSize returns the maximum size of vips thumb generator.
		VipsThumbMaxSize(ctx context.Context) int64
		// VipsPath returns the path of vips executable.
		VipsPath(ctx context.Context) string
		// LibreOfficeThumbGeneratorEnabled returns true if libreoffice thumb generator is enabled.
		LibreOfficeThumbGeneratorEnabled(ctx context.Context) bool
		// LibreOfficeThumbExts returns the supported extensions of libreoffice thumb generator.
		LibreOfficeThumbExts(ctx context.Context) []string
		// LibreOfficeThumbMaxSize returns the maximum size of libreoffice thumb generator.
		LibreOfficeThumbMaxSize(ctx context.Context) int64
		// LibreOfficePath returns the path of libreoffice executable.
		LibreOfficePath(ctx context.Context) string
		// MusicCoverThumbGeneratorEnabled returns true if music cover thumb generator is enabled.
		MusicCoverThumbGeneratorEnabled(ctx context.Context) bool
		// MusicCoverThumbMaxSize returns the maximum size of music cover thumb generator.
		MusicCoverThumbMaxSize(ctx context.Context) int64
		// MusicCoverThumbExts returns the supported extensions of music cover thumb generator.
		MusicCoverThumbExts(ctx context.Context) []string
		// Cron returns the crontab settings.
		Cron(ctx context.Context, t CronType) string
		// Theme returns the theme settings.
		Theme(ctx context.Context) *Theme
		// Logo returns the logo settings.
		Logo(ctx context.Context) *Logo
		// LegalDocuments returns the legal documents settings.
		LegalDocuments(ctx context.Context) *LegalDocuments
		// Captcha returns the captcha settings.
		Captcha(ctx context.Context) *Captcha
		// ExplorerFrontendSettings returns the explorer frontend settings.
		ExplorerFrontendSettings(ctx context.Context) *ExplorerFrontendSettings
		// SearchCategoryQuery returns the search category query.
		SearchCategoryQuery(ctx context.Context, category SearchCategory) string
		// EmojiPresets returns the emoji presets used in file icon customization.
		EmojiPresets(ctx context.Context) string
		// MapSetting returns the EXIF GPS map related settings.
		MapSetting(ctx context.Context) *MapSetting
		// FolderPropsCacheTTL returns the cache TTL of folder summary.
		FolderPropsCacheTTL(ctx context.Context) int
		// FileViewers returns the file viewers settings.
		FileViewers(ctx context.Context) []types.ViewerGroup
		// ViewerSessionTTL returns the TTL of viewer session.
		ViewerSessionTTL(ctx context.Context) int
		// MimeMapping returns the extension to MIME mapping settings.
		MimeMapping(ctx context.Context) string
		// MaxParallelTransfer returns the maximum parallel transfer in workflows.
		MaxParallelTransfer(ctx context.Context) int
		// ArchiveDownloadSessionTTL returns the TTL of archive download session.
		ArchiveDownloadSessionTTL(ctx context.Context) int
		// AppSetting returns the app related settings.
		AppSetting(ctx context.Context) *AppSetting
		// Avatar returns the avatar settings.
		Avatar(ctx context.Context) *Avatar
		// AvatarProcess returns the avatar process settings.
		AvatarProcess(ctx context.Context) *AvatarProcess
		// UseFirstSiteUrl returns the first site URL.
		AllSiteURLs(ctx context.Context) []*url.URL
		// LibRawThumbGeneratorEnabled returns true if libraw thumb generator is enabled.
		LibRawThumbGeneratorEnabled(ctx context.Context) bool
		// LibRawThumbMaxSize returns the maximum size of libraw thumb generator.
		LibRawThumbMaxSize(ctx context.Context) int64
		// LibRawThumbExts returns the supported extensions of libraw thumb generator.
		LibRawThumbExts(ctx context.Context) []string
		// LibRawThumbPath returns the path of libraw executable.
		LibRawThumbPath(ctx context.Context) string
		// CustomProps returns the custom props settings.
		CustomProps(ctx context.Context) []types.CustomProps
		// CustomNavItems returns the custom nav items settings.
		CustomNavItems(ctx context.Context) []CustomNavItem
		// AuditEventEnabled reports whether events of the given audit type are recorded.
		AuditEventEnabled(ctx context.Context, ev eventtype.EventType) bool
		// CustomHTML returns the custom HTML settings.
		CustomHTML(ctx context.Context) *CustomHTML
		// FFMpegExtraArgs returns the extra arguments of ffmpeg thumb generator.
		FFMpegExtraArgs(ctx context.Context) string
		// MasterEncryptKey returns the master encrypt key.
		MasterEncryptKey(ctx context.Context) []byte
		// MasterEncryptKeyVault returns the master encrypt key vault type.
		MasterEncryptKeyVault(ctx context.Context) MasterEncryptKeyVaultType
		// MasterEncryptKeyFile returns the master encrypt key file.
		MasterEncryptKeyFile(ctx context.Context) string
		// ShowEncryptionStatus returns true if encryption status is shown.
		ShowEncryptionStatus(ctx context.Context) bool
		// EventHubMaxOfflineDuration returns the maximum offline duration of event hub.
		EventHubMaxOfflineDuration(ctx context.Context) time.Duration
		// EventHubEnabled returns true if event hub is enabled.
		EventHubEnabled(ctx context.Context) bool
		// EventHubDebounceDelay returns the debounce delay of event hub.
		EventHubDebounceDelay(ctx context.Context) time.Duration
		// FTSEnabled returns true if full-text search is enabled.
		FTSEnabled(ctx context.Context) bool
		// FTSIndexType returns the full-text search index type.
		FTSIndexType(ctx context.Context) FTSIndexType
		// FTSExtractorType returns the full-text search extractor type.
		FTSExtractorType(ctx context.Context) FTSExtractorType
		// FTSIndexMeilisearch returns Meilisearch index settings.
		FTSIndexMeilisearch(ctx context.Context) *FTSIndexMeilisearchSetting
		// FTSTikaExtractor returns Tika extractor settings.
		FTSTikaExtractor(ctx context.Context) *FTSTikaExtractorSetting
		// FTSChunkSize returns the maximum chunk size in bytes for full-text search indexing.
		FTSChunkSize(ctx context.Context) int
		// DefaultViewerMapping returns the default viewer mapping.
		DefaultViewerMapping(ctx context.Context) types.DefaultViewerMapping
		// ExposeUserEmail returns true if user email should be exposed to other
		// signed-in users in redacted responses.
		ExposeUserEmail(ctx context.Context) bool
	}
	UseFirstSiteUrlCtxKey = struct{}
)

// NewProvider creates a new setting provider.
func NewProvider(root SettingStoreAdapter) Provider {
	return &settingProvider{
		adapterChain: root,
	}
}

const (
	stringListDefault          = "DEFAULT"
	stringListDefaultSeparator = ","
)

var defaultBoolSet = &boolset.BooleanSet{}

type (
	SiteHostAllowListGetter interface {
		AllowedHost() []string
	}
	settingProvider struct {
		adapterChain SettingStoreAdapter
	}
)

func (s *settingProvider) ShowEncryptionStatus(ctx context.Context) bool {
	return s.getBoolean(ctx, "show_encryption_status", true)
}

func (s *settingProvider) MasterEncryptKeyFile(ctx context.Context) string {
	return s.getString(ctx, "encrypt_master_key_file", "")
}

func (s *settingProvider) MasterEncryptKeyVault(ctx context.Context) MasterEncryptKeyVaultType {
	return MasterEncryptKeyVaultType(s.getString(ctx, "encrypt_master_key_vault", "setting"))
}

func (s *settingProvider) MasterEncryptKey(ctx context.Context) []byte {
	encoded := s.getString(ctx, "encrypt_master_key", "")
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return key
}

func (s *settingProvider) CustomHTML(ctx context.Context) *CustomHTML {
	return &CustomHTML{
		HeadlessFooter: s.getString(ctx, "headless_footer_html", ""),
		HeadlessBody:   s.getString(ctx, "headless_bottom_html", ""),
		SidebarBottom:  s.getString(ctx, "sidebar_bottom_html", ""),
	}
}
func (s *settingProvider) CustomNavItems(ctx context.Context) []CustomNavItem {
	raw := s.getString(ctx, "custom_nav_items", "[]")
	var items []CustomNavItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return []CustomNavItem{}
	}
	return items
}
func (s *settingProvider) CustomProps(ctx context.Context) []types.CustomProps {
	raw := s.getString(ctx, "custom_props", "[]")
	var props []types.CustomProps
	if err := json.Unmarshal([]byte(raw), &props); err != nil {
		return []types.CustomProps{}
	}
	return props
}

// AuditEventEnabled reports whether events of the given type should be
// recorded. The setting holds a JSON array of enabled event names (see
// pkg/eventtype); a missing value enables every event type, while an explicit
// JSON array is authoritative (an empty array disables all events).
func (s *settingProvider) AuditEventEnabled(ctx context.Context, ev eventtype.EventType) bool {
	raw := s.getString(ctx, "audit_event_enabled", "")
	if raw == "" {
		return true
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return true
	}
	// An explicit (possibly empty) list is authoritative: an empty list means
	// the administrator turned every event off.
	return slices.Contains(names, ev.Name())
}

func (s *settingProvider) License(ctx context.Context) string {
	return s.getString(ctx, "license", "")
}

func (s *settingProvider) AvatarProcess(ctx context.Context) *AvatarProcess {
	return &AvatarProcess{
		Path:        s.getString(ctx, "avatar_path", "avatar"),
		MaxFileSize: s.getInt64(ctx, "avatar_size", 4194304),
		MaxWidth:    s.getInt(ctx, "avatar_size_l", 200),
	}
}

func (s *settingProvider) Avatar(ctx context.Context) *Avatar {
	return &Avatar{
		Gravatar: s.getString(ctx, "gravatar_server", ""),
		Path:     s.getString(ctx, "avatar_path", "avatar"),
	}
}

func (s *settingProvider) DefaultViewerMapping(ctx context.Context) types.DefaultViewerMapping {
	raw := s.getString(ctx, "viewer_default_apps", "{}")
	var mapping types.DefaultViewerMapping
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return types.DefaultViewerMapping{}
	}
	return mapping
}

func (s *settingProvider) FileViewers(ctx context.Context) []types.ViewerGroup {
	raw := s.getString(ctx, "file_viewers", "[]")
	var viewers []types.ViewerGroup
	if err := json.Unmarshal([]byte(raw), &viewers); err != nil {
		return []types.ViewerGroup{}
	}

	return viewers
}

func (s *settingProvider) AppSetting(ctx context.Context) *AppSetting {
	return &AppSetting{
		Promotion:        s.getBoolean(ctx, "show_app_promotion", false),
		DesktopPromotion: s.getBoolean(ctx, "show_desktop_app_promotion", false),
		Feedback:         s.getString(ctx, "app_feedback", ""),
		Forum:            s.getString(ctx, "app_forum", ""),
	}
}

func (s *settingProvider) MaxParallelTransfer(ctx context.Context) int {
	return s.getInt(ctx, "max_parallel_transfer", 4)
}

func (s *settingProvider) ArchiveDownloadSessionTTL(ctx context.Context) int {
	return s.getInt(ctx, "archive_timeout", 20)
}

func (s *settingProvider) ViewerSessionTTL(ctx context.Context) int {
	return s.getInt(ctx, "viewer_session_timeout", 36000)
}

func (s *settingProvider) MapSetting(ctx context.Context) *MapSetting {
	return &MapSetting{
		Provider:       MapProvider(s.getString(ctx, "map_provider", "openstreetmap")),
		GoogleTileType: MapGoogleTileType(s.getString(ctx, "map_google_tile_type", "roadmap")),
		MapboxAK:       s.getString(ctx, "map_mapbox_ak", ""),
	}
}

func (s *settingProvider) MimeMapping(ctx context.Context) string {
	return s.getString(ctx, "mime_mapping", "{}")
}

func (s *settingProvider) Logo(ctx context.Context) *Logo {
	return &Logo{
		Normal: s.getString(ctx, "site_logo", "/static/img/brand/logo.svg"),
		Light:  s.getString(ctx, "site_logo_light", "/static/img/brand/logo-light.svg"),
	}
}

func (s *settingProvider) ExplorerFrontendSettings(ctx context.Context) *ExplorerFrontendSettings {
	return &ExplorerFrontendSettings{
		Icons: s.getString(ctx, "explorer_icons", "[]"),
	}
}

func (s *settingProvider) SearchCategoryQuery(ctx context.Context, category SearchCategory) string {
	return s.getString(ctx, fmt.Sprintf("explorer_category_%s_query", category), "")
}

func (s *settingProvider) Captcha(ctx context.Context) *Captcha {
	return &Captcha{
		Height:             s.getInt(ctx, "captcha_height", 60),
		Width:              s.getInt(ctx, "captcha_width", 240),
		Mode:               CaptchaMode(s.getInt(ctx, "captcha_mode", int(CaptchaModeNumberAlphabet))),
		ComplexOfNoiseText: s.getInt(ctx, "captcha_ComplexOfNoiseText", 0),
		ComplexOfNoiseDot:  s.getInt(ctx, "captcha_ComplexOfNoiseDot", 0),
		IsShowHollowLine:   s.getBoolean(ctx, "captcha_IsShowHollowLine", false),
		IsShowNoiseDot:     s.getBoolean(ctx, "captcha_IsShowNoiseDot", false),
		IsShowNoiseText:    s.getBoolean(ctx, "captcha_IsShowNoiseText", false),
		IsShowSlimeLine:    s.getBoolean(ctx, "captcha_IsShowSlimeLine", false),
		IsShowSineLine:     s.getBoolean(ctx, "captcha_IsShowSineLine", false),
		Length:             s.getInt(ctx, "captcha_CaptchaLen", 6),
	}
}

func (s *settingProvider) LegalDocuments(ctx context.Context) *LegalDocuments {
	return &LegalDocuments{
		PrivacyPolicy:  s.getString(ctx, "privacy_policy_url", ""),
		TermsOfService: s.getString(ctx, "tos_url", ""),
	}
}

func (s *settingProvider) FolderPropsCacheTTL(ctx context.Context) int {
	return s.getInt(ctx, "folder_props_timeout", 300)
}

func (s *settingProvider) EmojiPresets(ctx context.Context) string {
	return s.getString(ctx, "emojis", "{}")
}

func (s *settingProvider) Theme(ctx context.Context) *Theme {
	return &Theme{
		Themes:       s.getString(ctx, "theme_options", "{}"),
		DefaultTheme: s.getString(ctx, "defaultTheme", ""),
	}
}

func (s *settingProvider) Cron(ctx context.Context, t CronType) string {
	return s.getString(ctx, "cron_"+string(t), "@hourly")
}

func (s *settingProvider) BuiltinThumbGeneratorEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_builtin_enabled", true)
}

func (s *settingProvider) BuiltinThumbMaxSize(ctx context.Context) int64 {
	return s.getInt64(ctx, "thumb_builtin_max_size", 78643200)
}

func (s *settingProvider) MusicCoverThumbGeneratorEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_music_cover_enabled", true)
}

func (s *settingProvider) MusicCoverThumbMaxSize(ctx context.Context) int64 {
	return s.getInt64(ctx, "thumb_music_cover_max_size", 1073741824)
}

func (s *settingProvider) MusicCoverThumbExts(ctx context.Context) []string {
	return s.getStringList(ctx, "thumb_music_cover_exts", []string{})
}

func (s *settingProvider) FFMpegPath(ctx context.Context) string {
	return s.getString(ctx, "thumb_ffmpeg_path", "ffmpeg")
}

func (s *settingProvider) FFMpegThumbGeneratorEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_ffmpeg_enabled", false)
}

func (s *settingProvider) FFMpegThumbExts(ctx context.Context) []string {
	return s.getStringList(ctx, "thumb_ffmpeg_exts", []string{})
}

func (s *settingProvider) FFMpegThumbSeek(ctx context.Context) string {
	return s.getString(ctx, "thumb_ffmpeg_seek", "00:00:01.00")
}

func (s *settingProvider) FFMpegExtraArgs(ctx context.Context) string {
	return s.getString(ctx, "thumb_ffmpeg_extra_args", "")
}

func (s *settingProvider) FFMpegThumbMaxSize(ctx context.Context) int64 {
	return s.getInt64(ctx, "thumb_ffmpeg_max_size", 10737418240)
}

func (s *settingProvider) VipsThumbGeneratorEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_vips_enabled", false)
}

func (s *settingProvider) VipsThumbMaxSize(ctx context.Context) int64 {
	return s.getInt64(ctx, "thumb_vips_max_size", 78643200)
}

func (s *settingProvider) VipsThumbExts(ctx context.Context) []string {
	return s.getStringList(ctx, "thumb_vips_exts", []string{})
}

func (s *settingProvider) VipsPath(ctx context.Context) string {
	return s.getString(ctx, "thumb_vips_path", "vips")
}

func (s *settingProvider) LibRawThumbGeneratorEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_libraw_enabled", false)
}

func (s *settingProvider) LibRawThumbMaxSize(ctx context.Context) int64 {
	return s.getInt64(ctx, "thumb_libraw_max_size", 78643200)
}

func (s *settingProvider) LibRawThumbExts(ctx context.Context) []string {
	return s.getStringList(ctx, "thumb_libraw_exts", []string{})
}

func (s *settingProvider) LibRawThumbPath(ctx context.Context) string {
	return s.getString(ctx, "thumb_libraw_path", "simple_dcraw")
}

func (s *settingProvider) LibreOfficeThumbGeneratorEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_libreoffice_enabled", false)
}

func (s *settingProvider) LibreOfficeThumbMaxSize(ctx context.Context) int64 {
	return s.getInt64(ctx, "thumb_libreoffice_max_size", 78643200)
}

func (s *settingProvider) LibreOfficePath(ctx context.Context) string {
	return s.getString(ctx, "thumb_libreoffice_path", "soffice")
}

func (s *settingProvider) LibreOfficeThumbExts(ctx context.Context) []string {
	return s.getStringList(ctx, "thumb_libreoffice_exts", []string{})
}

func (s *settingProvider) ThumbSize(ctx context.Context) (int, int) {
	return s.getInt(ctx, "thumb_width", 400), s.getInt(ctx, "thumb_height", 300)
}

func (s *settingProvider) ThumbEncode(ctx context.Context) *ThumbEncode {
	return &ThumbEncode{
		Format:  s.getString(ctx, "thumb_encode_method", "jpg"),
		Quality: s.getInt(ctx, "thumb_encode_quality", 85),
	}
}

func (s *settingProvider) ThumbEntitySuffix(ctx context.Context) string {
	return s.getString(ctx, "thumb_entity_suffix", "{blob_path}/{blob_name}._thumb")
}

func (s *settingProvider) ThumbSlaveSidecarSuffix(ctx context.Context) string {
	return s.getString(ctx, "thumb_slave_sidecar_suffix", "._thumb_sidecar")
}

func (s *settingProvider) ThumbGCAfterGen(ctx context.Context) bool {
	return s.getBoolean(ctx, "thumb_gc_after_gen", false)
}

func (s *settingProvider) TempPath(ctx context.Context) string {
	return s.getString(ctx, "temp_path", "temp")
}

func (s *settingProvider) MediaMetaFFProbePath(ctx context.Context) string {
	return s.getString(ctx, "media_meta_ffprobe_path", "ffprobe")
}

func (s *settingProvider) MediaMetaFFProbeSizeLimit(ctx context.Context) (int64, int64) {
	return s.getInt64(ctx, "media_meta_ffprobe_size_local", 0), s.getInt64(ctx, "media_meta_ffprobe_size_remote", 0)
}

func (s *settingProvider) MediaMetaFFProbeEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "media_meta_ffprobe", true)
}

func (s *settingProvider) MediaMetaMusicSizeLimit(ctx context.Context) (int64, int64) {
	return s.getInt64(ctx, "media_meta_music_size_local", 0), s.getInt64(ctx, "media_meta_music_size_remote", 0)
}

func (s *settingProvider) MediaMetaMusicEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "media_meta_music", true)
}

func (s *settingProvider) MediaMetaExifBruteForce(ctx context.Context) bool {
	return s.getBoolean(ctx, "media_meta_exif_brute_force", false)
}

func (s *settingProvider) MediaMetaExifSizeLimit(ctx context.Context) (int64, int64) {
	return s.getInt64(ctx, "media_meta_exif_size_local", 0), s.getInt64(ctx, "media_meta_exif_size_remote", 0)
}

func (s *settingProvider) MediaMetaExifEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "media_meta_exif", true)
}

func (s *settingProvider) MediaMetaEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "media_meta", true)
}

func (s *settingProvider) MediaMetaGeocodingEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "media_meta_geocoding", false)
}

func (s *settingProvider) MediaMetaGeocodingMapboxAK(ctx context.Context) string {
	return s.getString(ctx, "media_meta_geocoding_mapbox_ak", "")
}

func (s *settingProvider) PublicResourceMaxAge(ctx context.Context) int {
	return s.getInt(ctx, "public_resource_maxage", 0)
}

func (s *settingProvider) EntityUrlCacheMargin(ctx context.Context) int {
	return s.getInt(ctx, "entity_url_cache_margin", 600)
}

func (s *settingProvider) EntityUrlValidDuration(ctx context.Context) time.Duration {
	return time.Duration(s.getInt(ctx, "entity_url_default_ttl", 3600)) * time.Second
}

func (s *settingProvider) EventHubMaxOfflineDuration(ctx context.Context) time.Duration {
	return time.Duration(s.getInt(ctx, "fs_event_push_max_age", 1209600)) * time.Second
}

func (s *settingProvider) EventHubDebounceDelay(ctx context.Context) time.Duration {
	return time.Duration(s.getInt(ctx, "fs_event_push_debounce", 5)) * time.Second
}

func (s *settingProvider) EventHubEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "fs_event_push_enabled", true)
}

func (s *settingProvider) FTSEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "fts_enabled", false)
}

func (s *settingProvider) FTSIndexType(ctx context.Context) FTSIndexType {
	return FTSIndexType(s.getString(ctx, "fts_index_type", ""))
}

func (s *settingProvider) FTSExtractorType(ctx context.Context) FTSExtractorType {
	return FTSExtractorType(s.getString(ctx, "fts_extractor_type", ""))
}

func (s *settingProvider) FTSIndexMeilisearch(ctx context.Context) *FTSIndexMeilisearchSetting {
	return &FTSIndexMeilisearchSetting{
		Endpoint:         s.getString(ctx, "fts_meilisearch_endpoint", ""),
		APIKey:           s.getString(ctx, "fts_meilisearch_api_key", ""),
		PageSize:         s.getInt(ctx, "fts_meilisearch_page_size", 5),
		EmbeddingEnbaled: s.getBoolean(ctx, "fts_meilisearch_embed_enabled", false),
		EmbeddingSetting: s.getString(ctx, "fts_meilisearch_embed_config", "{}"),
	}
}

func (s *settingProvider) FTSTikaExtractor(ctx context.Context) *FTSTikaExtractorSetting {
	return &FTSTikaExtractorSetting{
		Endpoint:    s.getString(ctx, "fts_tika_endpoint", ""),
		Exts:        s.getStringList(ctx, "fts_tika_exts", []string{}),
		MaxFileSize: s.getInt64(ctx, "fts_tika_max_file_size_remote", 52428800),
	}
}

func (s *settingProvider) FTSChunkSize(ctx context.Context) int {
	return s.getInt(ctx, "fts_chunk_size", 2000)
}

func (s *settingProvider) Queue(ctx context.Context, queueType QueueType) *QueueSetting {
	queueTypeStr := string(queueType)
	return &QueueSetting{
		WorkerNum:          s.getInt(ctx, "queue_"+queueTypeStr+"_worker_num", 15),
		MaxExecution:       time.Duration(s.getInt(ctx, "queue_"+queueTypeStr+"_max_execution", 86400)) * time.Second,
		BackoffFactor:      s.getFloat64(ctx, "queue_"+queueTypeStr+"_backoff_factor", 4),
		BackoffMaxDuration: time.Duration(s.getInt(ctx, "queue_"+queueTypeStr+"_backoff_max_duration", 3600)) * time.Second,
		MaxRetry:           s.getInt(ctx, "queue_"+queueTypeStr+"_max_retry", 5),
		RetryDelay:         time.Duration(s.getInt(ctx, "queue_"+queueTypeStr+"_retry_delay", 5)) * time.Second,
	}
}

func (s *settingProvider) UseChunkBuffer(ctx context.Context) bool {
	return s.getBoolean(ctx, "use_temp_chunk_buffer", true)
}

func (s *settingProvider) ChunkRetryLimit(ctx context.Context) int {
	return s.getInt(ctx, "chunk_retries", 3)
}

func (s *settingProvider) SlaveRequestSignTTL(ctx context.Context) int {
	return s.getInt(ctx, "slave_api_timeout", 60)
}

func (s *settingProvider) MaxOnlineEditSize(ctx context.Context) int64 {
	return int64(s.getInt(ctx, "maxEditSize", 52428800))
}

func (s *settingProvider) UploadSessionTTL(ctx context.Context) time.Duration {
	return time.Duration(s.getInt(ctx, "upload_session_timeout", 86400)) * time.Second
}

func (s *settingProvider) MaxBatchedFile(ctx context.Context) int {
	return s.getInt(ctx, "max_batched_file", 3000)
}

func (s *settingProvider) DBFS(ctx context.Context) *DBFS {
	return &DBFS{
		UseCursorPagination:        s.getBoolean(ctx, "use_cursor_pagination", true),
		MaxPageSize:                s.getInt(ctx, "max_page_size", 2000),
		MaxRecursiveSearchedFolder: s.getInt(ctx, "max_recursive_searched_folder", 65535),
		UseSSEForSearch:            s.getBoolean(ctx, "use_sse_for_search", false),
	}
}

func (s *settingProvider) HashIDSalt(ctx context.Context) string {
	return s.getString(ctx, "hash_id_salt", "")
}

func (s *settingProvider) TokenAuth(ctx context.Context) *TokenAuth {
	return &TokenAuth{
		AccessTokenTTL:  time.Duration(s.getInt(ctx, "access_token_ttl", 3600)) * time.Second,
		RefreshTokenTTL: time.Duration(s.getInt(ctx, "refresh_token_ttl", 15552000)) * time.Second,
	}
}

func (s *settingProvider) ResetEmailTemplate(ctx context.Context) []EmailTemplate {
	src := s.getString(ctx, "mail_reset_template", "[]")
	var templates []EmailTemplate
	if err := json.Unmarshal([]byte(src), &templates); err != nil {
		return []EmailTemplate{}
	}

	return templates
}

func (s *settingProvider) ActivationEmailTemplate(ctx context.Context) []EmailTemplate {
	src := s.getString(ctx, "mail_activation_template", "[]")
	var templates []EmailTemplate
	if err := json.Unmarshal([]byte(src), &templates); err != nil {
		return []EmailTemplate{}
	}

	return templates
}

// ReceiptEmailTemplate returns the email template sent after a payment is fulfilled.
func (s *settingProvider) ReceiptEmailTemplate(ctx context.Context) []EmailTemplate {
	return s.emailTemplates(ctx, "mail_receipt_template")
}

// ExceedQuotaEmailTemplate returns the email template sent when the user runs out of storage.
func (s *settingProvider) ExceedQuotaEmailTemplate(ctx context.Context) []EmailTemplate {
	return s.emailTemplates(ctx, "mail_exceed_quota_template")
}

// DefaultSymbolics returns the site-wide pinned share IDs that are applied to
// newly registered users (Pro: 初始分享快捷方式).
func (s *settingProvider) DefaultSymbolics(ctx context.Context) []int {
	raw := s.getString(ctx, "default_symbolics", "[]")
	var ids []int
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}

	return ids
}

// emailTemplates decodes a mail template setting, falling back to an empty list on
// missing or malformed values.
func (s *settingProvider) emailTemplates(ctx context.Context, name string) []EmailTemplate {
	src := s.getString(ctx, name, "[]")
	var templates []EmailTemplate
	if err := json.Unmarshal([]byte(src), &templates); err != nil {
		return []EmailTemplate{}
	}

	return templates
}

func (s *settingProvider) SecretKey(ctx context.Context) string {
	return s.getString(ctx, "secret_key", "")
}

func (s *settingProvider) AllSiteURLs(ctx context.Context) []*url.URL {
	rawUrls := s.getStringList(ctx, "siteURL", []string{"http://localhost"})
	if len(rawUrls) == 0 {
		rawUrls = []string{"http://localhost"}
	}

	urls := make([]*url.URL, 0, len(rawUrls))
	for _, u := range rawUrls {
		parsedURL, err := url.Parse(u)
		if err != nil {
			continue
		}
		urls = append(urls, parsedURL)
	}
	return urls
}

func (s *settingProvider) SiteURL(ctx context.Context) *url.URL {
	rawUrls := s.getStringList(ctx, "siteURL", []string{"http://localhost"})
	if len(rawUrls) == 0 {
		rawUrls = []string{"http://localhost"}
	}

	urls := make([]*url.URL, 0, len(rawUrls))
	for _, u := range rawUrls {
		parsedURL, err := url.Parse(u)
		if err != nil {
			continue
		}
		urls = append(urls, parsedURL)
	}

	reqInfo := requestinfo.RequestInfoFromContext(ctx)
	_, useFirst := ctx.Value(UseFirstSiteUrlCtxKey{}).(bool)
	if !useFirst && reqInfo != nil && reqInfo.Host != "" {
		for _, u := range urls {
			if (u.Host) == reqInfo.Host {
				return u
			}
		}
	}

	return urls[0]
}

func (s *settingProvider) SMTP(ctx context.Context) *SMTP {
	return &SMTP{
		FromName:        s.getString(ctx, "fromName", ""),
		From:            s.getString(ctx, "fromAdress", ""),
		Host:            s.getString(ctx, "smtpHost", ""),
		ReplyTo:         s.getString(ctx, "replyTo", ""),
		User:            s.getString(ctx, "smtpUser", ""),
		Password:        s.getString(ctx, "smtpPass", ""),
		ForceEncryption: s.getBoolean(ctx, "smtpEncryption", false),
		Port:            s.getInt(ctx, "smtpPort", 25),
		Keepalive:       s.getInt(ctx, "mail_keepalive", 30),
	}
}

func (s *settingProvider) DefaultGroup(ctx context.Context) int {
	return s.getInt(ctx, "default_group", 2)
}

func (s *settingProvider) EmailActivationEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "email_active", false)
}

func (s *settingProvider) TcCaptcha(ctx context.Context) *TcCaptcha {
	return &TcCaptcha{
		AppID:        s.getString(ctx, "captcha_TCaptcha_CaptchaAppId", ""),
		AppSecretKey: s.getString(ctx, "captcha_TCaptcha_AppSecretKey", ""),
		SecretID:     s.getString(ctx, "captcha_TCaptcha_SecretId", ""),
		SecretKey:    s.getString(ctx, "captcha_TCaptcha_SecretKey", ""),
	}
}

func (s *settingProvider) TurnstileCaptcha(ctx context.Context) *Turnstile {
	return &Turnstile{
		Secret: s.getString(ctx, "captcha_turnstile_site_secret", ""),
		Key:    s.getString(ctx, "captcha_turnstile_site_key", ""),
	}
}

func (s *settingProvider) CapCaptcha(ctx context.Context) *Cap {
	return &Cap{
		InstanceURL: s.getString(ctx, "captcha_cap_instance_url", ""),
		SiteKey:     s.getString(ctx, "captcha_cap_site_key", ""),
		SecretKey:   s.getString(ctx, "captcha_cap_secret_key", ""),
		AssetServer: s.getString(ctx, "captcha_cap_asset_server", "jsdelivr"),
	}
}

func (s *settingProvider) ReCaptcha(ctx context.Context) *ReCaptcha {
	return &ReCaptcha{
		Secret: s.getString(ctx, "captcha_ReCaptchaSecret", ""),
		Key:    s.getString(ctx, "captcha_ReCaptchaKey", ""),
	}
}

func (s *settingProvider) CaptchaType(ctx context.Context) CaptchaType {
	return CaptchaType(s.getString(ctx, "captcha_type", string(CaptchaNormal)))
}

func (s *settingProvider) RegCaptchaEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "reg_captcha", false)
}

func (s *settingProvider) LoginCaptchaEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "login_captcha", false)
}

func (s *settingProvider) ForgotPasswordCaptchaEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "forget_captcha", false)
}

func (s *settingProvider) AuthnEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "authn_enabled", false)
}

func (s *settingProvider) RegisterEnabled(ctx context.Context) bool {
	return s.getBoolean(ctx, "register_enabled", false)
}

// FilterEmailProvider returns the email domain filter mode applied at registration.
func (s *settingProvider) FilterEmailProvider(ctx context.Context) EmailProviderFilter {
	return EmailProviderFilter(s.getString(ctx, "filter_email_provider", string(EmailProviderFilterDisabled)))
}

// FilterEmailProviderRule returns the normalized (lower-cased, trimmed, non-empty)
// email domains used by the registration filter.
func (s *settingProvider) FilterEmailProviderRule(ctx context.Context) []string {
	raw := s.getString(ctx, "filter_email_provider_rule", "")
	domains := make([]string, 0)
	for _, d := range strings.Split(raw, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" {
			domains = append(domains, d)
		}
	}
	return domains
}

func (s *settingProvider) DisableSubAddressEmail(ctx context.Context) bool {
	return s.getBoolean(ctx, "disable_sub_address_email", false)
}

// OIDC returns the OIDC SSO (G6) settings.
func (s *settingProvider) OIDC(ctx context.Context) *OIDC {
	scopes := strings.Fields(s.getString(ctx, "oidc_scopes", "openid profile email"))
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	return &OIDC{
		Enabled:               s.getBoolean(ctx, "oidc_enabled", false),
		Endpoint:              strings.TrimSuffix(s.getString(ctx, "oidc_endpoint", ""), "/"),
		ClientID:              s.getString(ctx, "oidc_client_id", ""),
		ClientSecret:          s.getString(ctx, "oidc_client_secret", ""),
		Scopes:                scopes,
		DisplayName:           s.getString(ctx, "oidc_display_name", "vas.sso"),
		AutoRedirect:          s.getBoolean(ctx, "oidc_auto_redirect", false),
		PasswordLoginDisabled: s.getBoolean(ctx, "oidc_password_login_disabled", false),
		AutoRegister:          s.getBoolean(ctx, "oidc_auto_register", false),
		AutoLinkEmail:         s.getBoolean(ctx, "oidc_auto_link_email", false),
		RequireEmailVerified:  s.getBoolean(ctx, "oidc_require_email_verified", false),
		SubjectClaim:          claimOrDefault(ctx, s, "oidc_subject_claim", "sub"),
		EmailClaim:            claimOrDefault(ctx, s, "oidc_email_claim", "email"),
		NameClaim:             claimOrDefault(ctx, s, "oidc_name_claim", "name"),
	}
}

// qqLoginConfig 是 qq_login_config 设置项的 JSON 结构。旧版占位默认值为
// {"direct_sign_in":false}，因此 auto_register 用指针区分「未配置」与「显式关闭」。
type qqLoginConfig struct {
	AppID        string `json:"app_id"`
	AppSecret    string `json:"app_secret"`
	DirectSignIn bool   `json:"direct_sign_in"`
	AutoRegister *bool  `json:"auto_register"`
	DisplayName  string `json:"display_name"`
}

const (
	// DefaultQQAPIBase 是 QQ 互联 authorize / token / me 的默认基址。
	DefaultQQAPIBase = "https://graph.qq.com/oauth2.0"
	// DefaultQQGraphBase 是 QQ 互联 user/get_user_info 的默认基址。
	DefaultQQGraphBase = "https://graph.qq.com"
	// DefaultQQDisplayName 是登录页 QQ 按钮的默认文案（i18n key）。
	DefaultQQDisplayName = "vas.qqConnect"
)

// QQLogin returns true when the QQ Connect login is turned on.
func (s *settingProvider) QQLogin(ctx context.Context) bool {
	return s.getBoolean(ctx, "qq_login", false)
}

// QQConfig returns the QQ Connect login settings. qq_api_base / qq_graph_base
// are hidden advanced settings (never rendered in the admin panel); they only
// exist to point the flow at a self-hosted proxy or at a test double.
func (s *settingProvider) QQConfig(ctx context.Context) *QQConfig {
	raw := s.getString(ctx, "qq_login_config", "{}")
	parsed := qqLoginConfig{}
	// 配置损坏时退化为默认值，而不是让登录流程整体失败。
	_ = json.Unmarshal([]byte(raw), &parsed)

	autoRegister := true
	if parsed.AutoRegister != nil {
		autoRegister = *parsed.AutoRegister
	}

	displayName := strings.TrimSpace(parsed.DisplayName)
	if displayName == "" {
		displayName = DefaultQQDisplayName
	}

	return &QQConfig{
		Enabled:      s.QQLogin(ctx),
		AppID:        strings.TrimSpace(parsed.AppID),
		AppSecret:    strings.TrimSpace(parsed.AppSecret),
		DirectSignIn: parsed.DirectSignIn,
		AutoRegister: autoRegister,
		DisplayName:  displayName,
		APIBase:      strings.TrimSuffix(s.getString(ctx, "qq_api_base", DefaultQQAPIBase), "/"),
		GraphBase:    strings.TrimSuffix(s.getString(ctx, "qq_graph_base", DefaultQQGraphBase), "/"),
	}
}

// claimOrDefault reads a claim name setting, falling back to the given default
// when it is empty or blank.
func claimOrDefault(ctx context.Context, s *settingProvider, name, defaultVal string) string {
	if v := strings.TrimSpace(s.getString(ctx, name, defaultVal)); v != "" {
		return v
	}

	return defaultVal
}

func (s *settingProvider) ExposeUserEmail(ctx context.Context) bool {
	return s.getBoolean(ctx, "expose_user_email", true)
}

func (s *settingProvider) SiteBasic(ctx context.Context) *SiteBasic {
	return &SiteBasic{
		Name:         s.getString(ctx, "siteName", ""),
		Title:        s.getString(ctx, "siteTitle", ""),
		ID:           s.getString(ctx, "siteID", ""),
		Description:  s.getString(ctx, "siteDes", ""),
		Script:       s.getString(ctx, "siteScript", ""),
		Announcement: s.getString(ctx, "siteAnnouncement", ""),
	}
}

func (s *settingProvider) PWA(ctx context.Context) *PWASetting {
	return &PWASetting{
		SmallIcon:       s.getString(ctx, "pwa_small_icon", ""),
		MediumIcon:      s.getString(ctx, "pwa_medium_icon", ""),
		LargeIcon:       s.getString(ctx, "pwa_large_icon", ""),
		Display:         s.getString(ctx, "pwa_display", ""),
		ThemeColor:      s.getString(ctx, "pwa_theme_color", ""),
		BackgroundColor: s.getString(ctx, "pwa_background_color", ""),
	}
}

func IsTrueValue(val string) bool {
	return val == "1" || val == "true"
}

func (s *settingProvider) getInt(ctx context.Context, name string, defaultVal int) int {
	val := s.adapterChain.Get(ctx, name, defaultVal)
	if intVal, ok := val.(int); ok {
		return intVal
	}

	strVal := val.(string)
	if intVal, err := strconv.Atoi(strVal); err == nil {
		return intVal
	}

	return defaultVal
}

func (s *settingProvider) getInt64(ctx context.Context, name string, defaultVal int64) int64 {
	val := s.adapterChain.Get(ctx, name, defaultVal)
	if intVal, ok := val.(int64); ok {
		return intVal
	}

	strVal := val.(string)
	if intVal, err := strconv.ParseInt(strVal, 10, 64); err == nil {
		return intVal
	}

	return defaultVal
}

func (s *settingProvider) getFloat64(ctx context.Context, name string, defaultVal float64) float64 {
	val := s.adapterChain.Get(ctx, name, defaultVal)
	if intVal, ok := val.(float64); ok {
		return intVal
	}

	strVal := val.(string)
	if intVal, err := strconv.ParseFloat(strVal, 64); err == nil {
		return intVal
	}

	return defaultVal
}

func (s *settingProvider) getBoolean(ctx context.Context, name string, defaultVal bool) bool {
	val := s.adapterChain.Get(ctx, name, defaultVal)
	if intVal, ok := val.(bool); ok {
		return intVal
	}

	strVal := val.(string)
	return IsTrueValue(strVal)
}

func (s *settingProvider) getString(ctx context.Context, name string, defaultVal string) string {
	val := s.adapterChain.Get(ctx, name, defaultVal)
	return val.(string)
}

func (s *settingProvider) getStringList(ctx context.Context, name string, defaultVal []string) []string {
	res, _ := s.getStringListRaw(ctx, name, defaultVal)
	return res
}

func (s *settingProvider) getStringListRaw(ctx context.Context, name string, defaultVal []string) ([]string, string) {
	val := s.getString(ctx, name, stringListDefault)
	if val == stringListDefault {
		return defaultVal, val
	}

	return strings.Split(val, stringListDefaultSeparator), val
}

func (s *settingProvider) getBoolSet(ctx context.Context, name string) *boolset.BooleanSet {
	val := s.getString(ctx, name, "")
	if val == "" {
		return defaultBoolSet
	}

	res, err := boolset.FromString(val)
	if err != nil {
		return defaultBoolSet
	}

	return res
}

func UseFirstSiteUrl(ctx context.Context) context.Context {
	return context.WithValue(ctx, UseFirstSiteUrlCtxKey{}, true)
}
