package httpapi

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

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

func TestHealthDepsReportsMemoryStoreAndQueues(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodGet, "/health/deps", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var body map[string]any
	decode(t, response, &body)
	if body["status"] != "ok" {
		t.Fatalf("expected ok deps health, got %#v", body)
	}
	deps := body["dependencies"].(map[string]any)
	database := deps["database"].(map[string]any)
	aiSummary := deps["ai_summary"].(map[string]any)
	wechat := deps["wechat"].(map[string]any)
	queues := body["queues"].(map[string]any)
	if database["kind"] != "memory" || database["status"] != "ok" {
		t.Fatalf("unexpected database health: %#v", database)
	}
	if aiSummary["status"] != "disabled" {
		t.Fatalf("unexpected ai summary health: %#v", aiSummary)
	}
	if wechat["status"] != "missing_config" {
		t.Fatalf("unexpected wechat health: %#v", wechat)
	}
	if queues["failed_business_result_retries"] != float64(0) {
		t.Fatalf("unexpected queue health: %#v", queues)
	}
}

func TestHealthDepsChecksAIAgentWhenConfigured(t *testing.T) {
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected ai-agent path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "ok",
			"provider": "local_fallback",
			"model":    "local-fallback",
		})
	}))
	defer agentServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		AIAgentURL:          agentServer.URL,
		AISummaryEnabled:    true,
	})
	response := request(t, server, http.MethodGet, "/health/deps", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	aiSummary := body["dependencies"].(map[string]any)["ai_summary"].(map[string]any)
	if aiSummary["status"] != "ok" || aiSummary["provider"] != "local_fallback" {
		t.Fatalf("unexpected ai summary health: %#v", aiSummary)
	}
}

func TestHealthDepsReportsDegradedAIAgent(t *testing.T) {
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer agentServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		AIAgentURL:          agentServer.URL,
		AISummaryEnabled:    true,
	})
	response := request(t, server, http.MethodGet, "/health/deps", nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	aiSummary := body["dependencies"].(map[string]any)["ai_summary"].(map[string]any)
	if body["status"] != "degraded" || aiSummary["status"] != "error" {
		t.Fatalf("unexpected degraded health: %#v", body)
	}
}

func TestValueMetricsSummarizesPilotValue(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "metrics-appointment",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发，电话是13811112222。",
	})
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "metrics-spam",
		"from_number": "+8613000000000",
		"to_number":   "8613736849910",
		"transcript":  "客户：我们可以代开发票和推广贷款。",
	})
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "metrics-urgent",
		"from_number": "+8613811113333",
		"to_number":   "8613736849910",
		"transcript":  "客户：有个投诉，麻烦尽快回电。",
	})
	request(t, server, http.MethodPatch, "/calls/metrics-appointment/inbox-item", map[string]any{
		"status": "handled",
	})
	created := request(t, server, http.MethodPost, "/calls/metrics-appointment/callback-requests", map[string]any{
		"target_number": "+8613811112222",
	})
	var createdBody map[string]any
	decode(t, created, &createdBody)
	callbackID := int(createdBody["callback_request"].(map[string]any)["id"].(float64))
	request(t, server, http.MethodPatch, "/calls/metrics-appointment/callback-requests/"+strconv.Itoa(callbackID), map[string]any{
		"status": "dialed",
	})

	response := request(t, server, http.MethodGet, "/value-metrics?merchant_id=demo-merchant&since=2000-01-01T00:00:00Z&until=2100-01-01T00:00:00Z", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	if body["period"] != "custom" ||
		body["total_calls"] != float64(3) ||
		body["effective_calls"] != float64(2) ||
		body["appointment_count"] != float64(1) ||
		body["spam_count"] != float64(1) ||
		body["handled_count"] != float64(1) ||
		body["callback_requested_count"] != float64(1) ||
		body["callback_dialed_count"] != float64(1) {
		t.Fatalf("unexpected value metrics: %#v", body)
	}
	if body["estimated_saved_minutes"].(float64) <= 0 {
		t.Fatalf("expected positive saved minutes: %#v", body)
	}
}

func TestServiceStatusReturnsPlanValueAndOnboarding(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "service-status-call",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})

	response := request(t, server, http.MethodGet, "/service-status?merchant_id=demo-merchant", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	subscription := body["subscription"].(map[string]any)
	plan := body["plan"].(map[string]any)
	metrics := body["metrics"].(map[string]any)
	steps := body["onboarding_steps"].([]any)
	instructions := body["call_forwarding_instructions"].([]any)
	if subscription["status"] != "not_started" || plan["price_text"] != "30 元/月" {
		t.Fatalf("unexpected service plan/status: %#v", body)
	}
	if _, ok := metrics["total_calls"]; !ok || len(steps) == 0 || len(instructions) != 3 {
		t.Fatalf("unexpected service value payload: %#v", body)
	}
}

