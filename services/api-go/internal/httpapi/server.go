package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rosie-api/internal/config"
	"rosie-api/internal/domain"
	"rosie-api/internal/industry"
	"rosie-api/internal/store"
	"rosie-api/internal/summary"
	"rosie-api/internal/wechat"
	"rosie-api/internal/wechatpay"
)

type Server struct {
	repo             store.Repository
	cfg              config.Config
	wechat           *wechat.Client
	summaryExtractor *summary.AgentExtractor
}

func NewServer(repo store.Repository, cfg config.Config) *Server {
	var extractor *summary.AgentExtractor
	if cfg.AISummaryEnabled && cfg.AIAgentURL != "" {
		extractor = summary.NewAgentExtractor(cfg.AIAgentURL, cfg.AISummaryTimeout)
	}
	return &Server{repo: repo, cfg: cfg, wechat: wechat.NewClient(cfg.WeChat, nil), summaryExtractor: extractor}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /health/deps", s.healthDeps)
	mux.HandleFunc("GET /admin", s.adminPage)
	mux.HandleFunc("GET /admin/access-numbers/route-check", s.checkAdminAccessNumberRoutes)
	mux.HandleFunc("GET /admin/access-numbers", s.listAdminAccessNumbers)
	mux.HandleFunc("POST /admin/access-numbers", s.upsertAdminAccessNumbers)
	mux.HandleFunc("POST /admin/jambonz/config-export", s.importAdminJambonzConfigExport)
	mux.HandleFunc("POST /admin/jambonz/sync", s.syncAdminJambonzConfig)
	mux.HandleFunc("GET /admin/pilots", s.listAdminPilots)
	mux.HandleFunc("GET /admin/pilots/{merchant_id}", s.getAdminPilot)
	mux.HandleFunc("POST /admin/pilots/{merchant_id}/assign-access-number", s.adminAssignAccessNumber)
	mux.HandleFunc("POST /admin/pilots/{merchant_id}/release-access-number", s.adminReleaseAccessNumber)
	mux.HandleFunc("POST /admin/pilots/{merchant_id}/activate-trial", s.adminActivateTrial)
	mux.HandleFunc("POST /admin/pilots/{merchant_id}/dispatch-notifications", s.adminDispatchNotifications)
	mux.HandleFunc("POST /admin/pilots/{merchant_id}/flush-retries", s.adminFlushBusinessResultRetries)
	mux.HandleFunc("POST /auth/wechat-login", s.wechatLogin)
	mux.HandleFunc("GET /merchants", s.listMerchants)
	mux.HandleFunc("POST /merchants", s.upsertMerchant)
	mux.HandleFunc("GET /value-metrics", s.getValueMetrics)
	mux.HandleFunc("GET /service-status", s.getServiceStatus)
	mux.HandleFunc("POST /service-status/activate-trial", s.activateTrialService)
	mux.HandleFunc("GET /payment-orders", s.listPaymentOrders)
	mux.HandleFunc("POST /payment-orders", s.createPaymentOrder)
	mux.HandleFunc("POST /internal/payment-orders/{order_no}/mark-paid", s.markPaymentOrderPaid)
	mux.HandleFunc("POST /internal/wechat-pay/notify", s.wechatPayNotify)
	mux.HandleFunc("GET /merchant-profile", s.getMerchantProfile)
	mux.HandleFunc("PUT /merchant-profile", s.updateMerchantProfile)
	mux.HandleFunc("GET /industry-templates", s.listIndustryTemplates)
	mux.HandleFunc("GET /calls", s.listCalls)
	mux.HandleFunc("GET /calls/{call_sid}", s.getCallDetail)
	mux.HandleFunc("PATCH /calls/{call_sid}/inbox-item", s.updateInboxItemStatus)
	mux.HandleFunc("POST /calls/{call_sid}/callback-requests", s.createCallbackRequest)
	mux.HandleFunc("PATCH /calls/{call_sid}/callback-requests/{callback_id}", s.updateCallbackRequestStatus)
	mux.HandleFunc("POST /simulate/call-result", s.simulateCallResult)
	mux.HandleFunc("POST /internal/realtime-call-result", s.realtimeCallResult)
	mux.HandleFunc("GET /inbox", s.listInbox)
	mux.HandleFunc("GET /digests/preview", s.digestPreview)
	mux.HandleFunc("POST /digests/generate", s.generateDigest)
	mux.HandleFunc("GET /digests", s.listDigests)
	mux.HandleFunc("GET /notification-preferences", s.getNotificationPreferences)
	mux.HandleFunc("PUT /notification-preferences", s.updateNotificationPreferences)
	mux.HandleFunc("POST /internal/digest-tick", s.digestTick)
	mux.HandleFunc("POST /internal/notifications/dispatch", s.dispatchNotifications)
	mux.HandleFunc("GET /notification-logs", s.listNotificationLogs)
	mux.HandleFunc("POST /internal/realtime-call-result-retries", s.enqueueBusinessResultRetry)
	mux.HandleFunc("GET /internal/realtime-call-result-retries", s.listBusinessResultRetries)
	mux.HandleFunc("POST /internal/realtime-call-result-retries/flush", s.flushBusinessResultRetries)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) healthDeps(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	deps := map[string]any{}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	db := map[string]any{
		"status": "ok",
		"kind":   "memory",
	}
	if s.cfg.DatabaseURL != "" {
		db["kind"] = "postgres"
	}
	if err := s.repo.Ping(ctx); err != nil {
		db["status"] = "error"
		db["error"] = err.Error()
		status = "degraded"
	}
	deps["database"] = db

	aiSummary := map[string]any{
		"status":     "disabled",
		"configured": false,
	}
	if s.cfg.AISummaryEnabled && s.cfg.AIAgentURL != "" {
		aiSummary["status"] = "ok"
		aiSummary["configured"] = true
		aiSummary["url"] = s.cfg.AIAgentURL
		if s.summaryExtractor != nil {
			body, err := s.summaryExtractor.Health(ctx)
			if err != nil {
				aiSummary["status"] = "error"
				aiSummary["error"] = err.Error()
				status = "degraded"
			} else if body != nil {
				if provider, ok := body["provider"]; ok {
					aiSummary["provider"] = provider
				}
				if model, ok := body["model"]; ok {
					aiSummary["model"] = model
				}
			}
		}
	}
	deps["ai_summary"] = aiSummary
	deps["wechat"] = map[string]any{
		"status":     wechatConfigStatus(s.cfg.WeChat),
		"configured": wechatConfigured(s.cfg.WeChat),
	}

	queues := map[string]any{}
	if retries, err := s.repo.ListBusinessResultRetries("failed", 200); err != nil {
		queues["business_result_retry_error"] = err.Error()
		status = "degraded"
	} else {
		queues["failed_business_result_retries"] = len(retries)
	}
	now := time.Now().UTC()
	if queued, err := s.repo.ListDueNotificationLogs("queued", 200, now); err != nil {
		queues["queued_notifications_error"] = err.Error()
		status = "degraded"
	} else {
		queues["due_queued_notifications"] = len(queued)
	}
	if failed, err := s.repo.ListDueNotificationLogs("failed", 200, now); err != nil {
		queues["failed_notifications_error"] = err.Error()
		status = "degraded"
	} else {
		queues["due_failed_notifications"] = len(failed)
	}

	httpStatus := http.StatusOK
	if status != "ok" {
		httpStatus = http.StatusServiceUnavailable
	}
	writeJSON(w, httpStatus, map[string]any{
		"status":       status,
		"dependencies": deps,
		"queues":       queues,
	})
}

type adminPilotSummary struct {
	Merchant                  domain.Merchant              `json:"merchant"`
	Profile                   domain.MerchantProfile       `json:"profile"`
	Subscription              domain.ServiceSubscription   `json:"subscription"`
	Metrics                   domain.ValueMetrics          `json:"metrics"`
	Status                    string                       `json:"status"`
	StatusText                string                       `json:"status_text"`
	TrialDaysRemaining        int                          `json:"trial_days_remaining"`
	Payment                   map[string]any               `json:"payment"`
	Notifications             map[string]any               `json:"notifications"`
	BusinessResultRetry       map[string]any               `json:"business_result_retry"`
	RecentCalls               []domain.Call                `json:"recent_calls,omitempty"`
	RecentOrders              []domain.PaymentOrder        `json:"recent_orders,omitempty"`
	RecentFailedNotifications []domain.NotificationLog     `json:"recent_failed_notifications,omitempty"`
	RecentFailedRetries       []domain.BusinessResultRetry `json:"recent_failed_retries,omitempty"`
}

type adminPilotOverview struct {
	Total               int `json:"total"`
	Healthy             int `json:"healthy"`
	NeedsAttention      int `json:"needs_attention"`
	NotStarted          int `json:"not_started"`
	Inactive            int `json:"inactive"`
	TotalCalls          int `json:"total_calls"`
	EffectiveCalls      int `json:"effective_calls"`
	Appointments        int `json:"appointments"`
	FailedNotifications int `json:"failed_notifications"`
	FailedRetries       int `json:"failed_retries"`
	PendingOrders       int `json:"pending_orders"`
	PaidOrders          int `json:"paid_orders"`
	TotalPaidCents      int `json:"total_paid_cents"`
}

type adminAccessNumberPayload struct {
	Number                 string   `json:"number"`
	Numbers                []string `json:"numbers"`
	Provider               string   `json:"provider"`
	ProviderNumberID       string   `json:"provider_number_id"`
	TrunkID                string   `json:"trunk_id"`
	JambonzApplicationID   string   `json:"jambonz_application_id"`
	JambonzApplicationName string   `json:"jambonz_application_name"`
	JambonzCallHookURL     string   `json:"jambonz_call_hook_url"`
	JambonzStatusHookURL   string   `json:"jambonz_status_hook_url"`
	Status                 string   `json:"status"`
	Notes                  string   `json:"notes"`
}

type accessNumberRouteCheck struct {
	Number     string              `json:"number"`
	Status     string              `json:"status"`
	RouteReady bool                `json:"route_ready"`
	Severity   string              `json:"severity"`
	Issues     []string            `json:"issues"`
	Checks     map[string]bool     `json:"checks"`
	Merchant   *domain.Merchant    `json:"merchant,omitempty"`
	Metadata   domain.AccessNumber `json:"metadata"`
}

type jambonzConfigExportPayload struct {
	ExpectedCallHookURL   string           `json:"expected_call_hook_url"`
	ExpectedStatusHookURL string           `json:"expected_status_hook_url"`
	Provider              string           `json:"provider"`
	PhoneNumbers          []map[string]any `json:"phone_numbers"`
	Numbers               []map[string]any `json:"numbers"`
	Applications          []map[string]any `json:"applications"`
	Raw                   map[string]any   `json:"raw"`
}

type jambonzSyncPayload struct {
	APIBaseURL            string `json:"api_base_url"`
	APIToken              string `json:"api_token"`
	ApplicationsPath      string `json:"applications_path"`
	PhoneNumbersPath      string `json:"phone_numbers_path"`
	Provider              string `json:"provider"`
	ExpectedCallHookURL   string `json:"expected_call_hook_url"`
	ExpectedStatusHookURL string `json:"expected_status_hook_url"`
}

type jambonzApplicationSnapshot struct {
	ID            string
	Name          string
	CallHookURL   string
	StatusHookURL string
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(adminPageHTML))
}

func (s *Server) listAdminAccessNumbers(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !validAccessNumberStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid access number status")
		return
	}
	items, err := s.repo.ListAccessNumbers(status, queryInt(r, "limit", 100, 500))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"overview": accessNumbersOverview(items),
	})
}

func (s *Server) checkAdminAccessNumberRoutes(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !validAccessNumberStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid access number status")
		return
	}
	items, err := s.repo.ListAccessNumbers(status, queryInt(r, "limit", 100, 500))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	expectedCallHookURL := valueOrString(strings.TrimSpace(r.URL.Query().Get("expected_call_hook_url")), s.cfg.Jambonz.ExpectedCallHookURL)
	expectedStatusHookURL := valueOrString(strings.TrimSpace(r.URL.Query().Get("expected_status_hook_url")), s.cfg.Jambonz.ExpectedStatusHookURL)
	checks := make([]accessNumberRouteCheck, 0, len(items))
	overview := map[string]int{
		"total":    len(items),
		"ready":    0,
		"warning":  0,
		"blocked":  0,
		"disabled": 0,
	}
	for _, item := range items {
		check, err := s.accessNumberRouteCheckWithExpected(item, expectedCallHookURL, expectedStatusHookURL)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		checks = append(checks, check)
		if _, ok := overview[check.Severity]; ok {
			overview[check.Severity]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    checks,
		"overview": overview,
	})
}

func (s *Server) upsertAdminAccessNumbers(w http.ResponseWriter, r *http.Request) {
	var payload adminAccessNumberPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	numbers := accessNumberPayloadNumbers(payload)
	if len(numbers) == 0 {
		writeError(w, http.StatusBadRequest, "number or numbers is required")
		return
	}
	status := valueOrString(strings.TrimSpace(payload.Status), "available")
	if !validAccessNumberStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid access number status")
		return
	}
	items := make([]domain.AccessNumber, 0, len(numbers))
	for _, number := range numbers {
		item, err := s.repo.UpsertAccessNumber(domain.AccessNumber{
			Number:                 number,
			Provider:               strings.TrimSpace(payload.Provider),
			ProviderNumberID:       strings.TrimSpace(payload.ProviderNumberID),
			TrunkID:                strings.TrimSpace(payload.TrunkID),
			JambonzApplicationID:   strings.TrimSpace(payload.JambonzApplicationID),
			JambonzApplicationName: strings.TrimSpace(payload.JambonzApplicationName),
			JambonzCallHookURL:     strings.TrimSpace(payload.JambonzCallHookURL),
			JambonzStatusHookURL:   strings.TrimSpace(payload.JambonzStatusHookURL),
			Status:                 status,
			Notes:                  strings.TrimSpace(payload.Notes),
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "items": items})
}

