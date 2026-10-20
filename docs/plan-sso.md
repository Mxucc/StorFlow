# G6 第三方登录 / SSO（OIDC）实现规格（前后端契约冻结版）

> 本文件是 G6 的实现契约，前后端两路并行开发都以本文件为准，不要各自发明字段名。
> 目标：把管理端「SSO / OIDC」从 Pro 占位变成**可用的 OIDC 授权码登录**，并支持账号绑定与自动注册。

## 1. 站点设置（`inventory/setting.go` 的 `DefaultSettings`）

全部为**独立字符串键**（不用 JSON blob，方便设置页逐项渲染）：

| key | 默认值 | 说明 |
| --- | --- | --- |
| `oidc_enabled` | `"0"` | 是否启用 OIDC 登录 |
| `oidc_endpoint` | `""` | IdP issuer，例如 `https://idp.example.com`（用于发现文档） |
| `oidc_client_id` | `""` | 客户端 ID |
| `oidc_client_secret` | `""` | 客户端密钥 |
| `oidc_scopes` | `"openid profile email"` | 空格分隔 |
| `oidc_display_name` | `"vas.sso"` | 登录页按钮文案，可为 i18n key 或字面量 |
| `oidc_auto_redirect` | `"0"` | 打开登录页时自动跳转 IdP |
| `oidc_password_login_disabled` | `"0"` | 隐藏并禁用密码登录 |
| `oidc_auto_register` | `"0"` | 首次登录时自动创建用户 |
| `oidc_auto_link_email` | `"0"` | 邮箱已存在时自动绑定到该用户 |
| `oidc_require_email_verified` | `"0"` | 要求 id_token 的 `email_verified` 为 true |
| `oidc_subject_claim` | `"sub"` | 唯一标识 claim |
| `oidc_email_claim` | `"email"` | 邮箱 claim |
| `oidc_name_claim` | `"name"` | 昵称 claim |

密钥类键名以 `_secret` 结尾 → 参考既有 `secret_key` 的 `ensureDefaultSettings` 跳过逻辑（本次默认值为空串，可正常补齐）。

## 2. 后端接口契约

### 2.1 `GET /api/v4/session/sso`（公开，无需登录）

```json
{"code":0,"data":{
  "enabled": true,
  "display_name": "vas.sso",
  "auto_redirect": false,
  "password_login_disabled": false,
  "authorize_url": "/api/v4/session/oidc/authorize"
},"msg":""}
```

- `oidc_enabled` 为关时 `enabled=false`，其余字段仍返回。
- 前端据此显示登录按钮 / 自动跳转 / 隐藏密码表单。

### 2.2 `GET /api/v4/session/oidc/authorize?redirect=<前端相对路径>`

- 生成随机 `state`（32 字节，hex）与 `nonce`；把 `{state, nonce, redirect}` 存进 KV（`dep.KV()`），TTL 10 分钟。
- 302 到 IdP 的 authorization endpoint，参数：`client_id`、`response_type=code`、`scope`、`redirect_uri`（= 站点 URL + `/api/v4/session/oidc/callback`）、`state`、`nonce`。
- `redirect` 只允许站点内相对路径（以 `/` 开头且不以 `//` 开头），否则回退 `/home`。
- 未启用 → `serializer.CodeParamErr`（40001，msg 明确）。

### 2.3 `GET /api/v4/session/oidc/callback?code=&state=`

成功：302 到 `"/oidc/callback#session=" + base64url(JSON({"user": User, "token": Token})) + "&redirect=" + urlencode(redirect)`

- `User` / `Token` 复用 `service/user/response.go` 的既有结构（`BuildUser` + 既有 token 结构）。
- 前端 `OIDCCallback` 路由解码后走既有 `refreshUserSession(session, redirect)`。

失败：302 到 `"/login?sso_error=<错误码数字>&sso_msg=<urlencoded 可读原因>"`。

失败原因（用 `serializer.CodeParamErr` 之外的既有码或自定义）：state 缺失/过期、code 交换失败、id_token 校验失败、邮箱未验证、邮箱已存在且未开自动绑定、用户被禁用。

### 2.4 账号绑定

- 新增 ent schema `ent/schema/oidcbinding.go`：
  - `issuer`（string, not empty）、`subject`（string, not empty）、`email`（string, optional）
  - `created_at` / `updated_at`（`field.Time().Default(time.Now).UpdateDefault(time.Now)`）
  - 唯一索引 `(issuer, subject)`
  - 边：`user` → `edge.From("user", User.Type).Ref("oidc_bindings").Unique().Required()`
