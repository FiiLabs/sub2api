package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// PublicBannerHandler 首页活动横幅（无鉴权）。
//
// 单独一个端点而不是塞进 /settings/public：那个接口是上游的一张扁平 key-value 表，
// 每加一个字段都要同时改 service 的 key 清单与 DTO，而横幅是我们自己的东西、
// 会随活动反复增删。挂在自己的路径上，改动面就只在本文件与 setting_homepage_banner.go。
type PublicBannerHandler struct {
	settingService *service.SettingService
}

// NewPublicBannerHandler 创建横幅 handler。
func NewPublicBannerHandler(settingService *service.SettingService) *PublicBannerHandler {
	return &PublicBannerHandler{settingService: settingService}
}

// PublicBannerResponse 是横幅的对外形态。
//
// **已经按访客语言挑好了**：只回一份 text/cta_text，而不是把中英两份都发出去让前端选。
// 两个理由——首页在未登录时也要渲染，前端此刻未必已经初始化好 i18n；以及运营只填了
// 一种语言时的回退规则只应该有一个落点（service 的 TextFor），前端再实现一遍必然漂移。
type PublicBannerResponse struct {
	// Enabled 关着时其余字段一律为空，前端只看这一个布尔。
	Enabled bool   `json:"enabled"`
	Text    string `json:"text"`
	CTAText string `json:"cta_text"`
	CTAURL  string `json:"cta_url"`
	Variant string `json:"variant"`
}

// GetBanner 返回首页横幅
// GET /api/v1/home/banner
//
// 不返回错误：这是首页上的一块营销位，读不到就当没有。让首页因为横幅读失败而报错，
// 是把一个装饰件提升成了关键路径。
func (h *PublicBannerHandler) GetBanner(c *gin.Context) {
	if h.settingService == nil {
		response.Success(c, &PublicBannerResponse{Enabled: false})
		return
	}

	settings := h.settingService.GetHomepageBannerSettings(c.Request.Context())
	if !settings.Active() {
		response.Success(c, &PublicBannerResponse{Enabled: false})
		return
	}

	// 语言优先看显式的 ?lang=，再回退到 Accept-Language。
	// 显式参数优先是因为站内的语言切换是前端状态，浏览器的 Accept-Language 不会跟着变——
	// 一个把界面切成英文的中文浏览器用户，应该看到英文横幅。
	lang := c.Query("lang")
	if lang == "" {
		lang = c.GetHeader("Accept-Language")
	}

	response.Success(c, &PublicBannerResponse{
		Enabled: true,
		Text:    settings.TextFor(lang),
		CTAText: settings.CTATextFor(lang),
		CTAURL:  settings.CTAURLFor(lang),
		Variant: settings.Variant,
	})
}
