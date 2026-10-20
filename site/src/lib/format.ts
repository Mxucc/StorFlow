/** 展示层格式化工具：金额统一按「分」存储，容量按字节。 */

/** 分 → "12.00"（不带货币符号）。 */
export const formatMoney = (cents?: number | null): string => {
  const value = Math.trunc(cents ?? 0);
  const negative = value < 0;
  const abs = Math.abs(value);
  return `${negative ? "-" : ""}${Math.floor(abs / 100)}.${(abs % 100).toString().padStart(2, "0")}`;
};

/** 分 → "¥12.00"。 */
export const displayMoney = (cents?: number | null, symbol = "¥"): string => `${symbol}${formatMoney(cents)}`;

/** 字节 → 人类可读容量。 */
export const formatSize = (bytes?: number): string => {
  const value = bytes ?? 0;
  if (value <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let index = 0;
  let scaled = value;
  while (scaled >= 1024 && index < units.length - 1) {
    scaled /= 1024;
    index += 1;
  }
  const digits = scaled >= 100 || index === 0 ? 0 : scaled >= 10 ? 1 : 2;
  return `${scaled.toFixed(digits)} ${units[index]}`;
};

/** 截断长文本，用于 meta description。 */
export const clampText = (text: string | undefined, max = 155): string => {
  const value = (text ?? "").replace(/\s+/g, " ").trim();
  if (value.length <= max) {
    return value;
  }
  return `${value.slice(0, max - 1)}…`;
};

/** 把可能是站内相对路径的图片转成绝对 URL（OG/JSON-LD 必须是绝对地址）。 */
export const absoluteURL = (url: string | undefined, origin: string): string | undefined => {
  if (!url) {
    return undefined;
  }
  if (/^https?:\/\//i.test(url)) {
    return url;
  }
  if (url.startsWith("//")) {
    return `https:${url}`;
  }
  return `${origin}${url.startsWith("/") ? "" : "/"}${url}`;
};

/**
 * 取当前访客实际看到的站点 origin。
 *
 * 站点在 Go 后端之后（middleware/site.go 反向代理），`Astro.url.origin` 会是
 * 站点进程自己的监听地址（如 `http://127.0.0.1:4321`）。canonical / og:url /
 * sitemap 必须用访客域名，否则搜索引擎会收录内网地址。约定按以下优先级：
 *
 *   1. 部署时显式配置的 `CR_SITE_URL`（例如 `https://pan.example.com`）；
 *   2. 代理透传的 `X-Forwarded-Host` + `X-Forwarded-Proto`；
 *   3. 请求自身的 `Host` 头（直连站点时）；
 *   4. `Astro.url.origin`（本地开发兜底）。
 */
export const resolveOrigin = (request: Request, fallback: URL): string => {
  const configured = (process.env.CR_SITE_URL || "").replace(/\/+$/, "");
  if (configured) {
    return configured;
  }
  // 多级代理时 X-Forwarded-Host 可能是逗号分隔列表，取最前面（最近的一跳）。
  const forwardedHost = (request.headers.get("x-forwarded-host") || "")
    .split(",")[0]
    .trim();
  const host = forwardedHost || (request.headers.get("host") || "").trim();
  if (!host) {
    return fallback.origin;
  }
  const forwardedProto = (request.headers.get("x-forwarded-proto") || "")
    .split(",")[0]
    .trim();
  const proto = forwardedProto || fallback.protocol.replace(":", "") || "http";
  return `${proto}://${host}`;
};
