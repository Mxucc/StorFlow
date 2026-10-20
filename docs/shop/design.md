# 网盘小店（StorFlow Shop）设计与实施方案

> 目标：在 StorFlow v4 后端之上，以**低侵入、可独立演进**的方式加入「易支付收单 + 网盘小店」体系，并补齐 Pro 版
> 「分享与协作」相关能力（分享定价、默认分享、文件权限、访客写入）。

---

## 1. 总体设计原则

| 原则 | 落地方式 |
| --- | --- |
| 小店代码相对独立 | 全部新增代码收敛在 `shop/` 一个目录树内，自成 `model / store / service / gateway` 分层；**不复用也不修改** `ent` schema，商品、订单、余额等数据存放在自己的 `crshop_*` 表中 |
| 极少的核心交集 | 只新增 3 个接入点：① `routers/router.go` 注册路由；② `service/user` 注册流程的返利挂点；③ `application/dependency` 暴露底层 `*sql.DB`。其余全部通过 StorFlow 已有的公开接口（`inventory.*Client`、`manager.FileManager`、`hashid`、`serializer`）调用 |
| 复用网盘能力 | 资源预览 / 转存复用 `storflow://share/...` 文件系统与「符号文件夹（symbolic folder）」机制，不重复实现文件存取 |
| 资金正确性 | 金额一律 `int64` 最小单位（分）；余额变更与流水在**同一个数据库事务**内完成；支付回调幂等 |
| 未来可换实现 | `store` 层只依赖 `database/sql`，方言差异（占位符、索引）集中处理；`service` 不感知 SQL |

### 1.1 目录结构

```
shop/
  doc.go                 # 包说明
  app.go                 # App 装配：DB / 配置 / 服务 / 钩子
  router.go              # 路由注册（唯一由 routers 包调用的入口）
  model/                 # 纯数据类型：枚举、实体、JSON 负载、金额工具
  store/                 # 原生 SQL 数据访问 + 建表 DDL（sqlite / mysql / postgres）
  service/               # 业务用例：小店、商品、收银台、支付、余额、提现、优惠、返利
  gateway/epay/          # 易支付网关：签名、下单 URL、异步通知验签、查单
pkg/sharegate/           # 核心侧极小的「付费分享准入」插槽（避免核心依赖 shop）
docs/shop/design.md      # 本文档
```

---

## 2. 数据模型（表前缀 `crshop_`）

时间统一为 `BIGINT`（Unix 秒），金额统一为 `BIGINT`（分），主键统一为 `TEXT`（UUIDv4，应用侧生成，
避免不同数据库自增/`RETURNING` 差异）。

| 表 | 说明 | 关键列 |
| --- | --- | --- |
| `crshop_setting` | 模块配置（KV） | `k`(PK), `v`, `updated_at` |
| `crshop_shop` | 小店（与站点用户 1:1） | `uid`(uniq), `slug`(uniq), `name`, `description`, `avatar`, `cover`, `theme`(JSON), `status`, `referrer_rate`, `stats`(JSON) |
| `crshop_balance` | 小店独立余额账户 | `shop_id`(PK), `available`, `frozen`, `total_income`, `total_withdraw` |
| `crshop_ledger` | 余额流水（审计） | `shop_id`, `kind`, `amount`, `balance`, `ref_id`, `remark` |
| `crshop_product` | 商品＝一个文件夹一个价格 | `shop_id`, `file_id`(核心 file id), `share_id`, `share_password`, `name`, `price`, `discount`, `discount_end`, `status`, `sales`, `props`(JSON) |
| `crshop_order` | 订单（商品单 / 分享单） | `order_no`(uniq), `kind`, `shop_id`, `object_id`, `buyer_uid`, `amount`, `discount_amount`, `payable`, `paid`, `status`, `channel`, `trade_no`, `expire_at`, `referrer_uid` |
| `crshop_entitlement` | 已购权益（更新/转存凭据） | `uid`, `kind`, `object_id`, `order_id`, `folder_id`, `status`；唯一键 `(uid,kind,object_id)` |
| `crshop_withdrawal` | 提现申请 | `shop_id`, `amount`, `fee`, `actual`, `channel`, `account`(JSON), `status`, `operator` |
| `crshop_coupon` | 折扣/优惠券 | `shop_id`, `code`, `kind`(percent/fixed), `value`, `min_amount`, `product_id`, `max_uses`, `used`, `per_user`, `start_at`, `end_at` |
| `crshop_coupon_use` | 用券记录（限次/审计） | `coupon_id`, `uid`, `order_id` |
| `crshop_referral` | 邀请关系 + 累计返利 | `inviter_uid`, `invitee_uid`(uniq), `source`, `product_id`, `rate`, `total_rebate` |

设计要点：

- **商品只持有核心 `file_id`**，商品与网盘文件是同一条记录的两个视角，不做内容副本 → 卖家更新文件夹内容，
  买家通过符号文件夹**自动看到最新内容**，天然满足「购买后获得后续更新」。
- **权益（entitlement）与商品上架状态解耦**：卖家关闭销售只是把 `status` 置为 `offsale`，
  内部共享与权益记录都保留 → 已购用户仍然可以预览、转存、跟随更新。
- **余额与流水分离**：`crshop_balance` 是快照，`crshop_ledger` 是事实来源，可对账。

