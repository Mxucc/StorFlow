# 网盘小店 前端对接契约（Frontend API contract）

> 后端实现见 `shop/`，设计与安全模型见 `docs/shop/design.md`。
> 本文档给前端（`assets/` 子模块）实现用：接口、字段、错误码、页面与流程。

---

## 0. 通用约定

- 所有接口前缀 `/api/v4`，与现有前端一致（`src/api/request.ts` 的 `ApiPrefix`）。
- 响应信封：`{ code: number, msg: string, data: T }`；`code === 0` 表示成功。
- 鉴权：需要登录的接口带 `Authorization: Bearer <access_token>`；未登录返回 `code: 401`。
- 金额：**全部为整数「分」**（`price: 5000` = 50.00 元）。前端展示时除以 100，保留两位小数。
- 时间：Unix 秒（`created_at` / `expire_at` / `paid_at` 等）。
- 关键错误码：

| code | 含义 | 前端处理 |
| --- | --- | --- |
| `0` | 成功 | — |
| `401` | 未登录 | 跳转 `/session`，并把 `redirect` 带上（同时带 `ref`，见 §4.1） |
| `403` | 无权限（例如 `403` 的 `Symbolic folder cannot be walked into`） | 提示 |
| `404` | 不存在 / 已下架 | 空状态 |
| `409` | 冲突（重复购买、优惠券已用完、提现已处理） | 提示 `msg` |
| `40083` | **需要购买该分享**（`You need to purchased this share`） | 弹「购买此分享」对话框（§5） |

---

## 1. 公开（无需登录）

### 1.1 小店主页

`GET /shop/store/:slug`

```jsonc
{
  "slug": "seller-shop",
  "name": "Seller Shop",
  "description": "",
  "avatar": "",
  "cover": "",
  "theme": {
    "template": "default|minimal|magazine",
    "primary_color": "#377dff",
    "background_color": "",
    "background_image": "",
    "banner": "",
    "logo": "",
    "slogan": "",
    "notice": "",
    "layout": ["banner", "products", "about", "notice", "links", "contact"],
    "links": [{ "label": "Docs", "url": "https://…" }],
    "footer": ""
  },
  "url": "https://site/shop/seller-shop",
  "owner": { "id": "X5UN", "nickname": "seller", "avatar": "" },
  "products": [ProductSummary]
}
```

`ProductSummary`：

```jsonc
{
  "id": "uuid",
  "name": "Season Bundle",
  "summary": "best bundle",      // props.summary
  "tags": ["video"],             // props.tags
  "cover": "",
  "price": 5000,                 // 原价（分）
  "final_price": 4500,           // 促销后价格（分），有促销时小于 price
  "discount": 10,                // 促销百分比，0 表示无
  "discount_end": 0,             // Unix 秒，0 表示不限时
  "sales": 3,
  "views": 42,
  "status": "onsale|offsale|draft|removed",
  "created_at": 1792263392,
  "previewable": true,
  "owned": false                 // 当前登录用户是否已购买
}
```

### 1.2 商品详情

`GET /shop/store/:slug/product/:id`

```jsonc
{ ...ProductSummary, "description": "…", "can_preview": true }
```

- 未上架（`offsale`）的商品对**非卖家、非买家**返回 `404`。
- `can_preview=false` 表示不允许预览（卖家在 `props.allow_preview=false` 关闭了预览，且当前用户未购买）。

### 1.3 文件夹预览（只读、无下载）

`GET /shop/store/:slug/product/:id/preview?path=<token>`

```jsonc
[
  { "name": "season-1", "type": "folder", "size": 0, "updated_at": 1792263392, "path": "c2Vhc29uLTE", "children": 2 },
  { "name": "readme.txt", "type": "file", "size": 18, "updated_at": 1792263392, "ext": "txt" }
]
```

- `path` 是**不透明 token**（base64url，无 padding）。进入子目录时把它原样回传；根目录不传。
- 响应**不含**任何下载地址、实体 ID 或共享凭据 —— 预览就是预览，不能拿。
- 非法 token / 越权路径返回 `40001` / `40016`。

---

## 2. 买家（需登录）

### 2.1 下单（收银台）

`POST /shop/order`

```jsonc
// 购买小店商品
{ "kind": "product", "object_id": "<product id>", "channel": "alipay", "coupon": "CODE" }
// 购买付费分享（分享链接页）
{ "kind": "share", "object_id": "<share hash id>", "channel": "alipay", "share_password": "1234" }
```

返回 `OrderView`：

