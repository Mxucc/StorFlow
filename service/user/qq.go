package user

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/oidcbinding"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/pkg/setting"
	"github.com/Mxucc/StorFlow/pkg/util"
	"github.com/gin-gonic/gin"
)

// QQ 互联（QQ Connect）登录：第三方 OAuth2 授权码登录 + 账号绑定 + 自动注册。
//
// 复用 OIDC 的绑定表（ent.OIDCBinding，issuer 固定为 "qq"）与前端回跳契约：
// 成功后同样 302 到 /oidc/callback#session=<base64url(LoginResponse)>&redirect=<...>，
// 前端既有的 OIDCCallback 路由可直接消费。契约见 docs/plan-qq.md。
const (
	// QQAuthorizePath 是 QQ 登录入口（公开接口）。
	QQAuthorizePath = "/api/v4/session/qq/authorize"
	// QQCallbackPath 是 redirect_uri 的路径，需要提前配置到 QQ 互联后台。
	QQCallbackPath = "/api/v4/session/qq/callback"
	// qqIssuer 是写入 oidc_bindings.issuer 的固定值，与 OIDC 的 issuer（IdP 地址）
	// 天然隔离，两种登录方式的绑定不会互相覆盖。
	qqIssuer = "qq"
	// qqStatePrefix 是待完成登录会话在 KV 中的前缀，完整键形如 qq:state:<state>。
	qqStatePrefix = "qq:state:"
	// qqStateTTL 是待完成登录会话的有效期（秒）。
	qqStateTTL = 600
	// qqRequestTimeout 限制单次换取 token / 拉取用户信息的网络耗时。
	qqRequestTimeout = 15 * time.Second
	// qqMaxResponseBytes 限制读取 QQ 响应体的上限，避免异常响应占用内存。
	qqMaxResponseBytes = 1 << 20
	// qqScope 是 QQ 互联要求的 scope（必须为 get_user_info）。
	qqScope = "get_user_info"
	// qqEmailDomainFallback 在站点 URL 不可用时用作占位邮箱的域名。
	qqEmailDomainFallback = "qq.local"
)

// qqHTTPClient 用于 QQ 的 token / me / get_user_info 请求。
var qqHTTPClient = &http.Client{Timeout: qqRequestTimeout}

// QQSSOConfig 是登录页所需的 QQ 公开配置（绝不包含 app_secret）。
type QQSSOConfig struct {
	Enabled      bool   `json:"enabled"`
	DisplayName  string `json:"display_name"`
	AutoRedirect bool   `json:"auto_redirect"`
	AuthorizeURL string `json:"authorize_url"`
}

// qqPublicConfig 组装 GET /api/v4/session/sso 响应中的 qq 字段。
func qqPublicConfig(c *gin.Context, dep dependency.Dep) *QQSSOConfig {
	cfg := dep.SettingProvider().QQConfig(c)

	return &QQSSOConfig{
		Enabled:      cfg.Enabled,
		DisplayName:  cfg.DisplayName,
		AutoRedirect: cfg.DirectSignIn,
		AuthorizeURL: QQAuthorizePath,
	}
}

// qqLoginSession 是授权请求与回调请求之间存放在 KV 里的待完成登录会话。
// QQ 没有 OIDC 的 nonce 机制，因此只需 state 与回跳地址。
type qqLoginSession struct {
	State    string `json:"state"`
	Redirect string `json:"redirect"`
}

type (
	// QQAuthorizeParamCtx 是授权服务的上下文键。
	QQAuthorizeParamCtx struct{}
	// QQAuthorizeService 处理 QQ 授权入口。
	QQAuthorizeService struct {
		// Redirect 是登录成功后要回到的前端相对路径。
		Redirect string `form:"redirect" json:"redirect"`
	}
)

// Authorize 生成 state 并 302 到 QQ 的 authorization endpoint。
func (service *QQAuthorizeService) Authorize(c *gin.Context) error {
	dep := dependency.FromContext(c)
	cfg := dep.SettingProvider().QQConfig(c)
	if !cfg.Enabled {
		return serializer.NewError(serializer.CodeParamErr, "QQ Connect login is not enabled", nil)
	}
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return serializer.NewError(serializer.CodeParamErr,
			"QQ Connect login is enabled but qq_login_config.app_id / app_secret is not configured", nil)
	}

	state, err := oidcRandomToken()
	if err != nil {
		return serializer.NewError(serializer.CodeInternalSetting, "Failed to generate SSO state", err)
	}

	session := qqLoginSession{
		State:    state,
		Redirect: SanitizeOIDCRedirect(service.Redirect),
	}
	if err := dep.KV().Set(qqStatePrefix+state, session, qqStateTTL); err != nil {
		return serializer.NewError(serializer.CodeInternalSetting, "Failed to persist SSO session", err)
	}

	queries := url.Values{}
	queries.Set("response_type", "code")
	queries.Set("client_id", cfg.AppID)
	queries.Set("redirect_uri", ssoRedirectURL(c, dep, QQCallbackPath))
	queries.Set("state", state)
	queries.Set("scope", qqScope)

	c.Redirect(http.StatusFound, cfg.APIBase+"/authorize?"+queries.Encode())

	return nil
}

