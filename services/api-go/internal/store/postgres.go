package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

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

func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *Postgres) UpsertMerchant(merchant domain.Merchant) (domain.Merchant, error) {
	merchant.AccessNumber = NormalizeNumber(merchant.AccessNumber)
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO merchants (
			merchant_id, merchant_name, access_number, original_number,
			transfer_phone, enabled
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6)
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
	if _, err := p.EnsureServiceSubscription(saved.MerchantID); err != nil {
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

func (p *Postgres) UpsertAccessNumber(item domain.AccessNumber) (domain.AccessNumber, error) {
	item.Number = NormalizeNumber(item.Number)
	if item.Number == "" {
		return domain.AccessNumber{}, ErrNotFound
	}
	if item.Status == "" {
		item.Status = "available"
	}
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO access_numbers (
			number, provider, provider_number_id, trunk_id,
			jambonz_application_id, jambonz_application_name, jambonz_call_hook_url,
			jambonz_status_hook_url, jambonz_config_synced_at, status, merchant_id, notes
		)
		VALUES (
			$1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''),
			NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''),
			NULLIF($8, ''), $9, $10, NULLIF($11, ''), NULLIF($12, '')
		)
		ON CONFLICT (number) DO UPDATE SET
			provider = EXCLUDED.provider,
			provider_number_id = EXCLUDED.provider_number_id,
			trunk_id = EXCLUDED.trunk_id,
			jambonz_application_id = EXCLUDED.jambonz_application_id,
			jambonz_application_name = EXCLUDED.jambonz_application_name,
			jambonz_call_hook_url = EXCLUDED.jambonz_call_hook_url,
			jambonz_status_hook_url = EXCLUDED.jambonz_status_hook_url,
			jambonz_config_synced_at = EXCLUDED.jambonz_config_synced_at,
			status = CASE
				WHEN access_numbers.status = 'assigned' AND EXCLUDED.status = 'available' THEN access_numbers.status
				ELSE EXCLUDED.status
			END,
			merchant_id = COALESCE(EXCLUDED.merchant_id, access_numbers.merchant_id),
			notes = EXCLUDED.notes,
			updated_at = now()
		RETURNING id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
	`, item.Number, item.Provider, item.ProviderNumberID, item.TrunkID,
		item.JambonzApplicationID, item.JambonzApplicationName, item.JambonzCallHookURL,
		item.JambonzStatusHookURL, item.JambonzConfigSyncedAt, item.Status, item.MerchantID, item.Notes)
	return scanAccessNumber(row)
}

func (p *Postgres) ListAccessNumbers(status string, limit int) ([]domain.AccessNumber, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
		FROM access_numbers
		WHERE ($1 = '' OR status = $1)
		ORDER BY status, created_at DESC
		LIMIT $2
	`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.AccessNumber
	for rows.Next() {
		item, err := scanAccessNumber(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) FindAccessNumberByNumber(number string) (domain.AccessNumber, bool, error) {
	item, err := scanAccessNumber(p.pool.QueryRow(context.Background(), `
		SELECT id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
		FROM access_numbers
		WHERE number = $1
	`, NormalizeNumber(number)))
	if err == pgx.ErrNoRows {
		return domain.AccessNumber{}, false, nil
	}
	if err != nil {
		return domain.AccessNumber{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) AssignAccessNumber(merchantID string, number string, assignedAt time.Time) (domain.AccessNumber, error) {
	return p.assignAccessNumber(context.Background(), merchantID, NormalizeNumber(number), assignedAt)
}

func (p *Postgres) AutoAssignAccessNumber(merchantID string, assignedAt time.Time) (domain.AccessNumber, bool, error) {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.AccessNumber{}, false, err
	}
	defer tx.Rollback(ctx)

	existing, err := scanAccessNumber(tx.QueryRow(ctx, `
		SELECT id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
		FROM access_numbers
		WHERE status = 'assigned' AND merchant_id = $1
		ORDER BY assigned_at DESC NULLS LAST, created_at DESC
		LIMIT 1
	`, merchantID))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return domain.AccessNumber{}, false, err
		}
		return existing, true, nil
	}
	if err != pgx.ErrNoRows {
		return domain.AccessNumber{}, false, err
	}

	var number string
	err = tx.QueryRow(ctx, `
		SELECT number
		FROM access_numbers
		WHERE status = 'available' AND merchant_id IS NULL
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&number)
	if err == pgx.ErrNoRows {
		if err := tx.Commit(ctx); err != nil {
			return domain.AccessNumber{}, false, err
		}
		return domain.AccessNumber{}, false, nil
	}
	if err != nil {
		return domain.AccessNumber{}, false, err
	}
	item, err := p.assignAccessNumberTx(ctx, tx, merchantID, number, assignedAt)
	if err != nil {
		return domain.AccessNumber{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AccessNumber{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) ReleaseAccessNumber(number string, releasedAt time.Time) (domain.AccessNumber, error) {
	ctx := context.Background()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.AccessNumber{}, err
	}
	defer tx.Rollback(ctx)

	current, err := scanAccessNumber(tx.QueryRow(ctx, `
		SELECT id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
		FROM access_numbers
		WHERE number = $1
		FOR UPDATE
	`, NormalizeNumber(number)))
	if err == pgx.ErrNoRows {
		return domain.AccessNumber{}, ErrNotFound
	}
	if err != nil {
		return domain.AccessNumber{}, err
	}
	if current.MerchantID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE merchants
			SET access_number = NULL, updated_at = now()
			WHERE merchant_id = $1 AND access_number = $2
		`, current.MerchantID, current.Number); err != nil {
			return domain.AccessNumber{}, err
		}
	}
	item, err := scanAccessNumber(tx.QueryRow(ctx, `
		UPDATE access_numbers
		SET status = 'available',
			merchant_id = NULL,
			assigned_at = NULL,
			released_at = $2,
			updated_at = now()
		WHERE number = $1
		RETURNING id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
	`, current.Number, releasedAt))
	if err != nil {
		return domain.AccessNumber{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AccessNumber{}, err
	}
	return item, nil
}

func (p *Postgres) assignAccessNumber(ctx context.Context, merchantID string, number string, assignedAt time.Time) (domain.AccessNumber, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.AccessNumber{}, err
	}
	defer tx.Rollback(ctx)
	item, err := p.assignAccessNumberTx(ctx, tx, merchantID, number, assignedAt)
	if err != nil {
		return domain.AccessNumber{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AccessNumber{}, err
	}
	return item, nil
}

func (p *Postgres) assignAccessNumberTx(ctx context.Context, tx pgx.Tx, merchantID string, number string, assignedAt time.Time) (domain.AccessNumber, error) {
	var enabled bool
	if err := tx.QueryRow(ctx, `
		SELECT enabled
		FROM merchants
		WHERE merchant_id = $1
		FOR UPDATE
	`, merchantID).Scan(&enabled); err == pgx.ErrNoRows {
		return domain.AccessNumber{}, ErrNotFound
	} else if err != nil {
		return domain.AccessNumber{}, err
	}
	if !enabled {
		return domain.AccessNumber{}, ErrNotFound
	}

	current, err := scanAccessNumber(tx.QueryRow(ctx, `
		SELECT id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
		FROM access_numbers
		WHERE number = $1
		FOR UPDATE
	`, number))
	if err == pgx.ErrNoRows {
		return domain.AccessNumber{}, ErrNotFound
	}
	if err != nil {
		return domain.AccessNumber{}, err
	}
	if current.Status == "disabled" || (current.MerchantID != "" && current.MerchantID != merchantID) {
		return domain.AccessNumber{}, ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE access_numbers
		SET status = 'available',
			merchant_id = NULL,
			assigned_at = NULL,
			released_at = $2,
			updated_at = now()
		WHERE merchant_id = $1 AND status = 'assigned' AND number <> $3
	`, merchantID, assignedAt, number); err != nil {
		return domain.AccessNumber{}, err
	}
	item, err := scanAccessNumber(tx.QueryRow(ctx, `
		UPDATE access_numbers
		SET status = 'assigned',
			merchant_id = $2,
			assigned_at = $3,
			released_at = NULL,
			updated_at = now()
		WHERE number = $1
		RETURNING id, number, COALESCE(provider, ''), COALESCE(provider_number_id, ''),
			COALESCE(trunk_id, ''), COALESCE(jambonz_application_id, ''), status,
			COALESCE(merchant_id, ''), COALESCE(notes, ''),
			COALESCE(jambonz_application_name, ''), COALESCE(jambonz_call_hook_url, ''),
			COALESCE(jambonz_status_hook_url, ''), jambonz_config_synced_at,
			assigned_at, released_at,
			created_at, updated_at
	`, number, merchantID, assignedAt))
	if err != nil {
		return domain.AccessNumber{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merchants
		SET access_number = $2, updated_at = now()
		WHERE merchant_id = $1
	`, merchantID, number); err != nil {
		return domain.AccessNumber{}, err
	}
	return item, nil
}

type accessNumberScanner interface {
	Scan(dest ...any) error
}

func scanAccessNumber(row accessNumberScanner) (domain.AccessNumber, error) {
	var item domain.AccessNumber
	var jambonzConfigSyncedAt sql.NullTime
	var assignedAt sql.NullTime
	var releasedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.Number,
		&item.Provider,
		&item.ProviderNumberID,
		&item.TrunkID,
		&item.JambonzApplicationID,
		&item.Status,
		&item.MerchantID,
		&item.Notes,
		&item.JambonzApplicationName,
		&item.JambonzCallHookURL,
		&item.JambonzStatusHookURL,
		&jambonzConfigSyncedAt,
		&assignedAt,
		&releasedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domain.AccessNumber{}, err
	}
	if jambonzConfigSyncedAt.Valid {
		item.JambonzConfigSyncedAt = &jambonzConfigSyncedAt.Time
	}
	if assignedAt.Valid {
		item.AssignedAt = &assignedAt.Time
	}
	if releasedAt.Valid {
		item.ReleasedAt = &releasedAt.Time
	}
	return item, nil
}