func TestAdminPilotsDashboardAggregatesPilotState(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "admin-pilot-call",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})
	request(t, server, http.MethodPost, "/service-status/activate-trial?merchant_id=demo-merchant", map[string]any{
		"plan_code": "pilot_basic",
	})
	create := request(t, server, http.MethodPost, "/payment-orders?merchant_id=demo-merchant", map[string]any{
		"order_type": "renewal",
		"plan_code":  "pilot_basic",
	})
	var createBody map[string]any
	decode(t, create, &createBody)
	orderNo := createBody["order"].(map[string]any)["order_no"].(string)
	request(t, server, http.MethodPost, "/internal/payment-orders/"+orderNo+"/mark-paid", map[string]any{
		"provider_trade_no": "wx-admin-pilot",
		"paid_at":           "2026-04-30T12:00:00Z",
	})
	request(t, server, http.MethodPost, "/internal/realtime-call-result-retries", map[string]any{
		"session_id": "admin-retry",
		"payload": map[string]any{
			"call_sid":    "admin-retry-call",
			"merchant_id": "demo-merchant",
			"transcript":  "客户：请帮我记录一个问题。",
		},
		"last_error": "business api unavailable",
	})

	page := request(t, server, http.MethodGet, "/admin", nil)
	if page.Code != http.StatusOK || !contains(page.Body.String(), "Rosie 运营后台") ||
		!contains(page.Body.String(), "商家列表") ||
		!contains(page.Body.String(), "号码路由校验") ||
		!contains(page.Body.String(), "同步 jambonz") {
		t.Fatalf("unexpected admin page: %d %s", page.Code, page.Body.String())
	}

	response := request(t, server, http.MethodGet, "/admin/pilots", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	items := body["items"].([]any)
	overview := body["overview"].(map[string]any)
	if len(items) != 1 {
		t.Fatalf("expected one pilot, got %#v", body)
	}
	if overview["total"] != float64(1) || overview["needs_attention"] != float64(1) {
		t.Fatalf("unexpected admin overview: %#v", overview)
	}
	item := items[0].(map[string]any)
	if item["status"] != "needs_attention" {
		t.Fatalf("expected failed retry to require attention, got %#v", item)
	}
	metrics := item["metrics"].(map[string]any)
	payment := item["payment"].(map[string]any)
	retry := item["business_result_retry"].(map[string]any)
	if metrics["total_calls"] != float64(1) || payment["paid_order_count"] != float64(1) ||
		retry["failed_count"] != float64(1) {
		t.Fatalf("unexpected admin pilot metrics: %#v", item)
	}

	detail := request(t, server, http.MethodGet, "/admin/pilots/demo-merchant", nil)
	var detailBody map[string]any
	decode(t, detail, &detailBody)
	if detailBody["recent_orders"] == nil || detailBody["recent_failed_retries"] == nil {
		t.Fatalf("expected detail collections, got %#v", detailBody)
	}

	filtered := request(t, server, http.MethodGet, "/admin/pilots?status=healthy&q=missing", nil)
	var filteredBody map[string]any
	decode(t, filtered, &filteredBody)
	if len(filteredBody["items"].([]any)) != 0 {
		t.Fatalf("expected filters to hide pilot, got %#v", filteredBody)
	}

	flush := request(t, server, http.MethodPost, "/admin/pilots/demo-merchant/flush-retries", map[string]any{
		"max_attempts": 5,
		"limit":        20,
	})
	if flush.Code != http.StatusOK {
		t.Fatalf("expected admin retry flush 200, got %d: %s", flush.Code, flush.Body.String())
	}
	var flushBody map[string]any
	decode(t, flush, &flushBody)
	if flushBody["total"] != float64(1) {
		t.Fatalf("expected one flushed retry, got %#v", flushBody)
	}
	pilotAfterFlush := flushBody["pilot"].(map[string]any)
	if pilotAfterFlush["status"] != "healthy" {
		t.Fatalf("expected pilot to recover after retry flush, got %#v", pilotAfterFlush)
	}
}

