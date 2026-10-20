import type { APIRoute } from "astro";
import { getJSON } from "../lib/api";
import { resolveOrigin } from "../lib/format";

/**
 * sitemap.xml
 *
 * 首页 + 每个上架店铺 + 每个店铺的在售商品。店铺列表来自后端新增的公开目录
 * 接口（`GET /api/v4/shop/store`），商品来自店铺前台接口；任一店铺拉取失败
 * 只跳过该店铺，不影响整份 sitemap。
 */
export const prerender = false;

interface ShopSummary {
  slug: string;
  name: string;
  updated_at?: number;
}

interface Storefront {
  products?: { id: string; name: string }[];
}

const escapeXML = (value: string) =>
  value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");

const isoDate = (unix?: number) => {
  if (!unix || unix <= 0) {
    return undefined;
  }
  return new Date(unix * 1000).toISOString();
};

export const GET: APIRoute = async ({ url, request }) => {
  const origin = resolveOrigin(request, url);

  type Entry = { loc: string; lastmod?: string; priority: string };
  const entries: Entry[] = [{ loc: `${origin}/`, priority: "1.0" }];

  const shops = await getJSON<ShopSummary[]>("/shop/store?limit=500");
  if (shops.ok) {
    for (const shop of shops.data ?? []) {
      const shopPath = `${origin}/shop/${encodeURIComponent(shop.slug)}`;
      entries.push({ loc: shopPath, lastmod: isoDate(shop.updated_at), priority: "0.8" });

      const storefront = await getJSON<Storefront>(`/shop/store/${encodeURIComponent(shop.slug)}`);
      if (!storefront.ok) {
        continue;
      }
      for (const product of storefront.data?.products ?? []) {
        entries.push({
          loc: `${shopPath}/product/${encodeURIComponent(product.id)}`,
          priority: "0.6",
        });
      }
    }
  }

  const body = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${entries
  .map(
    (entry) =>
      `  <url>\n    <loc>${escapeXML(entry.loc)}</loc>\n${
        entry.lastmod ? `    <lastmod>${entry.lastmod}</lastmod>\n` : ""
      }    <priority>${entry.priority}</priority>\n  </url>`,
  )
  .join("\n")}
</urlset>
`;

  return new Response(body, {
    headers: {
      "Content-Type": "application/xml; charset=utf-8",
      "Cache-Control": "public, max-age=600",
    },
  });
};