func (p *Postgres) EnsureServiceSubscription(merchantID string) (domain.ServiceSubscription, error) {
	_, err := p.pool.Exec(context.Background(), `
		INSERT INTO merchant_service_subscriptions (merchant_id)
		VALUES ($1)
		ON CONFLICT (merchant_id) DO NOTHING
	`, merchantID)
	if err != nil {
		return domain.ServiceSubscription{}, err
	}
	return p.findServiceSubscription(merchantID)
}

func (p *Postgres) ActivateTrialSubscription(merchantID string, planCode string, startedAt time.Time, endsAt time.Time) (domain.ServiceSubscription, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO merchant_service_subscriptions (
			merchant_id, plan_code, status, trial_started_at, trial_ends_at,
			current_period_ends_at, activated_at
		)
		VALUES ($1, $2, 'trialing', $3, $4, $4, $3)
		ON CONFLICT (merchant_id) DO UPDATE SET
			plan_code = CASE
				WHEN merchant_service_subscriptions.status IN ('active', 'trialing')
					THEN merchant_service_subscriptions.plan_code
				ELSE EXCLUDED.plan_code
			END,
			status = CASE
				WHEN merchant_service_subscriptions.status IN ('active', 'trialing')
					THEN merchant_service_subscriptions.status
				ELSE 'trialing'
			END,
			trial_started_at = CASE
				WHEN merchant_service_subscriptions.status IN ('active', 'trialing')
					THEN merchant_service_subscriptions.trial_started_at
				ELSE EXCLUDED.trial_started_at
			END,
			trial_ends_at = CASE
				WHEN merchant_service_subscriptions.status IN ('active', 'trialing')
					THEN merchant_service_subscriptions.trial_ends_at
				ELSE EXCLUDED.trial_ends_at
			END,
			current_period_ends_at = CASE
				WHEN merchant_service_subscriptions.status IN ('active', 'trialing')
					THEN merchant_service_subscriptions.current_period_ends_at
				ELSE EXCLUDED.current_period_ends_at
			END,
			activated_at = COALESCE(merchant_service_subscriptions.activated_at, EXCLUDED.activated_at),
			updated_at = now()
		RETURNING merchant_id, plan_code, status, trial_started_at, trial_ends_at,
			current_period_ends_at, activated_at, created_at, updated_at
	`, merchantID, valueOr(planCode, "pilot_basic"), startedAt, endsAt)
	return scanServiceSubscription(row)
}

func (p *Postgres) RenewServiceSubscription(merchantID string, planCode string, paidAt time.Time, months int) (domain.ServiceSubscription, error) {
	if months <= 0 {
		months = 1
	}
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO merchant_service_subscriptions (
			merchant_id, plan_code, status, current_period_ends_at, activated_at
		)
		VALUES ($1, $2, 'active', $3::timestamptz + ($4::text || ' months')::interval, $3)
		ON CONFLICT (merchant_id) DO UPDATE SET
			plan_code = EXCLUDED.plan_code,
			status = 'active',
			current_period_ends_at = (
				GREATEST(
					COALESCE(merchant_service_subscriptions.current_period_ends_at, $3::timestamptz),
					$3::timestamptz
				) + ($4::text || ' months')::interval
			),
			activated_at = COALESCE(merchant_service_subscriptions.activated_at, $3),
			updated_at = now()
		RETURNING merchant_id, plan_code, status, trial_started_at, trial_ends_at,
			current_period_ends_at, activated_at, created_at, updated_at
	`, merchantID, valueOr(planCode, "pilot_basic"), paidAt, months)
	return scanServiceSubscription(row)
}

