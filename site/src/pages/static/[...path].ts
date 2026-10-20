/**
 * 站点侧静态图片转发。
 *
 * SPA 的 `public/static/**`（logo、favicon、小店默认图等）与 Go 后端是同源的，
 * 这里把它们 302 到主站，避免在 Astro 里再维护一份素材。
 */
import type { APIRoute } from "astro";
import { ORIGIN } from "../../lib/api";

export const prerender = false;

export const GET: APIRoute = ({ params, url, redirect }) => {
  const path = params.path ?? "";
  const target = new URL(`${ORIGIN}/static/${path}`);
  // 保留查询串（例如 ?v=hash 的缓存破除参数）。
  target.search = url.search;
  return redirect(target.toString(), 302);
};
