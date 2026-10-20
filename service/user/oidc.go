package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/oidcbinding"
	"github.com/Mxucc/StorFlow/ent/user"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/pkg/setting"
	"github.com/Mxucc/StorFlow/pkg/util"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// G6 OIDC SSO：第三方 OIDC 授权码登录 + 账号绑定 + 自动注册。
// 契约见 docs/plan-sso.md。
const (
	// OIDCAuthorizePath 是 OIDC 授权入口（公开接口）。
	OIDCAuthorizePath = "/api/v4/session/oidc/authorize"
	// OIDCCallbackPath 是 redirect_uri 的路径，需要提前配置到 IdP。
	OIDCCallbackPath = "/api/v4/session/oidc/callback"
	// oidcStatePrefix 是待完成登录会话在 KV 中的前缀，完整键形如 oidc:state:<state>。
	oidcStatePrefix = "oidc:state:"
	// oidcStateTTL 是待完成登录会话的有效期（秒）。
	oidcStateTTL = 600
	// oidcFrontendCallbackPath 是前端接收登录结果的路由。
	oidcFrontendCallbackPath = "/oidc/callback"
	// oidcFrontendLoginPath 是登录失败时回跳的前端路由。
	oidcFrontendLoginPath = "/login"
	// oidcCodeEmailNotVerified 是 SSO 专用错误码（继续沿用 400xx 段位，
	// 与 pkg/serializer 中已有错误码不冲突），表示 IdP 未断言邮箱已验证。
	oidcCodeEmailNotVerified = 40076
	// oidcRequestTimeout 限制单次 IdP 发现 / 换取 token 的网络耗时。
	oidcRequestTimeout = 15 * time.Second
)

var (
	oidcProvidersMu sync.Mutex
	// oidcProviders 按 issuer 缓存已发现的 IdP 元数据，避免每次请求都做发现。
	// 切换 oidc_endpoint 时会丢弃旧条目，因此 endpoint 变更即时生效。
	oidcProviders = make(map[string]*oidc.Provider)

	// oidcHTTPClient 用于发现文档与 token 端点请求。
	oidcHTTPClient = &http.Client{Timeout: oidcRequestTimeout}
)

// discoverOIDCProvider 返回 endpoint 对应的 IdP 元数据，命中进程内缓存时不会发起网络请求。
func discoverOIDCProvider(ctx context.Context, endpoint string) (*oidc.Provider, error) {
	oidcProvidersMu.Lock()
	if cached, ok := oidcProviders[endpoint]; ok {
		oidcProvidersMu.Unlock()
		return cached, nil
	}
	oidcProvidersMu.Unlock()

	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, oidcHTTPClient), endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to discover the OIDC provider at %q: %w", endpoint, err)
	}

	oidcProvidersMu.Lock()
	// 只保留当前配置的 issuer，切换 oidc_endpoint 时旧 provider 立即失效。
	oidcProviders = map[string]*oidc.Provider{endpoint: provider}
	oidcProvidersMu.Unlock()

	return provider, nil
}

// ResetOIDCProviderCache 清空 IdP 元数据缓存，仅供测试使用。
func ResetOIDCProviderCache() {
	oidcProvidersMu.Lock()
	defer oidcProvidersMu.Unlock()
	oidcProviders = make(map[string]*oidc.Provider)
}

// oidcLoginSession 是授权请求与回调请求之间存放在 KV 里的待完成登录会话。
type oidcLoginSession struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Redirect string `json:"redirect"`
}

// oidcRandomToken 生成 32 字节的随机十六进制串，用于 state / nonce。
func oidcRandomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

// SanitizeOIDCRedirect 只放行站点内相对路径，其它情况一律回退到 /home。
func SanitizeOIDCRedirect(redirect string) string {
	if redirect == "" || !strings.HasPrefix(redirect, "/") ||
		strings.HasPrefix(redirect, "//") || strings.HasPrefix(redirect, "/\\") {
		return "/home"
	}

	return redirect
}

// ssoRedirectURL 拼出注册到 IdP 的 redirect_uri：站点 URL + 回调路径。
// OIDC 与 QQ 登录共用该逻辑，只有回调路径不同。
func ssoRedirectURL(ctx context.Context, dep dependency.Dep, callbackPath string) string {
	base := *dep.SettingProvider().SiteURL(ctx)
	base.Path = strings.TrimSuffix(base.Path, "/") + callbackPath
	base.RawQuery = ""
	base.Fragment = ""

	return base.String()
}