func (p *Postgres) InsertPaymentOrder(order domain.PaymentOrder) (domain.PaymentOrder, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO payment_orders (
			merchant_id, order_no, order_type, plan_code, add_on_code,
			amount_cents, currency, status, provider, provider_trade_no, prepay_id
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, NULLIF($10, ''), NULLIF($11, ''))
		ON CONFLICT (order_no) DO UPDATE SET
			updated_at = now()
		RETURNING id, merchant_id, order_no, order_type, plan_code, COALESCE(add_on_code, ''),
			amount_cents, currency, status, provider, COALESCE(provider_trade_no, ''),
			COALESCE(prepay_id, ''), paid_at, created_at, updated_at
	`, order.MerchantID, order.OrderNo, valueOr(order.OrderType, "renewal"),
		valueOr(order.PlanCode, "pilot_basic"), order.AddOnCode, order.AmountCents,
		valueOr(order.Currency, "CNY"), valueOr(order.Status, "pending"),
		valueOr(order.Provider, "wechat_pay"), order.ProviderTradeNo, order.PrepayID)
	return scanPaymentOrder(row)
}

func (p *Postgres) FindPaymentOrderByNo(orderNo string) (domain.PaymentOrder, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, merchant_id, order_no, order_type, plan_code, COALESCE(add_on_code, ''),
			amount_cents, currency, status, provider, COALESCE(provider_trade_no, ''),
			COALESCE(prepay_id, ''), paid_at, created_at, updated_at
		FROM payment_orders
		WHERE order_no = $1
	`, orderNo)
	item, err := scanPaymentOrder(row)
	if err == pgx.ErrNoRows {
		return domain.PaymentOrder{}, false, nil
	}
	if err != nil {
		return domain.PaymentOrder{}, false, err
	}
	return item, true, nil
}