func (s *Server) importAdminJambonzConfigExport(w http.ResponseWriter, r *http.Request) {
	var payload jambonzConfigExportPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	result, err := s.importJambonzConfigSnapshot(payload)
	if err != nil {
		writeIngestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) syncAdminJambonzConfig(w http.ResponseWriter, r *http.Request) {
	var payload jambonzSyncPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	apiBaseURL := valueOrString(strings.TrimSpace(payload.APIBaseURL), s.cfg.Jambonz.APIBaseURL)
	apiToken := valueOrString(strings.TrimSpace(payload.APIToken), s.cfg.Jambonz.APIToken)
	applicationsPath := valueOrString(strings.TrimSpace(payload.ApplicationsPath), s.cfg.Jambonz.ApplicationsPath)
	phoneNumbersPath := valueOrString(strings.TrimSpace(payload.PhoneNumbersPath), s.cfg.Jambonz.PhoneNumbersPath)
	if apiBaseURL == "" || apiToken == "" {
		writeError(w, http.StatusBadRequest, "jambonz api_base_url and api_token are required")
		return
	}

	applications, err := s.fetchJambonzMaps(r.Context(), apiBaseURL, applicationsPath, apiToken, "applications")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	phoneNumbers, err := s.fetchJambonzMaps(r.Context(), apiBaseURL, phoneNumbersPath, apiToken, "phone_numbers")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	result, err := s.importJambonzConfigSnapshot(jambonzConfigExportPayload{
		ExpectedCallHookURL:   valueOrString(strings.TrimSpace(payload.ExpectedCallHookURL), s.cfg.Jambonz.ExpectedCallHookURL),
		ExpectedStatusHookURL: valueOrString(strings.TrimSpace(payload.ExpectedStatusHookURL), s.cfg.Jambonz.ExpectedStatusHookURL),
		Provider:              valueOrString(strings.TrimSpace(payload.Provider), "jambonz"),
		Applications:          applications,
		PhoneNumbers:          phoneNumbers,
	})
	if err != nil {
		writeIngestError(w, err)
		return
	}
	result["sync"] = map[string]any{
		"api_base_url":          apiBaseURL,
		"applications_path":     applicationsPath,
		"phone_numbers_path":    phoneNumbersPath,
		"applications_fetched":  len(applications),
		"phone_numbers_fetched": len(phoneNumbers),
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) importJambonzConfigSnapshot(payload jambonzConfigExportPayload) (map[string]any, error) {
	applications := jambonzApplicationsByID(payload)
	phoneNumbers := jambonzPhoneNumbers(payload)
	if len(phoneNumbers) == 0 {
		return nil, apiError{status: http.StatusBadRequest, detail: "phone_numbers or numbers is required"}
	}

	now := time.Now().UTC()
	items := make([]domain.AccessNumber, 0, len(phoneNumbers))
	checks := make([]accessNumberRouteCheck, 0, len(phoneNumbers))
	for _, phoneNumber := range phoneNumbers {
		number := store.NormalizeNumber(firstMapString(phoneNumber, "number", "phone_number", "e164", "did", "phoneNumber"))
		if number == "" {
			continue
		}
		appID := firstMapString(phoneNumber, "jambonz_application_id", "application_id", "application_sid", "application_uuid", "application")
		app := applications[appID]
		if app.ID == "" {
			app.ID = appID
		}
		trunkID := firstMapString(phoneNumber, "trunk_id", "sip_trunk_id", "voip_carrier_sid", "carrier_sid", "carrier_id")
		item, err := s.repo.UpsertAccessNumber(domain.AccessNumber{
			Number:                 number,
			Provider:               valueOrString(strings.TrimSpace(payload.Provider), firstMapString(phoneNumber, "provider", "carrier", "carrier_name")),
			ProviderNumberID:       firstMapString(phoneNumber, "provider_number_id", "sid", "id", "phone_number_sid"),
			TrunkID:                trunkID,
			JambonzApplicationID:   app.ID,
			JambonzApplicationName: app.Name,
			JambonzCallHookURL:     app.CallHookURL,
			JambonzStatusHookURL:   app.StatusHookURL,
			JambonzConfigSyncedAt:  &now,
			Status:                 "available",
			Notes:                  "imported from jambonz config export",
		})
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		check, err := s.accessNumberRouteCheckWithExpected(
			item,
			valueOrString(strings.TrimSpace(payload.ExpectedCallHookURL), s.cfg.Jambonz.ExpectedCallHookURL),
			valueOrString(strings.TrimSpace(payload.ExpectedStatusHookURL), s.cfg.Jambonz.ExpectedStatusHookURL),
		)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return map[string]any{
		"status":   "ok",
		"imported": len(items),
		"items":    items,
		"checks":   checks,
	}, nil
}

func (s *Server) listAdminPilots(w http.ResponseWriter, r *http.Request) {
	merchants, err := s.repo.ListMerchants()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	limit := queryInt(r, "limit", 100, 500)
	if len(merchants) > limit {
		merchants = merchants[:limit]
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	now := time.Now().UTC()
	items := make([]adminPilotSummary, 0, len(merchants))
	for _, merchant := range merchants {
		item, err := s.adminPilotSummary(merchant, now, false)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if statusFilter != "" && item.Status != statusFilter {
			continue
		}
		if query != "" && !adminPilotMatches(item, query) {
			continue
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"overview":  adminPilotsOverview(items),
		"generated": now,
		"filters": map[string]any{
			"q":      query,
			"status": statusFilter,
			"limit":  limit,
		},
	})
}

func (s *Server) getAdminPilot(w http.ResponseWriter, r *http.Request) {
	merchantID := strings.TrimSpace(r.PathValue("merchant_id"))
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	item, err := s.adminPilotSummary(merchant, time.Now().UTC(), true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminAssignAccessNumber(w http.ResponseWriter, r *http.Request) {
	merchantID := strings.TrimSpace(r.PathValue("merchant_id"))
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload struct {
		Number string `json:"number"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	now := time.Now().UTC()
	accessNumber, err := s.assignRoutableAccessNumber(merchant.MerchantID, strings.TrimSpace(payload.Number), now)
	if err != nil {
		writeIngestError(w, err)
		return
	}
	updatedMerchant, ok, err := s.repo.FindMerchantByID(merchant.MerchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	pilot, err := s.adminPilotSummary(updatedMerchant, now, true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"access_number": accessNumber,
		"pilot":         pilot,
	})
}

func (s *Server) adminReleaseAccessNumber(w http.ResponseWriter, r *http.Request) {
	merchantID := strings.TrimSpace(r.PathValue("merchant_id"))
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	number := merchant.AccessNumber
	if number == "" {
		writeError(w, http.StatusConflict, "merchant has no access number")
		return
	}
	accessNumber, err := s.repo.ReleaseAccessNumber(number, time.Now().UTC())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	updatedMerchant, ok, err := s.repo.FindMerchantByID(merchant.MerchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	pilot, err := s.adminPilotSummary(updatedMerchant, time.Now().UTC(), true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"access_number": accessNumber,
		"pilot":         pilot,
	})
}

func (s *Server) adminActivateTrial(w http.ResponseWriter, r *http.Request) {
	merchantID := strings.TrimSpace(r.PathValue("merchant_id"))
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload struct {
		PlanCode string `json:"plan_code"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	plan := planDefinition(valueOrString(strings.TrimSpace(payload.PlanCode), "pilot_basic"))
	planCode, _ := plan["code"].(string)
	if planCode == "" {
		writeError(w, http.StatusBadRequest, "unknown plan_code")
		return
	}
	now := time.Now().UTC()
	merchant, err = s.ensureMerchantAccessNumber(merchant, now)
	if err != nil {
		writeIngestError(w, err)
		return
	}
	if _, err := s.repo.ActivateTrialSubscription(merchant.MerchantID, planCode, now, now.AddDate(0, 0, 14)); err != nil {
		writeStoreError(w, err)
		return
	}
	item, err := s.adminPilotSummary(merchant, now, true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "pilot": item})
}

func (s *Server) adminDispatchNotifications(w http.ResponseWriter, r *http.Request) {
	merchantID := strings.TrimSpace(r.PathValue("merchant_id"))
	if _, ok, err := s.repo.FindMerchantByID(merchantID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	status := valueOrString(strings.TrimSpace(payload.Status), "failed")
	if status != "queued" && status != "failed" {
		writeError(w, http.StatusBadRequest, "status must be queued or failed")
		return
	}
	limit := clampInt(valueOrInt(payload.Limit, 20), 1, 100)
	items, err := s.repo.ListNotificationLogs(merchantID, status, limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.Status != status {
			continue
		}
		result, err := s.dispatchNotification(r, item)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		results = append(results, result)
	}
	merchant, _, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	pilot, err := s.adminPilotSummary(merchant, time.Now().UTC(), true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"total":   len(results),
		"results": results,
		"pilot":   pilot,
	})
}

func (s *Server) adminFlushBusinessResultRetries(w http.ResponseWriter, r *http.Request) {
	merchantID := strings.TrimSpace(r.PathValue("merchant_id"))
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload struct {
		MaxAttempts int `json:"max_attempts"`
		Limit       int `json:"limit"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	maxAttempts := clampInt(valueOrInt(payload.MaxAttempts, 5), 1, 100)
	limit := clampInt(valueOrInt(payload.Limit, 20), 1, 100)
	items, err := s.adminBusinessResultRetries(merchantID, "failed", limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	results := make([]map[string]any, 0, len(items))
	for _, job := range items {
		result, err := s.retryBusinessResult(r.Context(), job, maxAttempts)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		results = append(results, result)
	}
	pilot, err := s.adminPilotSummary(merchant, time.Now().UTC(), true)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"total":   len(results),
		"results": results,
		"pilot":   pilot,
	})
}

func (s *Server) adminPilotSummary(merchant domain.Merchant, now time.Time, includeDetails bool) (adminPilotSummary, error) {
	subscription, err := s.repo.EnsureServiceSubscription(merchant.MerchantID)
	if err != nil {
		return adminPilotSummary{}, err
	}
	profile, err := s.repo.EnsureMerchantProfile(merchant.MerchantID)
	if err != nil {
		return adminPilotSummary{}, err
	}
	metrics, err := s.repo.GetValueMetrics(merchant.MerchantID, monthStart(now), now)
	if err != nil {
		return adminPilotSummary{}, err
	}
	metrics.Period = "month"
	orders, err := s.repo.ListPaymentOrders(merchant.MerchantID, 5)
	if err != nil {
		return adminPilotSummary{}, err
	}
	failedNotifications, err := s.repo.ListNotificationLogs(merchant.MerchantID, "failed", 20)
	if err != nil {
		return adminPilotSummary{}, err
	}
	exhaustedNotifications, err := s.repo.ListNotificationLogs(merchant.MerchantID, "exhausted", 20)
	if err != nil {
		return adminPilotSummary{}, err
	}
	queuedNotifications, err := s.repo.ListNotificationLogs(merchant.MerchantID, "queued", 20)
	if err != nil {
		return adminPilotSummary{}, err
	}
	failedRetries, err := s.adminBusinessResultRetries(merchant.MerchantID, "failed", 20)
	if err != nil {
		return adminPilotSummary{}, err
	}
	exhaustedRetries, err := s.adminBusinessResultRetries(merchant.MerchantID, "exhausted", 20)
	if err != nil {
		return adminPilotSummary{}, err
	}
	recentCalls, err := s.adminRecentCalls(merchant.MerchantID, 5)
	if err != nil {
		return adminPilotSummary{}, err
	}
	problemNotifications := appendNotificationLogs(failedNotifications, exhaustedNotifications, 20)
	problemRetries := appendBusinessResultRetries(failedRetries, exhaustedRetries, 20)
	item := adminPilotSummary{
		Merchant:           merchant,
		Profile:            profile,
		Subscription:       subscription,
		Metrics:            metrics,
		Status:             adminPilotStatus(subscription, len(problemNotifications), len(problemRetries)),
		StatusText:         adminPilotStatusText(subscription, len(problemNotifications), len(problemRetries)),
		TrialDaysRemaining: trialDaysRemaining(subscription, now),
		Payment: map[string]any{
			"latest_order":        firstPaymentOrder(orders),
			"recent_order_count":  len(orders),
			"pending_order_count": countPaymentOrders(orders, "pending"),
			"paid_order_count":    countPaymentOrders(orders, "paid"),
			"total_paid_cents":    sumPaidPaymentOrders(orders),
		},
		Notifications: map[string]any{
			"failed_count":    len(failedNotifications),
			"exhausted_count": len(exhaustedNotifications),
			"problem_count":   len(problemNotifications),
			"queued_count":    len(queuedNotifications),
			"latest_failed":   firstNotificationLog(problemNotifications),
		},
		BusinessResultRetry: map[string]any{
			"failed_count":    len(failedRetries),
			"exhausted_count": len(exhaustedRetries),
			"problem_count":   len(problemRetries),
			"latest_failed":   firstBusinessResultRetry(problemRetries),
		},
		RecentCalls: recentCalls,
	}
	if includeDetails {
		item.RecentOrders = orders
		item.RecentFailedNotifications = problemNotifications
		item.RecentFailedRetries = problemRetries
	}
	return item, nil
}

func (s *Server) adminRecentCalls(merchantID string, limit int) ([]domain.Call, error) {
	calls, err := s.repo.ListCalls(200)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Call, 0, limit)
	for _, call := range calls {
		if call.MerchantID == merchantID {
			items = append(items, call)
			if len(items) >= limit {
				break
			}
		}
	}
	return items, nil
}

func (s *Server) adminBusinessResultRetries(merchantID string, status string, limit int) ([]domain.BusinessResultRetry, error) {
	retries, err := s.repo.ListBusinessResultRetries(status, 200)
	if err != nil {
		return nil, err
	}
	items := make([]domain.BusinessResultRetry, 0, limit)
	for _, retry := range retries {
		if businessResultRetryMerchantID(retry) == merchantID {
			items = append(items, retry)
			if len(items) >= limit {
				break
			}
		}
	}
	return items, nil
}

func (s *Server) ensureMerchantAccessNumber(merchant domain.Merchant, now time.Time) (domain.Merchant, error) {
	if strings.TrimSpace(merchant.AccessNumber) != "" {
		return merchant, nil
	}
	if _, err := s.assignRoutableAccessNumber(merchant.MerchantID, "", now); err != nil {
		return domain.Merchant{}, err
	}
	updated, ok, err := s.repo.FindMerchantByID(merchant.MerchantID)
	if err != nil {
		return domain.Merchant{}, err
	}
	if !ok {
		return domain.Merchant{}, apiError{status: http.StatusNotFound, detail: "unknown merchant"}
	}
	return updated, nil
}

func (s *Server) assignRoutableAccessNumber(merchantID string, number string, now time.Time) (domain.AccessNumber, error) {
	number = strings.TrimSpace(number)
	if number != "" {
		item, ok, err := s.repo.FindAccessNumberByNumber(number)
		if err != nil {
			return domain.AccessNumber{}, err
		}
		if !ok {
			return domain.AccessNumber{}, apiError{status: http.StatusNotFound, detail: "unknown access number"}
		}
		if !s.accessNumberRouteReadyForAssignment(item) {
			return domain.AccessNumber{}, apiError{status: http.StatusConflict, detail: "access number route is not ready"}
		}
		return s.repo.AssignAccessNumber(merchantID, item.Number, now)
	}

	items, err := s.repo.ListAccessNumbers("available", 500)
	if err != nil {
		return domain.AccessNumber{}, err
	}
	for _, item := range items {
		if s.accessNumberRouteReadyForAssignment(item) {
			return s.repo.AssignAccessNumber(merchantID, item.Number, now)
		}
	}
	if len(items) == 0 {
		return domain.AccessNumber{}, apiError{status: http.StatusConflict, detail: "no available access number"}
	}
	return domain.AccessNumber{}, apiError{status: http.StatusConflict, detail: "no routable access number"}
}

func (s *Server) wechatLogin(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Code       string `json:"code"`
		MerchantID string `json:"merchant_id"`
		Role       string `json:"role"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	merchantID := payload.MerchantID
	if merchantID == "" {
		merchantID = s.cfg.DefaultMerchantID
	}
	if _, ok, err := s.repo.FindMerchantByID(merchantID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}

	session, err := s.wechat.CodeToSession(r.Context(), payload.Code)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	user, err := s.repo.UpsertAppUser(domain.AppUser{
		OpenID:     session.OpenID,
		UnionID:    session.UnionID,
		SessionKey: session.SessionKey,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	binding, err := s.repo.BindMerchantUser(merchantID, user.ID, payload.Role)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"user":        user,
		"binding":     binding,
		"openid":      user.OpenID,
		"unionid":     user.UnionID,
		"merchant_id": merchantID,
	})
}

func (s *Server) listMerchants(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListMerchants()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) upsertMerchant(w http.ResponseWriter, r *http.Request) {
	var payload domain.Merchant
	if !decodeJSON(w, r, &payload) {
		return
	}
	if payload.MerchantID == "" || payload.MerchantName == "" {
		writeError(w, http.StatusBadRequest, "merchant_id and merchant_name are required")
		return
	}
	merchant, err := s.repo.UpsertMerchant(payload)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "merchant_id": merchant.MerchantID})
}

func (s *Server) getValueMetrics(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	if _, ok, err := s.repo.FindMerchantByID(merchantID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}

	period, since, until, err := valueMetricsWindow(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	metrics, err := s.repo.GetValueMetrics(merchantID, since, until)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	metrics.Period = period
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) getServiceStatus(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	subscription, err := s.repo.EnsureServiceSubscription(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	profile, err := s.repo.EnsureMerchantProfile(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	metrics, err := s.repo.GetValueMetrics(merchantID, monthStart(time.Now().UTC()), time.Now().UTC())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	metrics.Period = "month"
	openID, bound, err := s.repo.FindPrimaryOpenIDByMerchantID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceStatusResponse(merchant, profile, subscription, metrics, bound && openID != ""))
}

func (s *Server) activateTrialService(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload struct {
		PlanCode string `json:"plan_code"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	plan := planDefinition(valueOrString(strings.TrimSpace(payload.PlanCode), "pilot_basic"))
	planCode, _ := plan["code"].(string)
	if planCode == "" {
		writeError(w, http.StatusBadRequest, "unknown plan_code")
		return
	}
	now := time.Now().UTC()
	merchant, err = s.ensureMerchantAccessNumber(merchant, now)
	if err != nil {
		writeIngestError(w, err)
		return
	}
	subscription, err := s.repo.ActivateTrialSubscription(merchantID, planCode, now, now.AddDate(0, 0, 14))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	profile, err := s.repo.EnsureMerchantProfile(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	metrics, err := s.repo.GetValueMetrics(merchantID, monthStart(now), now)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	metrics.Period = "month"
	openID, bound, err := s.repo.FindPrimaryOpenIDByMerchantID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceStatusResponse(merchant, profile, subscription, metrics, bound && openID != ""))
}

type paymentOrderPayload struct {
	OrderType string `json:"order_type"`
	PlanCode  string `json:"plan_code"`
	AddOnCode string `json:"add_on_code"`
}

func (s *Server) createPaymentOrder(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	if _, ok, err := s.repo.FindMerchantByID(merchantID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload paymentOrderPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	planCode := valueOrString(strings.TrimSpace(payload.PlanCode), "pilot_basic")
	plan := planDefinition(planCode)
	if plan["code"] == "" {
		writeError(w, http.StatusBadRequest, "unknown plan_code")
		return
	}
	amountCents := plan["monthly_price_cents"].(int)
	addOnCode := strings.TrimSpace(payload.AddOnCode)
	if addOnCode != "" {
		addOn := voiceCloneAddon()
		if addOnCode != addOn["code"] {
			writeError(w, http.StatusBadRequest, "unknown add_on_code")
			return
		}
		amountCents += addOn["monthly_price_cents"].(int)
	}
	order, err := s.repo.InsertPaymentOrder(domain.PaymentOrder{
		MerchantID:  merchantID,
		OrderNo:     newOrderNo(),
		OrderType:   valueOrString(strings.TrimSpace(payload.OrderType), "renewal"),
		PlanCode:    planCode,
		AddOnCode:   addOnCode,
		AmountCents: amountCents,
		Currency:    "CNY",
		Status:      "pending",
		Provider:    "wechat_pay",
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}

	paymentStatus := ""
	var requestParams map[string]string
	if wechatPayConfigured(s.cfg) {
		openID, ok, err := s.repo.FindPrimaryOpenIDByMerchantID(merchantID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok || openID == "" {
			openID = strings.TrimSpace(s.cfg.WeChat.DefaultOpenID)
		}
		if openID == "" {
			paymentStatus = "pending_openid_binding"
		} else {
			client, err := wechatpay.NewClient(s.cfg.WeChat.AppID, s.cfg.WeChatPay, nil)
			if err != nil {
				writeError(w, http.StatusBadGateway, "wechat pay client init failed: "+err.Error())
				return
			}
			jsapiOrder, err := client.CreateJSAPIOrder(r.Context(), wechatpay.JSAPIOrderRequest{
				OrderNo:     order.OrderNo,
				Description: "Rosie AI 试点基础版续费",
				AmountCents: order.AmountCents,
				Currency:    order.Currency,
				OpenID:      openID,
			})
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			order, err = s.repo.UpdatePaymentOrderPrepay(order.OrderNo, jsapiOrder.PrepayID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			requestParams = jsapiOrder.RequestParams
		}
	}
	writeJSON(w, http.StatusOK, paymentOrderResponse(order, s.cfg, paymentStatus, requestParams))
}

func (s *Server) listPaymentOrders(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	items, err := s.repo.ListPaymentOrders(merchantID, queryInt(r, "limit", 20, 100))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) markPaymentOrderPaid(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("order_no")
	if orderNo == "" {
		writeError(w, http.StatusBadRequest, "order_no is required")
		return
	}
	var payload struct {
		ProviderTradeNo string `json:"provider_trade_no"`
		PaidAt          string `json:"paid_at"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	paidAt := time.Now().UTC()
	if strings.TrimSpace(payload.PaidAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.PaidAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid paid_at value")
			return
		}
		paidAt = parsed.UTC()
	}
	order, err := s.repo.MarkPaymentOrderPaid(orderNo, strings.TrimSpace(payload.ProviderTradeNo), paidAt)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	subscription, err := s.repo.RenewServiceSubscription(order.MerchantID, order.PlanCode, paidAt, 1)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"order":        order,
		"subscription": subscription,
	})
}

func (s *Server) wechatPayNotify(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification body")
		return
	}
	notification, err := wechatpay.ParseNotification(s.cfg.WeChatPay, r.Header, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if notification.OutTradeNo == "" {
		writeError(w, http.StatusBadRequest, "missing out_trade_no")
		return
	}
	if notification.TradeState != "SUCCESS" {
		writeJSON(w, http.StatusOK, map[string]string{"code": "SUCCESS", "message": "成功"})
		return
	}
	paidAt := notification.SuccessTime
	if paidAt.IsZero() {
		paidAt = time.Now().UTC()
	}
	existing, ok, err := s.repo.FindPaymentOrderByNo(notification.OutTradeNo)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "payment order not found")
		return
	}
	if existing.Status == "paid" {
		writeJSON(w, http.StatusOK, map[string]string{"code": "SUCCESS", "message": "成功"})
		return
	}
	order, err := s.repo.MarkPaymentOrderPaid(notification.OutTradeNo, notification.TransactionID, paidAt)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, err := s.repo.RenewServiceSubscription(order.MerchantID, order.PlanCode, paidAt, 1); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": "SUCCESS", "message": "成功"})
}

type merchantProfilePayload struct {
	MerchantName     string           `json:"merchant_name"`
	AccessNumber     string           `json:"access_number"`
	OriginalNumber   string           `json:"original_number"`
	TransferPhone    string           `json:"transfer_phone"`
	Industry         string           `json:"industry"`
	Address          string           `json:"address"`
	BusinessHours    string           `json:"business_hours"`
	Services         []string         `json:"services"`
	FAQItems         []domain.FAQItem `json:"faq_items"`
	AppointmentRules string           `json:"appointment_rules"`
}

func (s *Server) getMerchantProfile(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	profile, err := s.repo.EnsureMerchantProfile(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, merchantProfileResponse(merchant, profile))
}

func (s *Server) updateMerchantProfile(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	merchant, ok, err := s.repo.FindMerchantByID(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var payload merchantProfilePayload
	if !decodeJSON(w, r, &payload) {
		return
	}

	merchant.MerchantName = valueOrString(strings.TrimSpace(payload.MerchantName), merchant.MerchantName)
	merchant.AccessNumber = valueOrString(strings.TrimSpace(payload.AccessNumber), merchant.AccessNumber)
	merchant.OriginalNumber = strings.TrimSpace(payload.OriginalNumber)
	merchant.TransferPhone = strings.TrimSpace(payload.TransferPhone)
	if merchant.MerchantName == "" || merchant.AccessNumber == "" {
		writeError(w, http.StatusBadRequest, "merchant_name and access_number are required")
		return
	}
	merchant.Enabled = true

	profile := domain.MerchantProfile{
		MerchantID:       merchant.MerchantID,
		Industry:         strings.TrimSpace(payload.Industry),
		Address:          strings.TrimSpace(payload.Address),
		BusinessHours:    strings.TrimSpace(payload.BusinessHours),
		Services:         cleanStrings(payload.Services),
		FAQItems:         cleanFAQItems(payload.FAQItems),
		AppointmentRules: strings.TrimSpace(payload.AppointmentRules),
	}
	if err := validateMerchantProfile(profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	merchant, err = s.repo.UpsertMerchant(merchant)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	profile, err = s.repo.UpdateMerchantProfile(profile)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, merchantProfileResponse(merchant, profile))
}

func (s *Server) listIndustryTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": industry.List()})
}

func (s *Server) listCalls(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListCalls(queryInt(r, "limit", 50, 200))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getCallDetail(w http.ResponseWriter, r *http.Request) {
	callSID := r.PathValue("call_sid")
	if callSID == "" {
		writeError(w, http.StatusBadRequest, "call_sid is required")
		return
	}

	call, ok, err := s.repo.FindCallBySID(callSID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown call")
		return
	}

	transcript, transcriptOK, err := s.repo.FindTranscriptByCallSID(callSID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	summaryItem, summaryOK, err := s.repo.FindSummaryByCallSID(callSID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	inbox, inboxOK, err := s.repo.FindInboxItemByCallSID(callSID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	callbacks, err := s.repo.ListCallbackRequestsByCallSID(callSID, 20)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if callbacks == nil {
		callbacks = []domain.CallbackRequest{}
	}

	var transcriptValue any
	if transcriptOK {
		transcriptValue = transcript
	}
	var summaryValue any
	if summaryOK {
		summaryValue = summaryItem
	}
	var inboxValue any
	if inboxOK {
		inboxValue = inbox
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"call":              call,
		"transcript":        transcriptValue,
		"summary":           summaryValue,
		"inbox":             inboxValue,
		"callback_requests": callbacks,
	})
}

type callbackRequestPayload struct {
	TargetNumber string `json:"target_number"`
	RequestedBy  string `json:"requested_by"`
	Reason       string `json:"reason"`
}

type statusUpdatePayload struct {
	Status    string `json:"status"`
	AuditNote string `json:"audit_note"`
}

func (s *Server) updateInboxItemStatus(w http.ResponseWriter, r *http.Request) {
	callSID := r.PathValue("call_sid")
	if callSID == "" {
		writeError(w, http.StatusBadRequest, "call_sid is required")
		return
	}
	var payload statusUpdatePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if !validInboxStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid inbox status")
		return
	}

	if _, ok, err := s.repo.FindCallBySID(callSID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown call")
		return
	}
	item, err := s.repo.UpdateInboxItemStatus(callSID, status)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"inbox":  item,
	})
}

func (s *Server) createCallbackRequest(w http.ResponseWriter, r *http.Request) {
	callSID := r.PathValue("call_sid")
	if callSID == "" {
		writeError(w, http.StatusBadRequest, "call_sid is required")
		return
	}

	var payload callbackRequestPayload
	if !decodeJSON(w, r, &payload) {
		return
	}

	call, ok, err := s.repo.FindCallBySID(callSID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown call")
		return
	}

	summaryItem, summaryOK, err := s.repo.FindSummaryByCallSID(callSID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	targetNumber := strings.TrimSpace(payload.TargetNumber)
	if targetNumber == "" && summaryOK {
		targetNumber = strings.TrimSpace(summaryItem.CustomerPhone)
	}
	if targetNumber == "" {
		targetNumber = strings.TrimSpace(call.FromNumber)
	}
	if targetNumber == "" {
		writeError(w, http.StatusBadRequest, "target_number is required")
		return
	}

	requestedBy := valueOrString(strings.TrimSpace(payload.RequestedBy), "api")
	reason := valueOrString(strings.TrimSpace(payload.Reason), "manual_callback")
	request, err := s.repo.InsertCallbackRequest(domain.CallbackRequest{
		MerchantID:      call.MerchantID,
		OriginalCallSID: call.CallSID,
		OriginalCallID:  call.CallID,
		TargetNumber:    targetNumber,
		RequestedBy:     requestedBy,
		Reason:          reason,
		Status:          "requested",
		AuditNote:       "manual callback requested by merchant; original inbound call bound; backend outbound dial not initiated",
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":               "ok",
		"callback_request":     request,
		"target_number":        targetNumber,
		"manual_dial_required": true,
	})
}

func (s *Server) updateCallbackRequestStatus(w http.ResponseWriter, r *http.Request) {
	callSID := r.PathValue("call_sid")
	callbackIDValue := r.PathValue("callback_id")
	callbackID, err := strconv.ParseInt(callbackIDValue, 10, 64)
	if callSID == "" || callbackID <= 0 || err != nil {
		writeError(w, http.StatusBadRequest, "call_sid and callback_id are required")
		return
	}
	var payload statusUpdatePayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if !validCallbackStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid callback status")
		return
	}
	updated, err := s.repo.UpdateCallbackRequestStatus(
		callbackID,
		callSID,
		status,
		strings.TrimSpace(payload.AuditNote),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"callback_request": updated,
	})
}

type callResultPayload struct {
	CallSID    string           `json:"call_sid"`
	CallID     string           `json:"call_id"`
	MerchantID string           `json:"merchant_id"`
	FromNumber string           `json:"from_number"`
	ToNumber   string           `json:"to_number"`
	Transcript string           `json:"transcript"`
	Turns      []callResultTurn `json:"turns"`
	TimingsMS  map[string]int   `json:"timings_ms"`
	Metadata   map[string]any   `json:"metadata"`
}

type callResultTurn struct {
	CustomerText string         `json:"customer_text"`
	AgentReply   string         `json:"agent_reply"`
	STTSource    string         `json:"stt_source"`
	AgentSource  string         `json:"agent_source"`
	TTSSource    string         `json:"tts_source"`
	TimingsMS    map[string]int `json:"timings_ms"`
}

type callResultIngest struct {
	CallSID     string
	SummaryID   int64
	InboxItemID int64
	Summary     domain.Summary
	Inbox       domain.InboxItem
	MerchantID  string
	Source      string
	RealtimeLog *domain.NotificationLog
}

type apiError struct {
	status int
	detail string
}

func (e apiError) Error() string {
	return e.detail
}

func (s *Server) simulateCallResult(w http.ResponseWriter, r *http.Request) {
	var payload callResultPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	result, ok := s.ingestCallResult(r.Context(), w, payload, "simulated")
	if !ok {
		return
	}

	writeCallResultResponse(w, result)
}

func (s *Server) realtimeCallResult(w http.ResponseWriter, r *http.Request) {
	var payload callResultPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	result, ok := s.ingestCallResult(r.Context(), w, payload, "realtime_voice")
	if !ok {
		return
	}
	writeCallResultResponse(w, result)
}

func (s *Server) ingestCallResult(ctx context.Context, w http.ResponseWriter, payload callResultPayload, source string) (callResultIngest, bool) {
	result, err := s.ingestCallResultPayload(ctx, payload, source)
	if err != nil {
		writeIngestError(w, err)
		return callResultIngest{}, false
	}
	return result, true
}

func (s *Server) ingestCallResultPayload(ctx context.Context, payload callResultPayload, source string) (callResultIngest, error) {
	payload.CallSID = strings.TrimSpace(payload.CallSID)
	payload.CallID = strings.TrimSpace(payload.CallID)
	payload.MerchantID = strings.TrimSpace(payload.MerchantID)
	payload.FromNumber = strings.TrimSpace(payload.FromNumber)
	payload.ToNumber = strings.TrimSpace(payload.ToNumber)
	payload.Transcript = strings.TrimSpace(valueOrString(payload.Transcript, transcriptFromTurns(payload.Turns)))
	if payload.CallSID == "" || payload.Transcript == "" {
		return callResultIngest{}, apiError{status: http.StatusBadRequest, detail: "call_sid and transcript are required"}
	}

	merchant, toNumber, err := s.resolveCallResultMerchantPayload(payload)
	if err != nil {
		return callResultIngest{}, err
	}

	raw, _ := json.Marshal(payload)
	callID := payload.CallID
	if callID == "" && source == "simulated" {
		callID = "sim-" + payload.CallSID
	}
	if _, err := s.repo.UpsertCall(domain.Call{
		CallSID:    payload.CallSID,
		CallID:     callID,
		MerchantID: merchant.MerchantID,
		FromNumber: payload.FromNumber,
		ToNumber:   store.NormalizeNumber(toNumber),
		CallStatus: "completed",
		Direction:  "inbound",
		RawPayload: string(raw),
	}); err != nil {
		return callResultIngest{}, err
	}
	if _, err := s.repo.UpsertTranscript(domain.Transcript{
		CallSID:    payload.CallSID,
		MerchantID: merchant.MerchantID,
		Transcript: payload.Transcript,
		Source:     source,
	}); err != nil {
		return callResultIngest{}, err
	}

	callSummary := s.summarizeCall(ctx, merchant, payload.Transcript)
	callSummary.CallSID = payload.CallSID
	callSummary.MerchantID = merchant.MerchantID
	callSummary, err = s.repo.UpsertSummary(callSummary)
	if err != nil {
		return callResultIngest{}, err
	}

	inbox, err := s.repo.UpsertInboxItem(domain.InboxItem{
		MerchantID:        merchant.MerchantID,
		CallSID:           payload.CallSID,
		ItemType:          "call_summary",
		Title:             summary.InboxTitle(callSummary),
		Body:              callSummary.Summary,
		Priority:          callSummary.Priority,
		Status:            summary.InboxStatus(callSummary),
		NeedHumanFollowup: callSummary.NeedHumanFollowup,
		DigestStatus:      "pending",
	})
	if err != nil {
		return callResultIngest{}, err
	}

	var realtimeLog *domain.NotificationLog
	if source == "realtime_voice" {
		log, err := s.queueRealtimeNotification(merchant.MerchantID, payload.CallSID, callSummary, inbox)
		if err != nil {
			return callResultIngest{}, err
		}
		if log.ID != 0 {
			realtimeLog = &log
		}
	}

	return callResultIngest{
		CallSID:     payload.CallSID,
		SummaryID:   callSummary.ID,
		InboxItemID: inbox.ID,
		Summary:     callSummary,
		Inbox:       inbox,
		MerchantID:  merchant.MerchantID,
		Source:      source,
		RealtimeLog: realtimeLog,
	}, nil
}

func (s *Server) summarizeCall(ctx context.Context, merchant domain.Merchant, transcript string) domain.Summary {
	fallback := summary.Fallback(transcript)
	if s.summaryExtractor == nil {
		return fallback
	}
	profile, err := s.repo.EnsureMerchantProfile(merchant.MerchantID)
	if err != nil {
		return fallback
	}
	item, ok, err := s.summaryExtractor.Extract(
		ctx,
		merchant.MerchantName,
		industry.BuildPrompt(merchant, profile),
		transcript,
		fallback,
	)
	if err != nil || !ok {
		return fallback
	}
	return item
}

func (s *Server) queueRealtimeNotification(
	merchantID string,
	callSID string,
	callSummary domain.Summary,
	inbox domain.InboxItem,
) (domain.NotificationLog, error) {
	prefs, err := s.repo.EnsurePreferences(merchantID)
	if err != nil {
		return domain.NotificationLog{}, err
	}
	key := "realtime_call:" + merchantID + ":" + callSID
	if existing, ok, err := s.repo.FindNotificationLogByKey(key); err != nil {
		return domain.NotificationLog{}, err
	} else if ok {
		return existing, nil
	}

	status := "skipped_realtime_disabled"
	if shouldQueueRealtimeNotification(prefs, callSummary, inbox) {
		status = "queued"
	}
	return s.repo.InsertNotificationLog(domain.NotificationLog{
		MerchantID:         merchantID,
		Channel:            digestChannel(prefs),
		MessageType:        "realtime_call_result",
		Subject:            "Rosie 实时来电提醒",
		Body:               realtimeNotificationBody(callSummary, inbox),
		RelatedInboxItemID: inbox.ID,
		IdempotencyKey:     key,
		Status:             status,
		RelatedDigestID:    0,
		Target:             "",
		AttemptCount:       0,
		LastError:          "",
	})
}

func (s *Server) resolveCallResultMerchant(w http.ResponseWriter, payload callResultPayload) (domain.Merchant, string, bool) {
	merchant, toNumber, err := s.resolveCallResultMerchantPayload(payload)
	if err != nil {
		writeIngestError(w, err)
		return domain.Merchant{}, "", false
	}
	return merchant, toNumber, true
}

func (s *Server) resolveCallResultMerchantPayload(payload callResultPayload) (domain.Merchant, string, error) {
	if payload.MerchantID != "" {
		merchant, ok, err := s.repo.FindMerchantByID(payload.MerchantID)
		if err != nil {
			return domain.Merchant{}, "", err
		}
		if !ok {
			return domain.Merchant{}, "", apiError{status: http.StatusNotFound, detail: "unknown merchant"}
		}
		return merchant, valueOrString(payload.ToNumber, merchant.AccessNumber), nil
	}

	toNumber := valueOrString(payload.ToNumber, s.cfg.DefaultAccessNumber)
	merchant, ok, err := s.repo.FindMerchantByAccessNumber(toNumber)
	if err != nil {
		return domain.Merchant{}, "", err
	}
	if !ok {
		return domain.Merchant{}, "", apiError{status: http.StatusNotFound, detail: "unknown access number"}
	}
	return merchant, toNumber, nil
}

func writeCallResultResponse(w http.ResponseWriter, result callResultIngest) {
	body := map[string]any{
		"status":        "ok",
		"call_sid":      result.CallSID,
		"merchant_id":   result.MerchantID,
		"source":        result.Source,
		"summary_id":    result.SummaryID,
		"inbox_item_id": result.InboxItemID,
		"summary":       result.Summary,
		"inbox": map[string]any{
			"title":               result.Inbox.Title,
			"status":              result.Inbox.Status,
			"priority":            result.Inbox.Priority,
			"need_human_followup": result.Inbox.NeedHumanFollowup,
		},
	}
	if result.RealtimeLog != nil {
		body["realtime_notification"] = map[string]any{
			"id":              result.RealtimeLog.ID,
			"status":          result.RealtimeLog.Status,
			"channel":         result.RealtimeLog.Channel,
			"message":         result.RealtimeLog.MessageType,
			"subject":         result.RealtimeLog.Subject,
			"inbox_id":        result.RealtimeLog.RelatedInboxItemID,
			"idempotency_key": result.RealtimeLog.IdempotencyKey,
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func transcriptFromTurns(turns []callResultTurn) string {
	var builder strings.Builder
	for _, turn := range turns {
		customerText := strings.TrimSpace(turn.CustomerText)
		agentReply := strings.TrimSpace(turn.AgentReply)
		if customerText != "" {
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString("客户：")
			builder.WriteString(customerText)
		}
		if agentReply != "" {
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString("Rosie：")
			builder.WriteString(agentReply)
		}
	}
	return builder.String()
}

func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	items, err := s.repo.ListInboxItems(merchantID, queryInt(r, "limit", 50, 200))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (s *Server) digestPreview(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	items, err := s.repo.ListPendingDigestItems(merchantID, queryInt(r, "limit", 100, 500))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	counts := digestCounts(items)
	writeJSON(w, http.StatusOK, map[string]any{
		"merchant_id":    merchantID,
		"total":          counts.total,
		"urgent_count":   counts.urgent,
		"followup_count": counts.followup,
		"spam_count":     counts.spam,
		"digest_text":    summary.DigestText(items),
		"items":          items,
	})
}

func (s *Server) generateDigest(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	digestType := queryString(r, "digest_type", "daily")
	digest, err := s.createDigest(merchantID, digestType, queryInt(r, "limit", 100, 500))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"digest_id":      digest.ID,
		"merchant_id":    digest.MerchantID,
		"digest_type":    digest.DigestType,
		"total":          digest.ItemCount,
		"urgent_count":   digest.UrgentCount,
		"followup_count": digest.FollowupCount,
		"spam_count":     digest.SpamCount,
		"digest_text":    digest.DigestText,
		"item_ids":       digest.ItemIDs,
	})
}

func (s *Server) listDigests(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	items, err := s.repo.ListDigests(merchantID, queryInt(r, "limit", 20, 100))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (s *Server) getNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	if _, ok, err := s.repo.FindMerchantByID(merchantID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	prefs, err := s.repo.EnsurePreferences(merchantID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) updateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	merchantID := queryString(r, "merchant_id", s.cfg.DefaultMerchantID)
	if _, ok, err := s.repo.FindMerchantByID(merchantID); err != nil {
		writeStoreError(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusNotFound, "unknown merchant")
		return
	}
	var prefs domain.NotificationPreferences
	if !decodeJSON(w, r, &prefs) {
		return
	}
	prefs.MerchantID = merchantID
	if err := validatePreferences(prefs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := s.repo.UpdatePreferences(prefs)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) digestTick(w http.ResponseWriter, r *http.Request) {
	tickTime, ok := parseTickTime(w, r.URL.Query().Get("now"))
	if !ok {
		return
	}
	tickClock := tickTime.Format("15:04")
	digestDate := tickTime.Format("2006-01-02")
	results := make([]map[string]any, 0)

	merchants, err := s.repo.ListMerchants()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for _, merchant := range merchants {
		if !merchant.Enabled {
			results = append(results, tickResult(merchant.MerchantID, false, "merchant_disabled", 0, 0, 0))
			continue
		}
		prefs, err := s.repo.EnsurePreferences(merchant.MerchantID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !digestDue(prefs, tickTime) {
			results = append(results, tickResult(merchant.MerchantID, false, "not_due", 0, 0, 0))
			continue
		}
		key := "digest:" + merchant.MerchantID + ":" + digestDate + ":" + tickClock
		if existing, ok, err := s.repo.FindNotificationLogByKey(key); err != nil {
			writeStoreError(w, err)
			return
		} else if ok {
			results = append(results, tickResult(merchant.MerchantID, true, "duplicate", existing.ID, existing.RelatedDigestID, 0))
			continue
		}
		items, err := s.repo.ListPendingDigestItems(merchant.MerchantID, queryInt(r, "limit", 100, 500))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if len(items) == 0 {
			log, err := s.repo.InsertNotificationLog(domain.NotificationLog{
				MerchantID:     merchant.MerchantID,
				Channel:        digestChannel(prefs),
				MessageType:    "scheduled_digest",
				Subject:        "Rosie " + digestDate + " " + tickClock + " 汇总",
				Body:           summary.DigestText(nil),
				IdempotencyKey: key,
				Status:         "skipped",
			})
			if err != nil {
				writeStoreError(w, err)
				return
			}
			results = append(results, tickResult(merchant.MerchantID, true, "skipped_no_pending_items", log.ID, 0, 0))
			continue
		}
		digest, err := s.createDigest(merchant.MerchantID, prefs.DigestMode, queryInt(r, "limit", 100, 500))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		log, err := s.repo.InsertNotificationLog(domain.NotificationLog{
			MerchantID:      merchant.MerchantID,
			Channel:         digestChannel(prefs),
			MessageType:     "scheduled_digest",
			Subject:         "Rosie " + digestDate + " " + tickClock + " 汇总",
			Body:            digest.DigestText,
			RelatedDigestID: digest.ID,
			IdempotencyKey:  key,
			Status:          "queued",
		})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		results = append(results, tickResult(merchant.MerchantID, true, "queued", log.ID, digest.ID, digest.ItemCount))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"now":     tickTime.Format("2006-01-02T15:04"),
		"results": results,
	})
}

func (s *Server) listNotificationLogs(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListNotificationLogs(
		r.URL.Query().Get("merchant_id"),
		r.URL.Query().Get("status"),
		queryInt(r, "limit", 50, 200),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

type businessResultRetryPayload struct {
	SessionID    string            `json:"session_id"`
	Payload      callResultPayload `json:"payload"`
	AttemptCount int               `json:"attempt_count"`
	LastError    string            `json:"last_error"`
}

func (s *Server) enqueueBusinessResultRetry(w http.ResponseWriter, r *http.Request) {
	var payload businessResultRetryPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.Payload.CallSID = strings.TrimSpace(payload.Payload.CallSID)
	if payload.SessionID == "" {
		payload.SessionID = payload.Payload.CallSID
	}
	if payload.SessionID == "" || payload.Payload.CallSID == "" {
		writeError(w, http.StatusBadRequest, "session_id and payload.call_sid are required")
		return
	}
	raw, err := json.Marshal(payload.Payload)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	attemptCount := payload.AttemptCount
	if attemptCount < 1 {
		attemptCount = 1
	}
	job, err := s.repo.UpsertBusinessResultRetry(domain.BusinessResultRetry{
		SessionID:    payload.SessionID,
		CallSID:      payload.Payload.CallSID,
		Payload:      string(raw),
		Status:       "failed",
		AttemptCount: attemptCount,
		LastError:    strings.TrimSpace(payload.LastError),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "item": job})
}

func (s *Server) listBusinessResultRetries(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListBusinessResultRetries(
		r.URL.Query().Get("status"),
		queryInt(r, "limit", 50, 200),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) flushBusinessResultRetries(w http.ResponseWriter, r *http.Request) {
	maxAttempts := queryInt(r, "max_attempts", 5, 100)
	items, err := s.repo.ListBusinessResultRetries("failed", queryInt(r, "limit", 20, 100))
	if err != nil {
		writeStoreError(w, err)
		return
	}

	results := make([]map[string]any, 0, len(items))
	for _, job := range items {
		result, err := s.retryBusinessResult(r.Context(), job, maxAttempts)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		results = append(results, result)
	}

	remaining, err := s.repo.ListBusinessResultRetries("failed", 200)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"total":     len(results),
		"remaining": len(remaining),
		"results":   results,
	})
}

func businessResultRetryResult(job domain.BusinessResultRetry, status string, lastError string) map[string]any {
	return map[string]any{
		"id":            job.ID,
		"session_id":    job.SessionID,
		"call_sid":      job.CallSID,
		"status":        status,
		"attempt_count": job.AttemptCount,
		"last_error":    lastError,
	}
}

func (s *Server) retryBusinessResult(ctx context.Context, job domain.BusinessResultRetry, maxAttempts int) (map[string]any, error) {
	if job.AttemptCount >= maxAttempts {
		updated, err := s.repo.UpdateBusinessResultRetryStatus(job.ID, "exhausted", job.AttemptCount, job.LastError)
		if err != nil {
			return nil, err
		}
		return businessResultRetryResult(updated, "exhausted", ""), nil
	}

	var payload callResultPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		updated, updateErr := s.repo.UpdateBusinessResultRetryStatus(job.ID, "failed", job.AttemptCount+1, err.Error())
		if updateErr != nil {
			return nil, updateErr
		}
		return businessResultRetryResult(updated, "failed", err.Error()), nil
	}

	_, ingestErr := s.ingestCallResultPayload(ctx, payload, "realtime_voice")
	status := "sent"
	lastError := ""
	if ingestErr != nil {
		status = "failed"
		lastError = ingestErr.Error()
	}
	updated, err := s.repo.UpdateBusinessResultRetryStatus(job.ID, status, job.AttemptCount+1, lastError)
	if err != nil {
		return nil, err
	}
	return businessResultRetryResult(updated, status, lastError), nil
}

func (s *Server) dispatchNotifications(w http.ResponseWriter, r *http.Request) {
	dispatchStatus := queryString(r, "status", "queued")
	if dispatchStatus != "queued" && dispatchStatus != "failed" {
		writeError(w, http.StatusBadRequest, "status must be queued or failed")
		return
	}
	idempotencyKey := strings.TrimSpace(r.URL.Query().Get("idempotency_key"))
	if idempotencyKey != "" {
		item, ok, err := s.repo.FindNotificationLogByKey(idempotencyKey)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "unknown notification")
			return
		}
		if item.Status != dispatchStatus {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok",
				"total":  0,
				"results": []map[string]any{
					{
						"id":            item.ID,
						"merchant_id":   item.MerchantID,
						"status":        item.Status,
						"attempt_count": item.AttemptCount,
						"last_error":    item.LastError,
					},
				},
			})
			return
		}
		result, err := s.dispatchNotification(r, item)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"total":   1,
			"results": []map[string]any{result},
		})
		return
	}

	items, err := s.repo.ListDueNotificationLogs(dispatchStatus, queryInt(r, "limit", 20, 100), time.Now().UTC())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result, err := s.dispatchNotification(r, item)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"total":   len(items),
		"results": results,
	})
}