// oidcRedirectURL 拼出注册到 IdP 的 redirect_uri：站点 URL + OIDC 回调路径。
func oidcRedirectURL(ctx context.Context, dep dependency.Dep) string {
	return ssoRedirectURL(ctx, dep, OIDCCallbackPath)
}

// oidcOAuth2Config 构造授权码流程所需的 oauth2 配置。
func oidcOAuth2Config(ctx context.Context, dep dependency.Dep, cfg *setting.OIDC, provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  oidcRedirectURL(ctx, dep),
		Scopes:       cfg.Scopes,
	}
}

// SSOCallbackError 是 SSO（OIDC / QQ）回调阶段的失败，浏览器仍会收到 302（回到登录页）。
type SSOCallbackError struct {
	Code int
	Msg  string
	// Provider 仅用于日志中标识失败来自哪种登录方式（"OIDC" / "QQ"）。
	Provider string
}

func (e *SSOCallbackError) Error() string {
	provider := e.Provider
	if provider == "" {
		provider = "SSO"
	}

	return fmt.Sprintf("%s SSO failed (%d): %s", provider, e.Code, e.Msg)
}

// NewSSOCallbackError 创建一个带回跳信息的回调错误。
func NewSSOCallbackError(provider string, code int, msg string) *SSOCallbackError {
	return &SSOCallbackError{Provider: provider, Code: code, Msg: msg}
}

// OIDCCallbackError 是 OIDC 回调错误的既有名字，底层类型为共享的 SSOCallbackError，
// 控制器与既有代码可以继续按原样使用它。
type OIDCCallbackError = SSOCallbackError

// NewOIDCCallbackError 创建一个带回跳信息的 OIDC 回调错误。
func NewOIDCCallbackError(code int, msg string) *OIDCCallbackError {
	return NewSSOCallbackError("OIDC", code, msg)
}

// LoginLocation 返回失败时前端应跳转的地址。
func (e *SSOCallbackError) LoginLocation() string {
	queries := url.Values{}
	queries.Set("sso_error", strconv.Itoa(e.Code))
	queries.Set("sso_msg", e.Msg)

	return oidcFrontendLoginPath + "?" + queries.Encode()
}

type (
	// SSOConfigParamCtx 是 SSO 配置服务的上下文键。
	SSOConfigParamCtx struct{}
	// SSOConfigService 返回登录页所需的 SSO 公开配置。
	SSOConfigService struct{}

	// SSOConfigResponse 对应 GET /api/v4/session/sso。
	SSOConfigResponse struct {
		Enabled               bool         `json:"enabled"`
		DisplayName           string       `json:"display_name"`
		AutoRedirect          bool         `json:"auto_redirect"`
		PasswordLoginDisabled bool         `json:"password_login_disabled"`
		AuthorizeURL          string       `json:"authorize_url"`
		QQ                    *QQSSOConfig `json:"qq"`
	}
)

// Config 返回 SSO 开关与展示配置，未启用时 enabled=false，其余字段照常返回。
func (service *SSOConfigService) Config(c *gin.Context) (*SSOConfigResponse, error) {
	dep := dependency.FromContext(c)
	cfg := dep.SettingProvider().OIDC(c)

	return &SSOConfigResponse{
		Enabled:               cfg.Enabled,
		DisplayName:           cfg.DisplayName,
		AutoRedirect:          cfg.AutoRedirect,
		PasswordLoginDisabled: cfg.PasswordLoginDisabled,
		AuthorizeURL:          OIDCAuthorizePath,
		QQ:                    qqPublicConfig(c, dep),
	}, nil
}

type (
	// OIDCAuthorizeParamCtx 是授权服务的上下文键。
	OIDCAuthorizeParamCtx struct{}
	// OIDCAuthorizeService 处理 OIDC 授权入口。
	OIDCAuthorizeService struct {
		// Redirect 是登录成功后要回到的前端相对路径。
		Redirect string `form:"redirect" json:"redirect"`
	}
)

