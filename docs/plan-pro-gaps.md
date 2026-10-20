# Pro 缺口补齐方案（含接口盘点与 ZXFile 复用评估）

> 目标：把社区版里仍然「Pro 徽标 / 能力缺失」的地方做成可交付的方案：
> ① 全量接口盘点；② 明确还缺什么；③ 评估 ZXFile 能否补齐；④ 分批实施计划。

## 1. 接口盘点（已完成）

- 结果：**245 个接口**（`METHOD + 路径` 去重，路径参数统一记为 `:param`），清单见
  [`docs/api-inventory.md`](api-inventory.md)。
- 生成方式：`python3 docs/tools/gen-api-inventory.py`（直接解析 `routers/router.go` +
  `shop/router.go`，改路由后重跑即可，避免文档漂移）。
- 分布：

  | 分类 | 数量 | 说明 |
  | --- | --- | --- |
  | 公开 | 22 | 登录 prepare/token、站点配置、分享访问、举报提交、支付回调入口 |
  | 用户（登录） | 89 | 文件/分享/离线下载/WebDAV/OAuth 授权/增值服务购买 |
  | 小店 | 45 | 店铺前台、店主、买家、退款、素材、后台小店管理 |
  | 管理员 | 78 | 用户/组/策略/节点/文件/Blob/分享/任务/事件/举报/订单/系统设置 |
  | 从机 / 回调 | 11 | 从机上传与回调、支付通知 |
  | **合计** | **245** | |

- 前端覆盖比对：`assets/src/api/*.ts` 里 180 处 `send("...")` 调用，逐一与后端路由做静态前缀
  匹配，**没有「前端调用、后端不存在」的接口**（自动比对仅剩 4 条误报：`/admin/policy${…}`、
  `/callback/${…}`、`/site/config/${…}` 等拼接路径）。
- 结论：**接口层没有缺口**；剩下的缺口都是「能力」缺口，需要新增接口/表/流程，见第 3 节。

## 2. ZXFile 复用评估（结论：补不了这些缺口）

仓库：<https://github.com/zixiang0520/ZXFile>（浅克隆到本地比对，未合分支、未改上游）。

| 比对项 | 结果 |
| --- | --- |
| 仓库性质 | StorFlow V4 的魔改分支，README 自述「自研复刻 Pro 独有功能（商店支付 / 积分账本 / 换组到期回退 / 文件导入 / 存储策略迁移 / 协作分享）」，前端子模块私有 |
| 路由表 | ZXFile 205 条 ⊆ 我们 245 条；**ZXFile 没有任何我们缺的接口**（唯一差集是我们新增的 `GET /share/default`） |
| 已移植区域规模 | `service/admin/audit.go` 257=257、`service/vas/payment.go` 417 vs 416、`service/share/abuse.go` 46=46 —— 说明这些功能我们此前已从它移植并继续扩展 |
| ent/schema | `group.go`、`user.go` 与本仓库**完全一致**，没有额外的字段/表 |
| 8 项 Pro 缺口检索 | `mail_receipt_template` / `mail_exceed_quota_template` / `load_balance` / `allowed_nodes` / `default_pinned` / `folder_direct_link` / `oidc` / `available_policy` / `CustomPropsTypeUser` 在 ZXFile 中**全部 0 命中** |
| ZXFile 独有代码 | 仅 `pkg/filemanager/driver/yun139/`（中国移动云盘驱动，4 个文件 1725 行） |

因此：

- 第 3 节的 8 项缺口 **必须自己实现**，ZXFile 没有现成代码可搬。
- 唯一值得搬的是 **yun139 存储驱动**（属于「存储策略提供商」缺口，不是 Pro 徽标项），
  搬运清单见 4.1。

### 2.1 前端「Pro 徽标」是怎么来的（没有统一开关）

结论：**前后端都没有统一的 Pro 检查/授权开关**，Pro 门禁是前端**逐处硬编码**的：

| 机制 | 位置 | 说明 |
| --- | --- | --- |
| `SettingForm ... pro` | `Pages/Setting/SettingForm.tsx` | 字段级：标题后加 `ProChip`，点击弹出 `ProDialog` |
| `<ProChip label="Pro" />` | 各设置页内联 | 同上，只是手写在 label 里 |
| `item.pro` | `Frame/NavBar/PageNavigation.tsx` | 导航项级：带 `Pro` 徽标并拦截点击 |
| `ProDialog` | `Admin/Common/ProDialog.tsx` | 纯营销弹窗（功能清单是写死的文案） |
| 首页推广卡片 | `Admin/Home/Home.tsx` | 营销位，点击只弹同一弹窗 |