---

## 3. 关键业务流

### 3.1 开店与装修

1. `POST /api/v4/shop` 开通小店（校验开关、建 `crshop_shop` + `crshop_balance`，`slug` 唯一）。
2. `PATCH /api/v4/shop` 更新资料 / 装修：`theme` 为结构化 JSON（模板、主色、背景图、Banner、公告、模块顺序、
   页脚、外链），保存时做白名单 + 长度 + URL 协议校验，禁止 XSS 载荷进入前端渲染链路。
3. `POST /api/v4/shop/status` 开启 / 关闭小店（关闭后对外接口返回 404，已购权益不受影响）。

#### 3.1.1 三套店铺模版（`theme.template`）

模版**只影响前台骨架**，后端只做白名单校验。三套模版必须一眼可辨，差异集中在
`assets/src/component/Pages/Shop/ShopTemplates.ts` 的 `SHOP_TEMPLATES` 配置里：

| 维度 | `default` 标准店铺 | `minimal` 极简文字 | `magazine` 杂志头版 |
| --- | --- | --- | --- |
| Hero | 封面大图 300px + 暗色渐变 + 84px 头像 | **不显示封面图**；4px 主色顶线 + 56px 头像居中 | 封面 440px + 更强渐变 + 超大标题 |
| 店名 | `34px/700` 左对齐 | `24px/600` **居中** | `56px/800` 白色带阴影 |
| 容器 | `lg`（实测 1200px） | **`md`（900px）** | `lg`（1200px） |
| 商品网格 | `minmax(260px,1fr)`，间距 24 | `minmax(190px,1fr)`，间距 16 | `minmax(300px,1fr)` + **头条大卡跨 2 列** |
| 商品卡 | 圆角 36px + hover 抬升 | **直角 0px + 仅边框变色** | 圆角 36px + hover 抬升 |
| 公告 | `Alert` 卡片 | **居中纯文字 + 上下 hairline** | **主色通栏白字** |
| 统计 | Chip × 2 | **不显示** | 一行小字 |
| 关于 | 单列卡片 | 单列纯文本 | 左标题 + 右正文两栏 |

实现约定：前台根节点带 `data-shop-template` 与 `data-shop-hero` / `data-shop-grid` /
`data-shop-notice` / `data-shop-stats` / `data-shop-headline` 标记，便于回归时做客观量测；
未知或空 `template` 一律回落到 `default`。装修页（`/shop/manage`）用三张带说明文案的
radio 卡切换，并给当前模版加「当前」徽标。

### 3.2 上架商品（文件夹 → 商品）

1. 卖家选择自己网盘中的文件夹 `uri`。
2. 服务端用卖家身份 `manager.Get` 校验：存在、是文件夹、属于本人、非根目录。
3. 生成一个**内部共享**（`share.Props.ShareView=true`，随机密码，`Internal=true`），
   它是小店访问该文件夹的唯一凭据；密码只存库，永不出现在任何对外响应中。
4. 保存商品 `file_id / share_id / share_password / price / props`。

### 3.3 访客预览（可看不可拿）

`GET /api/v4/shop/{slug}/product/{id}/preview?path=<token>`

- 以「访客身份 + 内部共享 URI」走 share 文件系统列举目录，**只返回元数据**
  （名称、大小、类型、修改时间、子项数、相对路径 token）。
- 响应中**不含** `file://` 实体下载地址、`direct link`、共享 ID 或共享密码；`path` 为不透明 token
  （base64，仅避免直接暴露路径字符串；真正的访问控制来自**隐藏的内部共享凭据**，它从不下发）。
- 因此访客可以浏览文件夹结构与目录树，但无法取得任何下载 / 转存入口。

### 3.4 下单与支付（易支付）

```
POST /api/v4/shop/order            （需登录；未登录返回 401 → 前端引导注册，注册时携带 ref）
  → 校验商品在售 / 未购买过；计算促销折扣与优惠券；生成 pending 订单
  → 返回 { order_no, payable, pay_url }
GET  /api/v4/shop/epay/redirect?... → 302 跳转易支付收银台
POST /api/v4/shop/epay/notify      （易支付异步通知，无登录态，凭 MD5 签名）
```

- 签名：`易支付 v1` 规范 —— 参数按键名 ASCII 升序拼接 `k=v&...`（剔除 `sign`、`sign_type` 与空值），
  末尾追加商户密钥后取 `MD5` 小写。
- 幂等：`notify` 先校验签名与金额，再在事务内 `UPDATE ... WHERE status='pending'`，
  已支付订单直接回 `success`；`trade_no` 落库备查。
- 金额校验：易支付回传 `money` 为元（两位小数），统一换算为分后与 `payable` 严格比对，不一致拒绝入账。

### 3.5 交付：转存 + 更新

支付成功后（事务外、可重试的副作用）：

1. 在买家网盘根目录创建**符号文件夹**：
   `name = 商品名`，元数据 `sys:shared_redirect = storflow://share/<id>:<password>`、
   `sys:shared_owner = <卖家 hashid>`。
2. 符号文件夹永久指向卖家文件夹 → 卖家后续新增/修改的内容，买家**实时可见**；
   即使卖家将商品下架（`offsale`），内部共享与符号文件夹都不删除，已购用户不受影响。
