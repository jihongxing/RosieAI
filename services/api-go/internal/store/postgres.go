package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"rosie-api/internal/domain"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
	p.pool.Close()
}

func (p *Postgres) UpsertMerchant(merchant domain.Merchant) (domain.Merchant, error) {
	merchant.AccessNumber = NormalizeNumber(merchant.AccessNumber)
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO merchants (
			merchant_id, merchant_name, access_number, original_number,
			transfer_phone, enabled
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
		ON CONFLICT (merchant_id) DO UPDATE SET
			merchant_name = EXCLUDED.merchant_name,
			access_number = EXCLUDED.access_number,
			original_number = EXCLUDED.original_number,
			transfer_phone = EXCLUDED.transfer_phone,
			enabled = EXCLUDED.enabled,
			updated_at = now()
		RETURNING merchant_id, merchant_name, access_number,
			COALESCE(original_number, ''), COALESCE(transfer_phone, ''),
			enabled, created_at
	`, merchant.MerchantID, merchant.MerchantName, merchant.AccessNumber,
		merchant.OriginalNumber, merchant.TransferPhone, merchant.Enabled)

	var saved domain.Merchant
	if err := row.Scan(
		&saved.MerchantID,
		&saved.MerchantName,
		&saved.AccessNumber,
		&saved.OriginalNumber,
		&saved.TransferPhone,
		&saved.Enabled,
		&saved.CreatedAt,
	); err != nil {
		return domain.Merchant{}, err
	}
	if _, err := p.EnsurePreferences(saved.MerchantID); err != nil {
		return domain.Merchant{}, err
	}
	if _, err := p.EnsureMerchantProfile(saved.MerchantID); err != nil {
		return domain.Merchant{}, err
	}
	return saved, nil
}

func (p *Postgres) ListMerchants() ([]domain.Merchant, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT merchant_id, merchant_name, access_number,
			COALESCE(original_number, ''), COALESCE(transfer_phone, ''),
			enabled, created_at
		FROM merchants
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.Merchant
	for rows.Next() {
		var item domain.Merchant
		if err := rows.Scan(
			&item.MerchantID,
			&item.MerchantName,
			&item.AccessNumber,
			&item.OriginalNumber,
			&item.TransferPhone,
			&item.Enabled,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) FindMerchantByAccessNumber(accessNumber string) (domain.Merchant, bool, error) {
	return p.findMerchant(`
		SELECT merchant_id, merchant_name, access_number,
			COALESCE(original_number, ''), COALESCE(transfer_phone, ''),
			enabled, created_at
		FROM merchants
		WHERE access_number = $1 AND enabled = true
	`, NormalizeNumber(accessNumber))
}

func (p *Postgres) FindMerchantByID(merchantID string) (domain.Merchant, bool, error) {
	return p.findMerchant(`
		SELECT merchant_id, merchant_name, access_number,
			COALESCE(original_number, ''), COALESCE(transfer_phone, ''),
			enabled, created_at
		FROM merchants
		WHERE merchant_id = $1 AND enabled = true
	`, merchantID)
}

func (p *Postgres) findMerchant(query string, arg string) (domain.Merchant, bool, error) {
	var item domain.Merchant
	err := p.pool.QueryRow(context.Background(), query, arg).Scan(
		&item.MerchantID,
		&item.MerchantName,
		&item.AccessNumber,
		&item.OriginalNumber,
		&item.TransferPhone,
		&item.Enabled,
		&item.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return domain.Merchant{}, false, nil
	}
	if err != nil {
		return domain.Merchant{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) EnsureMerchantProfile(merchantID string) (domain.MerchantProfile, error) {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO merchant_profiles (merchant_id)
		VALUES ($1)
		ON CONFLICT (merchant_id) DO NOTHING
	`, merchantID)
	if err != nil {
		return domain.MerchantProfile{}, err
	}
	return p.findMerchantProfile(merchantID)
}