```jsonc
{
  "order_no": "2026101802381547AF…",
  "kind": "product|share",
  "shop_id": "uuid",
  "object_id": "…",
  "buyer_uid": 2,
  "title": "Season Bundle",
  "amount": 5000, "discount": 500, "payable": 4500, "paid": 0,
  "status": "pending|paid|closed|refunded",
  "channel": "alipay|wxpay|qqpay",
  "referrer_uid": 1,
  "expire_at": 1792263195,
  "created_at": 1792262295,
  "pay_url": "https://pay.example.com/submit.php?…",   // 仅 pending 且有支付通道时存在
  "entitlement_id": ""                                  // 已支付后存在
}
```

前端流程：`pay_url` 存在 → `window.location.href = pay_url`；否则轮询 `GET /shop/order/:orderNo`（建议 2s 间隔，直到 `status != pending`）。

常见错误：`409` 已购买 / 优惠券已被使用；`404` 商品已下架；`40001` 通道或参数不合法。

### 2.2 订单查询

- `GET /shop/order/:orderNo` → `OrderView`（仅本人或管理员）
- `GET /shop/order?limit=20&offset=0` → `OrderView[]`（我的订单）
- `GET /shop/sold?limit=20&offset=0` → `OrderView[]`（我店的收款订单，卖家视角；额外带 `buyer_nickname`）
- 重复点击「购买」会复用同一张未支付订单（不会产生两张可支付订单），返回体与原订单一致。
- 退款后买家可以再次购买（权益为 `revoked` 不再阻止下单）。

### 2.3 我的购买

`GET /shop/purchase?limit=20&offset=0`

```jsonc
[{
  "id": "uuid", "uid": 2, "kind": "product|share", "object_id": "…",
  "order_no": "…", "shop_id": "…", "title": "Season Bundle",
  "status": "pending|granted|revoked", "folder_id": 5,
  "created_at": 1792262475, "updated_at": 1792262475
}]
```

- `status=granted` 表示内容已交付到买家网盘（一个**链接文件夹**，见 §6）。
- `status=pending` 表示交付失败，前端显示「重新交付」按钮：`POST /shop/purchase/:id/transfer`。
- `status=revoked` 表示已退款，门禁立即生效（再次打开会得到 `40083`）。

### 2.4 退款（买家申请 → 卖家/管理员审批）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/shop/purchase/:id/refund` | 买家申请退款：`{reason?}`；同一订单只能申请一次（`409`），超出退款窗口（默认 7 天）会被拒绝 |
| `GET` | `/shop/refund` | 我的退款申请（`[{id, order_no, amount, reason, status, remark, created_at}]`） |
| `GET` | `/shop/refund/received` | 卖家收到的退款申请（额外带 `buyer_nickname` / `title` / `order_status` / `pay_mode`） |
| `POST` | `/shop/refund/:id` | 审批：`{action:"approve"｜"reject", remark?}`（卖家仅能审自己店的，管理员可审全部）；自配渠道的凭证动作见 §3.2 |
| `GET` | `/shop/admin/refund` | 管理员：全站退款申请 |
| `POST` | `/shop/admin/refund/:id` | 管理员：审批（同上），并负责核验自配渠道的退款凭证 |

平台聚合支付：通过退款会订单转 `refunded`、权益转 `revoked`（门禁立即生效）、卖家余额扣回净收入
（仍处于待解冻的收入从 `pending` 扣回，不会把可用余额打负）、邀请返利扣回、
买家链接文件夹重命名为「…(已退款)」。`status` 取值 `pending|approved|rejected|refunded|confirmed`；
自配支付渠道的退款流程见 §3.2。

### 2.5 删除保护（卖家侧）

已售商品的内容受保护，卖家无法删除（前端在删除失败时展示后端 `msg` 即可）：

- 已售出的商品文件夹本身**不可删除**（应「下架」，已购用户继续可用）；
- 首次售出前已存在的内容**不可删除**；
- 售出后新增的内容有 **24 小时撤回时效**，超过后不可删除；
- 包含已售商品的上级文件夹**不可删除**。

---

