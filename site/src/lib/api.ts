/**
 * 站点侧数据访问层。
 *
 * Astro 以 SSR 方式运行在与 Go 后端同一台主机上，直接走内网回环地址拿数据，
 * 因此这里不需要浏览器 token：只调用公开接口（`/shop/store/*`、`/site/config/*`）。
 * 买家登录态保存在浏览器 localStorage 中，只有购物按钮那种客户端脚本才会用到。
 */

/** Go 后端地址；可通过 `CR_API_BASE` 覆盖（例如容器内 `http://storflow:5212`）。 */
export const ORIGIN = (process.env.CR_API_BASE || "http://127.0.0.1:5212").replace(/\/+$/, "");
const API = `${ORIGIN}/api/v4`;

export interface ShopTheme {
  template?: string;
  primary_color?: string;
  background_color?: string;
  background_image?: string;
  banner?: string;
  logo?: string;
  slogan?: string;
  notice?: string;
  layout?: string[];
  links?: { label: string; url: string }[];
  footer?: string;
}

export interface ShopProduct {
  id: string;
  name: string;
  summary?: string;
  tags?: string[];
  cover?: string;
  /** 单位：分 */
  price: number;
  /** 单位：分，含折扣后的实付价 */
  final_price: number;
  discount?: number;
  sales: number;
  status: string;
  previewable: boolean;
  owned: boolean;
  url?: string;
}

export interface ShopProductDetail extends ShopProduct {
  description?: string;
  /** 介绍里嵌的素材可见范围：public / login / purchased。 */
  description_visibility?: string;
  can_preview: boolean;
}

export interface ShopStorefront {
  slug: string;
  name: string;
  description?: string;
  avatar?: string;
  cover?: string;
  theme?: ShopTheme;
  url?: string;
  owner?: { id: string; nickname: string; avatar?: string };
  products?: ShopProduct[];
}

export interface SiteBasic {
  title?: string;
  logo?: string;
  logo_light?: string;
}

export interface PreviewEntry {
  name: string;
  type: "folder" | "file";
  size: number;
  ext?: string;
}

interface Envelope<T> {
  code: number;
  msg?: string;
  data?: T;
}

export type FetchResult<T> = { ok: true; data: T } | { ok: false; status: number; error: string };

/** 统一 GET：把后端信封（{code,data,msg}）摊平成结果对象，不抛异常。 */
export async function getJSON<T>(path: string, timeoutMs = 5000): Promise<FetchResult<T>> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${API}${path}`, {
      signal: controller.signal,
      headers: { Accept: "application/json" },
    });
    const text = await res.text();
    let body: Envelope<T> | undefined;
    try {
      body = text ? (JSON.parse(text) as Envelope<T>) : undefined;
    } catch {
      body = undefined;
    }
    if (!res.ok) {
      return { ok: false, status: res.status, error: body?.msg || `HTTP ${res.status}` };
    }
    if (body && typeof body.code === "number" && body.code !== 0) {
      return { ok: false, status: body.code, error: body.msg || "请求失败" };
    }
    return { ok: true, data: (body?.data ?? (undefined as unknown)) as T };
  } catch (e) {
    return { ok: false, status: 0, error: e instanceof Error ? e.message : "网络错误" };
  } finally {
    clearTimeout(timer);
  }
}

export const getSiteBasic = () => getJSON<SiteBasic>("/site/config/basic");

export const getStorefront = (slug: string) =>
  getJSON<ShopStorefront>(`/shop/store/${encodeURIComponent(slug)}`);

export const getStorefrontProduct = (slug: string, id: string) =>
  getJSON<ShopProductDetail>(`/shop/store/${encodeURIComponent(slug)}/product/${encodeURIComponent(id)}`);

/** 商品文件夹预览：作为商品页的补充内容（对 SEO 有帮助），失败时不阻塞页面。 */
export const getProductPreview = (slug: string, id: string) =>
  getJSON<PreviewEntry[]>(`/shop/store/${encodeURIComponent(slug)}/product/${encodeURIComponent(id)}/preview`);