3. 失败时权益记录标记 `pending`，买家可在「我的购买」中一键重试（`POST /shop/purchase/{id}/transfer`）。

> **实现说明（交付方式＝链接文件夹）**：实际落地时，交付物不是「共享快捷方式（shared_with_me）」而是
> **链接文件夹**：在买家网盘根目录下创建一个**符号文件夹**，它只保存共享地址
> （元数据 `sys:shared_redirect` / `sys:shared_owner`），不复制任何数据。
>
> 为此在核心文件系统补了一处**只读**的链接解析（`pkg/filemanager/fs/dbfs/dbfs.go`
> 的 `linkedFolderTarget`，仅作用于 `DBFS.Get` 与 `DBFS.List`）：当路径落在符号文件夹内时，
> 先把它翻译成共享地址再读取。于是：
>
> - 买家在自己的网盘里能像普通文件夹一样**浏览/下载**已购资源（Web UI、WebDAV 均可用）；
> - 卖家后续新增/修改内容，买家**立即可见**，无需重新转存；
> - 写入路径仍被 `ErrSymbolicFolderFound` 拦住 —— 链接文件夹对买家是**只读**的，不会误改卖家文件。
>
> 另修复了上游 `shared_with_me` 导航器不能打开单条快捷方式的问题
> （`sharewithme_navigator.go`），使「保存分享到我的网盘」(`POST /share/redeem`) 也能在 Web UI 中打开。

### 3.6 余额、提现与返利

- 订单支付成功：`卖家可用余额 += payable - 手续费`；写 `sale` 流水。
- 手续费：`shop_fee_percent`（万分比），四舍五入到分。
- 返利：买家存在邀请人时，`邀请人可用余额 += payable * rate`，写 `referral` 流水；
  邀请关系在**注册时**由 `ref` 参数建立（`POST /user` 携带 `ref=<卖家用户 hashid>`）。
- 提现：`POST /shop/withdraw` → 冻结余额（`available → frozen`），管理员审批：
  - `reject` → 解冻回可用；
  - `approve/paid` → 扣减冻结、累计提现、写 `withdraw_settle` 流水。
  提现只记账，实际打款由管理员线下完成（也可后续接入易支付代付 / 转账接口）。

### 3.7 折扣体系

三层，可叠加、按顺序计算：

1. **商品促销**：`product.discount`（百分比）+ `discount_end` 到期自动失效。
2. **优惠券**：`percent` / `fixed`，支持全场券与单品券、最低消费、总次数、每人次数、有效期。
3. **结算兜底**：应付金额向下取整到分，且保证 `>= 1` 分（易支付不接受 0 元单）。

---

## 4. 接口清单（前缀 `/api/v4/shop`）

### 4.1 卖家 / 店主

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/` | 开通小店 |
| `GET` | `/mine` | 我（或指定 slug）的小店信息 |
| `PATCH` | `/` | 更新资料 / 装修 |
| `POST` | `/status` | 开关小店 |
| `GET` | `/product` | 我的商品列表 |
| `POST` | `/product` | 上架商品（绑定文件夹） |
| `PATCH` | `/product/{id}` | 修改价格 / 促销 / 文案 |
| `POST` | `/product/{id}/status` | 上架 / 下架 / 删除 |
| `GET` | `/order` | 我店的订单 |
| `GET` | `/balance` | 余额 + 流水 |
| `POST` | `/withdraw` | 申请提现 |
| `GET` | `/withdraw` | 提现记录 |
| `GET/POST` | `/coupon` | 优惠券管理 |
| `GET` | `/referral` | 邀请与返利明细 |

### 4.2 公开（访客）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/{slug}` | 小店主页（装修 + 商品列表） |
| `GET` | `/{slug}/product/{id}` | 商品详情（价格、描述、已购状态） |
| `GET` | `/{slug}/product/{id}/preview` | 文件夹预览（只列元数据） |

### 4.3 买家（需登录）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/order` | 创建订单（返回支付地址） |
| `GET` | `/order/{orderNo}` | 订单状态（前端轮询） |
| `GET` | `/purchase` | 我的购买（权益列表） |
| `POST` | `/purchase/{id}/transfer` | 重新转存 |

