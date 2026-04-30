package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"rosie-api/internal/config"
	"rosie-api/internal/store"
)

func newTestServer() http.Handler {
	cfg := config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
	}
	repo := store.NewMemory()
	if _, err := repo.UpsertMerchant(cfg.DefaultMerchant()); err != nil {
		panic(err)
	}
	return NewServer(repo, cfg).Routes()
}

func newTestServerWithConfig(cfg config.Config) http.Handler {
	repo := store.NewMemory()
	if _, err := repo.UpsertMerchant(cfg.DefaultMerchant()); err != nil {
		panic(err)
	}
	return NewServer(repo, cfg).Routes()
}

func TestHealth(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodGet, "/health", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var body map[string]string
	decode(t, response, &body)
	if body["status"] != "ok" {
		t.Fatalf("expected ok health, got %#v", body)
	}
}

func TestSimulatedCallResultCreatesInboxItem(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-call-1",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发，我姓王。",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	summary := body["summary"].(map[string]any)
	if summary["intent"] != "appointment" {
		t.Fatalf("expected appointment summary, got %#v", summary)
	}
	if summary["appointment_time"] != "明天下午三点" {
		t.Fatalf("expected Chinese appointment time, got %#v", summary["appointment_time"])
	}

	inboxResponse := request(t, server, http.MethodGet, "/inbox", nil)
	var inboxBody map[string]any
	decode(t, inboxResponse, &inboxBody)
	items := inboxBody["items"].([]any)
	item := items[0].(map[string]any)
	if item["call_sid"] != "sim-call-1" || item["title"] != "预约意向" {
		t.Fatalf("unexpected inbox item: %#v", item)
	}
}

func TestCallDetailReturnsCallTranscriptSummaryAndInbox(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-call-detail",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发，我姓王。",
	})

	response := request(t, server, http.MethodGet, "/calls/sim-call-detail", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var body map[string]any
	decode(t, response, &body)
	call := body["call"].(map[string]any)
	transcript := body["transcript"].(map[string]any)
	summary := body["summary"].(map[string]any)
	inbox := body["inbox"].(map[string]any)

	if call["call_sid"] != "sim-call-detail" || call["from_number"] != "+8613811112222" {
		t.Fatalf("unexpected call detail: %#v", call)
	}
	if transcript["transcript"] != "你好，我想预约明天下午三点剪头发，我姓王。" {
		t.Fatalf("unexpected transcript: %#v", transcript)
	}
	if summary["intent"] != "appointment" {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if inbox["status"] != "needs_review" {
		t.Fatalf("unexpected inbox: %#v", inbox)
	}
}

func TestCallDetailReturnsNotFoundForUnknownCall(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodGet, "/calls/missing-call", nil)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRealtimeCallResultCreatesFormalBusinessRecords(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-call-1",
		"call_id":     "jambonz-call-1",
		"merchant_id": "demo-merchant",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。\nRosie：好的，请问怎么称呼？",
		"turns": []map[string]any{
			{
				"customer_text": "你好，我想预约明天下午三点剪头发。",
				"agent_reply":   "好的，请问怎么称呼？",
				"stt_source":    "funasr_stt",
				"agent_source":  "ai_agent",
				"tts_source":    "sherpa_onnx_tts",
				"timings_ms":    map[string]int{"stt_ms": 620, "agent_ms": 9, "tts_ms": 260, "total_ms": 900},
			},
		},
		"timings_ms": map[string]int{"total_ms": 900},
		"metadata":   map[string]any{"pipeline": "native"},
	})

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	if body["source"] != "realtime_voice" || body["merchant_id"] != "demo-merchant" {
		t.Fatalf("unexpected realtime response: %#v", body)
	}

	detail := request(t, server, http.MethodGet, "/calls/rt-call-1", nil)
	var detailBody map[string]any
	decode(t, detail, &detailBody)
	call := detailBody["call"].(map[string]any)
	transcript := detailBody["transcript"].(map[string]any)
	summaryItem := detailBody["summary"].(map[string]any)
	if call["call_id"] != "jambonz-call-1" || !contains(call["raw_payload"].(string), "sherpa_onnx_tts") {
		t.Fatalf("unexpected realtime call record: %#v", call)
	}
	if transcript["source"] != "realtime_voice" {
		t.Fatalf("expected realtime transcript source, got %#v", transcript)
	}
	if contains(summaryItem["summary"].(string), "Rosie") {
		t.Fatalf("summary should prefer customer text, got %#v", summaryItem)
	}
}