后端侧：`license` 只是 `inventory/setting.go` 里一个空默认值 + `setting.Provider.License(ctx)`
读取方法，**全仓库没有任何地方调用它**；前端也只有 `ManualRefreshLicenseService` 一个未使用的
类型定义。所以「Pro 功能打不开」从来不是后端开关没开，而是**某处仍写着 `pro` 标记**，
或者浏览器 Service Worker 还在跑旧包（旧包才带那些 `Pro` 徽标）。

因此解锁某个 Pro 功能的固定动作是：① 后端把能力补齐；② 前端删掉该处的 `pro` 标记并接真实数据。

已按此方式解锁：付费分享/协作分享、文件权限、多存储策略（G1）、回执与超额邮件（G5）、
事件记录开关、自定义属性用户类型（G7）、组默认固定分享（G2）、初始分享快捷方式
（`default_symbolics`，注册时写入新用户置顶）、用户详情里的积分 / 原始用户组 / 用户组过期日期、
文件详情里的主存储策略。

仍保留 Pro 的（后端确实没有实现，见第 3 节）：可用节点 / 允许选择节点（G3）、
SSO 单点登录（G6）、邮箱后缀黑白名单与子地址邮箱限制（未纳入 G 清单）。

> PWA 缓存问题：`vite.config.ts` 已从 `registerType: "prompt"` 改为 `autoUpdate`
> （并显式 `skipWaiting`/`clientsClaim`），新构建会自动接管，避免「改完了页面没变」。
> 但**从旧版本升级的第一次**仍需在浏览器里刷新一次（或点「有新版本，请刷新」），
> 因为刷新逻辑本身在旧包里。

## 3. 缺口清单与实现方案

| # | 缺口 | 现状 | 需要的底层能力 | 估时 |
| --- | --- | --- | --- | --- |
| G1 | 用户组「可用存储策略」多选 | ✅ **已实现**（2026-10-18）：组级白名单 `available_policies` + 上传/迁移校验 | 组级策略白名单 + 上传时策略选择校验 | 3–5 人日 |
| G2 | 默认固定分享 | ✅ **已实现**（2026-10-18）：组级默认置顶 + 合并进侧边栏 + 不可取消 | 组级默认置顶 + 与用户置顶列表合并、不可取消 | 2–3 人日 |
| G3 | 可用节点 / 允许选择节点 | ✅ **已实现**（2026-10-18）：组级节点白名单 + 任务按白名单分配 + 用户自选节点（含 `/workflow/nodes` 接口与任务对话框选择器） | 组级节点白名单 + 任务按节点分配/用户选择 | 5–8 人日 |
| G4 | 文件夹直链 | ✅ **已实现**（2026-10-18）：目录直链 + 访问时现场打包 ZIP（带并发上限） | 文件夹打包直链（按需生成 + 缓存 + 限流） | 3–5 人日 |
| G5 | 回执 / 超额提醒邮件模板 | ✅ **已实现**（2026-10-18）：两个模板 + 履约/超额发送点 | 两个模板默认值 + 履约/超额发送点 | 1–2 人日 |
| G6 | 第三方登录 / SSO | ✅ **已实现**（2026-10-19）：OIDC 授权码登录 + 账号绑定/自动注册 + 管理端配置；Logto 复用 OIDC 配置 | OIDC 授权码流程 + 账号绑定 | 5–8 人日（含新依赖） |
| G7 | 自定义属性「用户」类型 | ✅ **已实现**（2026-10-18）：`user` 类型 + 校验 + 渲染 | 新增 `user` 属性类型 + 校验 + 渲染 | 0.5–1 人日 |
| G8 | 负载均衡存储策略 | ✅ **已实现**（2026-10-18）：`load_balance` 复合策略 + 上传/迁移按权重选子策略 + 保存校验 + 前端子策略编辑器 | 策略复合类型 + 上传时按权重选子策略 | 4–6 人日 |
| G9 | 邮箱域过滤 / 子地址邮箱限制 | ✅ **已实现**（2026-10-18）：`filter_email_provider*` + `disable_sub_address_email` + 注册拦截 | 注册时按域名白/黑名单与 `+` 子地址拦截 | 1–2 人日 |
| G10 | 站点公告 | ✅ **已实现**（2026-10-19）：`siteAnnouncement` 可编辑 + 前台登录后弹窗（改文案重新提醒） | 站点级公告 + 前台展示与「已读」记忆 | 0.5–1 人日 |
| G11 | 客户端「反馈 / 论坛」链接 | ✅ **已实现**（2026-10-19）：`app_feedback` / `app_forum` + 站点配置下发 + 连接与挂载页入口 | 两个链接设置 + 客户端配置接口 + 前台入口 | 0.5 人日 |
| G12 | QQ 互联登录 | ✅ **已实现**（2026-10-19）：QQ OAuth2 授权码登录 + 复用 OIDC 绑定表与回跳契约 | QQ OAuth2 + openid + 账号绑定/自动注册 | 2–3 人日 |