func (p *Postgres) UpdateMerchantProfile(profile domain.MerchantProfile) (domain.MerchantProfile, error) {
	faqItems, err := json.Marshal(profile.FAQItems)
	if err != nil {
		return domain.MerchantProfile{}, err
	}
	if _, err := p.EnsureMerchantProfile(profile.MerchantID); err != nil {
		return domain.MerchantProfile{}, err
	}
	_, err = p.pool.Exec(context.Background(), `
		UPDATE merchant_profiles
		SET industry = $2,
			address = NULLIF($3, ''),
			business_hours = NULLIF($4, ''),
			services = $5,
			faq_items = $6::jsonb,
			appointment_rules = NULLIF($7, ''),
			updated_at = now()
		WHERE merchant_id = $1
	`, profile.MerchantID, valueOr(profile.Industry, "hair_salon"), profile.Address,
		profile.BusinessHours, emptyStringSlice(profile.Services), string(faqItems),
		profile.AppointmentRules)
	if err != nil {
		return domain.MerchantProfile{}, err
	}
	return p.findMerchantProfile(profile.MerchantID)
}

func (p *Postgres) findMerchantProfile(merchantID string) (domain.MerchantProfile, error) {
	var item domain.MerchantProfile
	var faqItems []byte
	err := p.pool.QueryRow(context.Background(), `
		SELECT merchant_id, industry, COALESCE(address, ''),
			COALESCE(business_hours, ''), services, faq_items,
			COALESCE(appointment_rules, ''), created_at, updated_at
		FROM merchant_profiles
		WHERE merchant_id = $1
	`, merchantID).Scan(
		&item.MerchantID,
		&item.Industry,
		&item.Address,
		&item.BusinessHours,
		&item.Services,
		&faqItems,
		&item.AppointmentRules,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return domain.MerchantProfile{}, err
	}
	if len(faqItems) > 0 {
		if err := json.Unmarshal(faqItems, &item.FAQItems); err != nil {
			return domain.MerchantProfile{}, err
		}
	}
	if item.Services == nil {
		item.Services = []string{}
	}
	if item.FAQItems == nil {
		item.FAQItems = []domain.FAQItem{}
	}
	return item, nil
}

func (p *Postgres) UpsertCall(call domain.Call) (domain.Call, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO calls (
			call_sid, call_id, merchant_id, from_number, to_number,
			call_status, direction, raw_payload
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''),
			NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8::jsonb)
		ON CONFLICT (call_sid) DO UPDATE SET
			call_id = EXCLUDED.call_id,
			merchant_id = EXCLUDED.merchant_id,
			from_number = EXCLUDED.from_number,
			to_number = EXCLUDED.to_number,
			call_status = EXCLUDED.call_status,
			direction = EXCLUDED.direction,
			raw_payload = EXCLUDED.raw_payload,
			updated_at = now()
		RETURNING id, call_sid, COALESCE(call_id, ''), COALESCE(merchant_id, ''),
			COALESCE(from_number, ''), COALESCE(to_number, ''), COALESCE(call_status, ''),
			COALESCE(direction, ''), raw_payload::text, created_at, updated_at
	`, call.CallSID, call.CallID, call.MerchantID, call.FromNumber, call.ToNumber,
		call.CallStatus, call.Direction, call.RawPayload)
	return scanCall(row)
}

func (p *Postgres) ListCalls(limit int) ([]domain.Call, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, call_sid, COALESCE(call_id, ''), COALESCE(merchant_id, ''),
			COALESCE(from_number, ''), COALESCE(to_number, ''), COALESCE(call_status, ''),
			COALESCE(direction, ''), raw_payload::text, created_at, updated_at
		FROM calls
		ORDER BY id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.Call
	for rows.Next() {
		item, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) FindCallBySID(callSID string) (domain.Call, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, call_sid, COALESCE(call_id, ''), COALESCE(merchant_id, ''),
			COALESCE(from_number, ''), COALESCE(to_number, ''), COALESCE(call_status, ''),
			COALESCE(direction, ''), raw_payload::text, created_at, updated_at
		FROM calls
		WHERE call_sid = $1
	`, callSID)
	item, err := scanCall(row)
	if err == pgx.ErrNoRows {
		return domain.Call{}, false, nil
	}
	if err != nil {
		return domain.Call{}, false, err
	}
	return item, true, nil
}