### 4.4 支付与运维

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/epay/redirect` | 跳转易支付 |
| `POST/GET` | `/epay/notify` | 异步通知（验签入账） |
| `GET` | `/epay/return` | 同步跳回（仅提示，不改状态） |
| `GET/PATCH` | `/admin/setting` | 管理端配置（易支付参数、手续费、开关） |
| `POST` | `/admin/withdraw/{id}` | 提现审批 |

---

## 5. Pro 能力映射

| Pro 功能 | 实现方式 | 状态 |
| --- | --- | --- |
| 为分享链接定价 | `types.ShareProps.PayEnabled / Price / PayCurrency`；`pkg/sharegate` 在 share 导航器 `Root()` 处拦截，付费后由小店订单（`kind=share`）写权益 | ✅ 已实现并端到端验证 |
| 默认分享 | `types.ShareProps.IsDefault`；`GET /api/v4/share/default` 公开默认分享列表 | ✅ 已实现 |
| 为文件设置权限（用户 / 用户组 / 匿名） | `ShareProps.Permission`（read/upload/edit，作用于所有人含匿名）+ `ShareProps.Grants`（按用户覆盖）；`dbfs` 分享能力集 + `shareWriteContext` 放行 | ✅ 已实现（分享粒度；文件夹级 ACL 见 §7） |
| 通过分享链接修改、删除文件 | 同上，`edit` 权限开放 rename/delete/softDelete/updateMetadata/upload | ✅ 已实现 |
| 匿名用户上传、修改 | 分享 `Permission=upload/edit` 对匿名访问者同样生效 | ✅ 已实现 |

> 与 Pro 的差距：Pro 的「文件权限」是**文件夹级 ACL**（可对任意用户组/用户授予某个文件夹的权限，
> 与分享无关）。本期实现的是**分享级**权限（覆盖「通过分享链接修改删除」「匿名上传」两条 Pro 功能），
> 文件夹级 ACL 仍按 §7 的规划分阶段落地。

---

## 6. 安全与正确性检查表

- [x] 金额只用整数分运算；任何用户输入的价格有上限（`≤ 100 万元`）与下限（`≥ 1 分`）。
- [x] 订单号服务端生成（时间前缀 + 随机），不可枚举。
- [x] 回调验签 + 金额比对 + 状态机幂等；重复通知不重复入账。
- [x] 余额变更全部走 `store` 内的事务方法，同时写流水；禁止服务层直接改余额。
- [x] 商品预览不泄露共享凭据 / 实体地址。
- [x] 所有店铺类接口校验 `shop.uid == 当前用户`；公开接口只读且在售商品。
- [x] 管理员接口复用 StorFlow 的 `middleware.IsAdmin` + `ScopeAdminWrite`。
- [x] 提现冻结先行，审批失败回滚，避免超提。

## 7. 后续阶段（已预留）

1. **文件权限（ACL）**：新增 `file_permission(id, file_id, subject_type, subject_id, allow_bits, deny_bits, inherit)`
   + `dbfs` 单一入口 `can(ctx, file, perm)`，把现有散落的 `OwnerID != user.ID` 判定收敛进来。
2. **分享可写 / 匿名写**：`ShareProps.AllowWrite`、`AllowAnonymousUpload`，配合 ACL 落地。
3. **易支付代付**：提现自动打款（`transfer` 接口）+ 对账任务。
4. **小店运营**：销量榜、优惠券领取中心、推广短链、数据看板。
5. **前端**：小店装修渲染器、收银台、我的购买（前端仓库为 `assets` 子模块，另行实施）。
6. **离线同步**：把链接文件夹的内容同步为买家网盘中的真实副本（需要后台任务 + 差异比对），
   当前交付方式是零拷贝的链接文件夹，已满足「持续更新」。

## 8. 配置项（`crshop_setting`，可由 `PATCH /shop/admin/setting` 修改）

| Key | 默认 | 说明 |
| --- | --- | --- |
| `enabled` | `true` | 小店总开关 |
| `epay_api_url` | `https://pay.example.com/` | 易支付网关地址 |
| `epay_pid` | `""` | 商户 ID |
| `epay_key` | `""` | 商户密钥 |
| `epay_sign_type` | `MD5` | 签名类型 |
| `epay_channels` | `alipay,wxpay,qqpay` | 可用支付通道 |
| `fee_percent` | `300` | 平台手续费（万分比，300 = 3%） |
| `referral_percent` | `1000` | 默认返利比例（万分比，1000 = 10%） |
| `min_withdraw` | `1000` | 最低提现（分） |
| `order_ttl` | `900` | 订单有效期（秒） |
| `site_url` | `""` | 回调基地址（空则取 StorFlow 站点地址） |
| `deposit_amount` | `0` | 开店保证金（分），`0` = 不要求 |
| `settle_delay_hours` | `24` | 平台渠道收入的解冻时长（小时），`0` = 立即到账 |

## 9. 对 ZXFile（zixiang0520/ZXFile）的评估：不整体合并，只定点移植

### 9.1 它是什么

`ZXFile` = StorFlow **4.14.3** + 一批自研功能（约 43k 行，其中 37k 是 ent 生成代码）：

| 模块 | 内容 |
| --- | --- |
| `service/vas/*` | 增值服务：SKU（积分 / 容量 / 用户组）、易支付 / 自定义网关 / Stripe 渠道、积分、充值卡、会员到期回退 |
| `service/{admin,explorer,share,user}/audit.go` + `abuse.go` | 审计日志、举报 |
| `service/admin/filestats.go` | 后台文件统计 |
| `pkg/filemanager/workflows/relocate.go` | 跨存储策略迁移任务 |
| `ent` | 新增 `payment`、`gift_code` 等实体，`user` 新增积分/容量字段 |
| `ShareProps.Permission/Grants` | 分享协作权限（read/upload/edit + 按用户授权），配合 `dbfs.shareWriteContext` 放开写操作 |

### 9.2 为什么不整体合并