type (
	// QQCallbackParamCtx 是回调服务的上下文键。
	QQCallbackParamCtx struct{}
	// QQCallbackService 处理 QQ 的授权码回调。
	QQCallbackService struct {
		Code  string `form:"code" json:"code"`
		State string `form:"state" json:"state"`
		// Error / ErrorDescription 由 QQ 在拒绝授权时带回。
		Error            string `form:"error" json:"error"`
		ErrorDescription string `form:"error_description" json:"error_description"`
	}
	// QQCallbackResult 是成功回调的结果，Location 为 302 目标。
	QQCallbackResult struct {
		Location string
	}
)

// newQQCallbackError 创建 QQ 回调错误；失败时浏览器仍会收到 302（回到登录页）。
func newQQCallbackError(code int, msg string) *OIDCCallbackError {
	return NewSSOCallbackError("QQ", code, msg)
}

// qqUserInfo 是 QQ 返回的用户资料（QQ 不返回邮箱）。
type qqUserInfo struct {
	Nickname string `json:"nickname"`
	// FigureURL 是 QQ 头像地址（figureurl_qq_2）。这里只解析不落库：本地 user.avatar
	// 存的是头像来源标识（gravatar / file），外部 URL 无法直接使用。
	FigureURL string `json:"figureurl_qq_2"`
}

// Callback 校验 state，依次换取 access_token / openid / 用户资料，解析或创建本地
// 用户并签发 token pair。失败时返回 *OIDCCallbackError，由控制器转成 302 到登录页。
func (service *QQCallbackService) Callback(c *gin.Context) (*QQCallbackResult, error) {
	dep := dependency.FromContext(c)
	cfg := dep.SettingProvider().QQConfig(c)
	if !cfg.Enabled {
		return nil, newQQCallbackError(serializer.CodeParamErr, "QQ Connect login is not enabled")
	}

	if service.Error != "" {
		reason := service.ErrorDescription
		if reason == "" {
			reason = service.Error
		}
		return nil, newQQCallbackError(serializer.CodeParamErr, "QQ rejected the login: "+reason)
	}

	if service.State == "" {
		return nil, newQQCallbackError(serializer.CodeParamErr, "Missing SSO state parameter")
	}

	rawSession, ok := dep.KV().Get(qqStatePrefix + service.State)
	if !ok {
		return nil, newQQCallbackError(serializer.CodeParamErr, "SSO session is expired or unknown, please sign in again")
	}
	// 用完即删，防止 state 重放。
	_ = dep.KV().Delete(qqStatePrefix, service.State)

	session, ok := rawSession.(qqLoginSession)
	if !ok {
		return nil, newQQCallbackError(serializer.CodeParamErr, "SSO session is corrupted, please sign in again")
	}
	redirect := SanitizeOIDCRedirect(session.Redirect)

	if service.Code == "" {
		return nil, newQQCallbackError(serializer.CodeParamErr, "Missing authorization code")
	}

	accessToken, cbErr := service.exchangeToken(c, dep, cfg)
	if cbErr != nil {
		return nil, cbErr
	}

	openID, cbErr := service.fetchOpenID(c, cfg, accessToken)
	if cbErr != nil {
		return nil, cbErr
	}

	profile := service.fetchProfile(c, dep, cfg, accessToken, openID)

	expectedUser, cbErr := service.resolveUser(c, dep, cfg, openID, profile)
	if cbErr != nil {
		return nil, cbErr
	}

	// 复用既有发令牌逻辑签发 token pair。
	util.WithValue(c, inventory.UserCtx{}, expectedUser)
	loginResponse, err := IssueToken(c)
	if err != nil {
		return nil, newQQCallbackError(serializer.CodeInternalSetting, "Failed to issue token pair: "+err.Error())
	}

	WriteAuthAudit(c, eventtype.UserLogin, expectedUser, expectedUser.Email, nil)

	payload, err := json.Marshal(loginResponse)
	if err != nil {
		return nil, newQQCallbackError(serializer.CodeInternalSetting, "Failed to encode the login session: "+err.Error())
	}

	// 与 OIDC 完全一致的片段契约，前端 /oidc/callback 路由负责解码。
	return &QQCallbackResult{
		Location: fmt.Sprintf("%s#session=%s&redirect=%s", oidcFrontendCallbackPath,
			base64.RawURLEncoding.EncodeToString(payload), url.QueryEscape(redirect)),
	}, nil
}

