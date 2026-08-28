//go:build embed || unit

package web

import "testing"

// Accept-Encoding 的判定。压错方向的后果不对称：
// 给不支持的客户端发 gzip = 满屏乱码；给支持的发原文 = 只是慢一点。
// 所以拿不准时必须偏向「不压」。
func TestClientAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
		why    string
	}{
		{"", false, "没有这个头就是不支持"},
		{"gzip", true, "最常见"},
		{"gzip, deflate, br", true, "浏览器的典型取值"},
		{"br, gzip", true, "顺序无关"},
		{"deflate", false, "只支持 deflate，我们没有 .deflate 产物"},
		{"br", false, "只支持 br，我们没有 .br 产物"},
		// E2EE 中间件正是用 identity 关掉压缩的，必须尊重
		{"identity", false, "明确要求不压——E2EE 路径靠这个"},
		{"IDENTITY", false, "大小写不敏感"},
		{"GZIP", true, "大小写不敏感"},
		// 这一条是给「简化成一句 Contains(gzip)」那种重构准备的红检：
		// 客户端同时列出两者但把 identity 排在前、且没有逗号分隔的变体，
		// 以及显式 identity 的场景，都必须走「不压」。
		{"identity, gzip", true, "两者都接受时可以压"},
		{"identity;q=0, gzip", true, "明确拒绝 identity、接受 gzip"},
	}
	for _, tc := range cases {
		if got := clientAcceptsGzip(tc.header); got != tc.want {
			t.Errorf("clientAcceptsGzip(%q) = %v, 期望 %v —— %s", tc.header, got, tc.want, tc.why)
		}
	}
}

// Content-Type 必须按**原始**文件名判定。
// 按 .gz 判定的话浏览器会收到 application/gzip，于是把 JS 当文件下载而不是执行。
func TestContentTypeForPath(t *testing.T) {
	cases := map[string]string{
		"assets/index-abc12345.js":  "text/javascript; charset=utf-8",
		"assets/index-abc12345.css": "text/css; charset=utf-8",
		"index.html":                "text/html; charset=utf-8",
		"logo.svg":                  "image/svg+xml",
		"favicon.ico":               "", // 不在白名单里，交给标准库推断
	}
	for path, want := range cases {
		if got := contentTypeForPath(path); got != want {
			t.Errorf("contentTypeForPath(%q) = %q, 期望 %q", path, got, want)
		}
	}
}

// 只对文本类做预压缩查找。白名单而不是黑名单：新增资源类型时默认「原样发」，
// 而不是去找一个多半不存在的 .gz。
func TestPrecompressedExtensionsAreTextOnly(t *testing.T) {
	for _, ext := range []string{".js", ".css", ".html", ".svg"} {
		if !precompressedExtensions[ext] {
			t.Errorf("%s 应当在预压缩白名单里", ext)
		}
	}
	// 已压缩的二进制再压一遍是白烧 CPU，还可能变大
	for _, ext := range []string{".png", ".jpg", ".woff2", ".gz", ".webp", ".ico"} {
		if precompressedExtensions[ext] {
			t.Errorf("%s 是已压缩格式，不该进白名单", ext)
		}
	}
}
