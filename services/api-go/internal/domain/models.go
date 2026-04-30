package domain

import "time"

type Merchant struct {
	MerchantID     string    `json:"merchant_id"`
	MerchantName   string    `json:"merchant_name"`
	AccessNumber   string    `json:"access_number"`
	OriginalNumber string    `json:"original_number,omitempty"`
	TransferPhone  string    `json:"transfer_phone,omitempty"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
}

type AccessNumber struct {
	ID                     int64      `json:"id"`
	Number                 string     `json:"number"`
	Provider               string     `json:"provider,omitempty"`
	ProviderNumberID       string     `json:"provider_number_id,omitempty"`
	TrunkID                string     `json:"trunk_id,omitempty"`
	JambonzApplicationID   string     `json:"jambonz_application_id,omitempty"`
	JambonzApplicationName string     `json:"jambonz_application_name,omitempty"`
	JambonzCallHookURL     string     `json:"jambonz_call_hook_url,omitempty"`
	JambonzStatusHookURL   string     `json:"jambonz_status_hook_url,omitempty"`
	JambonzConfigSyncedAt  *time.Time `json:"jambonz_config_synced_at,omitempty"`
	Status                 string     `json:"status"`
	MerchantID             string     `json:"merchant_id,omitempty"`
	Notes                  string     `json:"notes,omitempty"`
	AssignedAt             *time.Time `json:"assigned_at,omitempty"`
	ReleasedAt             *time.Time `json:"released_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type FAQItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type MerchantProfile struct {
	MerchantID       string    `json:"merchant_id"`
	Industry         string    `json:"industry"`
	Address          string    `json:"address,omitempty"`
	BusinessHours    string    `json:"business_hours,omitempty"`
	Services         []string  `json:"services"`
	FAQItems         []FAQItem `json:"faq_items"`
	AppointmentRules string    `json:"appointment_rules,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Call struct {
	ID         int64     `json:"id"`
	CallSID    string    `json:"call_sid"`
	CallID     string    `json:"call_id"`
	MerchantID string    `json:"merchant_id"`
	FromNumber string    `json:"from_number"`
	ToNumber   string    `json:"to_number"`
	CallStatus string    `json:"call_status"`
	Direction  string    `json:"direction"`
	RawPayload string    `json:"raw_payload"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Transcript struct {
	ID         int64     `json:"id"`
	CallSID    string    `json:"call_sid"`
	MerchantID string    `json:"merchant_id"`
	Transcript string    `json:"transcript"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
}

type Summary struct {
	ID                int64     `json:"id"`
	CallSID           string    `json:"call_sid"`
	MerchantID        string    `json:"merchant_id"`
	Summary           string    `json:"summary"`
	CustomerName      string    `json:"customer_name,omitempty"`
	CustomerPhone     string    `json:"customer_phone,omitempty"`
	Intent            string    `json:"intent"`
	AppointmentTime   string    `json:"appointment_time,omitempty"`
	Service           string    `json:"service,omitempty"`
	Priority          string    `json:"priority"`
	NeedHumanFollowup bool      `json:"need_human_followup"`
	RawResult         string    `json:"raw_result"`
	CreatedAt         time.Time `json:"created_at"`
}

type InboxItem struct {
	ID                int64     `json:"id"`
	MerchantID        string    `json:"merchant_id"`
	CallSID           string    `json:"call_sid"`
	ItemType          string    `json:"item_type"`
	Title             string    `json:"title"`
	Body              string    `json:"body"`
	Priority          string    `json:"priority"`
	Status            string    `json:"status"`
	NeedHumanFollowup bool      `json:"need_human_followup"`
	DigestStatus      string    `json:"digest_status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type CallbackRequest struct {
	ID              int64     `json:"id"`
	MerchantID      string    `json:"merchant_id"`
	OriginalCallSID string    `json:"original_call_sid"`
	OriginalCallID  string    `json:"original_call_id,omitempty"`
	TargetNumber    string    `json:"target_number"`
	RequestedBy     string    `json:"requested_by,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Status          string    `json:"status"`
	AuditNote       string    `json:"audit_note,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ValueMetrics struct {
	MerchantID             string    `json:"merchant_id"`
	Period                 string    `json:"period"`
	Since                  time.Time `json:"since"`
	Until                  time.Time `json:"until"`
	TotalCalls             int       `json:"total_calls"`
	EffectiveCalls         int       `json:"effective_calls"`
	AppointmentCount       int       `json:"appointment_count"`
	SpamCount              int       `json:"spam_count"`
	FollowupCount          int       `json:"followup_count"`
	UrgentCount            int       `json:"urgent_count"`
	HandledCount           int       `json:"handled_count"`
	ArchivedCount          int       `json:"archived_count"`
	CallbackRequestedCount int       `json:"callback_requested_count"`
	CallbackDialedCount    int       `json:"callback_dialed_count"`
	EstimatedSavedMinutes  int       `json:"estimated_saved_minutes"`
	AppointmentRate        float64   `json:"appointment_rate"`
	CallbackCompletionRate float64   `json:"callback_completion_rate"`
}

type ServiceSubscription struct {
	MerchantID          string     `json:"merchant_id"`
	PlanCode            string     `json:"plan_code"`
	Status              string     `json:"status"`
	TrialStartedAt      *time.Time `json:"trial_started_at,omitempty"`
	TrialEndsAt         *time.Time `json:"trial_ends_at,omitempty"`
	CurrentPeriodEndsAt *time.Time `json:"current_period_ends_at,omitempty"`
	ActivatedAt         *time.Time `json:"activated_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type PaymentOrder struct {
	ID              int64      `json:"id"`
	MerchantID      string     `json:"merchant_id"`
	OrderNo         string     `json:"order_no"`
	OrderType       string     `json:"order_type"`
	PlanCode        string     `json:"plan_code"`
	AddOnCode       string     `json:"add_on_code,omitempty"`
	AmountCents     int        `json:"amount_cents"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	Provider        string     `json:"provider"`
	ProviderTradeNo string     `json:"provider_trade_no,omitempty"`
	PrepayID        string     `json:"prepay_id,omitempty"`
	PaidAt          *time.Time `json:"paid_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Digest struct {
	ID            int64     `json:"id"`
	MerchantID    string    `json:"merchant_id"`
	DigestType    string    `json:"digest_type"`
	ItemCount     int       `json:"item_count"`
	UrgentCount   int       `json:"urgent_count"`
	FollowupCount int       `json:"followup_count"`
	SpamCount     int       `json:"spam_count"`
	DigestText    string    `json:"digest_text"`
	ItemIDs       []int64   `json:"item_ids"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type NotificationPreferences struct {
	MerchantID            string    `json:"merchant_id"`
	DigestMode            string    `json:"digest_mode"`
	DigestTimes           []string  `json:"digest_times"`
	RealtimeEnabled       bool      `json:"realtime_enabled"`
	UrgentRealtimeEnabled bool      `json:"urgent_realtime_enabled"`
	TeamWecomEnabled      bool      `json:"team_wecom_enabled"`
	SMSFallbackEnabled    bool      `json:"sms_fallback_enabled"`
	QuietHoursStart       string    `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd         string    `json:"quiet_hours_end,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type NotificationLog struct {
	ID                 int64      `json:"id"`
	MerchantID         string     `json:"merchant_id"`
	Channel            string     `json:"channel"`
	MessageType        string     `json:"message_type"`
	Target             string     `json:"target,omitempty"`
	Subject            string     `json:"subject,omitempty"`
	Body               string     `json:"body"`
	RelatedDigestID    int64      `json:"related_digest_id,omitempty"`
	RelatedInboxItemID int64      `json:"related_inbox_item_id,omitempty"`
	IdempotencyKey     string     `json:"idempotency_key"`
	Status             string     `json:"status"`
	AttemptCount       int        `json:"attempt_count"`
	MaxAttempts        int        `json:"max_attempts"`
	LastError          string     `json:"last_error,omitempty"`
	ErrorCategory      string     `json:"error_category,omitempty"`
	NextRetryAt        *time.Time `json:"next_retry_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type BusinessResultRetry struct {
	ID           int64     `json:"id"`
	SessionID    string    `json:"session_id"`
	CallSID      string    `json:"call_sid"`
	Payload      string    `json:"payload"`
	Status       string    `json:"status"`
	AttemptCount int       `json:"attempt_count"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AppUser struct {
	ID         int64     `json:"id"`
	OpenID     string    `json:"openid"`
	UnionID    string    `json:"unionid,omitempty"`
	SessionKey string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type MerchantUserBinding struct {
	ID         int64     `json:"id"`
	MerchantID string    `json:"merchant_id"`
	UserID     int64     `json:"user_id"`
	Role       string    `json:"role"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