// exchangeToken 用授权码换取 access_token。
func (service *QQCallbackService) exchangeToken(ctx context.Context, dep dependency.Dep,
	cfg *setting.QQConfig) (string, *OIDCCallbackError) {
	queries := url.Values{}
	queries.Set("grant_type", "authorization_code")
	queries.Set("client_id", cfg.AppID)
	queries.Set("client_secret", cfg.AppSecret)
	queries.Set("code", service.Code)
	queries.Set("redirect_uri", ssoRedirectURL(ctx, dep, QQCallbackPath))
	queries.Set("fmt", "json")

	body, cbErr := qqAPIRequest(ctx, cfg.APIBase+"/token?"+queries.Encode())
	if cbErr != nil {
		return "", cbErr
	}

	var payload struct {
		AccessToken      string          `json:"access_token"`
		Error            json.RawMessage `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", newQQCallbackError(serializer.CodeCredentialInvalid, "Failed to parse the QQ token response: "+err.Error())
	}
	if payload.AccessToken == "" {
		return "", newQQCallbackError(serializer.CodeCredentialInvalid,
			"The QQ token endpoint did not return an access_token: "+qqErrorDetail(payload.Error, payload.ErrorDescription))
	}

	return payload.AccessToken, nil
}

// fetchOpenID 用 access_token 换取 openid，它是本地绑定关系里的 subject。
func (service *QQCallbackService) fetchOpenID(ctx context.Context, cfg *setting.QQConfig,
	accessToken string) (string, *OIDCCallbackError) {
	queries := url.Values{}
	queries.Set("access_token", accessToken)
	queries.Set("fmt", "json")

	body, cbErr := qqAPIRequest(ctx, cfg.APIBase+"/me?"+queries.Encode())
	if cbErr != nil {
		return "", cbErr
	}

	var payload struct {
		OpenID           string          `json:"openid"`
		Error            json.RawMessage `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", newQQCallbackError(serializer.CodeCredentialInvalid, "Failed to parse the QQ openid response: "+err.Error())
	}
	if payload.OpenID == "" {
		return "", newQQCallbackError(serializer.CodeCredentialInvalid,
			"The QQ API did not return an openid: "+qqErrorDetail(payload.Error, payload.ErrorDescription))
	}

	return payload.OpenID, nil
}

