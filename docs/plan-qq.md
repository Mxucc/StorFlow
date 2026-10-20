# QQ 互联登录（Pro 占位 → 真实实现）规格

> 目标：把管理端「QQ 互联」从 Pro 占位变成**可用的 QQ OAuth2 登录**，复用 OIDC 已有的绑定表与回跳契约。
> 参考已完成的 OIDC 实现：`docs/plan-sso.md`、`service/user/oidc.go`、`ent/schema/oidcbinding.go`。

## 1. 设置（`inventory/setting.go`，键已存在，不要改名）

| key | 默认值 | 说明 |
| --- | --- | --- |
| `qq_login` | `"0"` | 是否启用 QQ 登录 |
| `qq_login_config` | `{"direct_sign_in":false}` | JSON，字段：`app_id`、`app_secret`、`direct_sign_in`(bool)、`auto_register`(bool，默认 true)、`display_name`(string，默认 `"vas.qqConnect"`) |
| `qq_api_base` | `"https://graph.qq.com/oauth2.0"` | **隐藏高级设置**（不在管理端渲染）：authorize/token/me 的基址，便于自建代理与端到端测试 |
| `qq_graph_base` | `"https://graph.qq.com"` | **隐藏高级设置**：`/user/get_user_info` 的基址 |

`pkg/setting` 增加 getter（例如 `QQLogin(ctx) bool`、`QQConfig(ctx) *QQConfig`），结构体放 `pkg/setting/types.go`。

## 2. 接口

### 2.1 扩展现有 `GET /api/v4/session/sso`

保持既有字段不变，新增：

```json
"qq": {"enabled": true, "display_name": "vas.qqConnect", "auto_redirect": false, "authorize_url": "/api/v4/session/qq/authorize"}
```

### 2.2 `GET /api/v4/session/qq/authorize?redirect=<前端相对路径>`

- 生成 `state`（32 字节 hex）存 KV（key `qq:state:<state>`，TTL 10 分钟），`redirect` 用与 OIDC 相同的白名单清洗（`SanitizeOIDCRedirect`）。
- 302 到 `<qq_api_base>/authorize?response_type=code&client_id=<app_id>&redirect_uri=<站点URL>/api/v4/session/qq/callback&state=<state>&scope=get_user_info`。

### 2.3 `GET /api/v4/session/qq/callback?code=&state=`

1. 校验并消费 state（缺失/过期 → 302 `/login?sso_error=40001&sso_msg=...`）。
2. `<qq_api_base>/token?grant_type=authorization_code&client_id&client_secret&code&redirect_uri&fmt=json` → `access_token`。
3. `<qq_api_base>/me?access_token=<token>&fmt=json` → `openid`。
4. `<qq_graph_base>/user/get_user_info?access_token=&oauth_consumer_key=<app_id>&openid=` → `nickname`、`figureurl_qq_2`（头像，可选）。
5. 登录：`(issuer="qq", subject=openid)` 命中 `oidc_bindings` → 该用户；未命中且 `auto_register` → 建新用户并绑定；否则 302 错误回跳（错误码可自定义，msg 可读）。
   - QQ 不返回邮箱：新建用户的邮箱用**确定性生成**（例如 `qq_<openid>@<站点域名>`），必须在代码注释里写清，并在报告里说明。
6. 成功 → 302 到 `/oidc/callback#session=<base64url(LoginResponse)>&redirect=<urlencoded redirect>`（与 OIDC 完全一致的片段契约，前端已有 `OIDCCallback` 路由可复用）。

## 3. 前端

- `SignIn.tsx`：`/session/sso` 返回的 `qq.enabled` 为真时，在 SSO 按钮旁边再显示一个 QQ 按钮（`t("login.qqSignIn")` 或 `display_name`），点击跳 `/api/v4/session/qq/authorize?redirect=...`；`qq.auto_redirect` 与 OIDC 的 `auto_redirect` 语义一致（只自动跳一次；两者都开时以 QQ 为准并注释说明）。
- `SSOSettings.tsx`：把「QQ 互联」手风琴真实化（去掉 Pro 徽标与 ProDialog 点击拦截）：启用开关 + `app_id` + `app_secret`(password) + `display_name` + `auto_register` + `direct_sign_in` + 只读展示回调地址。若该页已无任何 Pro 占位，可一并删除 `ProDialog`/`ProChip` 的 import 与用法。
- `Settings.tsx`：UserSession 页的 `SettingsWrapper settings=[...]` 补 `qq_login`、`qq_login_config`。
- i18n：`settings.qqLoginEnabled*`、`settings.qqAppId`、`settings.qqAppSecret`、`settings.qqDisplayName`、`settings.qqAutoRegister`、`settings.qqDirectSignIn`、`settings.qqCallbackUrl`、`login.qqSignIn`，至少 en-US / zh-CN / zh-TW（其它语言回退 en-US）。

## 4. 验证要求

1. `go build ./...`、`go test ./service/user/... ./pkg/setting/...` 通过。
2. **真实 HTTP 端到端**：用 `qq_api_base`/`qq_graph_base` 指向自建 Python mock（authorize/token/me/get_user_info），在**副本库 + 非 5212 端口**上跑：`/session/sso` 看到 qq.enabled → authorize 302 → 回调 → 拿到 `#session=` 且解出 user/token → `/user/me` 200 → 第二次登录复用同一 user.id；关闭 `qq_login` 后 authorize 报错。把每一步真实响应片段贴进报告。
3. 前端 `npx tsc --noEmit` 错误数不得超过 **120**；不跑 `yarn build`。