func TestRealtimeCallResultCanResolveMerchantByAccessNumberAndTurns(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-rt-access",
		"merchant_name": "实时语音测试店",
		"access_number": "+8617000000400",
		"enabled":       true,
	})

	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-call-access",
		"from_number": "+8613811113333",
		"to_number":   "+8617000000400",
		"turns": []map[string]any{
			{"customer_text": "我想预约明天下午三点剪头发", "agent_reply": "可以，请问怎么称呼？"},
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	detail := request(t, server, http.MethodGet, "/calls/rt-call-access", nil)
	var detailBody map[string]any
	decode(t, detail, &detailBody)
	call := detailBody["call"].(map[string]any)
	transcript := detailBody["transcript"].(map[string]any)
	if call["merchant_id"] != "merchant-rt-access" {
		t.Fatalf("expected merchant resolved by access number, got %#v", call)
	}
	if transcript["transcript"] != "客户：我想预约明天下午三点剪头发\nRosie：可以，请问怎么称呼？" {
		t.Fatalf("unexpected transcript from turns: %#v", transcript)
	}
}

func TestRealtimeCallResultQueuesRealtimeNotificationForActionableCall(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-call-notify",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	realtimeNotification := body["realtime_notification"].(map[string]any)
	if realtimeNotification["status"] != "queued" || realtimeNotification["message"] != "realtime_call_result" {
		t.Fatalf("unexpected realtime notification: %#v", realtimeNotification)
	}

	logsResponse := request(t, server, http.MethodGet, "/notification-logs?merchant_id=demo-merchant", nil)
	var logsBody map[string]any
	decode(t, logsResponse, &logsBody)
	logs := logsBody["items"].([]any)
	if len(logs) != 1 {
		t.Fatalf("expected one realtime notification log, got %#v", logsBody)
	}
	log := logs[0].(map[string]any)
	if log["message_type"] != "realtime_call_result" || log["status"] != "queued" {
		t.Fatalf("unexpected realtime notification log: %#v", log)
	}

	duplicate := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-call-notify",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午四点剪头发。",
	})
	var duplicateBody map[string]any
	decode(t, duplicate, &duplicateBody)
	duplicateNotification := duplicateBody["realtime_notification"].(map[string]any)
	if duplicateNotification["id"] != realtimeNotification["id"] {
		t.Fatalf("expected idempotent realtime notification, got %#v and %#v", realtimeNotification, duplicateNotification)
	}
}

func TestRealtimeCallResultSkipsRealtimeNotificationWhenPreferenceDisabled(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPut, "/notification-preferences?merchant_id=demo-merchant", map[string]any{
		"digest_mode":             "daily",
		"digest_times":            []string{"20:00"},
		"realtime_enabled":        false,
		"urgent_realtime_enabled": false,
		"team_wecom_enabled":      false,
		"sms_fallback_enabled":    false,
	})

	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-call-notify-disabled",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	realtimeNotification := body["realtime_notification"].(map[string]any)
	if realtimeNotification["status"] != "skipped_realtime_disabled" {
		t.Fatalf("unexpected realtime notification: %#v", realtimeNotification)
	}
}