// fetchProfile 拉取昵称与头像地址。get_user_info 属于补充信息，失败不影响登录，
// 只是新注册的账号没有昵称。
func (service *QQCallbackService) fetchProfile(ctx context.Context, dep dependency.Dep, cfg *setting.QQConfig,
	accessToken, openID string) *qqUserInfo {
	queries := url.Values{}
	queries.Set("access_token", accessToken)
	queries.Set("oauth_consumer_key", cfg.AppID)
	queries.Set("openid", openID)
	queries.Set("fmt", "json")

	body, cbErr := qqAPIRequest(ctx, cfg.GraphBase+"/user/get_user_info?"+queries.Encode())
	if cbErr != nil {
		dep.Logger().Warning("Failed to fetch the QQ user profile: %s", cbErr)
		return nil
	}

	var payload struct {
		Ret       int    `json:"ret"`
		Msg       string `json:"msg"`
		Nickname  string `json:"nickname"`
		FigureURL string `json:"figureurl_qq_2"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		dep.Logger().Warning("Failed to parse the QQ user profile: %s", err)
		return nil
	}
	if payload.Ret != 0 {
		dep.Logger().Warning("The QQ user profile API returned ret=%d: %s", payload.Ret, payload.Msg)
		return nil
	}

	return &qqUserInfo{
		Nickname:  strings.TrimSpace(payload.Nickname),
		FigureURL: strings.TrimSpace(payload.FigureURL),
	}
}

// resolveUser 按 (issuer="qq", subject=openid) → 自动注册的顺序解析本地用户。
// QQ 不返回邮箱，因此没有 OIDC 的「按邮箱自动绑定」这一步。
func (service *QQCallbackService) resolveUser(c *gin.Context, dep dependency.Dep, cfg *setting.QQConfig,
	openID string, profile *qqUserInfo) (*ent.User, *OIDCCallbackError) {
	db := dep.DBClient()
	userClient := dep.UserClient()
	loadCtx := context.WithValue(c, inventory.LoadUserGroup{}, true)

	binding, err := db.OIDCBinding.Query().
		Where(oidcbinding.IssuerEQ(qqIssuer), oidcbinding.SubjectEQ(openID)).
		Only(c)
	switch {
	case err == nil:
		// 已绑定：直接复用绑定到的本地账号。
		boundUser, getErr := userClient.GetByID(loadCtx, binding.UserID)
		if getErr != nil {
			return nil, newQQCallbackError(serializer.CodeUserNotFound,
				"The account bound to this QQ account is no longer available")
		}

		return oidcCheckUserStatus(boundUser)
	case !ent.IsNotFound(err):
		return nil, newQQCallbackError(serializer.CodeInternalSetting, "Failed to query the QQ binding: "+err.Error())
	}

	if !cfg.AutoRegister {
		return nil, newQQCallbackError(serializer.CodeUserNotFound,
			"No local account is bound to this QQ account and automatic registration is disabled")
	}

	nick := ""
	if profile != nil {
		nick = profile.Nickname
	}

	// QQ 不返回邮箱，而本地账号必须有唯一邮箱：用 openid 与站点域名确定性地生成
	// 占位邮箱 qq_<openid>@<站点域名>，保证同一 QQ 账号每次登录都得到同一邮箱，
	// 不会因为邮箱冲突而重复注册（第二次登录命中上面的绑定分支）。
	email := qqPlaceholderEmail(c, dep, openID)

	newUser, cbErr := createSSOUser(c, dep, email, nick)
	if cbErr != nil {
		return nil, cbErr
	}

	if cbErr := createSSOBinding(c, db, qqIssuer, newUser.ID, openID, email); cbErr != nil {
		return nil, cbErr
	}

	return newUser, nil
}

// qqPlaceholderEmail 生成 QQ 账号的确定性占位邮箱：qq_<openid>@<站点域名>。
func qqPlaceholderEmail(ctx context.Context, dep dependency.Dep, openID string) string {
	domain := ""
	if siteURL := dep.SettingProvider().SiteURL(ctx); siteURL != nil {
		domain = siteURL.Hostname()
	}
	if domain == "" {
		domain = qqEmailDomainFallback
	}

	return "qq_" + sanitizeQQEmailLocalPart(openID) + "@" + domain
}

// sanitizeQQEmailLocalPart 只保留邮箱本地部分允许的字符，保证生成的地址合法且确定。
func sanitizeQQEmailLocalPart(openID string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return unicode.ToLower(r)
		default:
			return -1
		}
	}, strings.TrimSpace(openID))
	if cleaned == "" {
		// 极端情况下（openid 全是不允许的字符）退化为十六进制编码，同样是确定性的。
		cleaned = fmt.Sprintf("%x", openID)
	}

	return cleaned
}

// qqAPIRequest 发起带超时的 GET 请求并返回响应体；非 2xx 状态码视为失败。
// 注意：URL 里带有 client_secret / access_token，任何情况下都不要把整个 URL 写进日志。
func qqAPIRequest(ctx context.Context, rawURL string) ([]byte, *OIDCCallbackError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, newQQCallbackError(serializer.CodeInternalSetting, "Failed to build the QQ API request: "+err.Error())
	}

	resp, err := qqHTTPClient.Do(req)
	if err != nil {
		return nil, newQQCallbackError(serializer.CodeCredentialInvalid, "Failed to reach the QQ API: "+err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, qqMaxResponseBytes))
	if err != nil {
		return nil, newQQCallbackError(serializer.CodeCredentialInvalid, "Failed to read the QQ API response: "+err.Error())
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newQQCallbackError(serializer.CodeCredentialInvalid,
			fmt.Sprintf("The QQ API returned HTTP %d: %s", resp.StatusCode, qqTruncate(string(body))))
	}

	return body, nil
}

// qqErrorDetail 把 QQ 返回的 error / error_description 拼成可读的错误详情。
// QQ 的 error 既可能是数字也可能是字符串，因此按原始 JSON 处理。
func qqErrorDetail(raw json.RawMessage, description string) string {
	detail := strings.TrimSpace(description)
	if len(raw) == 0 || string(raw) == "null" {
		if detail == "" {
			return "no error detail returned"
		}

		return detail
	}

	code := strings.Trim(string(raw), `"`)
	if detail == "" {
		return code
	}

	return code + ": " + detail
}

// qqTruncate 截断过长的响应体片段，避免错误信息被异常响应撑爆。
func qqTruncate(raw string) string {
	const maxLen = 256
	raw = strings.TrimSpace(raw)
	if len(raw) <= maxLen {
		return raw
	}

	return raw[:maxLen] + "..."
}