func TestAdminAccessNumberPoolAssignsOnTrialActivation(t *testing.T) {
	server := newTestServer()

	imported := request(t, server, http.MethodPost, "/admin/access-numbers", map[string]any{
		"numbers":                []string{"+8617000000800", "+8617000000801"},
		"provider":               "pilot-carrier",
		"trunk_id":               "sip-trunk-1",
		"jambonz_application_id": "jambonz-app-1",
		"jambonz_call_hook_url":  "https://voice.example.com/webhooks/jambonz/call",
		"status":                 "available",
		"notes":                  "pilot pool",
	})
	if imported.Code != http.StatusOK {
		t.Fatalf("expected import 200, got %d: %s", imported.Code, imported.Body.String())
	}

	createdMerchant := request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-no-number",
		"merchant_name": "未分配号码试点店",
		"enabled":       true,
	})
	if createdMerchant.Code != http.StatusOK {
		t.Fatalf("expected merchant without access number, got %d: %s", createdMerchant.Code, createdMerchant.Body.String())
	}

	activated := request(t, server, http.MethodPost, "/admin/pilots/merchant-no-number/activate-trial", map[string]any{
		"plan_code": "pilot_basic",
	})
	if activated.Code != http.StatusOK {
		t.Fatalf("expected trial activation to auto assign access number, got %d: %s", activated.Code, activated.Body.String())
	}
	var activatedBody map[string]any
	decode(t, activated, &activatedBody)
	pilot := activatedBody["pilot"].(map[string]any)
	merchant := pilot["merchant"].(map[string]any)
	if merchant["access_number"] != "8617000000800" {
		t.Fatalf("expected first pool number assigned, got %#v", merchant)
	}

	status := request(t, server, http.MethodGet, "/service-status?merchant_id=merchant-no-number", nil)
	var statusBody map[string]any
	decode(t, status, &statusBody)
	instructions := statusBody["call_forwarding_instructions"].([]any)
	if !contains(instructions[0].(map[string]any)["dial_code"].(string), "8617000000800") {
		t.Fatalf("expected forwarding instructions to use assigned number, got %#v", instructions)
	}

	list := request(t, server, http.MethodGet, "/admin/access-numbers", nil)
	var listBody map[string]any
	decode(t, list, &listBody)
	overview := listBody["overview"].(map[string]any)
	if overview["assigned"] != float64(1) || overview["available"] != float64(1) {
		t.Fatalf("unexpected access number overview: %#v", overview)
	}

	routeCheck := request(t, server, http.MethodGet, "/admin/access-numbers/route-check", nil)
	var routeBody map[string]any
	decode(t, routeCheck, &routeBody)
	routeOverview := routeBody["overview"].(map[string]any)
	if routeOverview["ready"] != float64(2) {
		t.Fatalf("expected route-ready numbers, got %#v", routeBody)
	}

	released := request(t, server, http.MethodPost, "/admin/pilots/merchant-no-number/release-access-number", map[string]any{})
	if released.Code != http.StatusOK {
		t.Fatalf("expected release 200, got %d: %s", released.Code, released.Body.String())
	}
	var releasedBody map[string]any
	decode(t, released, &releasedBody)
	accessNumber := releasedBody["access_number"].(map[string]any)
	releasedPilot := releasedBody["pilot"].(map[string]any)
	releasedMerchant := releasedPilot["merchant"].(map[string]any)
	if accessNumber["status"] != "available" || releasedMerchant["access_number"] != "" {
		t.Fatalf("expected number released and merchant unbound, got %#v", releasedBody)
	}
}

func TestAdminAccessNumberRouteCheckBlocksMissingJambonzBinding(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/admin/access-numbers", map[string]any{
		"number":   "+8617000000810",
		"provider": "pilot-carrier",
		"trunk_id": "sip-trunk-1",
		"status":   "available",
	})
	request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-route-not-ready",
		"merchant_name": "路由未完成试点店",
		"enabled":       true,
	})

	check := request(t, server, http.MethodGet, "/admin/access-numbers/route-check", nil)
	var checkBody map[string]any
	decode(t, check, &checkBody)
	item := checkBody["items"].([]any)[0].(map[string]any)
	checks := item["checks"].(map[string]any)
	if item["route_ready"] != false || checks["has_jambonz_application"] != false {
		t.Fatalf("expected missing jambonz binding to block route readiness, got %#v", checkBody)
	}

	assign := request(t, server, http.MethodPost, "/admin/pilots/merchant-route-not-ready/assign-access-number", map[string]any{})
	if assign.Code != http.StatusConflict {
		t.Fatalf("expected unroutable number assignment to fail, got %d: %s", assign.Code, assign.Body.String())
	}

	activate := request(t, server, http.MethodPost, "/service-status/activate-trial?merchant_id=merchant-route-not-ready", map[string]any{
		"plan_code": "pilot_basic",
	})
	if activate.Code != http.StatusConflict {
		t.Fatalf("expected trial activation to require routable number, got %d: %s", activate.Code, activate.Body.String())
	}
}

func TestJambonzConfigExportImportsRouteSnapshotAndValidatesRosieHook(t *testing.T) {
	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		Jambonz: config.JambonzConfig{
			ExpectedCallHookURL:   "https://voice.example.com/webhooks/jambonz/call",
			ExpectedStatusHookURL: "https://voice.example.com/webhooks/jambonz/status",
		},
	})

	response := request(t, server, http.MethodPost, "/admin/jambonz/config-export", map[string]any{
		"provider": "pilot-carrier",
		"applications": []map[string]any{
			{
				"application_id":  "app-good",
				"name":            "Rosie inbound",
				"call_hook_url":   "https://voice.example.com/webhooks/jambonz/call",
				"status_hook_url": "https://voice.example.com/webhooks/jambonz/status",
			},
			{
				"application_id": "app-wrong",
				"name":           "Old app",
				"call_hook_url":  "https://old.example.com/webhooks/jambonz/call",
			},
		},
		"phone_numbers": []map[string]any{
			{
				"number":         "+8617000000820",
				"trunk_id":       "sip-trunk-1",
				"application_id": "app-good",
			},
			{
				"number":         "+8617000000821",
				"trunk_id":       "sip-trunk-1",
				"application_id": "app-wrong",
			},
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected config import 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	if body["imported"] != float64(2) {
		t.Fatalf("expected two imported numbers, got %#v", body)
	}

	check := request(t, server, http.MethodGet, "/admin/access-numbers/route-check", nil)
	var checkBody map[string]any
	decode(t, check, &checkBody)
	overview := checkBody["overview"].(map[string]any)
	if overview["ready"] != float64(1) || overview["warning"] != float64(1) {
		t.Fatalf("expected one ready and one warning route, got %#v", checkBody)
	}

	request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-jambonz-route",
		"merchant_name": "jambonz 路由试点店",
		"enabled":       true,
	})
	assign := request(t, server, http.MethodPost, "/admin/pilots/merchant-jambonz-route/assign-access-number", map[string]any{})
	if assign.Code != http.StatusOK {
		t.Fatalf("expected assignment to choose route-ready number, got %d: %s", assign.Code, assign.Body.String())
	}
	var assignBody map[string]any
	decode(t, assign, &assignBody)
	accessNumber := assignBody["access_number"].(map[string]any)
	if accessNumber["number"] != "8617000000820" {
		t.Fatalf("expected assignment to skip wrong call hook, got %#v", assignBody)
	}
}