func (s *Server) dispatchNotification(r *http.Request, item domain.NotificationLog) (map[string]any, error) {
	target := item.Target
	if target == "" {
		openID, ok, err := s.repo.FindPrimaryOpenIDByMerchantID(item.MerchantID)
		if err != nil {
			return nil, err
		}
		if ok {
			target = openID
		}
	}
	if target == "" {
		target = s.cfg.WeChat.DefaultOpenID
	}

	status := "sent"
	lastError := ""
	errorCategory := ""
	var nextRetryAt time.Time
	maxAttempts := valueOrInt(item.MaxAttempts, 5)
	attemptCount := item.AttemptCount + 1
	if err := s.wechat.SendSubscribe(r.Context(), item, target); err != nil {
		status = "failed"
		lastError = err.Error()
		errorCategory = classifyNotificationError(lastError)
		if attemptCount >= maxAttempts || !retryableNotificationError(errorCategory) {
			status = "exhausted"
		} else {
			nextRetryAt = time.Now().UTC().Add(notificationBackoff(attemptCount, errorCategory))
		}
	}
	updated, err := s.repo.UpdateNotificationLogDispatch(
		item.ID,
		status,
		attemptCount,
		maxAttempts,
		lastError,
		errorCategory,
		nextRetryAt,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":             updated.ID,
		"merchant_id":    updated.MerchantID,
		"status":         updated.Status,
		"attempt_count":  updated.AttemptCount,
		"max_attempts":   updated.MaxAttempts,
		"last_error":     updated.LastError,
		"error_category": updated.ErrorCategory,
		"next_retry_at":  updated.NextRetryAt,
	}, nil
}

