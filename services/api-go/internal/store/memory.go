package store

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"rosie-api/internal/domain"
)

var ErrNotFound = errors.New("not found")

type Memory struct {
	mu                     sync.Mutex
	nextID                 int64
	merchants              map[string]domain.Merchant
	accessNumbers          map[string]domain.AccessNumber
	subscriptions          map[string]domain.ServiceSubscription
	paymentOrders          []domain.PaymentOrder
	paymentOrdersByNo      map[string]int
	profiles               map[string]domain.MerchantProfile
	accessIndex            map[string]string
	calls                  map[string]domain.Call
	transcripts            map[string]domain.Transcript
	summaries              map[string]domain.Summary
	inbox                  map[string]domain.InboxItem
	callbacks              []domain.CallbackRequest
	digests                []domain.Digest
	preferences            map[string]domain.NotificationPreferences
	logs                   []domain.NotificationLog
	logsByKey              map[string]int
	resultRetries          []domain.BusinessResultRetry
	resultRetriesBySession map[string]int
	users                  map[string]domain.AppUser
	bindings               map[string]domain.MerchantUserBinding
}

func NewMemory() *Memory {
	return &Memory{
		nextID:                 1,
		merchants:              map[string]domain.Merchant{},
		accessNumbers:          map[string]domain.AccessNumber{},
		subscriptions:          map[string]domain.ServiceSubscription{},
		paymentOrders:          []domain.PaymentOrder{},
		paymentOrdersByNo:      map[string]int{},
		profiles:               map[string]domain.MerchantProfile{},
		accessIndex:            map[string]string{},
		calls:                  map[string]domain.Call{},
		transcripts:            map[string]domain.Transcript{},
		summaries:              map[string]domain.Summary{},
		inbox:                  map[string]domain.InboxItem{},
		callbacks:              []domain.CallbackRequest{},
		digests:                []domain.Digest{},
		preferences:            map[string]domain.NotificationPreferences{},
		logs:                   []domain.NotificationLog{},
		logsByKey:              map[string]int{},
		resultRetries:          []domain.BusinessResultRetry{},
		resultRetriesBySession: map[string]int{},
		users:                  map[string]domain.AppUser{},
		bindings:               map[string]domain.MerchantUserBinding{},
	}
}

func (m *Memory) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (m *Memory) UpsertMerchant(merchant domain.Merchant) (domain.Merchant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if merchant.CreatedAt.IsZero() {
		if existing, ok := m.merchants[merchant.MerchantID]; ok {
			merchant.CreatedAt = existing.CreatedAt
		} else {
			merchant.CreatedAt = now
		}
	}
	merchant.AccessNumber = NormalizeNumber(merchant.AccessNumber)
	if !merchant.Enabled {
		merchant.Enabled = true
	}
	if existing, ok := m.merchants[merchant.MerchantID]; ok && existing.AccessNumber != merchant.AccessNumber {
		delete(m.accessIndex, existing.AccessNumber)
	}
	m.merchants[merchant.MerchantID] = merchant
	if merchant.AccessNumber != "" {
		m.accessIndex[merchant.AccessNumber] = merchant.MerchantID
	}
	m.ensureServiceSubscriptionLocked(merchant.MerchantID)
	m.ensureMerchantProfileLocked(merchant.MerchantID)
	m.ensurePreferencesLocked(merchant.MerchantID)
	return merchant, nil
}