func TestJambonzAPISyncImportsApplicationsAndPhoneNumbers(t *testing.T) {
	seenApplicationsAuth := ""
	seenNumbersAuth := ""
	jambonzServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/Applications":
			seenApplicationsAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"applications": []map[string]any{
					{
						"application_sid":  "app-sync",
						"name":             "Rosie sync app",
						"call_hook":        map[string]any{"url": "https://voice.example.com/webhooks/jambonz/call"},
						"call_status_hook": map[string]any{"url": "https://voice.example.com/webhooks/jambonz/status"},
					},
				},
			})
		case "/v1/PhoneNumbers":
			seenNumbersAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"phone_numbers": []map[string]any{
					{
						"number":          "+8617000000830",
						"sip_trunk_id":    "sip-trunk-sync",
						"application_sid": "app-sync",
					},
				},
			})
		default:
			t.Fatalf("unexpected jambonz path: %s", r.URL.Path)
		}
	}))
	defer jambonzServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		Jambonz: config.JambonzConfig{
			APIBaseURL:            jambonzServer.URL + "/v1",
			APIToken:              "token-sync",
			ApplicationsPath:      "/Applications",
			PhoneNumbersPath:      "/PhoneNumbers",
			ExpectedCallHookURL:   "https://voice.example.com/webhooks/jambonz/call",
			ExpectedStatusHookURL: "https://voice.example.com/webhooks/jambonz/status",
		},
	})

	response := request(t, server, http.MethodPost, "/admin/jambonz/sync", map[string]any{})
	if response.Code != http.StatusOK {
		t.Fatalf("expected sync 200, got %d: %s", response.Code, response.Body.String())
	}
	if seenApplicationsAuth != "Bearer token-sync" || seenNumbersAuth != "Bearer token-sync" {
		t.Fatalf("expected bearer auth, got apps=%q nums=%q", seenApplicationsAuth, seenNumbersAuth)
	}
	var body map[string]any
	decode(t, response, &body)
	if body["imported"] != float64(1) {
		t.Fatalf("expected one synced number, got %#v", body)
	}
	check := body["checks"].([]any)[0].(map[string]any)
	if check["route_ready"] != true {
		t.Fatalf("expected synced route ready, got %#v", body)
	}

	request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-jambonz-sync",
		"merchant_name": "jambonz 同步试点店",
		"enabled":       true,
	})
	assign := request(t, server, http.MethodPost, "/admin/pilots/merchant-jambonz-sync/assign-access-number", map[string]any{})
	if assign.Code != http.StatusOK {
		t.Fatalf("expected assignment after sync, got %d: %s", assign.Code, assign.Body.String())
	}
}

func TestTrialActivationRequiresAvailableAccessNumberForUnboundMerchant(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/merchants", map[string]any{
		"merchant_id":   "merchant-without-pool",
		"merchant_name": "无号码池试点店",
		"enabled":       true,
	})

	response := request(t, server, http.MethodPost, "/service-status/activate-trial?merchant_id=merchant-without-pool", map[string]any{
		"plan_code": "pilot_basic",
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409 when no access number is available, got %d: %s", response.Code, response.Body.String())
	}
}

func TestActivateTrialServiceIsIdempotent(t *testing.T) {
	server := newTestServer()
	first := request(t, server, http.MethodPost, "/service-status/activate-trial?merchant_id=demo-merchant", map[string]any{
		"plan_code": "pilot_basic",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.Code, first.Body.String())
	}
	var firstBody map[string]any
	decode(t, first, &firstBody)
	firstSubscription := firstBody["subscription"].(map[string]any)
	if firstSubscription["status"] != "trialing" || firstBody["trial_days_remaining"].(float64) <= 0 {
		t.Fatalf("unexpected trial activation: %#v", firstBody)
	}

	second := request(t, server, http.MethodPost, "/service-status/activate-trial?merchant_id=demo-merchant", map[string]any{
		"plan_code": "pilot_basic",
	})
	var secondBody map[string]any
	decode(t, second, &secondBody)
	secondSubscription := secondBody["subscription"].(map[string]any)
	if secondSubscription["status"] != "trialing" ||
		secondSubscription["trial_ends_at"] != firstSubscription["trial_ends_at"] {
		t.Fatalf("expected idempotent trial activation, got %#v and %#v", firstBody, secondBody)
	}
}