1. **目标不同**：ZXFile 的 `vas` 是"平台卖积分/容量/会员"，没有小店、店主独立余额、提现、商品（文件夹定价）、已购权益与更新跟踪。我们要的东西它没有。
2. **合并成本高于收益**：它的 ent 生成代码是 4.14.3 基线产物，我们的 ent 是 4.14.0 基线；整树替换会引入 37k 行不可审阅的生成差异，并且 `user` 表加列属于破坏性 schema 变更（需要 `entc` 重新生成 + 迁移验证）。
3. **前端错位**：它前端是自建 fork（含 VAS 页面、发现页等）。我们的 `assets` 是上游 4.14.0 子模块，直接合并后端会带来"接口存在但页面不存在"和"页面期待字段但接口没有"的双向不一致。
4. **版本号**：ZXFile 为 `4.14.3`，本仓库为 `4.14.0`（不是我们更新，而是它多打了几个 patch）。反向回移会把它的其他小改动一起带进来。

### 9.3 我们实际采用的部分（逐条对照，均为小范围精确移植）

| 来源 | 采用方式 | 落点 |
| --- | --- | --- |
| `ShareProps.Permission/Grants` + `SharePermission` 枚举 | 移植（含 `types.NormalizeSharePermission`） | `inventory/types/types.go` |
| `dbfs` 分享能力集（read/upload/edit）与 owner 校验放行 | 移植并抽出 `sharegate`，避免核心依赖 shop | `pkg/filemanager/fs/dbfs/{share_navigator,manage,upload,lock}.go`、`pkg/filemanager/fs/dbfs/dbfs.go` |
| 易支付 `MD5(ASCII 排序 + key)` 签名、`/submit.php` 收银台、`notify` 验签后幂等入账 | 作为协议参照，在我们自己的 `shop/gateway/epay` 里实现（不引入它的 `ent.Payment`） | `shop/gateway/epay` |
| SKU / 积分 / 充值卡 / 会员到期 | 不采用（与"小店"无关；若要"平台自营增值服务"，可直接把我们 `shop` 的 order 扩展商品类型，无需第二套支付表） | — |

### 9.4 结论

**自行实现小店主体 + 定点移植 ZXFile 的分享协作与易支付协议**。ZXFile 的价值集中在"分享权限 + 支付通道协议"两点，
这两点我们以可测试的方式吸收；其余部分属于另一条产品线，合并进来只会增加维护面积。

### 9.5 若坚持整体合并，需要额外完成的前置工作（成本参考）

1. `ent` 全量重新生成并验证 `user` 加列迁移（含 SQLite/MySQL/Postgres 三方言）。
2. 前端 `assets` 子模块切换到对应 fork 版本，或为新接口补页面。
3. 引入其审计/举报/统计后，需补对应后台权限点与前端入口。
4. 与本期 `crshop_*` 表的两套支付/订单体系做归属划分（否则会出现两套"我的订单"）。

## 10. 实施记录（本次交付）

### 10.1 新增：网盘小店（`shop/`）

```
shop/
  app.go           模块装配：建表、加载配置、注册 sharegate/shoplink 钩子、后台 janitor
  router.go        路由注册（鉴权中间件由 routers 包注入，避免依赖环）
  controller.go    HTTP 入口
  model/           枚举、金额（分）、Theme/Props/Account 校验、实体
  store/           原生 SQL：11 张 crshop_* 表 + 多方言 DDL + 事务化余额/订单/权益
  service/         小店、商品、预览、收银台、支付回调、交付、余额、提现、优惠券、返利、后台
  gateway/epay/    易支付协议（签名/收银台 URL/通知验签/查单）
```

接口前缀 `/api/v4/shop`：公开 `store/:slug`、`store/:slug/product/:id`、`store/:slug/product/:id/preview`、
`epay/notify`；登录态 `POST /`、`GET|PATCH /`、`POST /status`、`product*`、`order*`、`sold`、`purchase*`、
`balance`、`withdraw*`、`coupon*`、`referral*`；管理员 `admin/setting`、`admin/withdraw*`、`admin/shop/:id/stats`。

### 10.2 新增：核心侧最小插槽

| 文件 | 作用 |
| --- | --- |
| `pkg/sharegate/sharegate.go` | 付费分享准入插槽 + 分享权限解析（核心不依赖 shop） |
| `pkg/shoplink/shoplink.go` | 注册返利钩子（`service/user` 不依赖 shop） |
| `application/dependency/dependency.go` | `RawDB()`：把底层连接池交给自建表模块 |
| `inventory/client.go` | `NewRawEntClientWithDB()`：暴露 `*sql.DB` 与方言 |
| `pkg/filemanager/fs/dbfs/*` | 分享能力集、`shareWriteContext`、链接文件夹只读解析 |
| `service/share/{manage,visit,default,shortcut}.go` | 分享权限/价格/默认分享/保存快捷方式 |
| `service/explorer/response.go` | 分享响应新增 `pay_enabled/price/purchased/is_default/permission` |
| `application/application.go` | master 启动时初始化小店模块 |
| `routers/router.go` | 注册小店路由、默认分享路由 |

### 10.3 同时移植：ZXFile 增值/审计/举报/统计/迁移

按 §9 的结论，从 ZXFile 移植了：审计日志（`service/*/audit.go`、`pkg/eventtype`）、举报
（`service/{admin,share}/abuse.go`）、后台文件统计（`service/admin/filestats.go`）、跨存储策略迁移
（`pkg/filemanager/workflows/relocate.go`）、增值服务 VAS（`service/vas/*`、`routers/controllers/vas.go`、
`ent/schema/{payment,giftcode,pointsledger,auditlog,abusereport}.go` 及重新生成的 ent 代码），
以及对应的路由、设置项（`siteAnnouncement`、`cron_group_expire`、`vas_skus`、`vas_providers`）与
scope（`ScopeVASRead/Write`）。未移植 `yun139` 驱动（与本次需求无关）。

