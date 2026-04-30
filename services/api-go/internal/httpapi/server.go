package httpapi

import (
	"encoding/json"
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
)

type Server struct {
	repo   store.Repository
	cfg    config.Config
	wechat *wechat.Client
}

func NewServer(repo store.Repository, cfg config.Config) *Server {
	return &Server{repo: repo, cfg: cfg, wechat: wechat.NewClient(cfg.WeChat, nil)}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /auth/wechat-login", s.wechatLogin)
	mux.HandleFunc("GET /merchants", s.listMerchants)
	mux.HandleFunc("POST /merchants", s.upsertMerchant)
	mux.HandleFunc("GET /merchant-profile", s.getMerchantProfile)
	mux.HandleFunc("PUT /merchant-profile", s.updateMerchantProfile)
	mux.HandleFunc("GET /industry-templates", s.listIndustryTemplates)
	mux.HandleFunc("GET /calls", s.listCalls)
	mux.HandleFunc("GET /calls/{call_sid}", s.getCallDetail)
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
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	if payload.MerchantID == "" || payload.MerchantName == "" || payload.AccessNumber == "" {
		writeError(w, http.StatusBadRequest, "merchant_id, merchant_name and access_number are required")
		return
	}
	merchant, err := s.repo.UpsertMerchant(payload)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "merchant_id": merchant.MerchantID})
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
		"call":       call,
		"transcript": transcriptValue,
		"summary":    summaryValue,
		"inbox":      inboxValue,
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

func (s *Server) simulateCallResult(w http.ResponseWriter, r *http.Request) {
	var payload callResultPayload
	if !decodeJSON(w, r, &payload) {
		return
	}
	result, ok := s.ingestCallResult(w, payload, "simulated")
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
	result, ok := s.ingestCallResult(w, payload, "realtime_voice")
	if !ok {
		return
	}
	writeCallResultResponse(w, result)
}

func (s *Server) ingestCallResult(w http.ResponseWriter, payload callResultPayload, source string) (callResultIngest, bool) {
	payload.CallSID = strings.TrimSpace(payload.CallSID)
	payload.CallID = strings.TrimSpace(payload.CallID)
	payload.MerchantID = strings.TrimSpace(payload.MerchantID)
	payload.FromNumber = strings.TrimSpace(payload.FromNumber)
	payload.ToNumber = strings.TrimSpace(payload.ToNumber)
	payload.Transcript = strings.TrimSpace(valueOrString(payload.Transcript, transcriptFromTurns(payload.Turns)))
	if payload.CallSID == "" || payload.Transcript == "" {
		writeError(w, http.StatusBadRequest, "call_sid and transcript are required")
		return callResultIngest{}, false
	}

	merchant, toNumber, ok := s.resolveCallResultMerchant(w, payload)
	if !ok {
		return callResultIngest{}, false
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
		writeStoreError(w, err)
		return callResultIngest{}, false
	}
	if _, err := s.repo.UpsertTranscript(domain.Transcript{
		CallSID:    payload.CallSID,
		MerchantID: merchant.MerchantID,
		Transcript: payload.Transcript,
		Source:     source,
	}); err != nil {
		writeStoreError(w, err)
		return callResultIngest{}, false
	}

	callSummary := summary.Fallback(payload.Transcript)
	callSummary.CallSID = payload.CallSID
	callSummary.MerchantID = merchant.MerchantID
	callSummary, err := s.repo.UpsertSummary(callSummary)
	if err != nil {
		writeStoreError(w, err)
		return callResultIngest{}, false
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
		writeStoreError(w, err)
		return callResultIngest{}, false
	}

	var realtimeLog *domain.NotificationLog
	if source == "realtime_voice" {
		log, err := s.queueRealtimeNotification(merchant.MerchantID, payload.CallSID, callSummary, inbox)
		if err != nil {
			writeStoreError(w, err)
			return callResultIngest{}, false
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
	}, true
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
	if payload.MerchantID != "" {
		merchant, ok, err := s.repo.FindMerchantByID(payload.MerchantID)
		if err != nil {
			writeStoreError(w, err)
			return domain.Merchant{}, "", false
		}
		if !ok {
			writeError(w, http.StatusNotFound, "unknown merchant")
			return domain.Merchant{}, "", false
		}
		return merchant, valueOrString(payload.ToNumber, merchant.AccessNumber), true
	}

	toNumber := valueOrString(payload.ToNumber, s.cfg.DefaultAccessNumber)
	merchant, ok, err := s.repo.FindMerchantByAccessNumber(toNumber)
	if err != nil {
		writeStoreError(w, err)
		return domain.Merchant{}, "", false
	}
	if !ok {
		writeError(w, http.StatusNotFound, "unknown access number")
		return domain.Merchant{}, "", false
	}
	return merchant, toNumber, true
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

	items, err := s.repo.ListNotificationLogs("", dispatchStatus, queryInt(r, "limit", 20, 100))
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
	if err := s.wechat.SendSubscribe(r.Context(), item, target); err != nil {
		status = "failed"
		lastError = err.Error()
	}
	updated, err := s.repo.UpdateNotificationLogStatus(item.ID, status, item.AttemptCount+1, lastError)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":            updated.ID,
		"merchant_id":   updated.MerchantID,
		"status":        updated.Status,
		"attempt_count": updated.AttemptCount,
		"last_error":    updated.LastError,
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
	writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": err.Error()})
}

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