func TestPaymentOrderRenewalExtendsSubscriptionWhenPaid(t *testing.T) {
	server := newTestServer()
	create := request(t, server, http.MethodPost, "/payment-orders?merchant_id=demo-merchant", map[string]any{
		"order_type": "renewal",
		"plan_code":  "pilot_basic",
	})
	if create.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", create.Code, create.Body.String())
	}
	var createBody map[string]any
	decode(t, create, &createBody)
	if createBody["status"] != "pending_provider_config" {
		t.Fatalf("expected pending provider config, got %#v", createBody)
	}
	order := createBody["order"].(map[string]any)
	if order["amount_cents"] != float64(3000) || order["status"] != "pending" {
		t.Fatalf("unexpected payment order: %#v", order)
	}
	orderNo := order["order_no"].(string)

	list := request(t, server, http.MethodGet, "/payment-orders?merchant_id=demo-merchant", nil)
	var listBody map[string]any
	decode(t, list, &listBody)
	if len(listBody["items"].([]any)) != 1 {
		t.Fatalf("expected one payment order, got %#v", listBody)
	}

	paid := request(t, server, http.MethodPost, "/internal/payment-orders/"+orderNo+"/mark-paid", map[string]any{
		"provider_trade_no": "wx-trade-1",
		"paid_at":           "2026-04-30T12:00:00Z",
	})
	if paid.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", paid.Code, paid.Body.String())
	}
	var paidBody map[string]any
	decode(t, paid, &paidBody)
	paidOrder := paidBody["order"].(map[string]any)
	subscription := paidBody["subscription"].(map[string]any)
	if paidOrder["status"] != "paid" || subscription["status"] != "active" {
		t.Fatalf("unexpected paid renewal: %#v", paidBody)
	}
}

func TestPaymentOrderCreatesWeChatPayJSAPIOrder(t *testing.T) {
	merchantKeyPath := writeRSAPrivateKey(t, mustRSAKey(t))
	var received map[string]any
	wechatPayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/pay/transactions/jsapi" {
			t.Fatalf("unexpected wechat pay path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatal("expected wechat pay authorization header")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"prepay_id": "wx-prepay-1"})
	}))
	defer wechatPayServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		WeChat: config.WeChatConfig{
			AppID:         "wx-app-id",
			DefaultOpenID: "openid-default",
		},
		WeChatPay: config.WeChatPayConfig{
			APIBaseURL:       wechatPayServer.URL,
			MchID:            "mch-1",
			NotifyURL:        "https://example.test/internal/wechat-pay/notify",
			MerchantSerialNo: "merchant-serial",
			PrivateKeyPath:   merchantKeyPath,
			APIv3Key:         "0123456789abcdef0123456789abcdef",
		},
	})

	create := request(t, server, http.MethodPost, "/payment-orders?merchant_id=demo-merchant", map[string]any{
		"order_type": "renewal",
		"plan_code":  "pilot_basic",
	})
	if create.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", create.Code, create.Body.String())
	}
	var body map[string]any
	decode(t, create, &body)
	if body["status"] != "pending_payment" {
		t.Fatalf("expected pending payment, got %#v", body)
	}
	payment := body["payment"].(map[string]any)
	params := payment["request_params"].(map[string]any)
	if payment["prepay_id"] != "wx-prepay-1" || params["package"] != "prepay_id=wx-prepay-1" || params["signType"] != "RSA" {
		t.Fatalf("unexpected jsapi payment response: %#v", payment)
	}
	payer := received["payer"].(map[string]any)
	if payer["openid"] != "openid-default" || received["appid"] != "wx-app-id" {
		t.Fatalf("unexpected wechat pay request: %#v", received)
	}
}

