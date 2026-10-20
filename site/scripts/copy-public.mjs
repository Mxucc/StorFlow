/**
 * 把 public 目录里的静态文件镜像进 dist/client。
 *
 * Astro 的 SSR 构建不会自动复制 `public/`（那是静态站点模式的行为），但独立
 * node server 需要从 `dist/client` 提供资源。前端图片（logo / banner / 默认商品图）
 * 已经在 SPA 的 public 里维护，这里做一次镜像，避免出现两套素材。
 */
import { cpSync, existsSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const projectRoot = resolve(here, "..");
const publicDir = resolve(projectRoot, "public");
const target = resolve(projectRoot, "dist", "client");

if (!existsSync(publicDir)) {
  process.exit(0);
}

mkdirSync(target, { recursive: true });
cpSync(publicDir, target, { recursive: true, force: true });
console.log(`[copy-public] mirrored ${publicDir} -> ${target}`);