// Authorize 生成 state/nonce 并 302 到 IdP 的 authorization endpoint。
func (service *OIDCAuthorizeService) Authorize(c *gin.Context) error {
	dep := dependency.FromContext(c)
	cfg := dep.SettingProvider().OIDC(c)
	if !cfg.Enabled {
		return serializer.NewError(serializer.CodeParamErr, "OIDC SSO login is not enabled", nil)
	}
	if cfg.Endpoint == "" || cfg.ClientID == "" {
		return serializer.NewError(serializer.CodeParamErr, "OIDC SSO is enabled but oidc_endpoint / oidc_client_id is not configured", nil)
	}

	provider, err := discoverOIDCProvider(c, cfg.Endpoint)
	if err != nil {
		return serializer.NewError(serializer.CodeParamErr, "Failed to discover the OIDC provider: "+err.Error(), err)
	}

	state, err := oidcRandomToken()
	if err != nil {
		return serializer.NewError(serializer.CodeInternalSetting, "Failed to generate SSO state", err)
	}
	nonce, err := oidcRandomToken()
	if err != nil {
		return serializer.NewError(serializer.CodeInternalSetting, "Failed to generate SSO nonce", err)
	}

	session := oidcLoginSession{
		State:    state,
		Nonce:    nonce,
		Redirect: SanitizeOIDCRedirect(service.Redirect),
	}
	if err := dep.KV().Set(oidcStatePrefix+state, session, oidcStateTTL); err != nil {
		return serializer.NewError(serializer.CodeInternalSetting, "Failed to persist SSO session", err)
	}

	authorizeURL := oidcOAuth2Config(c, dep, cfg, provider).AuthCodeURL(state, oidc.Nonce(nonce))
	c.Redirect(http.StatusFound, authorizeURL)

	return nil
}

type (
	// OIDCCallbackParamCtx 是回调服务的上下文键。
	OIDCCallbackParamCtx struct{}
	// OIDCCallbackService 处理 IdP 的授权码回调。
	OIDCCallbackService struct {
		Code  string `form:"code" json:"code"`
		State string `form:"state" json:"state"`
		// Error / ErrorDescription 由 IdP 在拒绝授权时带回。
		Error            string `form:"error" json:"error"`
		ErrorDescription string `form:"error_description" json:"error_description"`
	}
	// OIDCCallbackResult 是成功回调的结果，Location 为 302 目标。
	OIDCCallbackResult struct {
		Location string
	}
)

// Callback 校验 state / id_token，解析或创建本地用户，并签发 token pair。
// 失败时返回 *OIDCCallbackError，由控制器转成 302 到登录页。
func (service *OIDCCallbackService) Callback(c *gin.Context) (*OIDCCallbackResult, error) {
	dep := dependency.FromContext(c)
	cfg := dep.SettingProvider().OIDC(c)
	if !cfg.Enabled {
		return nil, NewOIDCCallbackError(serializer.CodeParamErr, "OIDC SSO login is not enabled")
	}

	if service.Error != "" {
		reason := service.ErrorDescription
		if reason == "" {
			reason = service.Error
		}
		return nil, NewOIDCCallbackError(serializer.CodeParamErr, "The identity provider rejected the login: "+reason)
	}

	if service.State == "" {
		return nil, NewOIDCCallbackError(serializer.CodeParamErr, "Missing SSO state parameter")
	}

	rawSession, ok := dep.KV().Get(oidcStatePrefix + service.State)
	if !ok {
		return nil, NewOIDCCallbackError(serializer.CodeParamErr, "SSO session is expired or unknown, please sign in again")
	}
	// 用完即删，防止 state 重放。
	_ = dep.KV().Delete(oidcStatePrefix, service.State)

	session, ok := rawSession.(oidcLoginSession)
	if !ok {
		return nil, NewOIDCCallbackError(serializer.CodeParamErr, "SSO session is corrupted, please sign in again")
	}
	redirect := SanitizeOIDCRedirect(session.Redirect)

	if service.Code == "" {
		return nil, NewOIDCCallbackError(serializer.CodeParamErr, "Missing authorization code")
	}

	idToken, cbErr := service.verifyIDToken(c, dep, cfg, session)
	if cbErr != nil {
		return nil, cbErr
	}

	claims := make(map[string]any)
	if err := idToken.Claims(&claims); err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid, "Failed to parse id_token claims: "+err.Error())
	}

	subject := oidcClaimString(claims, cfg.SubjectClaim)
	email := strings.ToLower(strings.TrimSpace(oidcClaimString(claims, cfg.EmailClaim)))
	nick := strings.TrimSpace(oidcClaimString(claims, cfg.NameClaim))

	if subject == "" {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid,
			fmt.Sprintf("The id_token does not contain the subject claim %q", cfg.SubjectClaim))
	}
	if cfg.RequireEmailVerified && !oidcClaimBool(claims, "email_verified") {
		return nil, NewOIDCCallbackError(oidcCodeEmailNotVerified,
			"The email address returned by the identity provider is not verified")
	}

	expectedUser, cbErr := service.resolveUser(c, dep, cfg, subject, email, nick)
	if cbErr != nil {
		return nil, cbErr
	}

	// 复用既有发令牌逻辑签发 token pair。
	util.WithValue(c, inventory.UserCtx{}, expectedUser)
	loginResponse, err := IssueToken(c)
	if err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to issue token pair: "+err.Error())
	}

	WriteAuthAudit(c, eventtype.UserLogin, expectedUser, expectedUser.Email, nil)

	payload, err := json.Marshal(loginResponse)
	if err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to encode the login session: "+err.Error())
	}

	return &OIDCCallbackResult{
		Location: fmt.Sprintf("%s#session=%s&redirect=%s", oidcFrontendCallbackPath,
			base64.RawURLEncoding.EncodeToString(payload), url.QueryEscape(redirect)),
	}, nil
}