func TestWeChatPayNotifyVerifiesSignatureAndRenewsSubscription(t *testing.T) {
	merchantKeyPath := writeRSAPrivateKey(t, mustRSAKey(t))
	platformKey := mustRSAKey(t)
	platformKeyPath := writeRSAPublicKey(t, &platformKey.PublicKey)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	wechatPayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"prepay_id": "wx-prepay-callback"})
	}))
	defer wechatPayServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		WeChat: config.WeChatConfig{
			AppID:         "wx-app-id",
			DefaultOpenID: "openid-default",
		},
		WeChatPay: config.WeChatPayConfig{
			APIBaseURL:       wechatPayServer.URL,
			MchID:            "mch-1",
			NotifyURL:        "https://example.test/internal/wechat-pay/notify",
			MerchantSerialNo: "merchant-serial",
			PrivateKeyPath:   merchantKeyPath,
			APIv3Key:         apiV3Key,
			PlatformSerialNo: "platform-serial",
			PlatformKeyPath:  platformKeyPath,
		},
	})
	create := request(t, server, http.MethodPost, "/payment-orders?merchant_id=demo-merchant", map[string]any{
		"order_type": "renewal",
		"plan_code":  "pilot_basic",
	})
	var createBody map[string]any
	decode(t, create, &createBody)
	order := createBody["order"].(map[string]any)
	orderNo := order["order_no"].(string)

	successTime := "2026-04-30T12:00:00Z"
	notifyBody := wechatPayNotifyBody(t, apiV3Key, map[string]any{
		"out_trade_no":   orderNo,
		"transaction_id": "wx-trade-callback",
		"trade_state":    "SUCCESS",
		"success_time":   successTime,
	})
	headers := signedWeChatPayHeaders(t, platformKey, "platform-serial", notifyBody)
	notify := requestWithHeaders(t, server, http.MethodPost, "/internal/wechat-pay/notify", notifyBody, headers)
	if notify.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", notify.Code, notify.Body.String())
	}

	list := request(t, server, http.MethodGet, "/payment-orders?merchant_id=demo-merchant", nil)
	var listBody map[string]any
	decode(t, list, &listBody)
	paidOrder := listBody["items"].([]any)[0].(map[string]any)
	if paidOrder["status"] != "paid" || paidOrder["provider_trade_no"] != "wx-trade-callback" {
		t.Fatalf("expected paid order from notify, got %#v", paidOrder)
	}

	status := request(t, server, http.MethodGet, "/service-status?merchant_id=demo-merchant&now=2026-04-30T12:00:00Z", nil)
	var statusBody map[string]any
	decode(t, status, &statusBody)
	subscription := statusBody["subscription"].(map[string]any)
	if subscription["status"] != "active" {
		t.Fatalf("expected active subscription, got %#v", statusBody)
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
	callbacks := body["callback_requests"].([]any)
	if len(callbacks) != 0 {
		t.Fatalf("expected no callback requests, got %#v", callbacks)
	}
}