func scanCall(row pgx.Row) (domain.Call, error) {
	var item domain.Call
	err := row.Scan(
		&item.ID,
		&item.CallSID,
		&item.CallID,
		&item.MerchantID,
		&item.FromNumber,
		&item.ToNumber,
		&item.CallStatus,
		&item.Direction,
		&item.RawPayload,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (p *Postgres) UpsertTranscript(transcript domain.Transcript) (domain.Transcript, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO call_transcripts (call_sid, merchant_id, transcript, source)
		VALUES ($1, NULLIF($2, ''), $3, $4)
		ON CONFLICT (call_sid) DO UPDATE SET
			merchant_id = EXCLUDED.merchant_id,
			transcript = EXCLUDED.transcript,
			source = EXCLUDED.source
		RETURNING id, call_sid, COALESCE(merchant_id, ''), transcript, source, created_at
	`, transcript.CallSID, transcript.MerchantID, transcript.Transcript, valueOr(transcript.Source, "manual"))
	var item domain.Transcript
	err := row.Scan(&item.ID, &item.CallSID, &item.MerchantID, &item.Transcript, &item.Source, &item.CreatedAt)
	return item, err
}

func (p *Postgres) FindTranscriptByCallSID(callSID string) (domain.Transcript, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, call_sid, COALESCE(merchant_id, ''), transcript, source, created_at
		FROM call_transcripts
		WHERE call_sid = $1
	`, callSID)
	var item domain.Transcript
	err := row.Scan(&item.ID, &item.CallSID, &item.MerchantID, &item.Transcript, &item.Source, &item.CreatedAt)
	if err == pgx.ErrNoRows {
		return domain.Transcript{}, false, nil
	}
	if err != nil {
		return domain.Transcript{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) UpsertSummary(summary domain.Summary) (domain.Summary, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO call_summaries (
			call_sid, merchant_id, summary, customer_name, customer_phone,
			intent, appointment_time, service, priority, need_human_followup, raw_result
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11::jsonb)
		ON CONFLICT (call_sid) DO UPDATE SET
			merchant_id = EXCLUDED.merchant_id,
			summary = EXCLUDED.summary,
			customer_name = EXCLUDED.customer_name,
			customer_phone = EXCLUDED.customer_phone,
			intent = EXCLUDED.intent,
			appointment_time = EXCLUDED.appointment_time,
			service = EXCLUDED.service,
			priority = EXCLUDED.priority,
			need_human_followup = EXCLUDED.need_human_followup,
			raw_result = EXCLUDED.raw_result
		RETURNING id, call_sid, COALESCE(merchant_id, ''), COALESCE(summary, ''),
			COALESCE(customer_name, ''), COALESCE(customer_phone, ''), COALESCE(intent, ''),
			COALESCE(appointment_time, ''), COALESCE(service, ''), priority,
			need_human_followup, raw_result::text, created_at
	`, summary.CallSID, summary.MerchantID, summary.Summary, summary.CustomerName,
		summary.CustomerPhone, summary.Intent, summary.AppointmentTime, summary.Service,
		valueOr(summary.Priority, "normal"), summary.NeedHumanFollowup, valueOr(summary.RawResult, "{}"))
	return scanSummary(row)
}

func (p *Postgres) FindSummaryByCallSID(callSID string) (domain.Summary, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, call_sid, COALESCE(merchant_id, ''), COALESCE(summary, ''),
			COALESCE(customer_name, ''), COALESCE(customer_phone, ''), COALESCE(intent, ''),
			COALESCE(appointment_time, ''), COALESCE(service, ''), priority,
			need_human_followup, raw_result::text, created_at
		FROM call_summaries
		WHERE call_sid = $1
	`, callSID)
	item, err := scanSummary(row)
	if err == pgx.ErrNoRows {
		return domain.Summary{}, false, nil
	}
	if err != nil {
		return domain.Summary{}, false, err
	}
	return item, true, nil
}

func scanSummary(row pgx.Row) (domain.Summary, error) {
	var item domain.Summary
	err := row.Scan(
		&item.ID,
		&item.CallSID,
		&item.MerchantID,
		&item.Summary,
		&item.CustomerName,
		&item.CustomerPhone,
		&item.Intent,
		&item.AppointmentTime,
		&item.Service,
		&item.Priority,
		&item.NeedHumanFollowup,
		&item.RawResult,
		&item.CreatedAt,
	)
	return item, err
}

func (p *Postgres) UpsertInboxItem(item domain.InboxItem) (domain.InboxItem, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO inbox_items (
			merchant_id, call_sid, item_type, title, body, priority,
			status, need_human_followup, digest_status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (call_sid) DO UPDATE SET
			merchant_id = EXCLUDED.merchant_id,
			item_type = EXCLUDED.item_type,
			title = EXCLUDED.title,
			body = EXCLUDED.body,
			priority = EXCLUDED.priority,
			status = EXCLUDED.status,
			need_human_followup = EXCLUDED.need_human_followup,
			digest_status = 'pending',
			updated_at = now()
		RETURNING id, merchant_id, call_sid, item_type, title, body, priority,
			status, need_human_followup, digest_status, created_at, updated_at
	`, item.MerchantID, item.CallSID, valueOr(item.ItemType, "call_summary"), item.Title,
		item.Body, valueOr(item.Priority, "normal"), valueOr(item.Status, "new"),
		item.NeedHumanFollowup, valueOr(item.DigestStatus, "pending"))
	return scanInboxItem(row)
}

func (p *Postgres) FindInboxItemByCallSID(callSID string) (domain.InboxItem, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, merchant_id, call_sid, item_type, title, body, priority,
			status, need_human_followup, digest_status, created_at, updated_at
		FROM inbox_items
		WHERE call_sid = $1
	`, callSID)
	item, err := scanInboxItem(row)
	if err == pgx.ErrNoRows {
		return domain.InboxItem{}, false, nil
	}
	if err != nil {
		return domain.InboxItem{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) ListInboxItems(merchantID string, limit int) ([]domain.InboxItem, error) {
	return p.listInboxItems(`
		SELECT id, merchant_id, call_sid, item_type, title, body, priority,
			status, need_human_followup, digest_status, created_at, updated_at
		FROM inbox_items
		WHERE merchant_id = $1
		ORDER BY id DESC
		LIMIT $2
	`, merchantID, limit)
}

func (p *Postgres) ListPendingDigestItems(merchantID string, limit int) ([]domain.InboxItem, error) {
	return p.listInboxItems(`
		SELECT id, merchant_id, call_sid, item_type, title, body, priority,
			status, need_human_followup, digest_status, created_at, updated_at
		FROM inbox_items
		WHERE merchant_id = $1 AND digest_status = 'pending'
		ORDER BY id ASC
		LIMIT $2
	`, merchantID, limit)
}

func (p *Postgres) listInboxItems(query string, merchantID string, limit int) ([]domain.InboxItem, error) {
	rows, err := p.pool.Query(context.Background(), query, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.InboxItem
	for rows.Next() {
		item, err := scanInboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanInboxItem(row pgx.Row) (domain.InboxItem, error) {
	var item domain.InboxItem
	err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.CallSID,
		&item.ItemType,
		&item.Title,
		&item.Body,
		&item.Priority,
		&item.Status,
		&item.NeedHumanFollowup,
		&item.DigestStatus,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (p *Postgres) InsertDigest(digest domain.Digest) (domain.Digest, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO digests (
			merchant_id, digest_type, item_count, urgent_count, followup_count,
			spam_count, digest_text, item_ids, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, merchant_id, digest_type, item_count, urgent_count,
			followup_count, spam_count, digest_text, item_ids, status, created_at
	`, digest.MerchantID, valueOr(digest.DigestType, "daily"), digest.ItemCount,
		digest.UrgentCount, digest.FollowupCount, digest.SpamCount, digest.DigestText,
		digest.ItemIDs, valueOr(digest.Status, "generated"))
	return scanDigest(row)
}

func (p *Postgres) MarkInboxItemsDigested(itemIDs []int64) error {
	if len(itemIDs) == 0 {
		return nil
	}
	_, err := p.pool.Exec(context.Background(), `
		UPDATE inbox_items
		SET digest_status = 'digested', updated_at = now()
		WHERE id = ANY($1)
	`, itemIDs)
	return err
}

func (p *Postgres) ListDigests(merchantID string, limit int) ([]domain.Digest, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, merchant_id, digest_type, item_count, urgent_count,
			followup_count, spam_count, digest_text, item_ids, status, created_at
		FROM digests
		WHERE merchant_id = $1
		ORDER BY id DESC
		LIMIT $2
	`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.Digest
	for rows.Next() {
		item, err := scanDigest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanDigest(row pgx.Row) (domain.Digest, error) {
	var item domain.Digest
	err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.DigestType,
		&item.ItemCount,
		&item.UrgentCount,
		&item.FollowupCount,
		&item.SpamCount,
		&item.DigestText,
		&item.ItemIDs,
		&item.Status,
		&item.CreatedAt,
	)
	return item, err
}

func (p *Postgres) EnsurePreferences(merchantID string) (domain.NotificationPreferences, error) {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO notification_preferences (merchant_id)
		VALUES ($1)
		ON CONFLICT (merchant_id) DO NOTHING
	`, merchantID)
	if err != nil {
		return domain.NotificationPreferences{}, err
	}
	return p.findPreferences(merchantID)
}

func (p *Postgres) UpdatePreferences(prefs domain.NotificationPreferences) (domain.NotificationPreferences, error) {
	if _, err := p.EnsurePreferences(prefs.MerchantID); err != nil {
		return domain.NotificationPreferences{}, err
	}
	_, err := p.pool.Exec(context.Background(), `
		UPDATE notification_preferences
		SET digest_mode = $2,
			digest_times = $3,
			realtime_enabled = $4,
			urgent_realtime_enabled = $5,
			team_wecom_enabled = $6,
			sms_fallback_enabled = $7,
			quiet_hours_start = NULLIF($8, ''),
			quiet_hours_end = NULLIF($9, ''),
			updated_at = now()
		WHERE merchant_id = $1
	`, prefs.MerchantID, prefs.DigestMode, prefs.DigestTimes, prefs.RealtimeEnabled,
		prefs.UrgentRealtimeEnabled, prefs.TeamWecomEnabled, prefs.SMSFallbackEnabled,
		prefs.QuietHoursStart, prefs.QuietHoursEnd)
	if err != nil {
		return domain.NotificationPreferences{}, err
	}
	return p.findPreferences(prefs.MerchantID)
}

func (p *Postgres) findPreferences(merchantID string) (domain.NotificationPreferences, error) {
	var item domain.NotificationPreferences
	err := p.pool.QueryRow(context.Background(), `
		SELECT merchant_id, digest_mode, digest_times, realtime_enabled,
			urgent_realtime_enabled, team_wecom_enabled, sms_fallback_enabled,
			COALESCE(quiet_hours_start, ''), COALESCE(quiet_hours_end, ''),
			created_at, updated_at
		FROM notification_preferences
		WHERE merchant_id = $1
	`, merchantID).Scan(
		&item.MerchantID,
		&item.DigestMode,
		&item.DigestTimes,
		&item.RealtimeEnabled,
		&item.UrgentRealtimeEnabled,
		&item.TeamWecomEnabled,
		&item.SMSFallbackEnabled,
		&item.QuietHoursStart,
		&item.QuietHoursEnd,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (p *Postgres) InsertNotificationLog(log domain.NotificationLog) (domain.NotificationLog, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO notification_logs (
			merchant_id, channel, message_type, target, subject, body,
			related_digest_id, related_inbox_item_id, idempotency_key,
			status, attempt_count, last_error
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6,
			NULLIF($7, 0), NULLIF($8, 0), $9, $10, $11, NULLIF($12, ''))
		ON CONFLICT (idempotency_key) DO UPDATE SET
			updated_at = now()
		RETURNING id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, COALESCE(last_error, ''),
			created_at, updated_at
	`, log.MerchantID, valueOr(log.Channel, "log"), log.MessageType, log.Target,
		log.Subject, log.Body, log.RelatedDigestID, log.RelatedInboxItemID,
		log.IdempotencyKey, valueOr(log.Status, "queued"), log.AttemptCount, log.LastError)
	return scanNotificationLog(row)
}

func (p *Postgres) FindNotificationLogByKey(key string) (domain.NotificationLog, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, COALESCE(last_error, ''),
			created_at, updated_at
		FROM notification_logs
		WHERE idempotency_key = $1
	`, key)
	item, err := scanNotificationLog(row)
	if err == pgx.ErrNoRows {
		return domain.NotificationLog{}, false, nil
	}
	if err != nil {
		return domain.NotificationLog{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) ListNotificationLogs(merchantID string, status string, limit int) ([]domain.NotificationLog, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, COALESCE(last_error, ''),
			created_at, updated_at
		FROM notification_logs
		WHERE ($1 = '' OR merchant_id = $1)
			AND ($2 = '' OR status = $2)
		ORDER BY id DESC
		LIMIT $3
	`, merchantID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.NotificationLog
	for rows.Next() {
		item, err := scanNotificationLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) UpdateNotificationLogStatus(id int64, status string, attemptCount int, lastError string) (domain.NotificationLog, error) {
	row := p.pool.QueryRow(context.Background(), `
		UPDATE notification_logs
		SET status = $2,
			attempt_count = $3,
			last_error = NULLIF($4, ''),
			updated_at = now()
		WHERE id = $1
		RETURNING id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, COALESCE(last_error, ''),
			created_at, updated_at
	`, id, status, attemptCount, lastError)
	item, err := scanNotificationLog(row)
	if err == pgx.ErrNoRows {
		return domain.NotificationLog{}, ErrNotFound
	}
	return item, err
}

func (p *Postgres) UpsertAppUser(user domain.AppUser) (domain.AppUser, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO app_users (openid, unionid, session_key)
		VALUES ($1, NULLIF($2, ''), $3)
		ON CONFLICT (openid) DO UPDATE SET
			unionid = EXCLUDED.unionid,
			session_key = EXCLUDED.session_key,
			updated_at = now()
		RETURNING id, openid, COALESCE(unionid, ''), session_key, created_at, updated_at
	`, user.OpenID, user.UnionID, user.SessionKey)
	var saved domain.AppUser
	err := row.Scan(&saved.ID, &saved.OpenID, &saved.UnionID, &saved.SessionKey, &saved.CreatedAt, &saved.UpdatedAt)
	return saved, err
}

func (p *Postgres) BindMerchantUser(merchantID string, userID int64, role string) (domain.MerchantUserBinding, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO merchant_user_bindings (merchant_id, user_id, role, enabled)
		VALUES ($1, $2, $3, true)
		ON CONFLICT (merchant_id, user_id) DO UPDATE SET
			role = EXCLUDED.role,
			enabled = true,
			updated_at = now()
		RETURNING id, merchant_id, user_id, role, enabled, created_at, updated_at
	`, merchantID, userID, valueOr(role, "owner"))
	var binding domain.MerchantUserBinding
	err := row.Scan(
		&binding.ID,
		&binding.MerchantID,
		&binding.UserID,
		&binding.Role,
		&binding.Enabled,
		&binding.CreatedAt,
		&binding.UpdatedAt,
	)
	return binding, err
}

func (p *Postgres) FindPrimaryOpenIDByMerchantID(merchantID string) (string, bool, error) {
	var openID string
	err := p.pool.QueryRow(context.Background(), `
		SELECT u.openid
		FROM merchant_user_bindings b
		JOIN app_users u ON u.id = b.user_id
		WHERE b.merchant_id = $1 AND b.enabled = true
		ORDER BY b.id ASC
		LIMIT 1
	`, merchantID).Scan(&openID)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return openID, true, nil
}

func scanNotificationLog(row pgx.Row) (domain.NotificationLog, error) {
	var item domain.NotificationLog
	var relatedDigestID sql.NullInt64
	var relatedInboxItemID sql.NullInt64
	err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.Channel,
		&item.MessageType,
		&item.Target,
		&item.Subject,
		&item.Body,
		&relatedDigestID,
		&relatedInboxItemID,
		&item.IdempotencyKey,
		&item.Status,
		&item.AttemptCount,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if relatedDigestID.Valid {
		item.RelatedDigestID = relatedDigestID.Int64
	}
	if relatedInboxItemID.Valid {
		item.RelatedInboxItemID = relatedInboxItemID.Int64
	}
	return item, err
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func emptyStringSlice(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