func (m *Memory) ListMerchants() ([]domain.Merchant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.Merchant, 0, len(m.merchants))
	for _, merchant := range m.merchants {
		items = append(items, merchant)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (m *Memory) FindMerchantByAccessNumber(accessNumber string) (domain.Merchant, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	merchantID, ok := m.accessIndex[NormalizeNumber(accessNumber)]
	if !ok {
		return domain.Merchant{}, false, nil
	}
	merchant := m.merchants[merchantID]
	if !merchant.Enabled {
		return domain.Merchant{}, false, nil
	}
	return merchant, true, nil
}

func (m *Memory) FindMerchantByID(merchantID string) (domain.Merchant, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	merchant, ok := m.merchants[merchantID]
	if !ok || !merchant.Enabled {
		return domain.Merchant{}, false, nil
	}
	return merchant, true, nil
}

func (m *Memory) UpsertAccessNumber(item domain.AccessNumber) (domain.AccessNumber, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	item.Number = NormalizeNumber(item.Number)
	if item.Number == "" {
		return domain.AccessNumber{}, ErrNotFound
	}
	if existing, ok := m.accessNumbers[item.Number]; ok {
		if item.ID == 0 {
			item.ID = existing.ID
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = existing.CreatedAt
		}
		if item.Status == "" {
			item.Status = existing.Status
		}
		if item.MerchantID == "" {
			item.MerchantID = existing.MerchantID
		}
		if item.AssignedAt == nil {
			item.AssignedAt = existing.AssignedAt
		}
		if item.ReleasedAt == nil {
			item.ReleasedAt = existing.ReleasedAt
		}
		if item.JambonzApplicationName == "" {
			item.JambonzApplicationName = existing.JambonzApplicationName
		}
		if item.JambonzCallHookURL == "" {
			item.JambonzCallHookURL = existing.JambonzCallHookURL
		}
		if item.JambonzStatusHookURL == "" {
			item.JambonzStatusHookURL = existing.JambonzStatusHookURL
		}
		if item.JambonzConfigSyncedAt == nil {
			item.JambonzConfigSyncedAt = existing.JambonzConfigSyncedAt
		}
	}
	if item.ID == 0 {
		item.ID = m.nextID
		m.nextID++
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.Status == "" {
		item.Status = "available"
	}
	item.UpdatedAt = now
	m.accessNumbers[item.Number] = item
	return item, nil
}

func (m *Memory) ListAccessNumbers(status string, limit int) ([]domain.AccessNumber, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limit <= 0 {
		limit = 100
	}
	items := make([]domain.AccessNumber, 0, len(m.accessNumbers))
	for _, item := range m.accessNumbers {
		if status != "" && item.Status != status {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Status == items[j].Status {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].Status < items[j].Status
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (m *Memory) FindAccessNumberByNumber(number string) (domain.AccessNumber, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.accessNumbers[NormalizeNumber(number)]
	return item, ok, nil
}

func (m *Memory) AssignAccessNumber(merchantID string, number string, assignedAt time.Time) (domain.AccessNumber, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.assignAccessNumberLocked(merchantID, NormalizeNumber(number), assignedAt)
}

func (m *Memory) AutoAssignAccessNumber(merchantID string, assignedAt time.Time) (domain.AccessNumber, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, item := range m.accessNumbers {
		if item.Status == "assigned" && item.MerchantID == merchantID {
			return item, true, nil
		}
	}
	items := make([]domain.AccessNumber, 0, len(m.accessNumbers))
	for _, item := range m.accessNumbers {
		if item.Status == "available" && item.MerchantID == "" {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].Number < items[j].Number
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if len(items) == 0 {
		return domain.AccessNumber{}, false, nil
	}
	item, err := m.assignAccessNumberLocked(merchantID, items[0].Number, assignedAt)
	return item, true, err
}

func (m *Memory) ReleaseAccessNumber(number string, releasedAt time.Time) (domain.AccessNumber, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.accessNumbers[NormalizeNumber(number)]
	if !ok {
		return domain.AccessNumber{}, ErrNotFound
	}
	if item.MerchantID != "" {
		if merchant, ok := m.merchants[item.MerchantID]; ok && merchant.AccessNumber == item.Number {
			merchant.AccessNumber = ""
			m.merchants[item.MerchantID] = merchant
		}
		delete(m.accessIndex, item.Number)
	}
	item.Status = "available"
	item.MerchantID = ""
	item.AssignedAt = nil
	item.ReleasedAt = &releasedAt
	item.UpdatedAt = releasedAt
	m.accessNumbers[item.Number] = item
	return item, nil
}

func (m *Memory) EnsureServiceSubscription(merchantID string) (domain.ServiceSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureServiceSubscriptionLocked(merchantID), nil
}

func (m *Memory) ActivateTrialSubscription(merchantID string, planCode string, startedAt time.Time, endsAt time.Time) (domain.ServiceSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.ensureServiceSubscriptionLocked(merchantID)
	if existing.Status == "active" || existing.Status == "trialing" {
		return existing, nil
	}
	now := time.Now().UTC()
	existing.PlanCode = valueOr(planCode, "pilot_basic")
	existing.Status = "trialing"
	existing.TrialStartedAt = timePtr(startedAt)
	existing.TrialEndsAt = timePtr(endsAt)
	existing.CurrentPeriodEndsAt = timePtr(endsAt)
	existing.ActivatedAt = timePtr(startedAt)
	existing.UpdatedAt = now
	m.subscriptions[merchantID] = existing
	return existing, nil
}

func (m *Memory) RenewServiceSubscription(merchantID string, planCode string, paidAt time.Time, months int) (domain.ServiceSubscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if months <= 0 {
		months = 1
	}
	existing := m.ensureServiceSubscriptionLocked(merchantID)
	base := paidAt.UTC()
	if existing.CurrentPeriodEndsAt != nil && existing.CurrentPeriodEndsAt.After(base) {
		base = existing.CurrentPeriodEndsAt.UTC()
	}
	periodEnd := base.AddDate(0, months, 0)
	existing.PlanCode = valueOr(planCode, "pilot_basic")
	existing.Status = "active"
	existing.CurrentPeriodEndsAt = timePtr(periodEnd)
	if existing.ActivatedAt == nil {
		existing.ActivatedAt = timePtr(paidAt)
	}
	existing.UpdatedAt = time.Now().UTC()
	m.subscriptions[merchantID] = existing
	return existing, nil
}

func (m *Memory) InsertPaymentOrder(order domain.PaymentOrder) (domain.PaymentOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index, ok := m.paymentOrdersByNo[order.OrderNo]; ok {
		return m.paymentOrders[index], nil
	}
	now := time.Now().UTC()
	order.ID = m.takeIDLocked()
	order.CreatedAt = now
	order.UpdatedAt = now
	order.Currency = valueOr(order.Currency, "CNY")
	order.Status = valueOr(order.Status, "pending")
	order.Provider = valueOr(order.Provider, "wechat_pay")
	order.OrderType = valueOr(order.OrderType, "renewal")
	m.paymentOrdersByNo[order.OrderNo] = len(m.paymentOrders)
	m.paymentOrders = append(m.paymentOrders, order)
	return order, nil
}

func (m *Memory) FindPaymentOrderByNo(orderNo string) (domain.PaymentOrder, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index, ok := m.paymentOrdersByNo[orderNo]
	if !ok {
		return domain.PaymentOrder{}, false, nil
	}
	return m.paymentOrders[index], true, nil
}

func (m *Memory) ListPaymentOrders(merchantID string, limit int) ([]domain.PaymentOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.PaymentOrder, 0)
	for _, order := range m.paymentOrders {
		if order.MerchantID == merchantID {
			items = append(items, order)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) UpdatePaymentOrderPrepay(orderNo string, prepayID string) (domain.PaymentOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index, ok := m.paymentOrdersByNo[orderNo]
	if !ok {
		return domain.PaymentOrder{}, ErrNotFound
	}
	order := m.paymentOrders[index]
	order.PrepayID = prepayID
	order.UpdatedAt = time.Now().UTC()
	m.paymentOrders[index] = order
	return order, nil
}

func (m *Memory) MarkPaymentOrderPaid(orderNo string, providerTradeNo string, paidAt time.Time) (domain.PaymentOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index, ok := m.paymentOrdersByNo[orderNo]
	if !ok {
		return domain.PaymentOrder{}, ErrNotFound
	}
	order := m.paymentOrders[index]
	if order.Status == "paid" {
		return order, nil
	}
	order.Status = "paid"
	order.ProviderTradeNo = providerTradeNo
	order.PaidAt = timePtr(paidAt)
	order.UpdatedAt = time.Now().UTC()
	m.paymentOrders[index] = order
	return order, nil
}

func (m *Memory) GetValueMetrics(merchantID string, since time.Time, until time.Time) (domain.ValueMetrics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	metrics := domain.ValueMetrics{
		MerchantID: merchantID,
		Since:      since,
		Until:      until,
	}
	spamCallSIDs := map[string]bool{}
	for _, call := range m.calls {
		if call.MerchantID == merchantID && inWindow(call.CreatedAt, since, until) {
			metrics.TotalCalls++
		}
	}
	for _, item := range m.inbox {
		if item.MerchantID != merchantID || !inWindow(item.CreatedAt, since, until) {
			continue
		}
		if item.Status != "filtered" {
			metrics.EffectiveCalls++
		}
		if item.Status == "filtered" {
			spamCallSIDs[item.CallSID] = true
		}
		if item.NeedHumanFollowup {
			metrics.FollowupCount++
		}
		if item.Priority == "urgent" || item.Priority == "high" {
			metrics.UrgentCount++
		}
		if item.Status == "handled" {
			metrics.HandledCount++
		}
		if item.Status == "archived" {
			metrics.ArchivedCount++
		}
	}
	for _, summary := range m.summaries {
		if summary.MerchantID != merchantID || !inWindow(summary.CreatedAt, since, until) {
			continue
		}
		if summary.Intent == "appointment" {
			metrics.AppointmentCount++
		}
		if summary.Intent == "spam" {
			spamCallSIDs[summary.CallSID] = true
		}
	}
	metrics.SpamCount = len(spamCallSIDs)
	for _, request := range m.callbacks {
		if request.MerchantID != merchantID || !inWindow(request.CreatedAt, since, until) {
			continue
		}
		metrics.CallbackRequestedCount++
		if request.Status == "dialed" {
			metrics.CallbackDialedCount++
		}
	}
	finalizeValueMetrics(&metrics)
	return metrics, nil
}

func (m *Memory) EnsureMerchantProfile(merchantID string) (domain.MerchantProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureMerchantProfileLocked(merchantID), nil
}

func (m *Memory) UpdateMerchantProfile(profile domain.MerchantProfile) (domain.MerchantProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.ensureMerchantProfileLocked(profile.MerchantID)
	profile.CreatedAt = existing.CreatedAt
	profile.UpdatedAt = time.Now().UTC()
	if profile.Industry == "" {
		profile.Industry = "hair_salon"
	}
	if profile.Services == nil {
		profile.Services = []string{}
	}
	if profile.FAQItems == nil {
		profile.FAQItems = []domain.FAQItem{}
	}
	m.profiles[profile.MerchantID] = profile
	return profile, nil
}

func (m *Memory) UpsertCall(call domain.Call) (domain.Call, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if existing, ok := m.calls[call.CallSID]; ok {
		call.ID = existing.ID
		call.CreatedAt = existing.CreatedAt
		call.UpdatedAt = now
	} else {
		call.ID = m.takeIDLocked()
		call.CreatedAt = now
		call.UpdatedAt = now
	}
	m.calls[call.CallSID] = call
	return call, nil
}

func (m *Memory) ListCalls(limit int) ([]domain.Call, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.Call, 0, len(m.calls))
	for _, call := range m.calls {
		items = append(items, call)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) FindCallBySID(callSID string) (domain.Call, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.calls[callSID]
	return item, ok, nil
}

func (m *Memory) UpsertTranscript(transcript domain.Transcript) (domain.Transcript, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.transcripts[transcript.CallSID]; ok {
		transcript.ID = existing.ID
		transcript.CreatedAt = existing.CreatedAt
	} else {
		transcript.ID = m.takeIDLocked()
		transcript.CreatedAt = time.Now().UTC()
	}
	m.transcripts[transcript.CallSID] = transcript
	return transcript, nil
}

func (m *Memory) FindTranscriptByCallSID(callSID string) (domain.Transcript, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.transcripts[callSID]
	return item, ok, nil
}

func (m *Memory) UpsertSummary(summary domain.Summary) (domain.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.summaries[summary.CallSID]; ok {
		summary.ID = existing.ID
		summary.CreatedAt = existing.CreatedAt
	} else {
		summary.ID = m.takeIDLocked()
		summary.CreatedAt = time.Now().UTC()
	}
	m.summaries[summary.CallSID] = summary
	return summary, nil
}

func (m *Memory) FindSummaryByCallSID(callSID string) (domain.Summary, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.summaries[callSID]
	return item, ok, nil
}

func (m *Memory) UpsertInboxItem(item domain.InboxItem) (domain.InboxItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if existing, ok := m.inbox[item.CallSID]; ok {
		item.ID = existing.ID
		item.CreatedAt = existing.CreatedAt
		item.UpdatedAt = now
	} else {
		item.ID = m.takeIDLocked()
		item.CreatedAt = now
		item.UpdatedAt = now
	}
	if item.ItemType == "" {
		item.ItemType = "call_summary"
	}
	if item.DigestStatus == "" {
		item.DigestStatus = "pending"
	}
	m.inbox[item.CallSID] = item
	return item, nil
}

func (m *Memory) FindInboxItemByCallSID(callSID string) (domain.InboxItem, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.inbox[callSID]
	return item, ok, nil
}

func (m *Memory) ListInboxItems(merchantID string, limit int) ([]domain.InboxItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.InboxItem, 0)
	for _, item := range m.inbox {
		if item.MerchantID == merchantID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) UpdateInboxItemStatus(callSID string, status string) (domain.InboxItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, ok := m.inbox[callSID]
	if !ok {
		return domain.InboxItem{}, ErrNotFound
	}
	item.Status = status
	item.UpdatedAt = time.Now().UTC()
	m.inbox[callSID] = item
	return item, nil
}

func (m *Memory) InsertCallbackRequest(request domain.CallbackRequest) (domain.CallbackRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	request.ID = m.takeIDLocked()
	request.CreatedAt = now
	request.UpdatedAt = now
	if request.Status == "" {
		request.Status = "requested"
	}
	m.callbacks = append(m.callbacks, request)
	return request, nil
}

func (m *Memory) ListCallbackRequestsByCallSID(callSID string, limit int) ([]domain.CallbackRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.CallbackRequest, 0)
	for _, request := range m.callbacks {
		if request.OriginalCallSID == callSID {
			items = append(items, request)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) UpdateCallbackRequestStatus(id int64, callSID string, status string, auditNote string) (domain.CallbackRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for index, request := range m.callbacks {
		if request.ID == id && request.OriginalCallSID == callSID {
			request.Status = status
			if auditNote != "" {
				request.AuditNote = auditNote
			}
			request.UpdatedAt = time.Now().UTC()
			m.callbacks[index] = request
			return request, nil
		}
	}
	return domain.CallbackRequest{}, ErrNotFound
}

func (m *Memory) ListPendingDigestItems(merchantID string, limit int) ([]domain.InboxItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.InboxItem, 0)
	for _, item := range m.inbox {
		if item.MerchantID == merchantID && item.DigestStatus == "pending" {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) InsertDigest(digest domain.Digest) (domain.Digest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	digest.ID = m.takeIDLocked()
	digest.CreatedAt = time.Now().UTC()
	if digest.Status == "" {
		digest.Status = "generated"
	}
	m.digests = append(m.digests, digest)
	return digest, nil
}

func (m *Memory) MarkInboxItemsDigested(itemIDs []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idSet := map[int64]bool{}
	for _, id := range itemIDs {
		idSet[id] = true
	}
	for callSID, item := range m.inbox {
		if idSet[item.ID] {
			item.DigestStatus = "digested"
			item.UpdatedAt = time.Now().UTC()
			m.inbox[callSID] = item
		}
	}
	return nil
}

func (m *Memory) ListDigests(merchantID string, limit int) ([]domain.Digest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.Digest, 0)
	for _, digest := range m.digests {
		if digest.MerchantID == merchantID {
			items = append(items, digest)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) EnsurePreferences(merchantID string) (domain.NotificationPreferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensurePreferencesLocked(merchantID), nil
}

func (m *Memory) UpdatePreferences(prefs domain.NotificationPreferences) (domain.NotificationPreferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.ensurePreferencesLocked(prefs.MerchantID)
	prefs.CreatedAt = existing.CreatedAt
	prefs.UpdatedAt = time.Now().UTC()
	m.preferences[prefs.MerchantID] = prefs
	return prefs, nil
}

func (m *Memory) InsertNotificationLog(log domain.NotificationLog) (domain.NotificationLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index, ok := m.logsByKey[log.IdempotencyKey]; ok {
		existing := m.logs[index]
		existing.UpdatedAt = time.Now().UTC()
		m.logs[index] = existing
		return existing, nil
	}
	now := time.Now().UTC()
	log.ID = m.takeIDLocked()
	log.CreatedAt = now
	log.UpdatedAt = now
	if log.Status == "" {
		log.Status = "queued"
	}
	if log.MaxAttempts == 0 {
		log.MaxAttempts = 5
	}
	m.logsByKey[log.IdempotencyKey] = len(m.logs)
	m.logs = append(m.logs, log)
	return log, nil
}

func (m *Memory) FindNotificationLogByKey(key string) (domain.NotificationLog, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	index, ok := m.logsByKey[key]
	if !ok {
		return domain.NotificationLog{}, false, nil
	}
	return m.logs[index], true, nil
}

func (m *Memory) ListNotificationLogs(merchantID string, status string, limit int) ([]domain.NotificationLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.NotificationLog, 0)
	for _, log := range m.logs {
		if merchantID != "" && log.MerchantID != merchantID {
			continue
		}
		if status != "" && log.Status != status {
			continue
		}
		items = append(items, log)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) UpdateNotificationLogStatus(id int64, status string, attemptCount int, lastError string) (domain.NotificationLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for index, item := range m.logs {
		if item.ID == id {
			item.Status = status
			item.AttemptCount = attemptCount
			item.LastError = lastError
			item.UpdatedAt = time.Now().UTC()
			m.logs[index] = item
			return item, nil
		}
	}
	return domain.NotificationLog{}, ErrNotFound
}

func (m *Memory) ListDueNotificationLogs(status string, limit int, now time.Time) ([]domain.NotificationLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.NotificationLog, 0)
	for _, log := range m.logs {
		if log.Status != status {
			continue
		}
		if log.MaxAttempts > 0 && log.AttemptCount >= log.MaxAttempts {
			continue
		}
		if log.NextRetryAt != nil && log.NextRetryAt.After(now) {
			continue
		}
		items = append(items, log)
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].NextRetryAt, items[j].NextRetryAt
		if left == nil && right != nil {
			return true
		}
		if left != nil && right == nil {
			return false
		}
		if left != nil && right != nil && !left.Equal(*right) {
			return left.Before(*right)
		}
		return items[i].ID < items[j].ID
	})
	return trim(items, limit), nil
}

func (m *Memory) UpdateNotificationLogDispatch(
	id int64,
	status string,
	attemptCount int,
	maxAttempts int,
	lastError string,
	errorCategory string,
	nextRetryAt time.Time,
) (domain.NotificationLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for index, item := range m.logs {
		if item.ID == id {
			item.Status = status
			item.AttemptCount = attemptCount
			item.MaxAttempts = maxAttempts
			item.LastError = lastError
			item.ErrorCategory = errorCategory
			if nextRetryAt.IsZero() {
				item.NextRetryAt = nil
			} else {
				value := nextRetryAt
				item.NextRetryAt = &value
			}
			item.UpdatedAt = time.Now().UTC()
			m.logs[index] = item
			return item, nil
		}
	}
	return domain.NotificationLog{}, ErrNotFound
}

func (m *Memory) UpsertBusinessResultRetry(job domain.BusinessResultRetry) (domain.BusinessResultRetry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if index, ok := m.resultRetriesBySession[job.SessionID]; ok {
		existing := m.resultRetries[index]
		existing.CallSID = job.CallSID
		existing.Payload = job.Payload
		existing.Status = valueOr(job.Status, "failed")
		existing.AttemptCount = job.AttemptCount
		existing.LastError = job.LastError
		existing.UpdatedAt = now
		m.resultRetries[index] = existing
		return existing, nil
	}

	job.ID = m.takeIDLocked()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Status == "" {
		job.Status = "failed"
	}
	m.resultRetriesBySession[job.SessionID] = len(m.resultRetries)
	m.resultRetries = append(m.resultRetries, job)
	return job, nil
}

func (m *Memory) ListBusinessResultRetries(status string, limit int) ([]domain.BusinessResultRetry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]domain.BusinessResultRetry, 0)
	for _, job := range m.resultRetries {
		if status != "" && job.Status != status {
			continue
		}
		items = append(items, job)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return trim(items, limit), nil
}

func (m *Memory) UpdateBusinessResultRetryStatus(id int64, status string, attemptCount int, lastError string) (domain.BusinessResultRetry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for index, job := range m.resultRetries {
		if job.ID == id {
			job.Status = status
			job.AttemptCount = attemptCount
			job.LastError = lastError
			job.UpdatedAt = time.Now().UTC()
			m.resultRetries[index] = job
			return job, nil
		}
	}
	return domain.BusinessResultRetry{}, ErrNotFound
}

func (m *Memory) UpsertAppUser(user domain.AppUser) (domain.AppUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if existing, ok := m.users[user.OpenID]; ok {
		user.ID = existing.ID
		user.CreatedAt = existing.CreatedAt
		user.UpdatedAt = now
	} else {
		user.ID = m.takeIDLocked()
		user.CreatedAt = now
		user.UpdatedAt = now
	}
	m.users[user.OpenID] = user
	return user, nil
}

func (m *Memory) BindMerchantUser(merchantID string, userID int64, role string) (domain.MerchantUserBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := merchantID + ":" + itoa64(userID)
	now := time.Now().UTC()
	binding := domain.MerchantUserBinding{
		MerchantID: merchantID,
		UserID:     userID,
		Role:       valueOr(role, "owner"),
		Enabled:    true,
		UpdatedAt:  now,
	}
	if existing, ok := m.bindings[key]; ok {
		binding.ID = existing.ID
		binding.CreatedAt = existing.CreatedAt
	} else {
		binding.ID = m.takeIDLocked()
		binding.CreatedAt = now
	}
	m.bindings[key] = binding
	return binding, nil
}

func (m *Memory) FindPrimaryOpenIDByMerchantID(merchantID string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var selected domain.MerchantUserBinding
	for _, binding := range m.bindings {
		if binding.MerchantID != merchantID || !binding.Enabled {
			continue
		}
		if selected.ID == 0 || binding.ID < selected.ID {
			selected = binding
		}
	}
	if selected.ID == 0 {
		return "", false, nil
	}
	for _, user := range m.users {
		if user.ID == selected.UserID {
			return user.OpenID, true, nil
		}
	}
	return "", false, nil
}

func (m *Memory) ensureMerchantProfileLocked(merchantID string) domain.MerchantProfile {
	if profile, ok := m.profiles[merchantID]; ok {
		return profile
	}
	now := time.Now().UTC()
	profile := domain.MerchantProfile{
		MerchantID: merchantID,
		Industry:   "hair_salon",
		Services:   []string{},
		FAQItems:   []domain.FAQItem{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.profiles[merchantID] = profile
	return profile
}

func (m *Memory) ensureServiceSubscriptionLocked(merchantID string) domain.ServiceSubscription {
	if subscription, ok := m.subscriptions[merchantID]; ok {
		return subscription
	}
	now := time.Now().UTC()
	subscription := domain.ServiceSubscription{
		MerchantID: merchantID,
		PlanCode:   "pilot_basic",
		Status:     "not_started",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.subscriptions[merchantID] = subscription
	return subscription
}

func (m *Memory) ensurePreferencesLocked(merchantID string) domain.NotificationPreferences {
	if prefs, ok := m.preferences[merchantID]; ok {
		return prefs
	}
	now := time.Now().UTC()
	prefs := domain.NotificationPreferences{
		MerchantID:            merchantID,
		DigestMode:            "daily",
		DigestTimes:           []string{"20:00"},
		RealtimeEnabled:       false,
		UrgentRealtimeEnabled: true,
		TeamWecomEnabled:      false,
		SMSFallbackEnabled:    false,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	m.preferences[merchantID] = prefs
	return prefs
}

func (m *Memory) assignAccessNumberLocked(merchantID string, number string, assignedAt time.Time) (domain.AccessNumber, error) {
	item, ok := m.accessNumbers[number]
	if !ok {
		return domain.AccessNumber{}, ErrNotFound
	}
	if item.Status == "disabled" {
		return domain.AccessNumber{}, ErrNotFound
	}
	if item.MerchantID != "" && item.MerchantID != merchantID {
		return domain.AccessNumber{}, ErrNotFound
	}
	merchant, ok := m.merchants[merchantID]
	if !ok || !merchant.Enabled {
		return domain.AccessNumber{}, ErrNotFound
	}
	for existingNumber, existing := range m.accessNumbers {
		if existing.Status == "assigned" && existing.MerchantID == merchantID && existingNumber != number {
			existing.Status = "available"
			existing.MerchantID = ""
			existing.AssignedAt = nil
			existing.ReleasedAt = &assignedAt
			existing.UpdatedAt = assignedAt
			m.accessNumbers[existingNumber] = existing
			delete(m.accessIndex, existingNumber)
		}
	}
	if merchant.AccessNumber != "" && merchant.AccessNumber != number {
		delete(m.accessIndex, merchant.AccessNumber)
	}
	item.Status = "assigned"
	item.MerchantID = merchantID
	item.AssignedAt = &assignedAt
	item.ReleasedAt = nil
	item.UpdatedAt = assignedAt
	m.accessNumbers[number] = item
	merchant.AccessNumber = number
	m.merchants[merchantID] = merchant
	m.accessIndex[number] = merchantID
	return item, nil
}

func timePtr(value time.Time) *time.Time {
	v := value.UTC()
	return &v
}

func (m *Memory) takeIDLocked() int64 {
	id := m.nextID
	m.nextID++
	return id
}

func NormalizeNumber(value string) string {
	re := regexp.MustCompile(`\D`)
	return re.ReplaceAllString(strings.TrimSpace(value), "")
}

func trim[T any](items []T, limit int) []T {
	if limit <= 0 || limit > len(items) {
		return items
	}
	return items[:limit]
}

func itoa64(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func inWindow(value time.Time, since time.Time, until time.Time) bool {
	return !value.Before(since) && value.Before(until)
}

func finalizeValueMetrics(metrics *domain.ValueMetrics) {
	metrics.EstimatedSavedMinutes = metrics.TotalCalls*3 + metrics.SpamCount*2 + metrics.HandledCount
	if metrics.EffectiveCalls > 0 {
		metrics.AppointmentRate = float64(metrics.AppointmentCount) / float64(metrics.EffectiveCalls)
	}
	if metrics.CallbackRequestedCount > 0 {
		metrics.CallbackCompletionRate = float64(metrics.CallbackDialedCount) / float64(metrics.CallbackRequestedCount)
	}
}
