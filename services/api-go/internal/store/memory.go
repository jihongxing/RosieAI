package store

import (
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
	mu          sync.Mutex
	nextID      int64
	merchants   map[string]domain.Merchant
	profiles    map[string]domain.MerchantProfile
	accessIndex map[string]string
	calls       map[string]domain.Call
	transcripts map[string]domain.Transcript
	summaries   map[string]domain.Summary
	inbox       map[string]domain.InboxItem
	digests     []domain.Digest
	preferences map[string]domain.NotificationPreferences
	logs        []domain.NotificationLog
	logsByKey   map[string]int
	users       map[string]domain.AppUser
	bindings    map[string]domain.MerchantUserBinding
}

func NewMemory() *Memory {
	return &Memory{
		nextID:      1,
		merchants:   map[string]domain.Merchant{},
		profiles:    map[string]domain.MerchantProfile{},
		accessIndex: map[string]string{},
		calls:       map[string]domain.Call{},
		transcripts: map[string]domain.Transcript{},
		summaries:   map[string]domain.Summary{},
		inbox:       map[string]domain.InboxItem{},
		digests:     []domain.Digest{},
		preferences: map[string]domain.NotificationPreferences{},
		logs:        []domain.NotificationLog{},
		logsByKey:   map[string]int{},
		users:       map[string]domain.AppUser{},
		bindings:    map[string]domain.MerchantUserBinding{},
	}
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
	m.merchants[merchant.MerchantID] = merchant
	m.accessIndex[merchant.AccessNumber] = merchant.MerchantID
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