func TestCallbackRequestCreatesAuditRecordForExistingCall(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-call-callback",
		"call_id":     "original-call-id",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发，我姓王。",
	})

	response := request(t, server, http.MethodPost, "/calls/sim-call-callback/callback-requests", map[string]any{
		"target_number": "+8613811112222",
		"requested_by":  "miniprogram",
		"reason":        "merchant_manual_call_detail",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	if body["manual_dial_required"] != true {
		t.Fatalf("expected manual dial boundary, got %#v", body)
	}
	callback := body["callback_request"].(map[string]any)
	if callback["merchant_id"] != "demo-merchant" ||
		callback["original_call_sid"] != "sim-call-callback" ||
		callback["original_call_id"] != "original-call-id" ||
		callback["target_number"] != "+8613811112222" ||
		callback["status"] != "requested" {
		t.Fatalf("unexpected callback request: %#v", callback)
	}
	if !contains(callback["audit_note"].(string), "backend outbound dial not initiated") {
		t.Fatalf("expected audit note to preserve outbound boundary: %#v", callback)
	}

	detail := request(t, server, http.MethodGet, "/calls/sim-call-callback", nil)
	var detailBody map[string]any
	decode(t, detail, &detailBody)
	callbacks := detailBody["callback_requests"].([]any)
	if len(callbacks) != 1 {
		t.Fatalf("expected callback request in call detail, got %#v", detailBody)
	}
}

func TestCallbackRequestUsesSummaryPhoneFallback(t *testing.T) {
	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
	})
	request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-callback-summary-phone",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我姓王，电话是13811112222，想预约明天下午三点剪头发。",
	})

	response := request(t, server, http.MethodPost, "/calls/rt-callback-summary-phone/callback-requests", map[string]any{
		"requested_by": "miniprogram",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	callback := body["callback_request"].(map[string]any)
	if callback["target_number"] != "13811112222" {
		t.Fatalf("expected summary phone fallback, got %#v", callback)
	}
}

func TestCallbackRequestRejectsUnknownCall(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPost, "/calls/missing-call/callback-requests", map[string]any{
		"target_number": "+8613811112222",
	})

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCallbackRequestRejectsMissingTargetWithoutFallback(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-callback-no-target",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，咨询一下。",
	})

	response := request(t, server, http.MethodPost, "/calls/rt-callback-no-target/callback-requests", map[string]any{})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCallbackRequestStatusCanBeUpdated(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-callback-status",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发。",
	})
	created := request(t, server, http.MethodPost, "/calls/sim-callback-status/callback-requests", map[string]any{
		"target_number": "+8613811112222",
		"requested_by":  "miniprogram",
	})
	var createdBody map[string]any
	decode(t, created, &createdBody)
	callback := createdBody["callback_request"].(map[string]any)
	callbackID := int(callback["id"].(float64))

	updated := request(t, server, http.MethodPatch, "/calls/sim-callback-status/callback-requests/"+strconv.Itoa(callbackID), map[string]any{
		"status":     "dialed",
		"audit_note": "wx.makePhoneCall invoked by merchant",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	var updatedBody map[string]any
	decode(t, updated, &updatedBody)
	updatedCallback := updatedBody["callback_request"].(map[string]any)
	if updatedCallback["status"] != "dialed" || !contains(updatedCallback["audit_note"].(string), "wx.makePhoneCall") {
		t.Fatalf("unexpected updated callback: %#v", updatedCallback)
	}

	detail := request(t, server, http.MethodGet, "/calls/sim-callback-status", nil)
	var detailBody map[string]any
	decode(t, detail, &detailBody)
	callbacks := detailBody["callback_requests"].([]any)
	if callbacks[0].(map[string]any)["status"] != "dialed" {
		t.Fatalf("expected detail to include updated status, got %#v", callbacks)
	}
}

func TestCallbackRequestStatusRejectsInvalidStatus(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPatch, "/calls/call-1/callback-requests/1", map[string]any{
		"status": "auto_dialing",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestInboxItemStatusCanBeUpdated(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/simulate/call-result", map[string]any{
		"call_sid":    "sim-inbox-status",
		"from_number": "+8613811112222",
		"to_number":   "8613736849910",
		"transcript":  "你好，我想预约明天下午三点剪头发。",
	})

	updated := request(t, server, http.MethodPatch, "/calls/sim-inbox-status/inbox-item", map[string]any{
		"status": "handled",
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	var updatedBody map[string]any
	decode(t, updated, &updatedBody)
	inbox := updatedBody["inbox"].(map[string]any)
	if inbox["status"] != "handled" {
		t.Fatalf("expected handled inbox, got %#v", inbox)
	}

	detail := request(t, server, http.MethodGet, "/calls/sim-inbox-status", nil)
	var detailBody map[string]any
	decode(t, detail, &detailBody)
	detailInbox := detailBody["inbox"].(map[string]any)
	if detailInbox["status"] != "handled" {
		t.Fatalf("expected detail inbox handled, got %#v", detailInbox)
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

func TestCallResultUsesAIAgentSummaryWhenConfigured(t *testing.T) {
	seenSystemPrompt := false
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Fatalf("unexpected ai-agent path: %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if contains(payload["system_prompt"].(string), "测试理发店") {
			seenSystemPrompt = true
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"model": "local-fallback",
			"result": `{
				"summary":"王女士想预约明天下午三点剪发，已留下手机号。",
				"customer_name":"王女士",
				"customer_phone":"13811112222",
				"intent":"appointment",
				"appointment_time":"明天下午三点",
				"service":"剪发",
				"priority":"high",
				"need_human_followup":true
			}`,
		})
	}))
	defer agentServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		AIAgentURL:          agentServer.URL,
		AISummaryEnabled:    true,
	})

	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-ai-summary",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我姓王，电话是13811112222，想预约明天下午三点剪头发。\nRosie：好的。",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	decode(t, response, &body)
	summaryItem := body["summary"].(map[string]any)
	if summaryItem["summary"] != "王女士想预约明天下午三点剪发，已留下手机号。" ||
		summaryItem["customer_name"] != "王女士" ||
		summaryItem["service"] != "剪发" {
		t.Fatalf("expected ai summary, got %#v", summaryItem)
	}
	if !contains(summaryItem["raw_result"].(string), "ai_agent") || !seenSystemPrompt {
		t.Fatalf("ai summary did not preserve raw result or merchant prompt: %#v prompt=%v", summaryItem, seenSystemPrompt)
	}
}