## 3. 卖家 / 店主（需登录）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/shop` | 开店：`{name, slug?, description?, avatar?, cover?}` → `ShopView` |
| `GET` | `/shop/mine` | 我的小店 → `ShopView`（未开店返回 `404`） |
| `PATCH` | `/shop` | 更新资料/装修：`{name?, slug?, description?, avatar?, cover?, theme?, referrer_rate?}` |
| `POST` | `/shop/status` | `{enabled: boolean}` 开关店铺 |
| `GET` | `/shop/product?limit&offset` | 我的商品（含 `offsale`） |
| `POST` | `/shop/product` | 上架：`{uri: "storflow://my/<folder>", name?, description?, cover?, price, status?, props?}` |
| `PATCH` | `/shop/product/:id` | 改价/改文案/促销：`{name?, description?, cover?, price?, discount?, discount_end?, status?, props?}` |
| `POST` | `/shop/product/:id/status` | `{status: "onsale"｜"offsale"｜"draft"}`（有销量的商品不允许 `removed`） |
| `GET` | `/shop/balance` | `{available, frozen, pending, total_income, total_withdraw, ledger:[{kind,amount,balance,ref_id,remark,created_at}], deposit_amount, deposit_paid, deposit_due, settle_delay_hours}`（`pending` = 待解冻收入） |
| `POST` | `/shop/withdraw` | `{amount, account:{channel:"alipay"｜"wxpay"｜"bank", name, account, bank?, remark?}}` |
| `GET` | `/shop/withdraw?limit&offset` | 提现记录 |
| `GET/POST` | `/shop/coupon`、`PATCH /shop/coupon/:id` | 优惠券：`{code?, kind:"percent"｜"fixed", value, min_amount?, product_id?, max_uses?, per_user?, status?, start_at?, end_at?}` |
| `GET` | `/shop/referral` | 邀请列表：`[{invitee_uid, nickname, source, rate, total_rebate, created_at}]` |
| `GET` | `/shop/referral/link` | 我的推广链接（`…/shop/<slug>?ref=<code>`） |
| `GET/PATCH` | `/shop/payment` | 支付方式与开店保证金：见 §3.2 |
| `POST` | `/shop/deposit` | 缴纳 / 补交开店保证金：见 §3.2 |

`ShopView` = `Shop` 字段 + `{url, product_count, sales, referral_code, pay_mode, deposit_due}`。
`props`（商品附加信息）：`{summary?, tags?, transfer_name?, allow_preview?, preview_limit?, update_notice?, sort?, revision?}`。

**上架商品时的文件选择**：用现有文件管理器/选择器拿到 `storflow://my/...` 的文件夹 URI（文件夹才允许上架）。
**店铺图片（头像 / 封面 / 装修图 / 商品封面）**：图片字段既可以直接粘贴外链，也可以走
「素材」接口上传或从网盘挑选，见 §3.1。

### 3.1 店铺图片素材

店铺前台是公开页面，网盘文件是私有的，所以图片不会直接引用网盘文件：**上传或挑选时后端会把
图片快照成一份「素材」**（表 `crshop_asset`，按用户隔离），前台只引用素材 URL。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/shop/asset` | 上传图片（`multipart/form-data`，字段名 `file`，≤ 4MB）→ `AssetView` |
| `POST` | `/shop/asset/adopt` | 把网盘文件快照成素材：`{uri: "storflow://my/xxx.png"}` → `AssetView` |
| `GET` | `/shop/asset` | 我的素材（最近 200 条）→ `[AssetView]` |
| `GET` | `/shop/asset/:id` | **公开**读取素材（长缓存、不可变），直接用于 `<img src>` / `background-image` |

`AssetView` = `{id, name?, mime, size, url, created_at}`，其中 `url` 就是 `/api/v4/shop/asset/<id>`（相对路径，同源可直接使用）。

约束与错误：

- 仅接受 `image/png`、`image/jpeg`、`image/gif`、`image/webp`、`image/bmp`（按内容嗅探，扩展名只作辅助校验；不含 SVG）；
- 单张 ≤ 4MB（`40001` 返回「图片过大」）；
- `adopt` 只允许**自己的**网盘文件，且必须是文件（不是文件夹）、扩展名与内容都是图片，否则 `40001` / `40016`；
- `GET /shop/asset/:id` 对不存在的 id 返回 HTTP `404`（`<img>` 会走 `onError`）；
- 未被任何店铺/商品引用的素材会在 7 天后由模块后台任务清理。

前端建议：图片字段统一使用「预览 + URL 输入 + 上传 / 从网盘选择 / 清除」四件套
（`assets/src/component/Common/Shop/ShopImageField.tsx`），落库的始终是 URL 字符串，
所以表单提交逻辑不需要为图片做特殊处理。

---

### 3.2 支付方式与开店保证金

店主可以选择买家的付款通道，并查看 / 补交平台要求的保证金：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/shop/payment` | 支付设置：`{pay_mode, epay_api_url?, epay_pid?, epay_key_set, custom_ready, platform_ready, channels, deposit_amount, deposit_paid, deposit_due, settle_delay_hours}` |
| `PATCH` | `/shop/payment` | `{pay_mode?:"platform"｜"custom", epay_api_url?, epay_pid?, epay_key?}`；`epay_key` 传 `********` 表示保持已保存的密钥不变，传空串表示清除 |
| `POST` | `/shop/deposit` | 缴纳 / 补交保证金：创建一笔平台支付订单 → `ShopOrder`（带 `pay_url`，直接跳收银台） |