// verifyIDToken 用授权码换取 token 并校验 id_token（签名、aud、iss、exp、nonce）。
func (service *OIDCCallbackService) verifyIDToken(ctx context.Context, dep dependency.Dep, cfg *setting.OIDC,
	session oidcLoginSession) (*oidc.IDToken, *OIDCCallbackError) {
	provider, err := discoverOIDCProvider(ctx, cfg.Endpoint)
	if err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid, "Failed to discover the OIDC provider: "+err.Error())
	}

	// 让 oauth2 / go-oidc 都使用带超时的 HTTP client。
	ctx = oidc.ClientContext(ctx, oidcHTTPClient)
	ctx = context.WithValue(ctx, oauth2.HTTPClient, oidcHTTPClient)

	token, err := oidcOAuth2Config(ctx, dep, cfg, provider).Exchange(ctx, service.Code)
	if err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid, "Failed to exchange the authorization code: "+err.Error())
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid, "The token endpoint did not return an id_token")
	}

	idToken, err := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid, "Failed to verify the id_token: "+err.Error())
	}

	// go-oidc 不校验 nonce，需要自行比对授权请求时下发的值。
	if idToken.Nonce != session.Nonce {
		return nil, NewOIDCCallbackError(serializer.CodeCredentialInvalid, "The id_token nonce does not match the login session")
	}

	return idToken, nil
}

// resolveUser 按 (issuer, subject) → 邮箱自动绑定 → 自动注册的顺序解析本地用户。
func (service *OIDCCallbackService) resolveUser(c *gin.Context, dep dependency.Dep, cfg *setting.OIDC,
	subject, email, nick string) (*ent.User, *OIDCCallbackError) {
	db := dep.DBClient()
	userClient := dep.UserClient()
	loadCtx := context.WithValue(c, inventory.LoadUserGroup{}, true)

	binding, err := db.OIDCBinding.Query().
		Where(oidcbinding.IssuerEQ(cfg.Endpoint), oidcbinding.SubjectEQ(subject)).
		Only(c)
	switch {
	case err == nil:
		// 已绑定：直接复用绑定到的本地账号。
		boundUser, getErr := userClient.GetByID(loadCtx, binding.UserID)
		if getErr != nil {
			return nil, NewOIDCCallbackError(serializer.CodeUserNotFound, "The account bound to this identity is no longer available")
		}

		return oidcCheckUserStatus(boundUser)
	case !ent.IsNotFound(err):
		return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to query the OIDC binding: "+err.Error())
	}

	if email != "" {
		existing, getErr := userClient.GetByEmail(loadCtx, email)
		switch {
		case getErr == nil:
			if !cfg.AutoLinkEmail {
				return nil, NewOIDCCallbackError(serializer.CodeEmailExisted,
					"An account with the same email address already exists, please sign in with your password first")
			}
			if cbErr := createSSOBinding(c, db, cfg.Endpoint, existing.ID, subject, email); cbErr != nil {
				return nil, cbErr
			}

			return oidcCheckUserStatus(existing)
		case !ent.IsNotFound(getErr):
			return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to look up the account by email: "+getErr.Error())
		}
	}

	if !cfg.AutoRegister {
		return nil, NewOIDCCallbackError(serializer.CodeUserNotFound,
			"No local account is bound to this identity and automatic registration is disabled")
	}
	if email == "" {
		return nil, NewOIDCCallbackError(serializer.CodeParamErr,
			fmt.Sprintf("The identity provider did not return the email claim %q", cfg.EmailClaim))
	}

	newUser, cbErr := createSSOUser(c, dep, email, nick)
	if cbErr != nil {
		return nil, cbErr
	}

	if cbErr := createSSOBinding(c, db, cfg.Endpoint, newUser.ID, subject, email); cbErr != nil {
		return nil, cbErr
	}

	return newUser, nil
}

