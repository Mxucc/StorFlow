import { Marked } from "marked";

/**
 * 商品介绍的 Markdown → HTML（站点侧，服务端渲染）。
 *
 * 与 SPA 侧 `assets/src/util/markdown.ts` 保持同一份规则，两边的渲染结果
 * 必须一致，否则同一个商品在店铺页和控制台里长得不一样。
 *
 * 商品页是公开页面、内容由卖家填写，所以这里不假设「作者可信」：
 *
 *   - 原始 HTML 一律丢弃，只渲染 Markdown 语法本身；
 *   - 链接与图片地址只允许 http/https、站内相对路径、锚点与 mailto；
 *   - `![名称](地址?k=video)` 渲染成 `<video>`、`?k=audio` 渲染成 `<audio>`。
 */

const SAFE_URL = /^(https?:|\/|#|mailto:)/i;

const escapeHTML = (value: string): string =>
  value.replace(/[&<>"']/g, (char) => {
    switch (char) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      default:
        return "&#39;";
    }
  });

const safeURL = (url?: string | null): string => {
  const value = (url ?? "").trim();
  return SAFE_URL.test(value) ? value : "";
};

const embedKind = (url: string): string | undefined => /[?&]k=(video|audio)\b/.exec(url)?.[1];

const markdown = new Marked({ gfm: true, breaks: true });

markdown.use({
  renderer: {
    html: () => "",
    link({ href, title, tokens }) {
      const text = this.parser.parseInline(tokens);
      const url = safeURL(href);
      if (!url) {
        return text;
      }
      const titleAttr = title ? ` title="${escapeHTML(title)}"` : "";
      return `<a href="${escapeHTML(url)}"${titleAttr} target="_blank" rel="noopener noreferrer nofollow">${text}</a>`;
    },
    image({ href, title, text }) {
      const url = safeURL(href);
      const alt = escapeHTML(text ?? "");
      if (!url) {
        return alt;
      }
      const titleAttr = title ? ` title="${escapeHTML(title)}"` : "";
      const kind = embedKind(url);
      if (kind === "video") {
        return `<video class="cr-embed" src="${escapeHTML(url)}"${titleAttr} controls preload="metadata" playsinline></video>`;
      }
      if (kind === "audio") {
        return `<audio class="cr-embed" src="${escapeHTML(url)}"${titleAttr} controls preload="metadata"></audio>`;
      }
      return `<img src="${escapeHTML(url)}" alt="${alt}"${titleAttr} loading="lazy" />`;
    },
  },
});

/** 把商品介绍的 Markdown 渲染成可安全插入的 HTML。 */
export const renderMarkdown = (source?: string | null): string => {
  if (!source) {
    return "";
  }
  return markdown.parse(source, { async: false });
};

/**
 * 把商品介绍压成纯文本，用于 meta description 与 JSON-LD。
 *
 * 直接把这些字段填成原始 Markdown 会有两个问题：搜索结果里出现 `##`、`**`
 * 这类标记；更麻烦的是 Markdown 里的 `</script>` 会提前闭合 JSON-LD 的
 * script 标签，把后面的内容变成可执行 HTML。
 */
export const markdownToPlainText = (source?: string | null): string =>
  (source ?? "")
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/^\s{0,3}#{1,6}\s*/gm, "")
    .replace(/^\s{0,3}>\s?/gm, "")
    .replace(/<[^>]*>/g, " ")
    .replace(/[*_~]/g, "")
    .replace(/\|/g, " ")
    .replace(/\s+/g, " ")
    .trim();