func (s *Server) createDigest(merchantID string, digestType string, limit int) (domain.Digest, error) {
	items, err := s.repo.ListPendingDigestItems(merchantID, limit)
	if err != nil {
		return domain.Digest{}, err
	}
	counts := digestCounts(items)
	itemIDs := make([]int64, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	digest, err := s.repo.InsertDigest(domain.Digest{
		MerchantID:    merchantID,
		DigestType:    digestType,
		ItemCount:     counts.total,
		UrgentCount:   counts.urgent,
		FollowupCount: counts.followup,
		SpamCount:     counts.spam,
		DigestText:    summary.DigestText(items),
		ItemIDs:       itemIDs,
		Status:        "generated",
	})
	if err != nil {
		return domain.Digest{}, err
	}
	if err := s.repo.MarkInboxItemsDigested(itemIDs); err != nil {
		return domain.Digest{}, err
	}
	return digest, nil
}

type counts struct {
	total    int
	urgent   int
	followup int
	spam     int
}

func digestCounts(items []domain.InboxItem) counts {
	result := counts{total: len(items)}
	for _, item := range items {
		if item.Priority == "urgent" {
			result.urgent++
		}
		if item.NeedHumanFollowup {
			result.followup++
		}
		if item.Status == "filtered" {
			result.spam++
		}
	}
	return result
}

func digestDue(prefs domain.NotificationPreferences, tickTime time.Time) bool {
	if prefs.DigestMode == "manual" {
		return false
	}
	if prefs.DigestMode == "hourly" {
		return tickTime.Minute() == 0
	}
	current := tickTime.Format("15:04")
	for _, item := range prefs.DigestTimes {
		if item == current {
			return true
		}
	}
	return false
}

func digestChannel(prefs domain.NotificationPreferences) string {
	if prefs.TeamWecomEnabled {
		return "wecom_robot"
	}
	return "wechat_subscription"
}

func shouldQueueRealtimeNotification(
	prefs domain.NotificationPreferences,
	callSummary domain.Summary,
	inbox domain.InboxItem,
) bool {
	if callSummary.Intent == "spam" || inbox.Status == "filtered" {
		return false
	}
	if prefs.RealtimeEnabled {
		return true
	}
	if !prefs.UrgentRealtimeEnabled {
		return false
	}
	return callSummary.Priority == "urgent" || (callSummary.Priority == "high" && callSummary.NeedHumanFollowup)
}

func realtimeNotificationBody(callSummary domain.Summary, inbox domain.InboxItem) string {
	body := strings.TrimSpace(callSummary.Summary)
	if body == "" {
		body = strings.TrimSpace(inbox.Body)
	}
	if body == "" {
		body = "有一通新的来电需要查看。"
	}
	return inbox.Title + "：" + truncateForNotification(body, 80)
}

func truncateForNotification(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func classifyNotificationError(message string) string {
	value := strings.ToLower(message)
	switch {
	case strings.Contains(value, "missing wechat app id") ||
		strings.Contains(value, "missing wechat openid") ||
		strings.Contains(value, "subscribe template id"):
		return "configuration"
	case strings.Contains(value, "43101"):
		return "authorization"
	case strings.Contains(value, "40003") || strings.Contains(value, "openid"):
		return "invalid_recipient"
	case strings.Contains(value, "45009") || strings.Contains(value, "rate"):
		return "rate_limited"
	case strings.Contains(value, "timeout") || strings.Contains(value, "connection") || strings.Contains(value, "temporary"):
		return "transient"
	default:
		return "transient"
	}
}

func retryableNotificationError(category string) bool {
	return category == "transient" || category == "rate_limited"
}

func notificationBackoff(attemptCount int, category string) time.Duration {
	if attemptCount < 1 {
		attemptCount = 1
	}
	minutes := []int{1, 5, 15, 60, 180}
	index := attemptCount - 1
	if index >= len(minutes) {
		index = len(minutes) - 1
	}
	if category == "rate_limited" && minutes[index] < 15 {
		return 15 * time.Minute
	}
	return time.Duration(minutes[index]) * time.Minute
}

func tickResult(merchantID string, due bool, status string, logID int64, digestID int64, total int) map[string]any {
	return map[string]any{
		"merchant_id":         merchantID,
		"due":                 due,
		"status":              status,
		"notification_log_id": nullableID(logID),
		"digest_id":           nullableID(digestID),
		"total":               total,
	}
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func merchantProfileResponse(merchant domain.Merchant, profile domain.MerchantProfile) map[string]any {
	return map[string]any{
		"merchant":      merchant,
		"profile":       profile,
		"template":      industry.Find(valueOrString(profile.Industry, industry.DefaultKey())),
		"system_prompt": industry.BuildPrompt(merchant, profile),
	}
}

func validateMerchantProfile(profile domain.MerchantProfile) error {
	if profile.Industry == "" {
		return errString("industry is required")
	}
	if !knownIndustry(profile.Industry) {
		return errString("unknown industry")
	}
	if len(profile.Services) > 30 {
		return errString("too many services")
	}
	if len(profile.FAQItems) > 30 {
		return errString("too many faq items")
	}
	for _, item := range profile.FAQItems {
		if item.Question == "" || item.Answer == "" {
			return errString("faq question and answer are required")
		}
	}
	return nil
}

func knownIndustry(value string) bool {
	for _, item := range industry.List() {
		if item.Key == value {
			return true
		}
	}
	return false
}

func cleanStrings(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func cleanFAQItems(items []domain.FAQItem) []domain.FAQItem {
	result := make([]domain.FAQItem, 0, len(items))
	for _, item := range items {
		question := strings.TrimSpace(item.Question)
		answer := strings.TrimSpace(item.Answer)
		if question == "" && answer == "" {
			continue
		}
		result = append(result, domain.FAQItem{Question: question, Answer: answer})
	}
	return result
}

func valueOrString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func valueOrInt(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func validInboxStatus(status string) bool {
	switch status {
	case "new", "needs_review", "handled", "archived", "filtered":
		return true
	default:
		return false
	}
}

func validCallbackStatus(status string) bool {
	switch status {
	case "requested", "dialed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func valueMetricsWindow(r *http.Request) (string, time.Time, time.Time, error) {
	sinceValue := strings.TrimSpace(r.URL.Query().Get("since"))
	untilValue := strings.TrimSpace(r.URL.Query().Get("until"))
	if sinceValue != "" || untilValue != "" {
		if sinceValue == "" || untilValue == "" {
			return "", time.Time{}, time.Time{}, errors.New("since and until must be provided together")
		}
		since, err := time.Parse(time.RFC3339, sinceValue)
		if err != nil {
			return "", time.Time{}, time.Time{}, errors.New("invalid since value")
		}
		until, err := time.Parse(time.RFC3339, untilValue)
		if err != nil {
			return "", time.Time{}, time.Time{}, errors.New("invalid until value")
		}
		if !since.Before(until) {
			return "", time.Time{}, time.Time{}, errors.New("since must be before until")
		}
		return "custom", since.UTC(), until.UTC(), nil
	}

	now := time.Now().UTC()
	period := queryString(r, "period", "month")
	switch period {
	case "month":
		since := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return period, since, now, nil
	case "7d":
		return period, now.AddDate(0, 0, -7), now, nil
	case "30d":
		return period, now.AddDate(0, 0, -30), now, nil
	default:
		return "", time.Time{}, time.Time{}, errors.New("period must be month, 7d or 30d")
	}
}

func serviceStatusResponse(
	merchant domain.Merchant,
	profile domain.MerchantProfile,
	subscription domain.ServiceSubscription,
	metrics domain.ValueMetrics,
	wechatBound bool,
) map[string]any {
	return map[string]any{
		"merchant_id":                  merchant.MerchantID,
		"merchant":                     merchant,
		"subscription":                 subscription,
		"plan":                         planDefinition(subscription.PlanCode),
		"add_ons":                      []map[string]any{voiceCloneAddon()},
		"metrics":                      metrics,
		"status_text":                  subscriptionStatusText(subscription),
		"trial_days_remaining":         trialDaysRemaining(subscription, time.Now().UTC()),
		"onboarding_steps":             onboardingSteps(merchant, profile, subscription, wechatBound),
		"call_forwarding_instructions": callForwardingInstructions(merchant.AccessNumber),
	}
}

func adminPilotStatus(subscription domain.ServiceSubscription, failedNotifications int, failedRetries int) string {
	if failedNotifications > 0 || failedRetries > 0 {
		return "needs_attention"
	}
	switch subscription.Status {
	case "active", "trialing":
		return "healthy"
	case "not_started":
		return "not_started"
	default:
		return "inactive"
	}
}

func adminPilotStatusText(subscription domain.ServiceSubscription, failedNotifications int, failedRetries int) string {
	if failedNotifications > 0 || failedRetries > 0 {
		return "需处理异常"
	}
	switch subscription.Status {
	case "active":
		return "服务中"
	case "trialing":
		return "试用中"
	case "not_started":
		return "未开通"
	default:
		return "未启用"
	}
}

func firstPaymentOrder(items []domain.PaymentOrder) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func countPaymentOrders(items []domain.PaymentOrder, status string) int {
	total := 0
	for _, item := range items {
		if item.Status == status {
			total++
		}
	}
	return total
}

func sumPaidPaymentOrders(items []domain.PaymentOrder) int {
	total := 0
	for _, item := range items {
		if item.Status == "paid" {
			total += item.AmountCents
		}
	}
	return total
}

func firstNotificationLog(items []domain.NotificationLog) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func firstBusinessResultRetry(items []domain.BusinessResultRetry) any {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

func appendNotificationLogs(primary []domain.NotificationLog, secondary []domain.NotificationLog, limit int) []domain.NotificationLog {
	result := make([]domain.NotificationLog, 0, minInt(limit, len(primary)+len(secondary)))
	for _, item := range primary {
		if len(result) >= limit {
			return result
		}
		result = append(result, item)
	}
	for _, item := range secondary {
		if len(result) >= limit {
			return result
		}
		result = append(result, item)
	}
	return result
}

func appendBusinessResultRetries(primary []domain.BusinessResultRetry, secondary []domain.BusinessResultRetry, limit int) []domain.BusinessResultRetry {
	result := make([]domain.BusinessResultRetry, 0, minInt(limit, len(primary)+len(secondary)))
	for _, item := range primary {
		if len(result) >= limit {
			return result
		}
		result = append(result, item)
	}
	for _, item := range secondary {
		if len(result) >= limit {
			return result
		}
		result = append(result, item)
	}
	return result
}

func businessResultRetryMerchantID(item domain.BusinessResultRetry) string {
	var payload struct {
		MerchantID string `json:"merchant_id"`
	}
	if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.MerchantID)
}

func validAccessNumberStatus(status string) bool {
	switch status {
	case "available", "reserved", "assigned", "disabled":
		return true
	default:
		return false
	}
}

func accessNumberPayloadNumbers(payload adminAccessNumberPayload) []string {
	seen := map[string]bool{}
	items := []string{}
	values := append([]string{}, payload.Numbers...)
	if payload.Number != "" {
		values = append(values, payload.Number)
	}
	for _, value := range values {
		number := store.NormalizeNumber(value)
		if number == "" || seen[number] {
			continue
		}
		seen[number] = true
		items = append(items, number)
	}
	return items
}

func accessNumbersOverview(items []domain.AccessNumber) map[string]int {
	result := map[string]int{
		"total":     len(items),
		"available": 0,
		"reserved":  0,
		"assigned":  0,
		"disabled":  0,
	}
	for _, item := range items {
		if _, ok := result[item.Status]; ok {
			result[item.Status]++
		}
	}
	return result
}

func (s *Server) fetchJambonzMaps(ctx context.Context, apiBaseURL string, path string, apiToken string, key string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURLPath(apiBaseURL, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errString("jambonz api " + path + " returned " + strconv.Itoa(res.StatusCode) + ": " + string(body))
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	return mapsFromAPIResponse(raw, key), nil
}

func joinURLPath(base string, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = strings.TrimSpace(path)
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func mapsFromAPIResponse(raw any, key string) []map[string]any {
	switch typed := raw.(type) {
	case []any:
		return mapSlice(typed)
	case map[string]any:
		for _, candidate := range []string{key, "items", "data", "rows", "results"} {
			if items := mapSlice(typed[candidate]); len(items) > 0 {
				return items
			}
		}
	}
	return nil
}

func jambonzPhoneNumbers(payload jambonzConfigExportPayload) []map[string]any {
	items := append([]map[string]any{}, payload.PhoneNumbers...)
	items = append(items, payload.Numbers...)
	if len(items) == 0 && payload.Raw != nil {
		items = append(items, mapSlice(payload.Raw["phone_numbers"])...)
		items = append(items, mapSlice(payload.Raw["numbers"])...)
	}
	return items
}

func jambonzApplicationsByID(payload jambonzConfigExportPayload) map[string]jambonzApplicationSnapshot {
	rawApplications := append([]map[string]any{}, payload.Applications...)
	if len(rawApplications) == 0 && payload.Raw != nil {
		rawApplications = mapSlice(payload.Raw["applications"])
	}
	items := map[string]jambonzApplicationSnapshot{}
	for _, raw := range rawApplications {
		app := jambonzApplicationSnapshot{
			ID:            firstMapString(raw, "jambonz_application_id", "application_id", "application_sid", "application_uuid", "sid", "id"),
			Name:          firstMapString(raw, "name", "application_name"),
			CallHookURL:   firstWebhookURL(raw, "call_hook", "call_hook_url", "calling_webhook", "webhook_url", "url"),
			StatusHookURL: firstWebhookURL(raw, "status_hook", "status_hook_url", "status_webhook", "call_status_hook", "call_status_hook_url"),
		}
		if app.ID != "" {
			items[app.ID] = app
		}
	}
	return items
}

func mapSlice(value any) []map[string]any {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item, ok := raw.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func firstMapString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := item[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case map[string]any:
			if nested := firstMapString(typed, "id", "sid", "url", "href", "name"); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func firstWebhookURL(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := item[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case map[string]any:
			if nested := firstMapString(typed, "url", "href", "webhook_url"); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func sameWebhookURL(actual string, expected string) bool {
	actual = strings.TrimRight(strings.TrimSpace(actual), "/")
	expected = strings.TrimRight(strings.TrimSpace(expected), "/")
	if actual == "" || expected == "" {
		return false
	}
	return strings.EqualFold(actual, expected)
}

func (s *Server) accessNumberRouteCheck(item domain.AccessNumber) (accessNumberRouteCheck, error) {
	return s.accessNumberRouteCheckWithExpected(item, s.cfg.Jambonz.ExpectedCallHookURL, s.cfg.Jambonz.ExpectedStatusHookURL)
}

func (s *Server) accessNumberRouteCheckWithExpected(item domain.AccessNumber, expectedCallHookURL string, expectedStatusHookURL string) (accessNumberRouteCheck, error) {
	check := accessNumberRouteCheck{
		Number:     item.Number,
		Status:     item.Status,
		RouteReady: true,
		Severity:   "ready",
		Issues:     []string{},
		Checks: map[string]bool{
			"has_number":                 strings.TrimSpace(item.Number) != "",
			"not_disabled":               item.Status != "disabled",
			"has_sip_trunk":              strings.TrimSpace(item.TrunkID) != "",
			"has_jambonz_application":    strings.TrimSpace(item.JambonzApplicationID) != "",
			"has_jambonz_call_hook":      strings.TrimSpace(item.JambonzCallHookURL) != "",
			"call_hook_matches_rosie":    expectedCallHookURL == "" || sameWebhookURL(item.JambonzCallHookURL, expectedCallHookURL),
			"status_hook_matches_rosie":  expectedStatusHookURL == "" || sameWebhookURL(item.JambonzStatusHookURL, expectedStatusHookURL),
			"assigned_merchant_exists":   true,
			"merchant_access_consistent": true,
			"inbound_lookup_consistent":  true,
		},
		Metadata: item,
	}
	if !check.Checks["has_number"] {
		check.Issues = append(check.Issues, "number is empty")
	}
	if !check.Checks["not_disabled"] {
		check.Issues = append(check.Issues, "number is disabled")
	}
	if !check.Checks["has_sip_trunk"] {
		check.Issues = append(check.Issues, "missing sip trunk binding")
	}
	if !check.Checks["has_jambonz_application"] {
		check.Issues = append(check.Issues, "missing jambonz application binding")
	}
	if !check.Checks["has_jambonz_call_hook"] {
		check.Issues = append(check.Issues, "missing jambonz call hook url")
	}
	if !check.Checks["call_hook_matches_rosie"] {
		check.Issues = append(check.Issues, "jambonz call hook does not match Rosie call hook")
	}
	if !check.Checks["status_hook_matches_rosie"] {
		check.Issues = append(check.Issues, "jambonz status hook does not match Rosie status hook")
	}

	if item.Status == "assigned" || item.MerchantID != "" {
		merchant, ok, err := s.repo.FindMerchantByID(item.MerchantID)
		if err != nil {
			return accessNumberRouteCheck{}, err
		}
		if !ok {
			check.Checks["assigned_merchant_exists"] = false
			check.Issues = append(check.Issues, "assigned merchant does not exist or is disabled")
		} else {
			check.Merchant = &merchant
			if store.NormalizeNumber(merchant.AccessNumber) != item.Number {
				check.Checks["merchant_access_consistent"] = false
				check.Issues = append(check.Issues, "merchant access_number does not match pool assignment")
			}
			inboundMerchant, inboundOK, err := s.repo.FindMerchantByAccessNumber(item.Number)
			if err != nil {
				return accessNumberRouteCheck{}, err
			}
			if !inboundOK || inboundMerchant.MerchantID != item.MerchantID {
				check.Checks["inbound_lookup_consistent"] = false
				check.Issues = append(check.Issues, "inbound lookup by access number does not resolve assigned merchant")
			}
		}
	}

	if len(check.Issues) > 0 {
		check.RouteReady = false
		check.Severity = "blocked"
		if item.Status == "available" || item.Status == "reserved" {
			check.Severity = "warning"
		}
		if item.Status == "disabled" {
			check.Severity = "disabled"
		}
	}
	return check, nil
}

func (s *Server) accessNumberRouteReadyForAssignment(item domain.AccessNumber) bool {
	return item.Status == "available" &&
		strings.TrimSpace(item.MerchantID) == "" &&
		strings.TrimSpace(item.Number) != "" &&
		strings.TrimSpace(item.TrunkID) != "" &&
		strings.TrimSpace(item.JambonzApplicationID) != "" &&
		strings.TrimSpace(item.JambonzCallHookURL) != "" &&
		(s.cfg.Jambonz.ExpectedCallHookURL == "" || sameWebhookURL(item.JambonzCallHookURL, s.cfg.Jambonz.ExpectedCallHookURL)) &&
		(s.cfg.Jambonz.ExpectedStatusHookURL == "" || sameWebhookURL(item.JambonzStatusHookURL, s.cfg.Jambonz.ExpectedStatusHookURL))
}

func adminPilotMatches(item adminPilotSummary, query string) bool {
	if query == "" {
		return true
	}
	values := []string{
		item.Merchant.MerchantID,
		item.Merchant.MerchantName,
		item.Merchant.AccessNumber,
		item.Merchant.OriginalNumber,
		item.Merchant.TransferPhone,
		item.Profile.Industry,
		item.Profile.Address,
		item.Subscription.PlanCode,
		item.Subscription.Status,
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func adminPilotsOverview(items []adminPilotSummary) adminPilotOverview {
	overview := adminPilotOverview{Total: len(items)}
	for _, item := range items {
		switch item.Status {
		case "healthy":
			overview.Healthy++
		case "needs_attention":
			overview.NeedsAttention++
		case "not_started":
			overview.NotStarted++
		default:
			overview.Inactive++
		}
		overview.TotalCalls += item.Metrics.TotalCalls
		overview.EffectiveCalls += item.Metrics.EffectiveCalls
		overview.Appointments += item.Metrics.AppointmentCount
		overview.FailedNotifications += anyInt(item.Notifications["problem_count"])
		overview.FailedRetries += anyInt(item.BusinessResultRetry["problem_count"])
		overview.PendingOrders += anyInt(item.Payment["pending_order_count"])
		overview.PaidOrders += anyInt(item.Payment["paid_order_count"])
		overview.TotalPaidCents += anyInt(item.Payment["total_paid_cents"])
	}
	return overview
}

func anyInt(value any) int {
	switch item := value.(type) {
	case int:
		return item
	case int64:
		return int(item)
	case float64:
		return int(item)
	default:
		return 0
	}
}

func paymentOrderResponse(order domain.PaymentOrder, cfg config.Config, paymentStatus string, requestParams map[string]string) map[string]any {
	configured := wechatPayConfigured(cfg)
	status := "pending_provider_config"
	if configured {
		status = "pending_payment"
	}
	if paymentStatus != "" {
		status = paymentStatus
	}
	return map[string]any{
		"status": status,
		"order":  order,
		"payment": map[string]any{
			"provider":       "wechat_pay_jsapi",
			"configured":     configured,
			"amount_cents":   order.AmountCents,
			"currency":       order.Currency,
			"notify_url":     cfg.WeChatPay.NotifyURL,
			"prepay_id":      order.PrepayID,
			"request_params": requestParams,
		},
	}
}

func planDefinition(planCode string) map[string]any {
	if planCode != "pilot_basic" {
		return map[string]any{}
	}
	return map[string]any{
		"code":                "pilot_basic",
		"name":                "试点基础版",
		"price_text":          "30 元/月",
		"monthly_price_cents": 3000,
		"trial_days":          14,
		"features": []string{
			"AI 接听来电并生成摘要",
			"小程序收件箱和通话详情",
			"每日汇总和关键提醒",
			"手动回拨审计",
			"试点价值看板",
		},
	}
}

func wechatPayConfigured(cfg config.Config) bool {
	return wechatpay.Configured(cfg.WeChat.AppID, cfg.WeChatPay)
}

func newOrderNo() string {
	var randomBytes [4]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "rosie_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "rosie_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + hex.EncodeToString(randomBytes[:])
}

func voiceCloneAddon() map[string]any {
	return map[string]any{
		"code":                "voice_clone",
		"name":                "老板音色增值服务",
		"price_text":          "+5 元/月",
		"monthly_price_cents": 500,
		"status":              "planned",
	}
}

func subscriptionStatusText(subscription domain.ServiceSubscription) string {
	switch subscription.Status {
	case "trialing":
		return "试用中"
	case "active":
		return "服务中"
	case "past_due":
		return "待续费"
	case "paused":
		return "已暂停"
	default:
		return "未开通"
	}
}

func trialDaysRemaining(subscription domain.ServiceSubscription, now time.Time) int {
	if subscription.TrialEndsAt == nil || subscription.TrialEndsAt.Before(now) {
		return 0
	}
	hours := subscription.TrialEndsAt.Sub(now).Hours()
	days := int(hours / 24)
	if hours > float64(days*24) {
		days++
	}
	return days
}

func onboardingSteps(
	merchant domain.Merchant,
	profile domain.MerchantProfile,
	subscription domain.ServiceSubscription,
	wechatBound bool,
) []map[string]any {
	return []map[string]any{
		onboardingStep("activate_trial", "开通试用", subscription.Status == "trialing" || subscription.Status == "active"),
		onboardingStep("merchant_profile", "完善商家配置", merchant.MerchantName != "" && merchant.AccessNumber != "" && len(profile.Services) > 0),
		onboardingStep("wechat_binding", "绑定微信通知", wechatBound),
		onboardingStep("call_forwarding", "设置条件呼叫转移", merchant.AccessNumber != ""),
	}
}

func onboardingStep(key string, label string, done bool) map[string]any {
	status := "pending"
	if done {
		status = "done"
	}
	return map[string]any{
		"key":    key,
		"label":  label,
		"status": status,
		"done":   done,
	}
}

func callForwardingInstructions(accessNumber string) []map[string]string {
	return []map[string]string{
		{"scenario": "无人接听", "dial_code": "**61*" + accessNumber + "#"},
		{"scenario": "忙线", "dial_code": "**67*" + accessNumber + "#"},
		{"scenario": "不可及", "dial_code": "**62*" + accessNumber + "#"},
	}
}

func monthStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func wechatConfigured(cfg config.WeChatConfig) bool {
	return cfg.AppID != "" && cfg.AppSecret != "" && cfg.TemplateID != ""
}

func wechatConfigStatus(cfg config.WeChatConfig) string {
	if wechatConfigured(cfg) {
		return "configured"
	}
	return "missing_config"
}

var timePattern = regexp.MustCompile(`^\d{2}:\d{2}$`)

func validatePreferences(prefs domain.NotificationPreferences) error {
	switch prefs.DigestMode {
	case "daily", "twice_daily", "hourly", "manual":
	default:
		return errString("invalid digest_mode")
	}
	if len(prefs.DigestTimes) == 0 {
		return errString("digest_times cannot be empty")
	}
	for _, item := range prefs.DigestTimes {
		if !validClock(item) {
			return errString("invalid digest time: " + item)
		}
	}
	if prefs.QuietHoursStart != "" && !validClock(prefs.QuietHoursStart) {
		return errString("invalid quiet hour: " + prefs.QuietHoursStart)
	}
	if prefs.QuietHoursEnd != "" && !validClock(prefs.QuietHoursEnd) {
		return errString("invalid quiet hour: " + prefs.QuietHoursEnd)
	}
	return nil
}

func validClock(value string) bool {
	if !timePattern.MatchString(value) {
		return false
	}
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func parseTickTime(w http.ResponseWriter, value string) (time.Time, bool) {
	if value == "" {
		return time.Now(), true
	}
	if timePattern.MatchString(value) {
		now := time.Now()
		parsed, err := time.Parse("15:04", value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid now value")
			return time.Time{}, false
		}
		return time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location()), true
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", value)
	if err != nil {
		parsed, err = time.Parse("2006-01-02T15:04", value)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid now value")
		return time.Time{}, false
	}
	return parsed, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
}

func writeIngestError(w http.ResponseWriter, err error) {
	if item, ok := err.(apiError); ok {
		writeError(w, item.status, item.detail)
		return
	}
	writeStoreError(w, err)
}

const adminPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Rosie 运营后台</title>
  <style>
    :root { color-scheme: light; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; background: #f5f6f8; color: #1d2129; }
    header { height: 60px; display: flex; align-items: center; justify-content: space-between; padding: 0 24px; background: #fff; border-bottom: 1px solid #e5e6eb; }
    h1 { margin: 0; font-size: 18px; letter-spacing: 0; }
    button, input, select { font: inherit; }
    button { border: 1px solid #c9cdd4; background: #fff; border-radius: 6px; padding: 8px 12px; cursor: pointer; }
    button.primary { border-color: #165dff; background: #165dff; color: #fff; }
    button.danger { border-color: #f53f3f; color: #c41d1d; }
    button:disabled { cursor: not-allowed; opacity: .55; }
    input, select { border: 1px solid #c9cdd4; border-radius: 6px; padding: 8px 10px; background: #fff; min-width: 150px; }
    main { padding: 18px 24px 24px; }
    .bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 14px; }
    .filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
    .muted { color: #606773; font-size: 13px; }
    .overview { display: grid; grid-template-columns: repeat(6, minmax(130px, 1fr)); gap: 10px; margin-bottom: 14px; }
    .metric { background: #fff; border: 1px solid #e5e6eb; border-radius: 8px; padding: 12px; }
    .metric .label { color: #606773; font-size: 12px; }
    .metric .value { margin-top: 6px; font-size: 22px; font-weight: 700; }
    .layout { display: grid; grid-template-columns: minmax(720px, 1fr) 420px; gap: 14px; align-items: start; }
    .panel { background: #fff; border: 1px solid #e5e6eb; border-radius: 8px; overflow: hidden; }
    .route-panel { margin-bottom: 14px; }
    .panel-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; border-bottom: 1px solid #e5e6eb; }
    .panel-title { font-weight: 700; }
    table { width: 100%; border-collapse: collapse; table-layout: fixed; }
    th, td { padding: 11px 12px; border-bottom: 1px solid #edf0f3; text-align: left; vertical-align: top; font-size: 13px; }
    th { color: #606773; background: #f7f8fa; font-weight: 600; }
    tr { cursor: pointer; }
    tr:hover td, tr.active td { background: #f2f6ff; }
    .merchant-name { font-weight: 700; margin-bottom: 3px; overflow-wrap: anywhere; }
    .cell-sub { color: #606773; font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
    .badge { display: inline-flex; align-items: center; border-radius: 999px; padding: 3px 8px; font-size: 12px; background: #e8f3ff; color: #165dff; white-space: nowrap; }
    .badge.needs_attention { background: #fff1f0; color: #cf1322; }
    .badge.not_started, .badge.inactive { background: #f2f3f5; color: #4e5969; }
    .badge.ready { background: #e8ffea; color: #1f7a1f; }
    .badge.warning { background: #fff7e6; color: #ad6800; }
    .badge.blocked { background: #fff1f0; color: #cf1322; }
    .badge.disabled { background: #f2f3f5; color: #606773; }
    .money { font-weight: 700; }
    .detail { padding: 14px; }
    .detail h2 { margin: 0 0 4px; font-size: 18px; letter-spacing: 0; }
    .actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin: 14px 0; }
    .section { border-top: 1px solid #edf0f3; padding-top: 12px; margin-top: 12px; }
    .section-title { font-size: 13px; font-weight: 700; margin-bottom: 8px; }
    .kv { display: grid; grid-template-columns: 112px 1fr; gap: 6px 10px; font-size: 13px; line-height: 1.55; }
    .kv div:nth-child(odd) { color: #606773; }
    .list { display: grid; gap: 8px; }
    .item { border: 1px solid #edf0f3; border-radius: 6px; padding: 8px; font-size: 13px; line-height: 1.55; background: #fafbfc; overflow-wrap: anywhere; }
    .empty, .error { padding: 18px; color: #606773; text-align: center; }
    .error { display: none; color: #cf1322; background: #fff1f0; border: 1px solid #ffccc7; border-radius: 6px; margin-bottom: 12px; }
    .toast { position: fixed; right: 18px; bottom: 18px; max-width: 420px; background: #1d2129; color: #fff; border-radius: 8px; padding: 12px 14px; font-size: 13px; display: none; }
    @media (max-width: 1180px) {
      .overview { grid-template-columns: repeat(3, minmax(130px, 1fr)); }
      .layout { grid-template-columns: 1fr; }
    }
    @media (max-width: 720px) {
      header { padding: 0 14px; }
      main { padding: 14px; }
      .overview { grid-template-columns: repeat(2, minmax(120px, 1fr)); }
      .bar { align-items: stretch; flex-direction: column; }
      input, select, button { width: 100%; }
      th:nth-child(4), td:nth-child(4), th:nth-child(5), td:nth-child(5) { display: none; }
      .actions { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header>
    <h1>Rosie 运营后台</h1>
    <div id="generated" class="muted"></div>
  </header>
  <main>
    <div class="bar">
      <div class="filters">
        <input id="query" placeholder="搜索商家、号码、行业">
        <select id="status">
          <option value="">全部状态</option>
          <option value="needs_attention">需处理异常</option>
          <option value="healthy">服务中</option>
          <option value="not_started">未开通</option>
          <option value="inactive">未启用</option>
        </select>
        <button id="search" class="primary">查询</button>
        <button id="refresh">刷新</button>
      </div>
      <div class="muted">试点商家管理、异常处理、付费和价值跟踪</div>
    </div>
    <div id="error" class="error"></div>
    <section id="overview" class="overview"></section>
    <section class="panel route-panel">
      <div class="panel-head">
        <div>
          <div class="panel-title">号码路由校验</div>
          <div id="routeSummary" class="muted"></div>
        </div>
        <div class="filters">
          <button id="syncJambonz">同步 jambonz</button>
          <button id="refreshRoutes">刷新校验</button>
        </div>
      </div>
      <div id="routes"></div>
    </section>
    <section class="layout">
      <div class="panel">
        <div class="panel-head">
          <div class="panel-title">商家列表</div>
          <div id="total" class="muted"></div>
        </div>
        <div id="table"></div>
      </div>
      <aside class="panel">
        <div class="panel-head">
          <div class="panel-title">商家详情</div>
          <a id="jsonLink" class="muted" href="#" target="_blank">JSON</a>
        </div>
        <div id="detail" class="detail empty">请选择一个商家</div>
      </aside>
    </section>
  </main>
  <div id="toast" class="toast"></div>
  <script>
    const state = { items: [], selected: "", loading: false };
    const table = document.querySelector("#table");
    const detail = document.querySelector("#detail");
    const overview = document.querySelector("#overview");
    const routes = document.querySelector("#routes");
    const routeSummary = document.querySelector("#routeSummary");
    const errorBox = document.querySelector("#error");
    const generated = document.querySelector("#generated");
    const total = document.querySelector("#total");
    const toast = document.querySelector("#toast");
    const jsonLink = document.querySelector("#jsonLink");
    document.querySelector("#search").addEventListener("click", load);
    document.querySelector("#refresh").addEventListener("click", load);
    document.querySelector("#refreshRoutes").addEventListener("click", loadRoutes);
    document.querySelector("#syncJambonz").addEventListener("click", syncJambonz);
    document.querySelector("#query").addEventListener("keydown", (event) => { if (event.key === "Enter") load(); });
    document.querySelector("#status").addEventListener("change", load);

    function money(cents) {
      return (Number(cents || 0) / 100).toFixed(2) + " 元";
    }
    function dateText(value) {
      if (!value) return "-";
      return String(value).replace("T", " ").replace(/\.\d+Z$/, "").replace("Z", "");
    }
    function pct(value) {
      return (Number(value || 0) * 100).toFixed(0) + "%";
    }
    function escapeHtml(value) {
      return String(value ?? "").replace(/[&<>"']/g, ch => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[ch]));
    }
    function metric(label, value) {
      return '<div class="metric"><div class="label">' + label + '</div><div class="value">' + escapeHtml(value) + '</div></div>';
    }
    function renderOverview(data) {
      const item = data || {};
      overview.innerHTML =
        metric("试点商家", item.total || 0) +
        metric("需处理", item.needs_attention || 0) +
        metric("本月来电", item.total_calls || 0) +
        metric("有效线索", item.effective_calls || 0) +
        metric("预约", item.appointments || 0) +
        metric("已收款", money(item.total_paid_cents));
      total.textContent = "共 " + (item.total || 0) + " 家";
    }
    function renderTable(items) {
      if (!items.length) {
        table.innerHTML = '<div class="empty">没有符合条件的商家</div>';
        return;
      }
      table.innerHTML = '<table><thead><tr>' +
        '<th style="width:28%">商家</th><th style="width:15%">状态</th><th style="width:15%">价值</th><th style="width:15%">支付</th><th style="width:14%">异常</th><th style="width:13%">最近来电</th>' +
        '</tr></thead><tbody>' + items.map(rowHTML).join("") + '</tbody></table>';
      table.querySelectorAll("tr[data-id]").forEach((row) => {
        row.addEventListener("click", () => selectPilot(row.dataset.id));
      });
    }
    function renderRoutes(data) {
      const overview = data.overview || {};
      const items = data.items || [];
      routeSummary.textContent = '共 ' + (overview.total || 0) + ' 个号码，ready ' + (overview.ready || 0) + '，warning ' + (overview.warning || 0) + '，blocked ' + (overview.blocked || 0);
      if (!items.length) {
        routes.innerHTML = '<div class="empty">暂无号码路由配置</div>';
        return;
      }
      routes.innerHTML = '<table><thead><tr>' +
        '<th style="width:18%">接入号</th><th style="width:12%">状态</th><th style="width:18%">SIP / App</th><th style="width:28%">Call Hook</th><th style="width:24%">问题</th>' +
        '</tr></thead><tbody>' + items.map(routeRowHTML).join("") + '</tbody></table>';
    }
    function routeRowHTML(item) {
      const meta = item.metadata || {};
      const issues = item.issues || [];
      return '<tr>' +
        '<td><div class="merchant-name">' + escapeHtml(item.number || "-") + '</div><div class="cell-sub">' + escapeHtml((item.merchant || {}).merchant_id || meta.merchant_id || "-") + '</div></td>' +
        '<td><span class="badge ' + escapeHtml(item.severity || "") + '">' + escapeHtml(item.severity || "-") + '</span><div class="cell-sub">' + escapeHtml(item.status || "-") + '</div></td>' +
        '<td>' + escapeHtml(meta.trunk_id || "-") + '<div class="cell-sub">' + escapeHtml(meta.jambonz_application_id || "-") + '</div></td>' +
        '<td>' + escapeHtml(meta.jambonz_call_hook_url || "-") + '<div class="cell-sub">' + escapeHtml(meta.jambonz_status_hook_url || "") + '</div></td>' +
        '<td>' + (issues.length ? escapeHtml(issues.join("；")) : '<span class="muted">OK</span>') + '</td>' +
      '</tr>';
    }
    function rowHTML(item) {
      const merchant = item.merchant || {};
      const metrics = item.metrics || {};
      const payment = item.payment || {};
      const notifications = item.notifications || {};
      const retry = item.business_result_retry || {};
      const latest = (item.recent_calls || [])[0];
      const abnormal = Number(notifications.problem_count || notifications.failed_count || 0) + Number(retry.problem_count || retry.failed_count || 0);
      return '<tr data-id="' + escapeHtml(merchant.merchant_id || "") + '" class="' + (merchant.merchant_id === state.selected ? "active" : "") + '">' +
        '<td><div class="merchant-name">' + escapeHtml(merchant.merchant_name || merchant.merchant_id) + '</div><div class="cell-sub">' + escapeHtml(merchant.merchant_id || "") + '<br>接入号 ' + escapeHtml(merchant.access_number || "-") + '</div></td>' +
        '<td><span class="badge ' + escapeHtml(item.status || "") + '">' + escapeHtml(item.status_text || item.status || "-") + '</span><div class="cell-sub">' + escapeHtml((item.subscription || {}).plan_code || "-") + '</div></td>' +
        '<td><strong>' + (metrics.total_calls || 0) + '</strong> 通<div class="cell-sub">有效 ' + (metrics.effective_calls || 0) + '，预约率 ' + pct(metrics.appointment_rate) + '</div></td>' +
        '<td><span class="money">' + money(payment.total_paid_cents) + '</span><div class="cell-sub">待付 ' + (payment.pending_order_count || 0) + '，已付 ' + (payment.paid_order_count || 0) + '</div></td>' +
        '<td><strong>' + abnormal + '</strong><div class="cell-sub">通知 ' + (notifications.problem_count || notifications.failed_count || 0) + '，上报 ' + (retry.problem_count || retry.failed_count || 0) + '</div></td>' +
        '<td>' + dateText(latest && latest.created_at) + '</td>' +
      '</tr>';
    }
    async function selectPilot(id) {
      state.selected = id;
      renderTable(state.items);
      detail.innerHTML = '<div class="empty">加载中</div>';
      jsonLink.href = "/admin/pilots/" + encodeURIComponent(id);
      try {
        const res = await fetch("/admin/pilots/" + encodeURIComponent(id));
        if (!res.ok) throw new Error(await res.text());
        renderDetail(await res.json());
      } catch (err) {
        detail.innerHTML = '<div class="error" style="display:block">' + escapeHtml(err.message || String(err)) + '</div>';
      }
    }
    function renderDetail(item) {
      const merchant = item.merchant || {};
      const profile = item.profile || {};
      const subscription = item.subscription || {};
      const metrics = item.metrics || {};
      const payment = item.payment || {};
      const notifications = item.notifications || {};
      const retry = item.business_result_retry || {};
      detail.innerHTML =
        '<h2>' + escapeHtml(merchant.merchant_name || merchant.merchant_id) + '</h2>' +
        '<div class="muted">' + escapeHtml(profile.industry || "未设置行业") + ' / ' + escapeHtml(merchant.access_number || "-") + '</div>' +
        '<div class="actions">' +
          '<button class="primary" data-action="activate">开通 14 天试用</button>' +
          '<button data-action="assignNumber">分配接入号</button>' +
          '<button data-action="releaseNumber">释放接入号</button>' +
          '<button data-action="notify">重派失败通知</button>' +
          '<button data-action="retry">重放失败上报</button>' +
          '<button class="danger" data-action="json">打开 JSON</button>' +
        '</div>' +
        section("开通与支付", kv([
          ["状态", item.status_text || item.status || "-"],
          ["套餐", subscription.plan_code || "-"],
          ["订阅", subscription.status || "-"],
          ["接入号", merchant.access_number || "-"],
          ["试用剩余", (item.trial_days_remaining || 0) + " 天"],
          ["当前周期至", dateText(subscription.current_period_ends_at)],
          ["已收款", money(payment.total_paid_cents)]
        ])) +
        section("本月价值", kv([
          ["来电", metrics.total_calls || 0],
          ["有效线索", metrics.effective_calls || 0],
          ["预约", metrics.appointment_count || 0],
          ["回拨请求", metrics.callback_requested_count || 0],
          ["节省时间", (metrics.estimated_saved_minutes || 0) + " 分钟"],
          ["预约率", pct(metrics.appointment_rate)]
        ])) +
        section("异常队列", kv([
          ["失败通知", notifications.problem_count || notifications.failed_count || 0],
          ["排队通知", notifications.queued_count || 0],
          ["失败上报", retry.problem_count || retry.failed_count || 0],
          ["通知错误", ((notifications.latest_failed || {}).last_error || "-")],
          ["上报错误", ((retry.latest_failed || {}).last_error || "-")]
        ])) +
        section("最近订单", listOrders(item.recent_orders || [])) +
        section("最近来电", listCalls(item.recent_calls || []));
      detail.querySelector('[data-action="activate"]').addEventListener("click", () => action("activate"));
      detail.querySelector('[data-action="assignNumber"]').addEventListener("click", () => action("assignNumber"));
      detail.querySelector('[data-action="releaseNumber"]').addEventListener("click", () => action("releaseNumber"));
      detail.querySelector('[data-action="notify"]').addEventListener("click", () => action("notify"));
      detail.querySelector('[data-action="retry"]').addEventListener("click", () => action("retry"));
      detail.querySelector('[data-action="json"]').addEventListener("click", () => window.open(jsonLink.href, "_blank"));
    }
    function section(title, body) {
      return '<div class="section"><div class="section-title">' + title + '</div>' + body + '</div>';
    }
    function kv(rows) {
      return '<div class="kv">' + rows.map((row) => '<div>' + escapeHtml(row[0]) + '</div><div>' + escapeHtml(row[1]) + '</div>').join("") + '</div>';
    }
    function listOrders(items) {
      if (!items.length) return '<div class="muted">暂无订单</div>';
      return '<div class="list">' + items.map((item) => '<div class="item">' + escapeHtml(item.order_no) + ' / ' + escapeHtml(item.status) + '<br>' + money(item.amount_cents) + ' / ' + dateText(item.created_at) + '</div>').join("") + '</div>';
    }
    function listCalls(items) {
      if (!items.length) return '<div class="muted">暂无来电</div>';
      return '<div class="list">' + items.map((item) => '<div class="item">' + escapeHtml(item.call_sid) + '<br>' + escapeHtml(item.from_number || "-") + ' / ' + dateText(item.created_at) + '</div>').join("") + '</div>';
    }
    async function action(type) {
      if (!state.selected || state.loading) return;
      const map = {
        activate: ["/admin/pilots/" + encodeURIComponent(state.selected) + "/activate-trial", {plan_code: "pilot_basic"}],
        assignNumber: ["/admin/pilots/" + encodeURIComponent(state.selected) + "/assign-access-number", {}],
        releaseNumber: ["/admin/pilots/" + encodeURIComponent(state.selected) + "/release-access-number", {}],
        notify: ["/admin/pilots/" + encodeURIComponent(state.selected) + "/dispatch-notifications", {status: "failed", limit: 20}],
        retry: ["/admin/pilots/" + encodeURIComponent(state.selected) + "/flush-retries", {max_attempts: 5, limit: 20}]
      };
      const target = map[type];
      if (!target) return;
      state.loading = true;
      showToast("处理中");
      try {
        const res = await fetch(target[0], {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(target[1])});
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        showToast("操作完成，处理 " + (data.total ?? 1) + " 条");
        await load(true);
        await selectPilot(state.selected);
      } catch (err) {
        showToast(err.message || String(err));
      } finally {
        state.loading = false;
      }
    }
    function showToast(text) {
      toast.textContent = text;
      toast.style.display = "block";
      window.clearTimeout(showToast.timer);
      showToast.timer = window.setTimeout(() => { toast.style.display = "none"; }, 2800);
    }
    async function loadRoutes() {
      try {
        const res = await fetch("/admin/access-numbers/route-check");
        if (!res.ok) throw new Error(await res.text());
        renderRoutes(await res.json());
      } catch (err) {
        routes.innerHTML = '<div class="error" style="display:block">' + escapeHtml(err.message || String(err)) + '</div>';
      }
    }
    async function syncJambonz() {
      if (state.loading) return;
      state.loading = true;
      showToast("正在同步 jambonz");
      try {
        const res = await fetch("/admin/jambonz/sync", {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}"});
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        showToast("同步完成，导入 " + (data.imported || 0) + " 个号码");
        await loadRoutes();
        await load(true);
      } catch (err) {
        showToast(err.message || String(err));
      } finally {
        state.loading = false;
      }
    }
    async function load(keepSelection) {
      errorBox.style.display = "none";
      const params = new URLSearchParams();
      const q = document.querySelector("#query").value.trim();
      const status = document.querySelector("#status").value;
      if (q) params.set("q", q);
      if (status) params.set("status", status);
      try {
        const res = await fetch("/admin/pilots?" + params.toString());
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        state.items = data.items || [];
        generated.textContent = "生成时间 " + dateText(data.generated);
        renderOverview(data.overview || {});
        renderTable(state.items);
        await loadRoutes();
        if (!keepSelection && state.items.length) {
          await selectPilot((state.items[0].merchant || {}).merchant_id || "");
        } else if (!state.items.length) {
          state.selected = "";
          detail.innerHTML = '<div class="empty">请选择一个商家</div>';
        }
      } catch (err) {
        errorBox.textContent = err.message || String(err);
        errorBox.style.display = "block";
      }
    }
    load(false);
  </script>
</body>
</html>`

func queryString(r *http.Request, name string, fallback string) string {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback
	}
	return value
}

func queryInt(r *http.Request, name string, fallback int, max int) int {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	if parsed > max {
		return max
	}
	return parsed
}

type errString string

func (e errString) Error() string {
	return string(e)
}