// createSSOUser 复用既有注册逻辑创建本地账号（默认用户组、注册来源审计）。
func createSSOUser(c *gin.Context, dep dependency.Dep, email, nick string) (*ent.User, *OIDCCallbackError) {
	args := &inventory.NewUserArgs{
		Email: email,
		Nick:  nick,
		// IdP 已验证身份，直接置为可用；SSO 账号不设置本地密码。
		Status:  user.StatusActive,
		GroupID: dep.SettingProvider().DefaultGroup(c),
	}

	userClient := dep.UserClient()
	uc, tx, _, err := inventory.WithTx(c, userClient)
	if err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to start transaction: "+err.Error())
	}

	newUser, err := uc.Create(c, args)
	if err != nil {
		_ = inventory.Rollback(tx)
		if errors.Is(err, inventory.ErrUserEmailExisted) || errors.Is(err, inventory.ErrInactiveUserExisted) {
			return nil, NewOIDCCallbackError(serializer.CodeEmailExisted, "An account with the same email address already exists")
		}

		return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to create the account: "+err.Error())
	}

	if err := inventory.Commit(tx); err != nil {
		return nil, NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to commit the account: "+err.Error())
	}

	// 注册来源标记：自动注册的账号写入 user_signup 审计事件。
	WriteAuthAudit(c, eventtype.UserSignup, newUser, newUser.Email, nil)

	return newUser, nil
}

// createSSOBinding 写入 (issuer, subject) 与本地用户的绑定关系。
// OIDC 与 QQ 登录共用 oidc_bindings 表，仅 issuer 不同。
func createSSOBinding(ctx context.Context, db *ent.Client, issuer string, uid int,
	subject, email string) *OIDCCallbackError {
	builder := db.OIDCBinding.Create().
		SetUserID(uid).
		SetIssuer(issuer).
		SetSubject(subject)
	if email != "" {
		builder = builder.SetEmail(email)
	}

	if _, err := builder.Save(ctx); err != nil {
		return NewOIDCCallbackError(serializer.CodeInternalSetting, "Failed to create the SSO binding: "+err.Error())
	}

	return nil
}

// oidcCheckUserStatus 拒绝被禁用 / 未激活的账号登录。
func oidcCheckUserStatus(u *ent.User) (*ent.User, *OIDCCallbackError) {
	switch u.Status {
	case user.StatusManualBanned, user.StatusSysBanned:
		return nil, NewOIDCCallbackError(serializer.CodeUserBaned, "This account has been blocked")
	case user.StatusInactive:
		return nil, NewOIDCCallbackError(serializer.CodeUserNotActivated, "This account is not activated")
	}

	return u, nil
}

// oidcClaimString 读取字符串型 claim，缺失或类型不符时返回空串。
func oidcClaimString(claims map[string]any, name string) string {
	if name == "" {
		return ""
	}

	value, ok := claims[name]
	if !ok {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

// oidcClaimBool 读取布尔型 claim，兼容字符串形式的 "true"。
func oidcClaimBool(claims map[string]any, name string) bool {
	value, ok := claims[name]
	if !ok {
		return false
	}

	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return false
	}
}
