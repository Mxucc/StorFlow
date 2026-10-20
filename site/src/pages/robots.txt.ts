import type { APIRoute } from "astro";
import { resolveOrigin } from "../lib/format";

/**
 * robots.txt
 *
 * 只放行公开页面（首页 / 店铺 / 商品），登录后的控制台与 API 一律禁止抓取；
 * `Sitemap` 指向同源 `/sitemap.xml`（由 sitemap.xml.ts 动态生成）。
 */
export const prerender = false;

export const GET: APIRoute = ({ url, request }) => {
  const origin = resolveOrigin(request, url);
  const body = `User-agent: *
Allow: /$
Allow: /shop/
Disallow: /api/
Disallow: /admin/
Disallow: /session/
Disallow: /home
Disallow: /shop/manage
Disallow: /shop/purchases
Disallow: /shop/order/
Disallow: /store

Sitemap: ${origin}/sitemap.xml
`;

  return new Response(body, {
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Cache-Control": "public, max-age=3600",
    },
  });
};