各项关键技术点：

- **G1** ✅：`inventory/types.GroupSetting` 增 `available_policies []int` + 共享判定函数
  `types.AllowedStoragePolicies(setting, defaultPolicyID)`（白名单为空 → 退回「只允许组绑定策略」，
  两者都为空 → 不限制）；`pkg/filemanager/fs/dbfs/upload.go` 在 `policy_id` 显式指定时校验
  ∈ 白名单（管理员豁免），`dbfs.getPreferredPolicy` 在组未绑定单一策略时退回白名单第一项；
  `service/explorer/workflows.go` 的 `migrate_policy` 目的地判断从「等于组策略」改为「∈ 组白名单」。
  前端 `PolicySelectionInput` 改为多选（复选 + Chips），`BasicInfoSection` 把整份列表写入
  `settings.available_policies`、第一项作为组默认策略，并移除「可多选」Pro 徽标。
- **G2** ✅：`inventory/types.GroupSetting` 增 `default_pinned_shares []int`、`PinedFile` 增
  `locked bool`；`service/user/pins.go` 负责把分享数字 ID 解析为 `storflow://<hash>@share` 置顶项
  （跳过已删除分享）并合并到用户置顶列表（组默认项固定在前，不重复），在 `BuildUserWithGroupPins`
  里接入 `/user/me` 与登录/令牌响应；`service/explorer/pin.go` 的取消置顶对组默认项返回 40007。
  前端 `DefaultPinnedSection` 去掉 Pro 徽标并把真实的 `SharesInput`（按数字 ID 搜索）写入
  `settings.default_pinned_shares`；侧边栏置顶行对锁定项显示锁图标且不可取消。
- **G3** ✅：`GroupSetting` 增 `allowed_nodes []int` + `allow_select_node bool`（避免新增权限位编号）；
  `cluster.NodePool.Get(ctx, capability, preferred, allowed []int)` 按白名单过滤（空=不限，全部被过滤时
  返回 `ErrNoAvailableNode`）；`workflows.NodeSelection{NodeID,AllowedNodes}` 贯穿
  `NewCreateArchiveTask` / `NewExtractArchiveTask` / `NewRemoteDownloadTask`，执行期以
  `NodeState.AllowedNodes` 再兜底；任务创建接口 `node_id`（hash）由 `resolveNodeSelection` 校验
  （未开启 → 40007，非法 hash → 40001，不在白名单 → 40007）；新增
  `GET /workflow/nodes` 返回当前用户组可选的节点（未开启或过滤后为空则返回空数组）。
  前端 `MultipleNodeSelectionInput` 改为真实多选（`getNodeList`）、`FileManagementSection` 两个开关
  去掉 Pro 徽标并落库，任务对话框（压缩/解压/离线下载）在节点列表非空时显示节点选择器。
- **G4** ✅：`GroupSetting.FolderDirectLink`（`folder_direct_link`，用户组开关）控制是否允许；
  `manager.GetDirectLink` 对目录创建跳转型直链（目录无单文件实体，只能走 `/f/:id/:name`）；
  `RedirectDirectLink` 命中目录时由 `serveFolderDirectLink` 现场 `CreateArchive` 流式返回 ZIP
  （`Content-Type: application/zip`，含并发上限 `folderLinkConcurrency=4`，超限返回 429 文案）；
  用户组 DTO 增 `folder_direct_link`，前端「允许目录直链」开关去掉 Pro 徽标并真正落库，
  文件管理器对目录「获取直链」时直接生成一条打包直链（不再展开目录内文件）。
  - 已知取舍：ZIP 未做磁盘缓存（每次请求重新打包）；大批量目录建议后续加缓存层。