两种支付方式：

- `platform`（平台聚合支付）：货款由平台代收。订单支付成功后收入进入**待解冻**（`balance.pending`），
  订单完成 + 管理员配置的 `settle_delay_hours`（默认 24，0 = 立即到账）后自动解冻到可用余额，
  之后才能申请提现，且必须达到 `min_withdraw`；
- `custom`（自配支付渠道）：店主填自己的易支付网关地址 / 商户 ID / 密钥，买家直接付给店主。
  平台只负责**记录订单并交付内容**：不计入余额、不收手续费、不发放邀请返利。
  异步回调仍然打到平台（`/shop/epay/notify`），平台按订单快照的 `pay_mode` 用店主自己的密钥验签。

保证金（`deposit_amount`，0 = 不要求）：由平台保管、**不进入钱包**（`available/frozen/pending` 都不变）。
`ShopView.deposit_due > 0` 时控制台应提示补交（管理员事后提高金额同样会产生待补交差额），
未缴清期间 `POST /shop/withdraw` 会被拒绝（`403`）。

**自配渠道的退款**（`pay_mode=custom`）：真实退款由店主线下完成，平台负责流程与核验：

```
买家申请 pending --审批通过--> approved --店主提交凭证--> refunded --平台核验--> confirmed
                                                        refunded --平台退回--> approved
```

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/shop/refund/:id` | 店主：`{action:"approve"｜"reject"}` 审批；`{action:"evidence", evidence:"交易号 / 退款截图地址"}` 提交退款凭证（4–500 字） |
| `POST` | `/shop/admin/refund/:id` | 管理员：`{action:"confirm", remark?}` 核验通过；`{action:"return", remark?}` 退回重交 |

`ShopRefundView` 额外返回 `pay_mode` 与 `evidence / evidence_at / verifier / verified_at`，
前端据此决定是否显示「提交退款凭证」按钮（`status=approved` 且 `pay_mode=custom`）。

## 4. 注册邀请（返利）

### 4.1 分享/小店链接带 `ref`

买家从 `…/shop/<slug>?ref=<code>` 或分享链接进入时：

1. 未登录 → 跳 `/session/signup?ref=<code>&redirect=<当前页面>`；
2. 注册接口 `POST /api/v4/user` 增加可选字段 `ref`（后端已支持）；
3. 注册成功后回到 `redirect` 页面继续购买。

前端需要：注册页读取 `ref` 查询参数并透传给注册接口。

---

## 5. 付费分享（Pro 收费分享）入口

1. 分享页/文件管理器加载 `storflow://<shareId>[:password]@share` 时，若后端返回 **`40083`**，说明该分享需要购买。
2. 调 `GET /share/info/:shareId` 拿详情（公开接口）：

```jsonc
{
  "id": "l9ha", "name": "bundle", "unlocked": true, "source_type": 1,
  "owner": { "id": "JZhD", "nickname": "s" },
  "pay_enabled": true, "price": 1000, "currency": "", "purchased": false,
  "is_default": false, "permission": "read"
}
```

3. 展示「购买此分享」对话框：价格 + 支付方式（`alipay/wxpay/qqpay`）→ `POST /shop/order {kind:"share", object_id:"<shareId>", share_password?}` → 跳 `pay_url` → 回跳 `…/shop/order/<order_no>` → 轮询订单 → `paid` 后刷新当前分享页面（此时 `purchased=true`，内容可读）。
4. 分享创建/编辑对话框增加「收费分享」开关 + 价格输入（`pay_enabled` / `price`），以及「设为默认分享」开关（`is_default`）。
5. 默认分享列表：`GET /share/default`（公开，返回 `{shares: Share[], pagination}`），可在首页/登录后展示。

---

## 6. 「链接文件夹」：交付与更新

- 购买成功后，买家网盘根目录会出现一个同名**链接文件夹**（元数据 `sys:shared_redirect`）。
- 在文件管理器中它像普通文件夹一样可进入、可下载；内容是**卖家文件夹的实时视图**：
  卖家后续新增/修改的文件，买家刷新即可见。