### 10.3.1 支付健壮性与资金正确性补充（本轮）

> 另见 §11.4：退款流程、删除保护（含付费分享）、程序内限流。

| 能力 | 说明 |
| --- | --- |
| 重复下单保护 | 同一买家对同一对象存在未过期 `pending` 订单时**复用**该订单，避免「点两次买两份」 |
| 迟到支付 | 订单已 `closed` 后网关仍收到款时**照常入账并交付**（买家不能付了钱拿不到东西）；已 `refunded` 的订单不会被重复入账 |
| 回调丢失补偿 | 模块后台任务每 5 分钟对「创建超过 3 分钟且未过期」的待支付订单调用易支付查单接口（`api.php?act=order`），金额一致即补单；管理员也可用 `POST /shop/admin/reconcile` 立即触发 |
| 对账 | 每 5 分钟校验 `available + frozen == SUM(crshop_ledger.amount)`，不一致打 `Error` 日志（只告警不改数） |
| 退款 | 订单 → `refunded`、权益 → `revoked`（门禁立即生效，买家再访问得到 `40083`）、卖家余额扣回净收入（可为负＝待追缴）、邀请返利同步扣回、买家链接文件夹重命名为「…(已退款)」；二次退款 `409` |
| 卖家侧订单 | `GET /shop/sold` 返回买家**昵称**（便于售后），不下发买家邮箱 |
| 小店列表（后台） | `GET /shop/admin/shop` 返回各店钱包与销量概览 |

### 10.4 验证（真实执行）

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `gofmt -l`（改动目录） | 无输出 |
| `go test ./shop/... ./pkg/filemanager/... ./service/... ./inventory/...` | 通过（含新增 `shop/model`、`shop/store`、`shop/gateway/epay` 用例） |
| `pkg/{cache,conf,request}` 单测 | **构建失败，属仓库既有问题**（测试文件停留在旧 API，本次未触碰） |
| 真实进程端到端（SQLite + 真实 HTTP） | 开店 → 上架文件夹 → 匿名预览 → 注册带 `ref` → 下单 → 易支付通知 → 余额入账(3% 手续费) → 权益授予 → 买家网盘出现链接文件夹 → 卖家新增内容买家立即可见 → 下架后买家仍可用 → 文件下载签名 URL → 第三方/匿名仍被拦(40083) → 提现冻结/审批/打款/重复审批拒绝 → 邀请返利入账 |


## 11. 安全模型与攻击面验证

### 11.1 角色与信任边界

| 角色 | 能力 |
| --- | --- |
| 匿名访客 | 只能访问公开小店页、商品详情、**元数据预览**；无任何下载/转存入口 |
| 已注册用户（未购买） | 同上；不能读取任何付费内容 |
| 买家（已购买） | 可读/下载已购资源，且**持续跟随卖家更新** |
| 卖家 | 只能操作自己的小店、商品、余额、提现；不能读别人的资源 |
| 管理员 | 小店配置、提现审批、查看统计；**看不到易支付密钥明文**（接口返回 `********`） |

### 11.2 关键设计：内容凭据不出网 + 付费分享门禁

1. **内部共享**：每个商品对应一个内部共享（随机 16 位密码），只存在于服务端与买家网盘元数据中，
   从不通过小店 API 下发。
2. **内部共享同样被标记为付费分享**（`PayEnabled=true`，价格＝商品价）。这一点是防「偷文件」的核心：
   买家能从自己网盘元数据里读到该地址（这是「链接文件夹」机制所必需的），
   因此**必须假定地址会泄露**。标记为付费后，任何人拿到地址都还需要一份权益：
   - 分享级权益：`kind=share` + 分享 hashid（通过 `/s/` 链接购买的情况）；
   - 商品级权益：`kind=product` + 商品 ID（通过小店购买的情况，门禁会由 share_id 反查商品）。
3. **预览豁免是有作用域的**：小店预览端点在单次请求上下文里标记「允许预览该 share_id」，
   门禁仅对该 ID 放行，且该上下文不会被下载类接口复用。
4. **地址解析不写日志**：链接解析日志只记录「已解析」，不落库分享密码。

### 11.3 已实际执行的攻击尝试（真实进程 + SQLite，逐条验证）