- **G5** ✅：`inventory/setting.go` 新增 `mail_receipt_template` / `mail_exceed_quota_template`
  两个默认值（11 种语言，正文复用激活邮件 HTML 外壳、只做占位符改名，避免复制 10KB ×2）；
  `pkg/email/template.go` 新增 `NewReceiptEmail` / `NewExceedQuotaEmail`（含 `FormatSize`/`UsedPercent`）；
  `pkg/setting` 新增两个模板读取方法；发送点：`service/vas/payment.go` 履约成功后发回执、
  `service/explorer/quota_notify.go` 在上传因容量不足被拒时发超额提醒（按用户 24h 节流，走 KV）；
  `inventory/client.go` 新增 `ensureDefaultSettings`：**每次启动补齐缺失的默认设置**（旧库不升版本号
  也能拿到新模板；跳过 `secret_key`/`hash_id_salt`/`encrypt_master_key` 这类随机默认值），
  否则新模板键在既有安装上永远不存在。前端 `EmailTemplates.tsx` 去掉两处 `pro: true`，
  `Settings.tsx` 的邮件页把两个键加入加载列表。
- **G6** ✅（2026-10-19）：引入 `github.com/coreos/go-oidc/v3`（+ `golang.org/x/oauth2`、`go-jose/v4` 间接依赖）；
  新增 ent schema `OIDCBinding`（`(issuer, subject)` 唯一索引，`user` 反向边），`inventory/user.go` 的
  `userClient.Delete` 会一并清理绑定（否则删用户会撞外键约束）；
  `service/user/oidc.go` 实现发现文档缓存、授权码交换、`id_token` 验签（含 nonce 校验）、
  state 防重放（KV，10 分钟 TTL）、账号绑定 / 邮箱自动绑定 / 自动注册；
  14 个 `oidc_*` 站点设置（`inventory/setting.go` + `pkg/setting` 的 `OIDC()` getter）；
  三个公开路由：`GET /session/sso`、`GET /session/oidc/authorize`、`GET /session/oidc/callback`
  （成功 302 到 `/oidc/callback#session=<base64url(LoginResponse)>&redirect=`，失败 302 到 `/login?sso_error=&sso_msg=`）；
  前端：登录页 SSO 按钮 / `auto_redirect` / `password_login_disabled`、`/oidc/callback` 路由、
  管理端「OIDC」手风琴真实化（回调地址一键复制 + 13 个字段）；**Logto 是标准 OIDC 提供方**，
  改为提示直接用 OIDC 配置（去掉 Pro 徽标），仅「QQ 互联」仍为未实现的 Pro 占位。
- **G7**：`types.CustomPropsTypeUser = "user"`；`service/admin` 的自定义属性校验值必须是用户 hash id；
  前端属性编辑器复用 `UserSearchInput`，展示用 `UserAvatar`。
- **G8** ✅：新增策略类型 `load_balance`（`PolicySetting.LoadBalanceSubPolicies`：`{policy_id,weight}` 列表）；
  `service/admin/policy.go` 的 `validateLoadBalancePolicy` 在保存时校验（非空、不能指自己/另一个负载均衡、
  子策略必须存在、`weight<=0` 归一化为 1）；`pkg/filemanager/fs/loadbalance.go` 的
  `ResolveLoadBalancePolicy` 按权重随机取子策略，在 `dbfs.getPreferredPolicy`（用户组默认/白名单）与
  `dbfs.upload.PrepareUpload`（显式 `policy_id`）两处解析，落库/上传会话记录的都是**子策略 ID**；
  `manager.GetStorageDriver` 对未解析的 `load_balance` 返回明确错误而非泛化失败；迁移目标策略同样先解析。
  前端策略向导/编辑页新增子策略编辑器（`LoadBalanceSubPolicyInput`），去掉卡片 Pro 徽标，13 种语言补齐
  `policy.loadBalance*` 文案。
  - 已知取舍：白名单校验针对 `load_balance` 策略本身，其子策略视为管理员可信配置（同一策略可被多个白名单
    不同的用户组共用）；删除被引用的子策略不做拦截，运行时返回「Sub policy #N not found」。
