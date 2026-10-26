package application

import (
	"strings"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

// Channels a visit arrives through (Change 35, docs/SPEC.md §4i). The first matching rule of
// classify decides; codes are stored, the UI names them.
const (
	ChannelPaidSearch    = "paid_search"
	ChannelOrganicSearch = "organic_search"
	ChannelPaidSocial    = "paid_social"
	ChannelSocial        = "social"
	ChannelMessengers    = "messengers"
	ChannelAI            = "ai_assistants"
	ChannelEmail         = "email"
	ChannelReferral      = "referral"
	ChannelDirect        = "direct"
)

type sourceKind int

const (
	kindOther sourceKind = iota
	kindSearch
	kindSocial
	kindMessenger
	kindAI
	kindEmail
)

type knownSource struct {
	// host is matched as the host itself or any subdomain of it; a trailing "." matches any
	// top-level domain ("google." is google.com, google.ru, …).
	host string
	name string
	kind sourceKind
}

// knownSources is ordered most specific first: an assistant, a mail client or Дзен on a search
// engine's domain (alice.yandex.ru, gemini.google.com, mail.yandex.ru, zen.yandex.ru) precedes
// that engine.
var knownSources = []knownSource{
	{"alice.yandex.ru", "Алиса", kindAI},
	{"gemini.google.com", "Gemini", kindAI},
	{"chatgpt.com", "ChatGPT", kindAI},
	{"chat.openai.com", "ChatGPT", kindAI},
	{"perplexity.ai", "Perplexity", kindAI},
	{"claude.ai", "Claude", kindAI},
	{"copilot.microsoft.com", "Copilot", kindAI},
	{"chat.deepseek.com", "DeepSeek", kindAI},
	{"giga.chat", "GigaChat", kindAI},
	{"zen.yandex.ru", "Дзен", kindSocial},
	{"mail.yandex.ru", "Яндекс Почта", kindEmail},
	{"e.mail.ru", "Почта Mail", kindEmail},
	{"mail.google.com", "Gmail", kindEmail},
	{"go.mail.ru", "Поиск Mail", kindSearch},
	{"yandex.", "Яндекс", kindSearch},
	{"ya.ru", "Яндекс", kindSearch},
	{"google.", "Google", kindSearch},
	{"bing.com", "Bing", kindSearch},
	{"duckduckgo.com", "DuckDuckGo", kindSearch},
	{"search.yahoo.com", "Yahoo", kindSearch},
	{"nova.rambler.ru", "Рамблер", kindSearch},
	{"vk.com", "VK", kindSocial},
	{"vk.ru", "VK", kindSocial},
	{"ok.ru", "Одноклассники", kindSocial},
	{"dzen.ru", "Дзен", kindSocial},
	{"youtube.com", "YouTube", kindSocial},
	{"youtu.be", "YouTube", kindSocial},
	{"tiktok.com", "TikTok", kindSocial},
	{"pikabu.ru", "Пикабу", kindSocial},
	{"rutube.ru", "Rutube", kindSocial},
	{"t.me", "Telegram", kindMessenger},
	{"telegram.org", "Telegram", kindMessenger},
	{"web.whatsapp.com", "WhatsApp", kindMessenger},
	{"wa.me", "WhatsApp", kindMessenger},
	{"max.ru", "MAX", kindMessenger},
}

// utmSources names common utm_source spellings the way referrers are named.
var utmSources = map[string]knownSource{
	"yandex":    {name: "Яндекс", kind: kindSearch},
	"google":    {name: "Google", kind: kindSearch},
	"vk":        {name: "VK", kind: kindSocial},
	"vkontakte": {name: "VK", kind: kindSocial},
	"ok":        {name: "Одноклассники", kind: kindSocial},
	"dzen":      {name: "Дзен", kind: kindSocial},
	"youtube":   {name: "YouTube", kind: kindSocial},
	"telegram":  {name: "Telegram", kind: kindMessenger},
	"tg":        {name: "Telegram", kind: kindMessenger},
	"whatsapp":  {name: "WhatsApp", kind: kindMessenger},
	"max":       {name: "MAX", kind: kindMessenger},
	"chatgpt":   {name: "ChatGPT", kind: kindAI},
	"email":     {name: "Email", kind: kindEmail},
}

var paidMedia = map[string]bool{"cpc": true, "ppc": true, "paid": true, "paidsearch": true, "paid_social": true, "paidsocial": true, "cpm": true}

func lookupHost(host string) (knownSource, bool) {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	for _, k := range knownSources {
		if strings.HasSuffix(k.host, ".") {
			// "google." — the label itself or a subdomain of it, with any top-level domain
			label := strings.TrimSuffix(k.host, ".")
			if strings.HasPrefix(host, label+".") || strings.Contains(host, "."+label+".") {
				return k, true
			}
			continue
		}
		if host == k.host || strings.HasSuffix(host, "."+k.host) {
			return k, true
		}
	}
	return knownSource{}, false
}

// classify names a visit's source and channel from its external referrer host, UTM parameters
// and paid-click flag (Change 35). A referrer wins over utm_source for the name; a referrer the
// dictionary does not know is named by its host.
func classify(referrerHost string, utm domain.UTM, paid bool) (source, channel string) {
	known, isKnown := lookupHost(referrerHost)
	if !isKnown && referrerHost == "" && utm.Source != "" && utm.Source != domain.InvalidUTM {
		known, isKnown = utmSources[utm.Source]
	}
	switch {
	case isKnown:
		source = known.name
	case referrerHost != "":
		source = strings.TrimPrefix(strings.ToLower(referrerHost), "www.")
	case utm.Source != "":
		source = utm.Source
	}
	paidMedium := paidMedia[utm.Medium]
	switch {
	case paid || (paidMedium && known.kind == kindSearch):
		channel = ChannelPaidSearch
	case paidMedium && known.kind == kindSocial:
		channel = ChannelPaidSocial
	case known.kind == kindSearch || utm.Medium == "organic":
		channel = ChannelOrganicSearch
	case known.kind == kindSocial || utm.Medium == "social":
		channel = ChannelSocial
	case known.kind == kindMessenger || utm.Medium == "messenger":
		channel = ChannelMessengers
	case known.kind == kindAI:
		channel = ChannelAI
	case known.kind == kindEmail || utm.Medium == "email":
		channel = ChannelEmail
	case referrerHost != "" || !utm.Empty():
		channel = ChannelReferral
	default:
		channel = ChannelDirect
	}
	return source, channel
}
