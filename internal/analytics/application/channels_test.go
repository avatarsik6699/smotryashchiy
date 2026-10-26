package application

import (
	"testing"

	"github.com/avatarsik6699/smotryashchiy/internal/analytics/domain"
)

// Change 35: the first matching rule names the channel; a known referrer names the source.
func TestClassify(t *testing.T) {
	cases := []struct {
		name                string
		ref                 string
		utm                 domain.UTM
		paid                bool
		wantSource, wantChn string
	}{
		{"direct", "", domain.UTM{}, false, "", ChannelDirect},
		{"yandex search", "yandex.ru", domain.UTM{}, false, "Яндекс", ChannelOrganicSearch},
		{"ya.ru", "ya.ru", domain.UTM{}, false, "Яндекс", ChannelOrganicSearch},
		{"google any tld", "www.google.com.tr", domain.UTM{}, false, "Google", ChannelOrganicSearch},
		{"mail search", "go.mail.ru", domain.UTM{}, false, "Поиск Mail", ChannelOrganicSearch},
		{"yclid", "yandex.ru", domain.UTM{}, true, "Яндекс", ChannelPaidSearch},
		{"cpc on google", "google.ru", domain.UTM{Medium: "cpc"}, false, "Google", ChannelPaidSearch},
		{"vk", "vk.com", domain.UTM{}, false, "VK", ChannelSocial},
		{"m.vk", "m.vk.com", domain.UTM{}, false, "VK", ChannelSocial},
		{"vk ads", "vk.com", domain.UTM{Medium: "cpc"}, false, "VK", ChannelPaidSocial},
		{"dzen on yandex domain", "zen.yandex.ru", domain.UTM{}, false, "Дзен", ChannelSocial},
		{"telegram", "t.me", domain.UTM{}, false, "Telegram", ChannelMessengers},
		{"telegram by utm only", "", domain.UTM{Source: "telegram"}, false, "Telegram", ChannelMessengers},
		{"alice before yandex", "alice.yandex.ru", domain.UTM{}, false, "Алиса", ChannelAI},
		{"gemini before google", "gemini.google.com", domain.UTM{}, false, "Gemini", ChannelAI},
		{"chatgpt", "chatgpt.com", domain.UTM{}, false, "ChatGPT", ChannelAI},
		{"yandex mail", "mail.yandex.ru", domain.UTM{}, false, "Яндекс Почта", ChannelEmail},
		{"email medium", "", domain.UTM{Source: "newsletter", Medium: "email"}, false, "newsletter", ChannelEmail},
		{"unknown referrer", "www.example.org", domain.UTM{}, false, "example.org", ChannelReferral},
		{"unknown utm", "", domain.UTM{Source: "partner"}, false, "partner", ChannelReferral},
		{"googleusercontent is not google", "googleusercontent.com", domain.UTM{}, false, "googleusercontent.com", ChannelReferral},
	}
	for _, c := range cases {
		source, channel := classify(c.ref, c.utm, c.paid)
		if source != c.wantSource || channel != c.wantChn {
			t.Errorf("%s: classify(%q, %+v, %v) = %q, %q; want %q, %q", c.name, c.ref, c.utm, c.paid, source, channel, c.wantSource, c.wantChn)
		}
	}
}
