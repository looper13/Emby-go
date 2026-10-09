package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"

	"emby-go/internal/logging"
)

// requestLogger 构造请求日志中间件：输出到请求日志文件（同时保留控制台），
// 并跳过静态资源/流/图片的 2xx 请求。
func requestLogger() gin.HandlerFunc {
	return requestLoggerTo(logging.RequestWriter())
}

func requestLoggerTo(output io.Writer) gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		Output: output,
		Skip:   skipRequestLog,
		Formatter: func(param gin.LogFormatterParams) string {
			return fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %q\n",
				param.TimeStamp.Format("2006/01/02 - 15:04:05"), param.StatusCode, param.Latency,
				param.ClientIP, param.Method, redactRequestTarget(param.Path))
		},
	})
}

func sensitiveRequestField(key string) bool {
	key = strings.ToLower(key)
	key = strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(key)
	return key == "pw" || strings.Contains(key, "password") || strings.HasSuffix(key, "token") ||
		strings.HasSuffix(key, "apikey") || strings.Contains(key, "authorization") ||
		key == "cookie" || key == "setcookie" || key == "secret" || key == "clientsecret"
}

func redactRequestTarget(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return strings.SplitN(target, "?", 2)[0]
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveRequestField(key) {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// Log a detached header snapshot. Never consume or persist request bodies:
// passwords and provider credentials may occur in JSON, forms or uploads.
func redactedRequestDump(request *http.Request) ([]byte, error) {
	copy := request.Clone(request.Context())
	copy.URL.User = nil
	_, copy.URL.RawQuery, _ = strings.Cut(redactRequestTarget("/?"+request.URL.RawQuery), "?")
	copy.RequestURI = redactRequestTarget(request.RequestURI)
	copy.Body = nil
	for key := range copy.Header {
		if sensitiveRequestField(key) {
			copy.Header.Set(key, "[REDACTED]")
		}
	}
	return httputil.DumpRequest(copy, false)
}

// skipRequestLog 判断该请求是否不记入请求日志。
// Gin 在 c.Next() 之后才调用本函数，此时状态码已确定：非 2xx 一律记录，
// 2xx 的静态资源/流/图片则跳过——否则海报墙与播放会把日志淹没。
// API 请求（含 /api/admin 与 Emby 兼容端点）始终记录。
func skipRequestLog(c *gin.Context) bool {
	status := c.Writer.Status()
	if status < 200 || status >= 300 {
		return false
	}
	return quietPath(c.Request.URL.Path)
}

// quietPath 判断路径是否属于高频噪声来源。Emby 路由同时注册了 PascalCase 与全小写
// 变体，客户端还会带 /emby 前缀，故先归一化（去前缀 + 转小写）再判断。
func quietPath(path string) bool {
	p := strings.ToLower(strings.TrimPrefix(strings.ToLower(path), "/emby"))
	switch {
	case p == "/favicon.ico", strings.HasPrefix(p, "/web/"):
		return true
	case strings.Contains(p, "/images/"):
		return true
	case strings.Contains(p, "/scrape/image"):
		// 刮削预览的缩略图代理：一次预览就是十几张图，与海报墙同类噪声。
		return true
	case strings.Contains(p, "/videos/") && (strings.Contains(p, "/stream") || strings.Contains(p, "/proxy")):
		return true
	case strings.HasPrefix(p, "/audio/"), p == "/audio":
		return true
	}
	return false
}

// requestContentLogger debug 模式只记录脱敏请求头，不记录正文。
func requestContentLogger() gin.HandlerFunc {
	return requestContentLoggerTo(logging.RequestWriter())
}

func requestContentLoggerTo(output io.Writer) gin.HandlerFunc {
	return func(c *gin.Context) {
		request, err := redactedRequestDump(c.Request)
		if err == nil {
			fmt.Fprintf(output, "[API REQUEST]\n%s\n[body omitted]\n", string(request))
		}
		c.Next()
	}
}

// Gin's default recovery dumps raw query strings and authentication headers.
// Retain stack diagnostics using the same redacted snapshot as request logging.
func requestRecoveryTo(output io.Writer) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, recovered any) {
		request, _ := redactedRequestDump(c.Request)
		fmt.Fprintf(output, "[API PANIC] type=%T\n%s\n%s\n", recovered, request, debug.Stack())
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