- **G9** ✅：`register.go` 在创建用户前拦截：`disable_sub_address_email` 开启时拒绝 `+` 子地址；
  `filter_email_provider`（disabled/whitelist/blacklist）+ `filter_email_provider_rule`（逗号分隔、大小写不敏感）
  按域名白/黑名单拦截，统一返回 40031；三个键进 `DefaultSettings`（`ensureDefaultSettings` 会为旧库补齐）；
  前端 `UserSession.tsx` 两处去掉 Pro 徽标（选择器 + `+` 子地址开关 + 规则输入框）。
- **G10** ✅（站点公告）：设置键 `siteAnnouncement`（`inventory/setting.go` 早已存在，本次把管理端
  只读+Pro 的占位改成真实多行输入，并加入站点信息页的 `SettingsWrapper` 加载列表）；
  `settings.SiteBasic().Announcement` → `GET /site/config/basic.announcement` 下发；
  前端新增 `Common/SiteAnnouncement.tsx`（登录后弹窗，`localStorage` 按公告文案哈希记录「已读」，
  改文案会重新提醒），挂在 `AppMain` 里。
- **G11** ✅（客户端反馈/论坛链接）：新增设置键 `app_feedback` / `app_forum`（默认空 = 不展示），
  `pkg/setting.AppSetting` 增 `Feedback`/`Forum`，`service/basic/site.go` 的 `app`（及 basic）片段
  下发 `app_feedback` / `app_forum`；管理端「移动客户端」区块两个字段改为可编辑（原来 readOnly+Pro），
  `vas.appLinkDes` 去掉「仅当 VOL 授权有效」的误导文案；前端 `Pages/Devices/AppLinks.tsx`
  在「连接与挂载」的移动客户端 / 桌面客户端引导页展示对应按钮（未配置则不渲染）。
- **G12** ✅（QQ 互联）：`service/user/qq.go` 实现 QQ OAuth2 授权码登录（authorize → token → me(openid)
  → user/get_user_info），state 存 KV 防重放，绑定复用 `oidc_bindings`（`issuer="qq"`），
  回跳与 OIDC 完全一致的 `/oidc/callback#session=…`；`/session/sso` 增加 `qq{enabled,display_name,auto_redirect,authorize_url}`；
  管理端「QQ 互联」手风琴真实化（启用/app_id/app_secret/display_name/auto_register/direct_sign_in + 回调地址），
  登录页新增 QQ 按钮；`qq_api_base`/`qq_graph_base` 为隐藏高级设置（自建代理/测试用）。
  QQ 不返回邮箱，自动注册时用确定性占位邮箱 `qq_<openid>@<站点域名>`（代码注释写明）。


## 4. 分批实施计划

### 4.1 批次 1（P0，约 2–3 人日，低风险，建议立刻做）

1. ✅ **已完成：搬运 ZXFile 的 yun139 驱动**（复制合并，未合分支）：
   - `pkg/filemanager/driver/yun139/{api,auth,cron,driver}.go`（1725 行，原样复制）；
   - `inventory/types/types.go`：`PolicyTypeYun139 = "yun139"` + `PolicySetting` 五个字段
     （`yun139_token` / `yun139_username` / `yun139_password` / `yun139_mail_cookies` / `yun139_root_folder_id`）；
   - `pkg/filemanager/manager/fs.go`：`case types.PolicyTypeYun139: return yun139.New(ctx, policy, m.config, m.l)`；
   - `pkg/setting/types.go`：`CronTypeYun139TokenRefresh`；`inventory/setting.go`：
     `"cron_yun139_token_refresh": "@every 6h"`；
   - 前端：`api/explorer.ts` 的 `PolicyType` 加 `yun139`、`api/dashboard.ts` 的 `PolicySetting` 加 5 个字段、
     `StoragePolicySetting.tsx` 的 `PolicyPropsMap` 加卡片、新增 `Wizards/Yun139/Yun139Wizard.tsx`、
     `EditStoragePolicy/FormSections/BasicInfoSection.tsx` 加 5 个编辑字段、`public/static/img/yun139.svg`、
     i18n `policy.yun139*`（中/英）。
   - 已验证：`go build ./...` 通过；策略页「添加存储策略」列表出现「移动云盘（139）」；
     `PUT /admin/policy` 创建 → `POST /admin/policy` 读回 `settings.yun139_*` → 删除，均成功。
   - 待补（需要真实账号）：上传/下载/删除的真实链路验证，以及 `cron_yun139_token_refresh` 续期日志。
