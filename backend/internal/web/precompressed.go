//go:build embed || unit

// APEXONE-EXT: 内嵌静态资源的预压缩发送。
//
// # 为什么需要
//
// 首页要下的 JS/CSS 是纯文本，gzip 后普遍只剩 15~30%（实测那个 250KB 的 CSS
// 压到 33KB，省 86%）。而线上实测下载速率只有 16~93 KB/s，这几百 KB 就是
// 用户盯着白屏的那几秒。
//
// # 为什么是「预压缩」而不是压缩中间件
//
// 一个全局的 gzip 中间件要处理三类**绝不能压**的响应，每一类压了都是线上事故：
//
//   - SSE 流式（text/event-stream）：压缩会缓冲，逐字输出变成一次性吐出，
//     Claude Code 那种流式体验直接没了；
//   - E2EE 密文：middleware/e2ee.go 明确把 Accept-Encoding 设成 identity，
//     密文再压一遍既没收益又会破坏它自己的封装约定；
//   - 图片等已压缩内容：白烧 CPU，还可能变大。
//
// 预压缩只作用在「内嵌 dist 里的静态文件」这一条分支上，物理上够不到上面三类。
// 风险面小一个数量级，这是它相对中间件的**全部理由**。
//
// 顺带还省了 CPU：压缩在构建时做一次，不是每个请求做一次。TEE 里 CPU 更贵。
//
// # 没有 .gz 就原样发
//
// 前端构建若没生成 .gz（比如有人只跑了 vite build 没跑压缩脚本），这里退回
// 发原文——慢，但**不会坏**。任何时候都不要让「压缩产物缺失」变成 500。
package web

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// precompressedExtensions 只对这几类做预压缩查找。
//
// 白名单而不是黑名单：新增一种资源类型时，默认行为是「原样发」而不是
// 「去找一个可能不存在的 .gz」。
var precompressedExtensions = map[string]bool{
	".js":   true,
	".css":  true,
	".html": true,
	".json": true,
	".svg":  true,
	".map":  true,
	".txt":  true,
	".xml":  true,
}

// clientAcceptsGzip 判断客户端是否接受 gzip。
//
// 刻意不做 q 值解析：`gzip;q=0` 表示明确拒绝，这种客户端极为罕见，而为它写一个
// 半吊子的 q 值解析器，出错方式是「给一个说了不要的客户端发压缩内容」——那会
// 变成一个只在某类客户端上出现、极难复现的乱码 bug。宁可把这种情况也当成接受，
// 由下面的 identity 显式拒绝来兜底。
func clientAcceptsGzip(header string) bool {
	if header == "" {
		return false
	}
	lower := strings.ToLower(header)
	// identity 明确表示「别压」——E2EE 中间件正是这么关掉压缩的，必须尊重。
	if strings.Contains(lower, "identity;q=0") {
		return strings.Contains(lower, "gzip")
	}
	if strings.HasPrefix(strings.TrimSpace(lower), "identity") && !strings.Contains(lower, ",") {
		return false
	}
	return strings.Contains(lower, "gzip")
}

// servePrecompressedAsset 尝试用构建时生成的 .gz 应答。
//
// 返回 false 表示没找到可用的预压缩产物，调用方按原路径原样发送。
func servePrecompressedAsset(w http.ResponseWriter, r *http.Request, distFS fs.FS, cleanPath string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !precompressedExtensions[strings.ToLower(path.Ext(cleanPath))] {
		return false
	}
	if !clientAcceptsGzip(r.Header.Get("Accept-Encoding")) {
		return false
	}

	file, err := distFS.Open(cleanPath + ".gz")
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		return false
	}
	stat, err := file.Stat()
	if err != nil {
		return false
	}

	header := w.Header()
	header.Set("Content-Encoding", "gzip")
	// Vary 是必须的：同一个 URL 现在会因为 Accept-Encoding 不同而返回不同字节，
	// 少了它，中间缓存会把压缩版发给一个不支持 gzip 的客户端。
	header.Add("Vary", "Accept-Encoding")
	// Content-Type 要按**原始**文件名判定：.gz 会让标准库推断成 application/gzip，
	// 浏览器于是把 JS 当成文件下载而不是执行。
	if ctype := contentTypeForPath(cleanPath); ctype != "" {
		header.Set("Content-Type", ctype)
	}

	// 走 ServeContent 而不是 io.Copy：它负责 Range、If-Modified-Since、
	// Content-Length，以及 HEAD 请求不发 body。名字传原始路径只影响它自己的
	// 类型推断，上面已经显式覆盖过。
	http.ServeContent(w, r, cleanPath, modTimeOrZero(stat.ModTime()), seeker)
	return true
}

// contentTypeForPath 按扩展名给出 Content-Type。
func contentTypeForPath(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	}
	return ""
}

// modTimeOrZero 内嵌文件系统的 ModTime 是零值。
//
// 返回零值时 ServeContent 会跳过 Last-Modified 与条件请求处理，这正是我们要的：
// 内嵌资源的内容由文件名里的 hash 决定，用时间戳做缓存判据没有意义，而一个假的
// 时间戳会让 If-Modified-Since 给出错误的 304。
func modTimeOrZero(t time.Time) time.Time {
	return t
}