- `ent/schema/user.go` 增加 `edge.To("oidc_bindings", OIDCBinding.Type)`。
- 改完 schema 必须重新生成：`cd /Volumes/DA/Dli.li/E/storflow && go generate ./ent`（ent 代码已在本仓库纳入版本管理，重新生成会产生较大 diff，属预期）。
- 查找顺序：`(issuer, subject)` 命中 → 该用户；未命中且 `oidc_auto_link_email` 且邮箱命中已有用户 → 绑定该用户；否则 `oidc_auto_register` → 建新用户（默认用户组，注册来源标记）→ 绑定；再否则报错。

### 2.5 依赖

允许新增依赖（用户已批准）：`github.com/coreos/go-oidc/v3`（+ 其依赖 `github.com/go-jose/go-jose/v4`、`golang.org/x/oauth2`）。
用 `go get` 后确保 `go.mod` / `go.sum` 一致，`go build ./...` 通过。

## 3. 前端契约

### 3.1 登录页（`src/component/Pages/Login/Signin/SignIn.tsx`）

- 新增 `getSSOConfig()` thunk（`GET /session/sso`），`SignIn.tsx` 挂载时拉取。
- `enabled` → 在密码表单上方显示按钮（`display_name` 若能在 i18n 里查到就翻译，否则原样显示），点击跳 `/api/v4/session/oidc/authorize?redirect=<当前 path+search>`。
- `auto_redirect` → 挂载后立即跳转一次（用 `useRef` 防重复）。
- `password_login_disabled` → 隐藏密码/邮箱表单。

### 3.2 回调路由

- 新路由 `/oidc/callback`（**不需要登录守卫**），组件 `src/component/Pages/Login/OIDCCallback.tsx`：
  - 解析 `location.hash` 的 `session`（base64url）与 `redirect`；
  - `dispatch(refreshUserSession(session, redirect))`；
  - 解析失败 → `router.navigate("/login")` 并 snackbar 报错。

### 3.3 管理端设置页（`src/component/Admin/Settings/UserSession/SSOSettings.tsx`）

- 「OIDC」手风琴**真实化**（去掉 Pro 弹窗与 Pro 徽标）：启用开关 + 上表 12 个字段（密钥用 `type="password"`，布尔用 Switch，数字/文本用 `DenseFilledTextField`）。
- 顶部提供一个「复制回调地址」或只读展示 `站点URL + /api/v4/session/oidc/callback`，方便配置 IdP。
- 「QQ 互联」「Logto」两项保持现状（仍未实现，保留 Pro 徽标，但徽标要挂到各自手风琴上，不再用整块 `ProDialog` 包住全部三项）。
- `Settings.tsx` 的 UserSession 页 `SettingsWrapper settings={[...]}` 列表要补上全部 `oidc_*` 键。

### 3.4 i18n

- 新增 key 放 `settings.*`：`oidc`、`oidcEnabled`、`oidcEnabledDes`、`oidcEndpoint`、`oidcEndpointDes`、`oidcClientId`、`oidcClientSecret`、`oidcScopes`、`oidcDisplayName`、`oidcAutoRedirect`、`oidcPasswordLoginDisabled`、`oidcAutoRegister`、`oidcAutoLinkEmail`、`oidcRequireEmailVerified`、`oidcSubjectClaim`、`oidcEmailClaim`、`oidcNameClaim`、`oidcCallbackUrl`、`oidcCallbackUrlDes`。
- 至少补齐 `en-US`、`zh-CN`、`zh-TW`；其余语言缺失会回退到 `en-US`（`i18n.ts` 已配置 `fallbackLng: "en-US"`）。
- 登录页按钮文案 key：`login.ssoSignIn`（en-US / zh-CN / zh-TW），按钮用 `t(...)` 而不是裸字符串。

## 4. 验证要求（父代理负责端到端，子代理负责各自的可测部分）

1. 后端：`go build ./...`、`go test ./service/user/... ./inventory/... ./pkg/setting/...` 通过。
2. 前端：`npx tsc --noEmit` 错误数不得超过当前基线 **121**。
3. 父代理会用 Python 写的 mock OIDC IdP（discovery + authorize + token + JWKS，RS256）跑完整登录：302 → code → callback → 拿到 `#session=` → 解析出 token 与 user；第二次登录复用同一绑定；禁用时接口报错。