func TestCallResultFallsBackWhenAIAgentSummaryIsInvalid(t *testing.T) {
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"model":  "bad-model",
			"result": "not json",
		})
	}))
	defer agentServer.Close()

	server := newTestServerWithConfig(config.Config{
		Addr:                "127.0.0.1:8030",
		DefaultAccessNumber: "8613736849910",
		DefaultMerchantID:   "demo-merchant",
		DefaultMerchantName: "测试理发店",
		AIAgentURL:          agentServer.URL,
		AISummaryEnabled:    true,
	})

	response := request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-ai-summary-fallback",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。\nRosie：好的。",
	})
	var body map[string]any
	decode(t, response, &body)
	summaryItem := body["summary"].(map[string]any)
	if contains(summaryItem["summary"].(string), "Rosie") || summaryItem["intent"] != "appointment" {
		t.Fatalf("expected rule fallback summary, got %#v", summaryItem)
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

func TestBusinessResultRetryQueuePersistsAndFlushesRealtimePayload(t *testing.T) {
	server := newTestServer()
	response := request(t, server, http.MethodPost, "/internal/realtime-call-result-retries", map[string]any{
		"session_id":    "retry-session-1",
		"attempt_count": 1,
		"last_error":    "temporary ingest failure",
		"payload": map[string]any{
			"call_sid":    "retry-call-1",
			"merchant_id": "demo-merchant",
			"from_number": "+8613811112222",
			"to_number":   "8613736849910",
			"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	list := request(t, server, http.MethodGet, "/internal/realtime-call-result-retries?status=failed", nil)
	var listBody map[string]any
	decode(t, list, &listBody)
	items := listBody["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one retry item, got %#v", listBody)
	}
	item := items[0].(map[string]any)
	if item["session_id"] != "retry-session-1" || item["attempt_count"] != float64(1) {
		t.Fatalf("unexpected retry item: %#v", item)
	}

	flush := request(t, server, http.MethodPost, "/internal/realtime-call-result-retries/flush?max_attempts=5", nil)
	if flush.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", flush.Code, flush.Body.String())
	}
	var flushBody map[string]any
	decode(t, flush, &flushBody)
	results := flushBody["results"].([]any)
	result := results[0].(map[string]any)
	if result["status"] != "sent" || result["attempt_count"] != float64(2) {
		t.Fatalf("unexpected flush result: %#v", result)
	}

	detail := request(t, server, http.MethodGet, "/calls/retry-call-1", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected flushed call detail, got %d: %s", detail.Code, detail.Body.String())
	}
}

func TestBusinessResultRetryFlushExhaustsMaxAttempts(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/internal/realtime-call-result-retries", map[string]any{
		"session_id":    "retry-exhausted",
		"attempt_count": 5,
		"last_error":    "still failing",
		"payload": map[string]any{
			"call_sid":    "retry-exhausted-call",
			"merchant_id": "demo-merchant",
			"transcript":  "客户：需要人工回电。",
		},
	})

	flush := request(t, server, http.MethodPost, "/internal/realtime-call-result-retries/flush?max_attempts=5", nil)
	var body map[string]any
	decode(t, flush, &body)
	result := body["results"].([]any)[0].(map[string]any)
	if result["status"] != "exhausted" || result["attempt_count"] != float64(5) {
		t.Fatalf("expected exhausted retry, got %#v", result)
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

func TestDispatchNotificationsBacksOffTransientFailures(t *testing.T) {
	wechatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token", "expires_in": 7200})
		case "/cgi-bin/message/subscribe/send":
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 45009, "errmsg": "rate limit"})
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
	request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-dispatch-backoff",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})

	firstDispatch := request(t, server, http.MethodPost, "/internal/notifications/dispatch", nil)
	var firstBody map[string]any
	decode(t, firstDispatch, &firstBody)
	result := firstBody["results"].([]any)[0].(map[string]any)
	if result["status"] != "failed" || result["error_category"] != "rate_limited" || result["next_retry_at"] == nil {
		t.Fatalf("expected failed retry with backoff, got %#v", result)
	}

	failedRetry := request(t, server, http.MethodPost, "/internal/notifications/dispatch?status=failed", nil)
	var retryBody map[string]any
	decode(t, failedRetry, &retryBody)
	if retryBody["total"] != float64(0) {
		t.Fatalf("expected failed notification to wait for next_retry_at, got %#v", retryBody)
	}
}

func TestDispatchNotificationsExhaustsPermanentFailures(t *testing.T) {
	server := newTestServer()
	request(t, server, http.MethodPost, "/internal/realtime-call-result", map[string]any{
		"call_sid":    "rt-dispatch-config-error",
		"merchant_id": "demo-merchant",
		"transcript":  "客户：你好，我想预约明天下午三点剪头发。",
	})

	dispatch := request(t, server, http.MethodPost, "/internal/notifications/dispatch", nil)
	var body map[string]any
	decode(t, dispatch, &body)
	result := body["results"].([]any)[0].(map[string]any)
	if result["status"] != "exhausted" || result["error_category"] != "configuration" {
		t.Fatalf("expected exhausted configuration failure, got %#v", result)
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

func requestWithHeaders(t *testing.T, server http.Handler, method string, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
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

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func writeRSAPrivateKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	path := t.TempDir() + "/merchant-private.pem"
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRSAPublicKey(t *testing.T, key *rsa.PublicKey) string {
	t.Helper()
	path := t.TempDir() + "/platform-public.pem"
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(key)})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func wechatPayNotifyBody(t *testing.T, apiV3Key string, transaction map[string]any) []byte {
	t.Helper()
	plain, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := "notify-nonce"
	associatedData := "transaction"
	ciphertext := gcm.Seal(nil, []byte(nonce), plain, []byte(associatedData))
	body, err := json.Marshal(map[string]any{
		"id":            "notify-id",
		"create_time":   "2026-04-30T12:00:01Z",
		"event_type":    "TRANSACTION.SUCCESS",
		"resource_type": "encrypt-resource",
		"resource": map[string]any{
			"algorithm":       "AEAD_AES_256_GCM",
			"ciphertext":      base64.StdEncoding.EncodeToString(ciphertext),
			"nonce":           nonce,
			"associated_data": associatedData,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func signedWeChatPayHeaders(t *testing.T, key *rsa.PrivateKey, serial string, body []byte) map[string]string {
	t.Helper()
	timestamp := strconv.FormatInt(time.Date(2026, 4, 30, 12, 0, 1, 0, time.UTC).Unix(), 10)
	nonce := "header-nonce"
	message := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	hash := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"Wechatpay-Timestamp": timestamp,
		"Wechatpay-Nonce":     nonce,
		"Wechatpay-Signature": base64.StdEncoding.EncodeToString(signature),
		"Wechatpay-Serial":    serial,
	}
}
