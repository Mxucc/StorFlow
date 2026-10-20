// SiteProxy 反向代理公开站点（Astro SSR 生成的首页与小店前台）。
//
// 背景：登录后的控制台、结算、管理后台都由内嵌的 React SPA 提供，但首页和小店前台
// 需要真正的服务端 HTML 才能被搜索引擎正确收录（标题、描述、Open Graph、结构化数据）。
// 因此：
//
//   - `/`、`/shop/:slug`、`/shop/:slug/product/:id` 的 **HTML 请求**代理给 Astro 站点；
//   - XHR / fetch（`Accept: application/json`）仍然走 SPA 自己的 API，不影响页面内软导航；
//   - 历史遗留路径（`/shop/manage`、`/shop/purchases`…）继续由 SPA 处理；
//   - Astro 未部署或暂时不可用时，回退到 SPA 原来的行为（继续 gin 链）。
package middleware

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultProxy 是 Astro 站点默认监听地址（见 site/ 的 `npm start`）。
const DefaultProxy = "http://127.0.0.1:4321"

// proxyFailedHeader 是内部标记：上游不可用时由 ErrorHandler 打在请求头上。
const proxyFailedHeader = "X-Cr-Site-Proxy-Failed"

// spaOnlyPrefixes 命中后永远交给 SPA 处理（即使路径以 /shop/ 开头）。
var spaOnlyPrefixes = []string{
	"/shop/manage",
	"/shop/purchases",
	"/shop/order",
	"/shop/orders",
}

// EnvOrigin 是启用站点代理的环境变量：只有显式配置了地址才会注册中间件，
// 因此没部署 Astro 站点的实例行为与之前完全一致（首页仍由 SPA 渲染）。
const EnvOrigin = "CR_SITE_ORIGIN"

// New 返回把公开 HTML 页面代理给 Astro 站点的中间件；target 为空或不可解析时返回 nil。
func New(target string) gin.HandlerFunc {
	if strings.TrimSpace(target) == "" {
		return nil
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}

	proxy := httputil.NewSingleHostReverseProxy(parsed)
	// 转发原始 Host/协议：Astro 用 `Astro.url.origin` 生成 canonical / og:url /
	// sitemap，必须是访客看到的域名，而不是站点进程的 127.0.0.1:4321。
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		incomingHost := req.Host
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		if proto := req.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		}
		originalDirector(req)
		if incomingHost != "" {
			req.Header.Set("X-Forwarded-Host", incomingHost)
			req.Host = incomingHost
		}
		req.Header.Set("X-Forwarded-Proto", scheme)
	}
	// ErrorHandler 只能拿到 http.ResponseWriter / *http.Request，没有 gin.Context，
	// 因此用一个带状态的 writer 记录「本次代理是否失败」，供外层决定是否回退 SPA。
	// （不能靠改写 req.Header 传状态：那次请求随后就被丢弃了。）
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		if recorder, ok := w.(*proxyRecorder); ok {
			recorder.failed = true
			return
		}
		http.Error(w, "site upstream unavailable", http.StatusBadGateway)
	}
	proxy.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
	}

	return func(c *gin.Context) {
		if !shouldProxy(c) {
			c.Next()
			return
		}

		recorder := &proxyRecorder{ResponseWriter: c.Writer}
		proxy.ServeHTTP(recorder, c.Request)

		if recorder.failed && !recorder.written {
			// 站点没起来 / 超时：没有写出任何响应，交回 Gin 让内嵌 SPA 渲染（旧行为）。
			c.Next()
			return
		}
		c.Abort()
	}
}

// proxyRecorder 记录代理是否失败、以及是否已向客户端写过响应。
// 只有在「失败且未写出任何内容」时才回退 SPA，避免破坏已发送的响应。
type proxyRecorder struct {
	gin.ResponseWriter
	failed  bool
	written bool
}

func (r *proxyRecorder) WriteHeader(code int) {
	r.written = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *proxyRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

// Written 同步代理侧的写出状态，避免后续 handler 重复写响应头。
func (r *proxyRecorder) Written() bool {
	return r.written || r.ResponseWriter.Written()
}

// proxyPaths 是「站点自己渲染」的固定路径：SEO 入口与站点自带的静态资源。
// `/static/` 不在此列 —— 那是 SPA 的图片目录，由内嵌前端直接提供。
var proxyPaths = map[string]struct{}{
	"/":            {},
	"/index.html":  {},
	"/robots.txt":  {},
	"/sitemap.xml": {},
}

// shouldProxy 判断当前请求是否是「需要服务端渲染的公开页面」。
func shouldProxy(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}

	accept := c.GetHeader("Accept")
	// XHR / fetch（SPA 软导航、数据请求）不代理。
	if c.GetHeader("X-Requested-With") != "" || strings.Contains(accept, "application/json") {
		return false
	}

	path := c.Request.URL.Path
	if _, ok := proxyPaths[path]; ok {
		return true
	}
	// Astro 客户端资源（构建产物的 CSS/JS）。
	if strings.HasPrefix(path, "/site-assets/") {
		return true
	}
	// 其它静态资源（css/js/图片）交给内嵌前端。
	if accept != "" && !strings.Contains(accept, "text/html") {
		return false
	}

	for _, prefix := range spaOnlyPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return false
		}
	}

	if path == "/shop" || strings.HasPrefix(path, "/shop/") {
		// /shop 之后最多三段：slug、slug/product、slug/product/:id。
		return len(strings.Split(strings.Trim(path, "/"), "/")) <= 4
	}
	return false
}
