//go:build embed

package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 端到端：真的走一遍 ServeEmbeddedFrontend，确认压缩生效且内容正确。
func TestPrecompressedServingEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ServeEmbeddedFrontend())

	// 找一个真实存在的 hash 化 JS 资源
	distFS, _ := frontendFS.ReadDir("dist/assets")
	var target string
	for _, e := range distFS {
		if strings.HasSuffix(e.Name(), ".js") && !strings.HasSuffix(e.Name(), ".gz") {
			if info, err := e.Info(); err == nil && info.Size() > 50000 {
				target = "/assets/" + e.Name()
				break
			}
		}
	}
	if target == "" {
		t.Skip("没找到合适的测试资源")
	}

	// 1) 支持 gzip 的客户端应拿到压缩内容，且解压后与原文一致
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d", w.Code)
	}
	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("期望 Content-Encoding: gzip，实际 %q —— 压缩没生效", enc)
	}
	if v := w.Header().Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Errorf("缺少 Vary: Accept-Encoding，中间缓存会把压缩版发给不支持的客户端")
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type 是 %q —— .gz 会被推断成 application/gzip，浏览器会当下载而不执行", ct)
	}

	compressed := w.Body.Bytes()
	zr, err := gzip.NewReader(strings.NewReader(string(compressed)))
	if err != nil {
		t.Fatalf("响应不是合法 gzip: %v", err)
	}
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}

	// 2) 不支持 gzip 的客户端拿到原文，且两者必须逐字节相同
	req2 := httptest.NewRequest(http.MethodGet, target, nil)
	req2.Header.Set("Accept-Encoding", "identity")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if enc := w2.Header().Get("Content-Encoding"); enc == "gzip" {
		t.Fatalf("客户端明确要 identity，却发了 gzip")
	}
	if string(decompressed) != w2.Body.String() {
		t.Fatalf("解压后内容与原文不一致 —— 压缩损坏了资源")
	}

	t.Logf("%s: 原文 %d 字节 → 压缩 %d 字节（省 %d%%），解压后与原文逐字节一致",
		target, len(w2.Body.Bytes()), len(compressed),
		(len(w2.Body.Bytes())-len(compressed))*100/len(w2.Body.Bytes()))
}
