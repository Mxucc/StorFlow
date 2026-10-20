# 商品介绍（Markdown 富文本 + 网盘素材直连）

商品介绍不是一段纯文本，而是卖家写的 **Markdown 商品页**：教程步骤、对照表格、
截图、演示视频都可以放进去。介绍里嵌的图片与视频**直接引用卖家网盘里的文件**，
不复制、不占额外空间，卖家在网盘里替换同名文件后介绍页立刻生效。

## 数据

| 位置 | 字段 | 说明 |
| --- | --- | --- |
| `crshop_product.description` | TEXT | Markdown 原文，上限 `model.MaxProductDescription`（200 KB） |
| `crshop_product.props` | JSON | `description_visibility`：`public` / `login` / `purchased`，默认 `public` |

文字本身始终可见（商品页是 SEO 页面）；`description_visibility` 只决定**素材**
（图片 / 视频 / 音频）谁能加载：

- `public`：任何人，含未登录访客与搜索引擎；
- `login`：仅登录用户；
- `purchased`：仅已购买该商品的用户（店主自己始终可见）。

## 接口

| 方法 | 路径 | 谁 | 说明 |
| --- | --- | --- | --- |
| POST | `/api/v4/shop/product/:id/asset` | 卖家 | 把一个网盘文件签成可写进 Markdown 的长期 URL |
| GET | `/api/v4/shop/store/:slug/product/:id/asset/:token` | 公开 | 按商品的可见性设置流式返回文件内容 |

签发请求体是 `{ "uri": "storflow://my/教程/step-1.png" }`，响应：

```json
{ "url": "/api/v4/shop/store/demo-shop/product/<id>/asset/<token>",
  "name": "step-1.png", "mime": "image/png", "kind": "image", "size": 20480 }
```

`kind` 决定编辑器怎么写进 Markdown：图片用标准图片语法，视频 / 音频多带一个
`?k=video` / `?k=audio` 提示，渲染时换成对应的播放器：

```markdown
![step-1.png](/api/v4/shop/store/demo-shop/product/<id>/asset/<token>)
![演示视频.mp4](/api/v4/shop/store/demo-shop/product/<id>/asset/<token>?k=video)
```

## token 与权限

token 是 `base64(payload) + "." + base64(HMAC-SHA256 前 16 字节)`，密钥取
`dep.SettingProvider().SecretKey()`。payload 只有三项：商品 ID、文件 ID、过期时间
（十年）—— **不含路径**，所以泄露一个 token 也不会暴露卖家的目录结构。

服务端每次请求都会重新校验：

1. 签名与过期时间；
2. token 里的商品 ID 必须等于 URL 里的商品 ID（挡住跨商品复用）；
3. `publicProduct` 解析出商品与店铺（下架商品只有店主可见）；
4. 按 `description_visibility` 校验访客；
5. 文件必须仍然属于店铺 owner，且是文件而不是文件夹；
6. 以**卖家的身份**打开文件并流式返回（`EntitySource.Serve` 原生支持 Range 与 ETag，
   视频才能拖动播放）。

`Content-Type` 由扩展名给出：`EntitySource` 只在响应头没设置时才嗅探内容，
而 `mov` / `mkv` 这类容器靠嗅探并不可靠，视频会因此播不出来。

响应头：`public` 素材是 `Cache-Control: public, max-age=3600`；受限素材是
`private, max-age=60`，避免中间的 CDN 把它缓存给未授权访客。

允许的类型见 `shop/service/productasset.go` 的 `productAssetMimes`：
常见图片（不含 SVG）、视频与音频。其它文件类型会被拒绝并提示。

## 渲染

Markdown → HTML 有两份实现，规则必须一致：

- `assets/src/util/markdown.ts`（SPA 商品详情页）
- `site/src/lib/markdown.ts`（Astro 服务端渲染，SEO 页面）

两边都做了这些约束（商品页是公开的、内容由卖家填写）：

- **原始 HTML 一律丢弃**，只渲染 Markdown 语法本身；
- 链接与图片地址只允许 `http(s):`、站内相对路径、锚点与 `mailto:`，挡掉 `javascript:`；
- `?k=video` / `?k=audio` 的图片语法渲染成 `<video>` / `<audio>` 播放器；
- `markdownToPlainText()` 供 meta description 与 JSON-LD 使用：去掉标记、去掉 HTML 标签。

`Base.astro` 还会把 JSON-LD 里的 `<` 转义成 `\u003c` —— 否则介绍里写一个
`</script>` 就能提前闭合 JSON-LD 的 script 标签，把后面的内容变成可执行 HTML。

## 卖家侧

`ShopProductsTab` 的商品编辑对话框里：

- 「商品介绍」是一个 Markdown 富文本编辑器（复用 `Viewers/MarkdownEditor`，用
  `minHeight` 限制高度、`extraToolbar` 塞入自定义按钮、`ref` 暴露 `insertMarkdown`）；
- 「插入网盘素材」按钮 → 网盘文件选择器（`selectPath("pickFile")`，只允许选文件）
  → 调签发接口 → 把素材写进光标位置；
- 可见性下拉三选一。

**新建商品时不能插入素材**：签发是按商品维度的，需要先保存拿到商品 ID。按钮会提示
「请先保存商品，再插入网盘素材。」