func TestWechatLoginBindsOpenIDToMerchant(t *testing.T) {
	wechatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sns/jscode2session" {
			t.Fatalf("unexpected wechat path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("js_code") != "login-code" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"openid":      "openid-bound",
			"session_key": "session-key",
			"unionid":     "unionid-bound",
		})
	}))
	defer wechatServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		WeChat: config.WeChatConfig{
			APIBaseURL: wechatServer.URL,
			AppID:      "app-id",
			AppSecret:  "secret",
		},
	})

	response := request(t, server, http.MethodPost, "/auth/wechat-login", map[string]any{
		"code":        "login-code",
		"merchant_id": "demo-merchant",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var body map[string]any
	decode(t, response, &body)
	if body["openid"] != "openid-bound" || body["merchant_id"] != "demo-merchant" {
		t.Fatalf("unexpected login response: %#v", body)
	}
	user := body["user"].(map[string]any)
	if _, ok := user["session_key"]; ok {
		t.Fatalf("session_key must not be returned: %#v", user)
	}
}

func TestMerchantProfileCanBeUpdatedAndReturnsPrompt(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPut, "/merchant-profile?merchant_id=demo-merchant", map[string]any{
		"merchant_name":     "小王理发店",
		"access_number":     "8613736849910",
		"original_number":   "+8613811110000",
		"transfer_phone":    "+8613811119999",
		"industry":          "hair_salon",
		"address":           "人民路 18 号",
		"business_hours":    "10:00-21:00",
		"services":          []string{"剪发", "烫染", "护理"},
		"appointment_rules": "预约需留下称呼、电话和到店时间。",
		"faq_items": []map[string]string{
			{"question": "几点营业", "answer": "每天 10:00 到 21:00"},
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var body map[string]any
	decode(t, response, &body)
	merchant := body["merchant"].(map[string]any)
	profile := body["profile"].(map[string]any)
	template := body["template"].(map[string]any)
	prompt := body["system_prompt"].(string)

	if merchant["merchant_name"] != "小王理发店" || merchant["transfer_phone"] != "+8613811119999" {
		t.Fatalf("unexpected merchant: %#v", merchant)
	}
	if profile["industry"] != "hair_salon" || template["key"] != "hair_salon" {
		t.Fatalf("unexpected profile/template: %#v %#v", profile, template)
	}
	if !contains(prompt, "小王理发店") || !contains(prompt, "剪发、烫染、护理") {
		t.Fatalf("prompt does not include merchant context: %s", prompt)
	}

	getResponse := request(t, server, http.MethodGet, "/merchant-profile?merchant_id=demo-merchant", nil)
	var getBody map[string]any
	decode(t, getResponse, &getBody)
	getProfile := getBody["profile"].(map[string]any)
	if getProfile["business_hours"] != "10:00-21:00" {
		t.Fatalf("profile was not persisted: %#v", getProfile)
	}
}

func TestListIndustryTemplates(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodGet, "/industry-templates", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	items := body["items"].([]any)
	if len(items) < 2 {
		t.Fatalf("expected at least two templates, got %#v", body)
	}
}

func TestSimulatedCallResultIsIdempotentByCallSID(t *testing.T) {
	server := newTestServer()
	payload := map[string]any{
		"call_sid":    "sim-call-idempotent",
		"from_number": "+8613811115555",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发。",
	}
	first := request(t, server, http.MethodPost, "/simulate/call-result", payload)
	secondPayload := map[string]any{
		"call_sid":    "sim-call-idempotent",
		"from_number": "+8613811115555",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午四点剪头发。",
	}
	second := request(t, server, http.MethodPost, "/simulate/call-result", secondPayload)

	var firstBody map[string]any
	var secondBody map[string]any
	decode(t, first, &firstBody)
	decode(t, second, &secondBody)
	if firstBody["inbox_item_id"] != secondBody["inbox_item_id"] {
		t.Fatalf("expected same inbox item id, got %#v and %#v", firstBody["inbox_item_id"], secondBody["inbox_item_id"])
	}
}

func TestDigestTickQueuesNotificationAndIsIdempotent(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-digest-tick",
		"merchant_name": "定时汇总测试店",
		"access_number": "+8617000000200",
		"enabled":       true,
	})
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-call-digest-tick",
		"from_number": "+8613811117777",
		"to_number":   "+8617000000200",
		"transcript":  "你好，我想预约明天下午三点剪头发。",
	})

	response := request(t, server, http.MethodPost, "/internal/digest-tick?now=2026-04-30T20:00:00", nil)
	var body map[string]any
	decode(t, response, &body)
	result := findTickResult(t, body, "merchant-digest-tick")
	if result["status"] != "queued" || result["total"] != float64(1) {
		t.Fatalf("unexpected tick result: %#v", result)
	}

	duplicate := request(t, server, http.MethodPost, "/internal/digest-tick?now=2026-04-30T20:00:00", nil)
	var duplicateBody map[string]any
	decode(t, duplicate, &duplicateBody)
	duplicateResult := findTickResult(t, duplicateBody, "merchant-digest-tick")
	if duplicateResult["status"] != "duplicate" {
		t.Fatalf("expected duplicate tick result, got %#v", duplicateResult)
	}

	logsResponse := request(t, server, http.MethodGet, "/notification-logs?merchant_id=merchant-digest-tick", nil)
	var logsBody map[string]any
	decode(t, logsResponse, &logsBody)
	logs := logsBody["items"].([]any)
	if len(logs) != 1 {
		t.Fatalf("expected one notification log, got %d", len(logs))
	}
	log := logs[0].(map[string]any)
	if log["status"] != "queued" || log["channel"] != "wechat_subscription" {
		t.Fatalf("unexpected log: %#v", log)
	}
}