| 攻击 | 结果 |
| --- | --- |
| 匿名/非买家直接列出「泄露的内部共享地址」 | `40083 You need to purchased this share` |
| 非买家对泄露地址取 `file/url`（下载签名） | `40081`（内层 40083） |
| 非买家取缩略图 `file/thumb` | `40083` |
| 无签名 / 篡改金额的易支付通知 | `fail`（拒绝，不入账） |
| 重复通知 | `success` 但**不重复入账**（余额与流水仅一次） |
| 预览路径穿越（`..`、`../..`、`a/../../..`、`..%2F..`、`%2e%2e`、`/etc`、兄弟目录名） | 全部失败（`40001 Invalid path` / `40016 Folder is not available`） |
| 攻击者自建符号文件夹指向付费分享 | 被门禁拦截（`40083`/`403`） |
| 读取他人订单 / 购买 / 余额 / 收款订单 / 后台配置 | `404` / 空数组 / `403` |
| 卖家手动删除内部共享（会砸掉所有买家） | 拒绝（`This link belongs to a shop product…`），且该共享不出现在分享管理列表 |
| 用同一张「每人限一次」优惠券连开多单 | 第二单在建单时即 `409 Coupon already used`（优惠券在**建单时预占**，订单过期自动释放） |
| 全局限次优惠券被多人抢用 | 预占在事务内完成，超限即 `409` |
| 商品下架后非买家访问详情/预览 | `404`（卖家与已购买家仍可访问） |
| 提现重复审批 / 二次打款 | `409`（状态机条件更新，单次生效） |
| 未登录调用卖家/买家/后台接口 | `401` / `403` |

### 11.4 运营层事项（已按确认结论落地）

| 事项 | 结论与实现 |
| --- | --- |
| 退款流程 | ✅ 已实现：买家 `POST /shop/purchase/:id/refund` 申请（同一订单一次、默认 7 天窗口可配），卖家/管理员 `POST /shop/refund/:id` 审批；通过后订单 `refunded`、权益 `revoked`（门禁立即生效）、扣回卖家净收入与邀请返利、买家文件夹重命名为「…(已退款)」。实际打款仍需线下（易支付协议无退款接口） |
| 已售内容被删除 | ✅ 已实现删除保护（`pkg/fileguard` + `dbfs` 软删/硬删两条路径），对**商品文件夹**与**已售付费分享文件夹**同时生效：① 已售文件夹本身、② 售前已存在的内容、③ 售出后超过 **24 小时**的新增内容、④ 包含已售内容的上级文件夹，均**不可删除**；仅 24 小时内新增的内容可撤回。买家因此始终拿得到已购内容 |
| 公开端点限流 | ✅ 已实现**程序内**限流（`shop/ratelimit.go`，按 IP 令牌桶）：小店公开接口与支付回调分别限流，`rate_limit_per_minute` 可配（0 关闭），超限返回 HTTP 429 + 统一错误体（带 `Retry-After: 60`）；支付回调保底 120 次/分钟，避免网关重试被误杀。注意：调高限额后令牌桶按新速率回填，需要数秒才完全恢复 |
| 易支付密钥 | 明文存于 `crshop_settings`（接口脱敏），按确认结论保持不变 |
| 交付方式 | 零拷贝**链接文件夹**（已确认接受），买家可见/可下载/随卖家更新 |
| 对账 | ✅ 每 5 分钟校验 `available + frozen == SUM(ledger.amount)`，只告警不改数 |

### 11.5 其它运营注意事项（非代码缺陷）

1. **退款**：已实现 `POST /api/v4/shop/admin/order/:orderNo/refund`（管理员）：订单转 `refunded`、
   买家权益转 `revoked`（门禁立刻生效，买家再访问得到 `40083`）、卖家余额扣回净收入（余额可为负，代表待追缴）、
   邀请返利同步扣回、买家链接文件夹重命名为「…(已退款)」。**实际打款仍需线下完成**（易支付协议无退款接口）。
2. **卖家删除已售文件夹**：会使其分享失效，买家链接文件夹随之不可读。当前已阻止「有销量的商品被删除」，
   但无法阻止卖家直接删除网盘里的源文件夹；建议后续在删除路径上加钩子（存在有效权益时提示/拒绝）。
3. **限流**：预览与支付回调是公开端点。建议在反向代理层对 `/api/v4/shop/store/*` 与 `/api/v4/shop/epay/notify`
   做速率限制（StorFlow 本身对公开分享接口也没有内置限流）。预览的浏览计数已改为只在根目录 +1，避免写放大。
4. **密钥存储**：易支付商户密钥以明文存于 `crshop_setting`（接口已脱敏）。建议数据库访问权限收敛，
   或后续接入主密钥加密（可复用 `pkg/credmanager`）。
5. **交付方式**：目前是零拷贝的**链接文件夹**（买家可见、可下载、随卖家更新），不是真实副本快照。
   若产品上必须提供「独立副本」，需要额外的后台同步任务（见 §7.6）。
6. **对账**：已内置——模块后台任务每 5 分钟校验每个小店 `available + frozen + pending == SUM(crshop_ledger.amount)`，
   不一致时打 `Error` 日志（只告警不静默改数）。

---

## 12. 支付方式、开店保证金与货款解冻（本轮新增）

### 12.1 两种支付方式（店铺级）

店主可在控制台「支付方式」中选择买家付款走哪条通道（`crshop_shop.pay_mode`）：

| 模式 | 含义 | 资金与责任 |
| --- | --- | --- |
| `platform`（默认，平台聚合支付） | 买家付到平台自营的易支付通道 | 平台代收：收入先进入钱包的**待解冻**桶，解冻后可提现；平台收手续费、发邀请返利、负责退款扣回 |
| `custom`（自配支付渠道） | 店主填自己的易支付网关地址 / 商户 ID / 密钥（`crshop_shop.epay_api_url/pid/key`） | 买家直接付给店主：平台**只记录订单并交付内容**，不计余额、不收手续费、不发返利；退款由店主线下完成、平台核验凭证 |

要点：