- 它**只读**：重命名/删除/上传会被后端拒绝（`403 Symbolic folder cannot be walked into`）。
- 「我的购买」里点击条目 → 跳 `/home?path=storflow://my/<transfer_name or product name>`。
- **退款后**：权益变为 `revoked`，链接文件夹被重命名为「<名称> (已退款)」，
  买家再次打开会收到 `40083`（与付费分享一致）→ 前端可按「重新购买」处理。

---

## 7. 管理员

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/shop/admin/setting` | 模块配置（`epay_key` 返回 `********` 表示已配置） |
| `PATCH` | `/shop/admin/setting` | `{values:{enabled, epay_api_url, epay_pid, epay_key, epay_channels, fee_percent, referral_percent, withdraw_fee_percent, min_withdraw, order_ttl, site_url, refund_window_days, rate_limit_per_minute, deposit_amount, settle_delay_hours}}`（比例均为万分比；`deposit_amount` 为保证金（分），0 = 不要求；`settle_delay_hours` 为平台渠道收入的解冻时长（小时），0 = 立即到账；`refund_window_days=0` 表示不限；`rate_limit_per_minute=0` 关闭限流） |
| `GET` | `/shop/admin/withdraw?limit&offset` | 全站提现申请 |
| `POST` | `/shop/admin/withdraw/:id` | `{action:"approve"｜"reject"｜"paid", remark?}` |
| `GET` | `/shop/admin/shop?limit&offset` | 全站小店列表：`[{shop, balance, sales, sold_count}]` |
| `GET` | `/shop/admin/shop/:id/stats` | `{shop, balance, product_count, order_count, sold_count}` |
| `POST` | `/shop/admin/order/:orderNo/refund` | `{reason?}` 退款：订单转 `refunded`、买家权益 `revoked`、扣回卖家收入与邀请返利 |
| `POST` | `/shop/admin/reconcile` | 主动向支付网关查单并补单（用于回调丢失），返回 `{settled: n}` |

---

## 8. 页面清单（需要实现）

| 路由 | 页面 | 是否需登录 |
| --- | --- | --- |
| `/shop/:slug` | 小店主页：装修（banner/公告/主色/布局）+ 商品网格（价格、促销标签、销量） | 否 |
| `/shop/:slug/product/:id` | 商品详情：描述、价格、**文件夹预览**（可进入子目录）、购买按钮（未登录先注册/登录，带 `ref`） | 否（购买需登录） |
| `/shop/order/:orderNo` | 收银台回跳页：展示订单状态，`pending` 时轮询，`paid` 时提示「已交付，去我的网盘」 | 是 |
| `/shop/purchases` | 我的购买：权益列表 + 进入链接文件夹 + 重新交付 | 是 |
| `/shop/manage` | 卖家控制台（Tabs）：小店资料/装修、商品、订单、退款、余额与提现、**支付方式**（平台聚合支付 / 自配渠道 + 保证金补交）、优惠券、邀请返利 | 是 |
| `/shop/orders` | 我的订单（可并入 `/shop/purchases`） | 是 |
| `/admin/shop` | 后台：小店配置 + 提现审批 | 管理员 |
| 分享页 | 付费分享购买对话框 + 创建分享时的「收费分享/默认分享」开关 | 视情况 |

---

## 9. 构建与打包（前端 → 后端可服务）

前端是 `assets/` 子模块（StorFlow 官方前端），后端通过内嵌 `application/statics/assets.zip` 提供服务：

```bash
# 1) 前端依赖与构建（版本号必须与后端 constants.BackendVersion 一致，否则启动时会报版本不匹配）
cd assets
yarn install --frozen-lockfile
yarn version --new-version 4.14.0 --no-git-tag-version   # 与后端版本对齐
yarn build                                              # 产物在 assets/build

# 2) 打包成后端内嵌的 zip（注意：zip 内路径必须是 assets/build/...）
cd ..
rm -f application/statics/assets.zip
zip -q -r application/statics/assets.zip assets/build

# 3) 重新编译后端（zip 是编译期内嵌的）
go build -o storflow .
```

也可以直接用仓库脚本：`.build/build-assets.sh 4.14.0`（脚本会 install + version + build + zip）。

**运行期注意**：若数据目录下存在 `statics/` 文件夹，后端会优先服务该目录而不是内嵌 zip；
前端改动后记得清掉旧的 `statics/`（日志会提示版本不匹配）。