func TestDispatchNotificationsSendsQueuedWechatMessage(t *testing.T) {
	sentTo := ""
	wechatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sns/jscode2session":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"openid":      "openid-bound",
				"session_key": "session-key",
			})
		case "/cgi-bin/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 7200})
		case "/cgi-bin/message/subscribe/send":
			if r.URL.Query().Get("access_token") != "token" {
				t.Fatalf("unexpected access token: %s", r.URL.RawQuery)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			sentTo = payload["touser"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
		default:
			t.Fatalf("unexpected wechat path: %s", r.URL.Path)
		}
	}))
	defer wechatServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		WeChat: config.WeChatConfig{
			APIBaseURL:       wechatServer.URL,
			AppID:            "app-id",
			AppSecret:        "secret",
			TemplateID:       "template-id",
			Page:             "pages/inbox/index",
			MiniprogramState: "trial",
			Lang:             "zh_CN",
			TitleKey:         "thing1",
			SummaryKey:       "thing2",
			TimeKey:          "time3",
		},
	})
	request(t, server, http.MethodPost, "/auth/wechat-login", map[string]any{
		"code":        "login-code",
		"merchant_id": "demo-merchant",
	})
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-call-dispatch",
		"from_number": "+8613811117777",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发。",
	})
	request(t, server, http.MethodPost, "/internal/digest-tick?now=2026-04-30T20:00:00", nil)

	response := request(t, server, http.MethodPost, "/internal/notifications/dispatch", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	if body["total"] != float64(1) {
		t.Fatalf("expected one dispatched notification, got %#v", body)
	}
	if sentTo != "openid-bound" {
		t.Fatalf("expected dispatch to bound openid, got %s", sentTo)
	}

	logsResponse := request(t, server, http.MethodGet, "/notification-logs?status=sent", nil)
	var logsBody map[string]any
	decode(t, logsResponse, &logsBody)
	logs := logsBody["items"].([]any)
	if len(logs) != 1 {
		t.Fatalf("expected one sent log, got %#v", logsBody)
	}
	log := logs[0].(map[string]any)
	if log["status"] != "sent" || log["attempt_count"] != float64(1) {
		t.Fatalf("unexpected sent log: %#v", log)
	}
}

func TestDispatchNotificationsCanTargetRealtimeNotificationByIdempotencyKey(t *testing.T) {
	sentTo := ""
	wechatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 7200})
		case "/cgi-bin/message/subscribe/send":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			sentTo = payload["touser"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok"})
		default:
			t.Fatalf("unexpected wechat path: %s", r.URL.Path)
		}
	}))
	defer wechatServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		WeChat: config.WeChatConfig{
			APIBaseURL:    wechatServer.URL,
			AppID:         "app-id",
			AppSecret:     "secret",
			TemplateID:    "template-id",
			DefaultOpenID: "openid-default",
			TitleKey:      "thing1",
			SummaryKey:    "thing2",
			TimeKey:       "time3",
		},
	})

	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-dispatch-key",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})
	var body map[string]any
	decode(t, response, &body)
	notification := body["realtime_notification"].(map[string]any)
	key := notification["idempotency_key"].(string)

	dispatch := request(t, server, http.MethodPost, "/internal/notifications/dispatch?idempotency_key="+key, nil)
	if dispatch.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", dispatch.Code, dispatch.Body.String())
	}
	if sentTo != "openid-default" {
		t.Fatalf("expected dispatch to default openid, got %s", sentTo)
	}
	var dispatchBody map[string]any
	decode(t, dispatch, &dispatchBody)
	if dispatchBody["total"] != float64(1) {
		t.Fatalf("expected targeted dispatch, got %#v", dispatchBody)
	}
	result := dispatchBody["results"].([]any)[0].(map[string]any)
	if result["status"] != "sent" || result["attempt_count"] != float64(1) {
		t.Fatalf("unexpected targeted dispatch result: %#v", result)
	}
}

func TestRejectsInvalidNotificationDigestTime(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPut, "/notification-preferences", map[string]any{
		"digest_mode":             "daily",
		"digest_times":            []string{"25:00"},
		"realtime_enabled":        false,
		"urgent_realtime_enabled": true,
		"team_wecom_enabled":      false,
		"sms_fallback_enabled":    false,
	})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func request(t *testing.T, server http.Handler, method string, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	return recorder
}

func decode(t *testing.T, response *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

func findTickResult(t *testing.T, body map[string]any, merchantID string) map[string]any {
	t.Helper()
	results := body["results"].([]any)
	for _, item := range results {
		result := item.(map[string]any)
		if result["merchant_id"] == merchantID {
			return result
		}
	}
	t.Fatalf("missing tick result for %s in %#v", merchantID, body)
	return nil
}

func contains(value string, fragment string) bool {
	return len(fragment) == 0 || (len(value) >= len(fragment) && stringContains(value, fragment))
}

func stringContains(value string, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