- 下单时把模式**快照**到订单（`crshop_order.pay_mode`），之后店铺改配置不会改变已存在订单的验签与结算方式；
- 自配渠道的异步回调仍然打到平台 `/shop/epay/notify`，平台按订单快照用**店主自己的密钥**验签，
  然后记录订单、发放权益、交付内容（`settle()` 的 `custom` 分支不碰钱包、不写流水）；
- 查单补单（`ReconcilePendingOrders`）同样按订单解析网关，因此自配渠道的丢单也能补；
- 自配渠道的网关地址在保存时做 SSRF 校验（仅 `http(s)` 且解析到公网地址），避免把后台查单变成内网探测；
- 商户密钥明文存于 `crshop_shop.epay_key`，接口只回显「是否已配置」；提交 `********` 表示保持不变，提交空串表示清除。
- 已知边界：自配渠道的订单用**店铺当前**的自配凭证验签，因此店主在把渠道切回平台（或清空密钥）之前，
  应先把在途的自配渠道订单处理完（支付或过期），否则这些待支付订单无法入账；

### 12.2 货款解冻（订单完成 + N 小时）

钱包从「可用 / 冻结」扩展为「可用 / 冻结 / **待解冻**」（`crshop_balance.pending`）：

- 平台渠道订单支付成功后，卖家净收入进入 `pending`（流水 `sale_pending`，金额计入对账总额）；
- 到达 `paid_at + settle_delay_hours` 后由模块后台任务（5 分钟一次）解冻：`pending → available`，
  写一条**零金额**流水 `sale_release`（只换桶，不动总额）；
- 解冻时长由管理员配置（`settle_delay_hours`，默认 24，`0` = 立即到账）；在途资金的冻结截止时间在**支付时快照**
  （`crshop_order.settle_at/settle_state`），事后改配置不影响已在途的订单；
- 提现仍然要求 `available ≥ min_withdraw`，因此「解冻」是提现的前置条件之一；
- 对账不变量升级为 `available + frozen + pending == SUM(crshop_ledger.amount)`；
- 退款时若该订单收入仍在 `pending`，按流水里的实际金额从 `pending` 扣回，不会把可用余额打成负数。

### 12.3 开店保证金

- 管理员设置 `deposit_amount`（分，`0` = 不要求）；`crshop_shop.deposit_paid` 记录该店已缴金额，
  差额即待补交（接口字段 `deposit_due`）；
- 缴纳走平台易支付：`POST /shop/deposit` 生成一笔 `kind=deposit` 订单（金额 = 差额，**始终走平台通道**），
  支付成功后 `settleDeposit()` 累加 `deposit_paid`；
- 保证金**不进入钱包、不写流水**（平台保管的押金不是可提现余额），因此不参与对账总额；
- 开店后才设置（或提高）保证金时，已开店的店主会在控制台看到「补交保证金」提示；
  未缴清期间 `POST /shop/withdraw` 直接拒绝，其余功能不受影响（产品确认过的宽松策略）。

### 12.4 自配渠道的退款：商家退款 + 平台核验

`crshop_refund` 增加 `evidence / evidence_at / verifier / verified_at`，状态机扩展为：

```
pending --审批--> approved --店主提交凭证--> refunded --平台核验--> confirmed
                                              refunded --平台退回--> approved
```

- 平台渠道订单：审批通过即终态（`approved`），行为与之前一致（权益撤销、余额扣回、返利扣回）；
- 自配渠道订单：审批通过只撤销权益、订单转 `refunded`、**不动钱包**（钱本来就没进平台），
  店主线下退款后提交凭证（4–500 字），管理员 `confirm` 核验或 `return` 退回重交；
- 权限：`evidence` 仅店主本人，`confirm` / `return` 仅管理员。

### 12.5 数据迁移

`Migrate()` 在 `CREATE TABLE IF NOT EXISTS` 之后逐列 `ALTER TABLE ... ADD COLUMN`（幂等，忽略「列已存在」），
老库升级会就地补齐：

| 表 | 新增列 |
| --- | --- |
| `crshop_shop` | `pay_mode`, `epay_api_url`, `epay_pid`, `epay_key`, `deposit_paid`, `deposit_paid_at` |
| `crshop_balance` | `pending` |
| `crshop_order` | `pay_mode`, `settle_at`, `settle_state` |
| `crshop_refund` | `evidence`, `evidence_at`, `verifier`, `verified_at` |

（`evidence` 用 `VARCHAR(512)`：MySQL 不允许 `TEXT` 列带默认值。）

### 12.6 本轮验证

| 检查 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `gofmt -l shop/` | 无输出 |
| `go test ./shop/...` | 通过（新增用例：待解冻生命周期与到期解冻、退款扣回待解冻、保证金不入钱包、老库 ALTER 迁移、退款凭证状态机、网关解析与保证金差额） |
| 前端 `npx tsc --noEmit` | 本次改动的文件无错误（仓库其它历史错误与本次无关） |
| 前端 `npx eslint src/api/shop.ts src/component/Pages/Shop src/component/Admin/Shop` | 无错误 |
| 前端 `npx vite build` | 成功 |

> 未执行：真实进程 + HTTP 的端到端回归（本轮以单测 + 构建 + 前端打包验证；接口清单见 `docs/shop/frontend-api.md` §3.2）。