func (p *Postgres) ListPaymentOrders(merchantID string, limit int) ([]domain.PaymentOrder, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, merchant_id, order_no, order_type, plan_code, COALESCE(add_on_code, ''),
			amount_cents, currency, status, provider, COALESCE(provider_trade_no, ''),
			COALESCE(prepay_id, ''), paid_at, created_at, updated_at
		FROM payment_orders
		WHERE merchant_id = $1
		ORDER BY id DESC
		LIMIT $2
	`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.PaymentOrder
	for rows.Next() {
		item, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) UpdatePaymentOrderPrepay(orderNo string, prepayID string) (domain.PaymentOrder, error) {
	row := p.pool.QueryRow(context.Background(), `
		UPDATE payment_orders
		SET prepay_id = NULLIF($2, ''),
			updated_at = now()
		WHERE order_no = $1
		RETURNING id, merchant_id, order_no, order_type, plan_code, COALESCE(add_on_code, ''),
			amount_cents, currency, status, provider, COALESCE(provider_trade_no, ''),
			COALESCE(prepay_id, ''), paid_at, created_at, updated_at
	`, orderNo, prepayID)
	item, err := scanPaymentOrder(row)
	if err == pgx.ErrNoRows {
		return domain.PaymentOrder{}, ErrNotFound
	}
	return item, err
}

func (p *Postgres) MarkPaymentOrderPaid(orderNo string, providerTradeNo string, paidAt time.Time) (domain.PaymentOrder, error) {
	row := p.pool.QueryRow(context.Background(), `
		UPDATE payment_orders
		SET status = 'paid',
			provider_trade_no = COALESCE(NULLIF($2, ''), provider_trade_no),
			paid_at = COALESCE(paid_at, $3),
			updated_at = now()
		WHERE order_no = $1
		RETURNING id, merchant_id, order_no, order_type, plan_code, COALESCE(add_on_code, ''),
			amount_cents, currency, status, provider, COALESCE(provider_trade_no, ''),
			COALESCE(prepay_id, ''), paid_at, created_at, updated_at
	`, orderNo, providerTradeNo, paidAt)
	item, err := scanPaymentOrder(row)
	if err == pgx.ErrNoRows {
		return domain.PaymentOrder{}, ErrNotFound
	}
	return item, err
}

func scanPaymentOrder(row pgx.Row) (domain.PaymentOrder, error) {
	var item domain.PaymentOrder
	var paidAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.OrderNo,
		&item.OrderType,
		&item.PlanCode,
		&item.AddOnCode,
		&item.AmountCents,
		&item.Currency,
		&item.Status,
		&item.Provider,
		&item.ProviderTradeNo,
		&item.PrepayID,
		&paidAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if paidAt.Valid {
		item.PaidAt = &paidAt.Time
	}
	return item, err
}

func (p *Postgres) findServiceSubscription(merchantID string) (domain.ServiceSubscription, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT merchant_id, plan_code, status, trial_started_at, trial_ends_at,
			current_period_ends_at, activated_at, created_at, updated_at
		FROM merchant_service_subscriptions
		WHERE merchant_id = $1
	`, merchantID)
	return scanServiceSubscription(row)
}

