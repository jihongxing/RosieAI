package wechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosie-api/internal/config"
	"rosie-api/internal/domain"
)

func TestSendSubscribeCallsWeChatAPI(t *testing.T) {
	var sentPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			if r.URL.Query().Get("appid") != "app-id" || r.URL.Query().Get("secret") != "secret" {
				t.Fatalf("unexpected token query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 7200})
		case "/cgi-bin/message/subscribe/send":
			if r.URL.Query().Get("access_token") != "token" {
				t.Fatalf("unexpected access token: %s", r.URL.RawQuery)
			}
			if err := json.NewDecoder(r.Body).Decode(&sentPayload); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(config.WeChatConfig{
		APIBaseURL:       server.URL,
		AppID:            "app-id",
		AppSecret:        "secret",
		TemplateID:       "template",
		Page:             "pages/inbox/index",
		MiniprogramState: "trial",
		Lang:             "zh_CN",
		TitleKey:         "thing1",
		SummaryKey:       "thing2",
		TimeKey:          "time3",
	}, server.Client())

	err := client.SendSubscribe(context.Background(), domain.NotificationLog{
		Subject: "Rosie 汇总",
		Body:    "今天有一条预约意向，需要老板处理。",
	}, "openid-1")
	if err != nil {
		t.Fatal(err)
	}
	if sentPayload["touser"] != "openid-1" || sentPayload["template_id"] != "template" {
		t.Fatalf("unexpected send payload: %#v", sentPayload)
	}
	data := sentPayload["data"].(map[string]any)
	if data["thing1"] == nil || data["thing2"] == nil || data["time3"] == nil {
		t.Fatalf("missing template data: %#v", data)
	}
}

func TestCodeToSessionCallsWeChatAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sns/jscode2session" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("js_code") != "login-code" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openid":      "openid-1",
			"session_key": "session-key",
			"unionid":     "unionid-1",
		})
	}))
	defer server.Close()

	client := NewClient(config.WeChatConfig{
		APIBaseURL: server.URL,
		AppID:      "app-id",
		AppSecret:  "secret",
	}, server.Client())

	session, err := client.CodeToSession(context.Background(), "login-code")
	if err != nil {
		t.Fatal(err)
	}
	if session.OpenID != "openid-1" || session.SessionKey != "session-key" || session.UnionID != "unionid-1" {
		t.Fatalf("unexpected session: %#v", session)
	}
}