2. ✅ **G7 自定义属性 user 类型**：`inventory/types.CustomPropsTypeUser` + `manager/metadata.go` 校验
   （值必须是可解析且存在的用户 hash id）+ 前端 `UserPropsContent.tsx`（搜索/选择用户，只读显示头像+昵称）；
   管理页与文件侧边栏均已浏览器验证（无 Pro 徽标）。
3. ✅ **G5 回执 / 超额邮件模板**（见第 3 节 G5）：真实 SMTP 抓包验证——履约后收到
   `[StorFlow] Payment successful`（含订单号/商品/金额/时间），容量不足被拒后收到
   `[StorFlow] 存储空间已用尽`（含 `1.0 GiB / 1.0 GiB（100%）`），第二次上传被 24h 节流拦住。

### 4.2 批次 2（P1，约 6–9 人日）

4. ✅ **G1 多存储策略**（含 `migrate_policy` 语义升级为「组白名单内自由迁移」）：接口验证
   `available_policies` 写入/读回 `[1,2]`；普通用户上传指定白名单外策略 → `40007`、
   白名单内 → `0`、不指定 → 沿用组默认；迁移目的地白名单外 → `40007`、白名单内 → 任务入队；
   组未绑定单一策略时默认策略退回白名单第一项。浏览器验证：多选下拉 3 项 3 个复选、
   选中后 Chips 显示策略名、保存后接口读回 `[1,4,5]`。
   - 遗留：前台还没有「上传时选择存储策略」的入口（接口已支持 `policy_id`），
     需要时在文件上传对话框加一个策略下拉即可。
5. ✅ **G2 默认固定分享**：接口验证——组配置 `default_pinned_shares=[1]` 后，组内用户
   `/user/me` 的 `pined` 出现 `storflow://B2c5@share`（name=season-bundle，`locked: true`）；
   对该项取消置顶返回 `40007`；用户自己的置顶排在组默认项之后且可正常取消；清空组配置后恢复。
   浏览器验证——管理页「默认固定分享」无 Pro 徽标、按数字 ID 搜到 `season-bundle #1` 并保存成功
   （接口读回 `[1]`）；用户侧边栏置顶行显示禁用状态的锁图标（tooltip「默认固定分享」）。

### 4.3 批次 3（P2，按需）

6. ✅ **G4 文件夹直链**（见第 3 节 G4）；7. ✅ **G8 负载均衡策略**；8. ✅ **G3 节点路由**；
9. ✅ **G6 SSO/OIDC**（2026-10-19）；10. ✅ **G10 站点公告**；11. ✅ **G11 客户端反馈/论坛链接**；
12. ✅ **G12 QQ 互联**（2026-10-19，见第 3 节 G10–G12）。

> **G1–G12 全部实现并由真实接口 + 浏览器验证**。前端已无任何 `SettingForm pro` / `pro: true` 占位
> （`ProChip`/`ProDialog` 组件仍保留，但只服务于「面板首页」的升级营销卡片，属产品设计而非功能门禁）。
> 「Logto」不再是独立实现，改为提示复用 OIDC 配置（Logto 是标准 OIDC 提供方）。

## 5. 验收与回归

每批次固定跑：

```bash
cd assets && npx tsc --noEmit   # 基线 120（G1–G9 全部完成后为 120，勿新增）
cd assets && yarn build
cd .. && zip -q -r application/statics/assets.zip assets/build   # 前端为编译期内嵌
go build ./... && go test ./shop/...
python3 docs/tools/gen-api-inventory.py                          # 接口清单刷新
```

> 邮件模板这类**新增默认设置**依赖启动时的 `ensureDefaultSettings` 补齐：改完要重启后端，
> 并在 `/admin/settings?tab=email` 里确认两个模板能加载出内容（空模板 = 键没进库）。

浏览器回归：`/admin/payment`、`/admin/event`、`/admin/abuse`、`/admin/settings?tab=vas`、
`/admin/group/:id`、`/admin/settings?tab=events`、`/shop/*`（前台+店主+买家）。

## 6. 风险与约定

- 存储策略相关改动（G1/G4/G8）会触碰上传路径，必须补单测（`pkg/filemanager/manager`、
  `service/explorer`）后再合。
- G6 需要新增第三方依赖，先确认是否允许。
- 前端构建产物内嵌在 Go 二进制里：**改完前端必须重新 `go build`**，否则页面还是旧包。
- ZXFile 只在 `pkg/filemanager/driver/yun139/` 复用；其余代码一律自己实现，避免合入来源不明的实现。
