# Pro 功能落地状态（社区版补齐）

> 目标：社区版前端里所有「Pro 徽标 + 无法打开」的入口，要么给出真实实现，要么给出
> 明确的、需要整块新能力的说明。本文档记录当前状态与实现位置。

## 已实现（真实可用）

| 功能 | 前端 | 后端 | 说明 |
| --- | --- | --- | --- |
| 订单（增值服务订单） | `component/Admin/Payment/PaymentList.tsx`，`/admin/payment` | `service/vas.AdminListPayments`、`AdminDeletePayments` | 列表、状态过滤、批量删除；金额按「分」显示为 `¥x.xx` |
| 事件（审计日志） | `component/Admin/Event/EventList.tsx`，`/admin/event` | `service/admin/audit.go`（列表/详情/批量删除/清理） | 类型 + 用户过滤、排序、详情弹窗（原件内容 JSON）、批量删除、按天数 + 类型清理 |
| 滥用举报 | `component/Admin/Abuse/AbuseList.tsx`，`/admin/abuse` | `service/admin/abuse.go` | 状态过滤、分享链接 + 目录名、处理（已处理/已驳回 + 备注）、批量删除 |
| 事件记录开关 | `component/Admin/Settings/Event/Events.tsx`（参数设置 → 事件） | `pkg/setting/provider.go` `AuditEventEnabled` + 三处审计写入点 | 配置项 `audit_event_enabled`（JSON 事件名数组）；缺省 = 全开，显式空数组 = 全关 |
| 增值服务设置页（商品 / 支付渠道 / 兑换码） | `component/Admin/Settings/VAS/*`（参数设置 → 增值服务） | `service/vas`：`/admin/vas/skus`、`/admin/vas/providers`、`/admin/vas/giftcode` | SKU 增删改（容量包/会员/积分包）、支付渠道（易支付/自定义 + 动态配置键校验）、兑换码批量生成与删除 |
| 无需购买分享链接（`share_free`） | 用户组 → 分享，真实开关 | 权限位 8；`shop/service/trade.go` `hasShareFree`（分享门禁放行） | 打开后该组成员可直接打开所有付费分享 |
| 提升匿名用户权限（`escalate_anonymity`） | 用户组 → 分享，真实开关 | 权限位 13；`service/share/manage.go` 创建分享时钳制 | 关闭时匿名访客权限最多为只读，显式授权用户不受影响 |
| 存储策略转移（`migrate_policy`） | 用户组 → 文件，真实开关 | 权限位 10；`service/explorer/workflows.go` `CreateRelocateTask` | 成员可为自己创建迁移任务；分组绑定了存储策略时只能迁回该策略（防止绕过策略限制） |
| 打开用户文件 | `component/Admin/User/UserDialog/UserForm.tsx` | 复用 `POST /admin/file`（`conditions.owner`） | 直接跳转 `/admin/file?owner=<用户 hash id>` |

权限位布局（与 `inventory/types/types.go`、`assets/src/api/user.ts` 一致）：
`8 = share_free`、`10 = migrate_policy`、`13 = escalate_anonymity`。其余编号保持原样，
因此既有用户组的权限串不受影响。

## 仍需整块新能力（保留 Pro 标识）

以下入口不是「开关没接」而是真的缺少底层能力，短时间无法用胶水补齐，保留 Pro 标识：

1. **用户组可用存储策略「可多选」**：分组目前只能绑定一个存储策略（`groups.storage_policy_id`），
   多选需要策略白名单模型 + 选择/切换逻辑。
2. **可用节点 / 允许选择节点**：需要多节点任务路由（离线下载、压缩任务按组可用的节点集合分配 + 用户选择）。
3. **默认固定分享**：需要「用户组级默认置顶」模型 + 与用户置顶列表的合并/不可取消逻辑。
4. **文件夹直链**：需要按次/长期生成文件夹打包直链（现只支持文件直链）。
5. **回执邮件模板 / 超额提醒邮件模板**：需要新增模板默认值 + 支付履约时发回执、超额时发提醒的发送点。
6. **第三方登录 / SSO**：仓库只有 `logto_*` 配置键，没有 OIDC 流程实现。
7. **自定义属性「用户」类型**：需要新的属性类型（值 = 用户 hash id）及其校验/渲染。
8. **负载均衡存储策略**：需要把文件分散到多个策略的策略类型。

## 验证方式（本轮）

- `go build ./...`、`go test ./shop/...`、`npx tsc --noEmit`（126 → 125 条既有错误，无新增）、`yarn build`。
- API：`share_free` 打开后未购买用户可列目录（40083 → 200），关闭后恢复；`escalate_anonymity`
  关闭时分享权限落库为 `read`，打开后为 `upload`；`migrate_policy` 无权限 403、有权限可排队任务。
- 浏览器（headless Chrome + CDP）：订单/事件/滥用举报/增值服务/用户组/事件设置六个页面渲染、
  过滤、处理、保存均生效，控制台无异常。