func scanServiceSubscription(row pgx.Row) (domain.ServiceSubscription, error) {
	var item domain.ServiceSubscription
	var trialStartedAt sql.NullTime
	var trialEndsAt sql.NullTime
	var currentPeriodEndsAt sql.NullTime
	var activatedAt sql.NullTime
	err := row.Scan(
		&item.MerchantID,
		&item.PlanCode,
		&item.Status,
		&trialStartedAt,
		&trialEndsAt,
		&currentPeriodEndsAt,
		&activatedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if trialStartedAt.Valid {
		item.TrialStartedAt = &trialStartedAt.Time
	}
	if trialEndsAt.Valid {
		item.TrialEndsAt = &trialEndsAt.Time
	}
	if currentPeriodEndsAt.Valid {
		item.CurrentPeriodEndsAt = &currentPeriodEndsAt.Time
	}
	if activatedAt.Valid {
		item.ActivatedAt = &activatedAt.Time
	}
	return item, err
}

func (p *Postgres) GetValueMetrics(merchantID string, since time.Time, until time.Time) (domain.ValueMetrics, error) {
	metrics := domain.ValueMetrics{
		MerchantID: merchantID,
		Since:      since,
		Until:      until,
	}
	var totalCalls int64
	var effectiveCalls int64
	var appointmentCount int64
	var spamCount int64
	var followupCount int64
	var urgentCount int64
	var handledCount int64
	var archivedCount int64
	var callbackRequestedCount int64
	var callbackDialedCount int64
	err := p.pool.QueryRow(context.Background(), `
		SELECT
			(SELECT COUNT(*) FROM calls
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COUNT(*) FROM inbox_items
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND status <> 'filtered'),
			(SELECT COUNT(*) FROM call_summaries
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND intent = 'appointment'),
			(SELECT COUNT(DISTINCT call_sid) FROM (
				SELECT call_sid FROM call_summaries
					WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
						AND intent = 'spam'
				UNION
				SELECT call_sid FROM inbox_items
					WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
						AND status = 'filtered'
			) spam_items),
			(SELECT COUNT(*) FROM inbox_items
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND need_human_followup = true),
			(SELECT COUNT(*) FROM inbox_items
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND priority IN ('urgent', 'high')),
			(SELECT COUNT(*) FROM inbox_items
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND status = 'handled'),
			(SELECT COUNT(*) FROM inbox_items
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND status = 'archived'),
			(SELECT COUNT(*) FROM callback_requests
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3),
			(SELECT COUNT(*) FROM callback_requests
				WHERE merchant_id = $1 AND created_at >= $2 AND created_at < $3
					AND status = 'dialed')
	`, merchantID, since, until).Scan(
		&totalCalls,
		&effectiveCalls,
		&appointmentCount,
		&spamCount,
		&followupCount,
		&urgentCount,
		&handledCount,
		&archivedCount,
		&callbackRequestedCount,
		&callbackDialedCount,
	)
	if err != nil {
		return domain.ValueMetrics{}, err
	}
	metrics.TotalCalls = int(totalCalls)
	metrics.EffectiveCalls = int(effectiveCalls)
	metrics.AppointmentCount = int(appointmentCount)
	metrics.SpamCount = int(spamCount)
	metrics.FollowupCount = int(followupCount)
	metrics.UrgentCount = int(urgentCount)
	metrics.HandledCount = int(handledCount)
	metrics.ArchivedCount = int(archivedCount)
	metrics.CallbackRequestedCount = int(callbackRequestedCount)
	metrics.CallbackDialedCount = int(callbackDialedCount)
	finalizeValueMetrics(&metrics)
	return metrics, nil
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

func (p *Postgres) UpdateInboxItemStatus(callSID string, status string) (domain.InboxItem, error) {
	row := p.pool.QueryRow(context.Background(), `
		UPDATE inbox_items
		SET status = $2, updated_at = now()
		WHERE call_sid = $1
		RETURNING id, merchant_id, call_sid, item_type, title, body, priority,
			status, need_human_followup, digest_status, created_at, updated_at
	`, callSID, status)
	item, err := scanInboxItem(row)
	if err == pgx.ErrNoRows {
		return domain.InboxItem{}, ErrNotFound
	}
	return item, err
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

func (p *Postgres) InsertCallbackRequest(request domain.CallbackRequest) (domain.CallbackRequest, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO callback_requests (
			merchant_id, original_call_sid, original_call_id, target_number,
			requested_by, reason, status, audit_note
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''),
			NULLIF($6, ''), $7, NULLIF($8, ''))
		RETURNING id, merchant_id, original_call_sid, COALESCE(original_call_id, ''),
			target_number, COALESCE(requested_by, ''), COALESCE(reason, ''),
			status, COALESCE(audit_note, ''), created_at, updated_at
	`, request.MerchantID, request.OriginalCallSID, request.OriginalCallID,
		request.TargetNumber, request.RequestedBy, request.Reason,
		valueOr(request.Status, "requested"), request.AuditNote)
	return scanCallbackRequest(row)
}

func (p *Postgres) ListCallbackRequestsByCallSID(callSID string, limit int) ([]domain.CallbackRequest, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, merchant_id, original_call_sid, COALESCE(original_call_id, ''),
			target_number, COALESCE(requested_by, ''), COALESCE(reason, ''),
			status, COALESCE(audit_note, ''), created_at, updated_at
		FROM callback_requests
		WHERE original_call_sid = $1
		ORDER BY id DESC
		LIMIT $2
	`, callSID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.CallbackRequest
	for rows.Next() {
		item, err := scanCallbackRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) UpdateCallbackRequestStatus(id int64, callSID string, status string, auditNote string) (domain.CallbackRequest, error) {
	row := p.pool.QueryRow(context.Background(), `
		UPDATE callback_requests
		SET status = $3,
			audit_note = COALESCE(NULLIF($4, ''), audit_note),
			updated_at = now()
		WHERE id = $1 AND original_call_sid = $2
		RETURNING id, merchant_id, original_call_sid, COALESCE(original_call_id, ''),
			target_number, COALESCE(requested_by, ''), COALESCE(reason, ''),
			status, COALESCE(audit_note, ''), created_at, updated_at
	`, id, callSID, status, auditNote)
	item, err := scanCallbackRequest(row)
	if err == pgx.ErrNoRows {
		return domain.CallbackRequest{}, ErrNotFound
	}
	return item, err
}

func scanCallbackRequest(row pgx.Row) (domain.CallbackRequest, error) {
	var item domain.CallbackRequest
	err := row.Scan(
		&item.ID,
		&item.MerchantID,
		&item.OriginalCallSID,
		&item.OriginalCallID,
		&item.TargetNumber,
		&item.RequestedBy,
		&item.Reason,
		&item.Status,
		&item.AuditNote,
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
			status, attempt_count, max_attempts, last_error
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6,
			NULLIF($7, 0), NULLIF($8, 0), $9, $10, $11, $12, NULLIF($13, ''))
		ON CONFLICT (idempotency_key) DO UPDATE SET
			updated_at = now()
		RETURNING id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, max_attempts, COALESCE(last_error, ''),
			COALESCE(error_category, ''), next_retry_at, created_at, updated_at
	`, log.MerchantID, valueOr(log.Channel, "log"), log.MessageType, log.Target,
		log.Subject, log.Body, log.RelatedDigestID, log.RelatedInboxItemID,
		log.IdempotencyKey, valueOr(log.Status, "queued"), log.AttemptCount,
		valueOrInt(log.MaxAttempts, 5), log.LastError)
	return scanNotificationLog(row)
}

func (p *Postgres) FindNotificationLogByKey(key string) (domain.NotificationLog, bool, error) {
	row := p.pool.QueryRow(context.Background(), `
		SELECT id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, max_attempts, COALESCE(last_error, ''),
			COALESCE(error_category, ''), next_retry_at, created_at, updated_at
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
			idempotency_key, status, attempt_count, max_attempts, COALESCE(last_error, ''),
			COALESCE(error_category, ''), next_retry_at, created_at, updated_at
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
			error_category = NULL,
			next_retry_at = NULL,
			updated_at = now()
		WHERE id = $1
		RETURNING id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, max_attempts, COALESCE(last_error, ''),
			COALESCE(error_category, ''), next_retry_at, created_at, updated_at
	`, id, status, attemptCount, lastError)
	item, err := scanNotificationLog(row)
	if err == pgx.ErrNoRows {
		return domain.NotificationLog{}, ErrNotFound
	}
	return item, err
}

func (p *Postgres) ListDueNotificationLogs(status string, limit int, now time.Time) ([]domain.NotificationLog, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, max_attempts, COALESCE(last_error, ''),
			COALESCE(error_category, ''), next_retry_at, created_at, updated_at
		FROM notification_logs
		WHERE status = $1
			AND attempt_count < max_attempts
			AND (next_retry_at IS NULL OR next_retry_at <= $2)
		ORDER BY next_retry_at ASC NULLS FIRST, id ASC
		LIMIT $3
	`, status, now, limit)
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

func (p *Postgres) UpdateNotificationLogDispatch(
	id int64,
	status string,
	attemptCount int,
	maxAttempts int,
	lastError string,
	errorCategory string,
	nextRetryAt time.Time,
) (domain.NotificationLog, error) {
	var nextRetryValue any
	if !nextRetryAt.IsZero() {
		nextRetryValue = nextRetryAt
	}
	row := p.pool.QueryRow(context.Background(), `
		UPDATE notification_logs
		SET status = $2,
			attempt_count = $3,
			max_attempts = $4,
			last_error = NULLIF($5, ''),
			error_category = NULLIF($6, ''),
			next_retry_at = $7,
			updated_at = now()
		WHERE id = $1
		RETURNING id, merchant_id, channel, message_type, COALESCE(target, ''),
			COALESCE(subject, ''), body, related_digest_id, related_inbox_item_id,
			idempotency_key, status, attempt_count, max_attempts, COALESCE(last_error, ''),
			COALESCE(error_category, ''), next_retry_at, created_at, updated_at
	`, id, status, attemptCount, valueOrInt(maxAttempts, 5), lastError, errorCategory, nextRetryValue)
	item, err := scanNotificationLog(row)
	if err == pgx.ErrNoRows {
		return domain.NotificationLog{}, ErrNotFound
	}
	return item, err
}

func (p *Postgres) UpsertBusinessResultRetry(job domain.BusinessResultRetry) (domain.BusinessResultRetry, error) {
	row := p.pool.QueryRow(context.Background(), `
		INSERT INTO business_result_retries (
			session_id, call_sid, payload, status, attempt_count, last_error
		)
		VALUES ($1, $2, $3::jsonb, $4, $5, NULLIF($6, ''))
		ON CONFLICT (session_id) DO UPDATE SET
			call_sid = EXCLUDED.call_sid,
			payload = EXCLUDED.payload,
			status = EXCLUDED.status,
			attempt_count = EXCLUDED.attempt_count,
			last_error = EXCLUDED.last_error,
			updated_at = now()
		RETURNING id, session_id, call_sid, payload::text, status,
			attempt_count, COALESCE(last_error, ''), created_at, updated_at
	`, job.SessionID, job.CallSID, job.Payload, valueOr(job.Status, "failed"),
		job.AttemptCount, job.LastError)
	return scanBusinessResultRetry(row)
}

func (p *Postgres) ListBusinessResultRetries(status string, limit int) ([]domain.BusinessResultRetry, error) {
	rows, err := p.pool.Query(context.Background(), `
		SELECT id, session_id, call_sid, payload::text, status,
			attempt_count, COALESCE(last_error, ''), created_at, updated_at
		FROM business_result_retries
		WHERE ($1 = '' OR status = $1)
		ORDER BY id DESC
		LIMIT $2
	`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []domain.BusinessResultRetry
	for rows.Next() {
		item, err := scanBusinessResultRetry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *Postgres) UpdateBusinessResultRetryStatus(id int64, status string, attemptCount int, lastError string) (domain.BusinessResultRetry, error) {
	row := p.pool.QueryRow(context.Background(), `
		UPDATE business_result_retries
		SET status = $2,
			attempt_count = $3,
			last_error = NULLIF($4, ''),
			updated_at = now()
		WHERE id = $1
		RETURNING id, session_id, call_sid, payload::text, status,
			attempt_count, COALESCE(last_error, ''), created_at, updated_at
	`, id, status, attemptCount, lastError)
	item, err := scanBusinessResultRetry(row)
	if err == pgx.ErrNoRows {
		return domain.BusinessResultRetry{}, ErrNotFound
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
	var nextRetryAt sql.NullTime
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
		&item.MaxAttempts,
		&item.LastError,
		&item.ErrorCategory,
		&nextRetryAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if relatedDigestID.Valid {
		item.RelatedDigestID = relatedDigestID.Int64
	}
	if relatedInboxItemID.Valid {
		item.RelatedInboxItemID = relatedInboxItemID.Int64
	}
	if nextRetryAt.Valid {
		item.NextRetryAt = &nextRetryAt.Time
	}
	return item, err
}

func scanBusinessResultRetry(row pgx.Row) (domain.BusinessResultRetry, error) {
	var item domain.BusinessResultRetry
	err := row.Scan(
		&item.ID,
		&item.SessionID,
		&item.CallSID,
		&item.Payload,
		&item.Status,
		&item.AttemptCount,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func valueOr(value string, fallback string) string {
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

func emptyStringSlice(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
