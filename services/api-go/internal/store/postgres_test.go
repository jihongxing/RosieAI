package store

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"rosie-api/internal/domain"
)

func TestPostgresStoreSmoke(t *testing.T) {
	databaseURL := os.Getenv("ROSIE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set ROSIE_TEST_DATABASE_URL to run postgres integration test")
	}

	repo, err := NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	migrationPaths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrationPaths)
	for _, migrationPath := range migrationPaths {
		migration, err := os.ReadFile(migrationPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.pool.Exec(context.Background(), string(migration)); err != nil {
			t.Fatalf("apply %s: %v", migrationPath, err)
		}
	}

	merchant, err := repo.UpsertMerchant(domain.Merchant{
		MerchantID:   "pg-smoke-merchant",
		MerchantName: "Postgres 测试店",
		AccessNumber: "+8617000000300",
		Enabled:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merchant.AccessNumber != "8617000000300" {
		t.Fatalf("expected normalized access number, got %s", merchant.AccessNumber)
	}

	found, ok, err := repo.FindMerchantByAccessNumber("+8617000000300")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || found.MerchantID != "pg-smoke-merchant" {
		t.Fatalf("merchant not found by access number: ok=%v item=%#v", ok, found)
	}
	if _, err := repo.UpdateMerchantProfile(domain.MerchantProfile{
		MerchantID:       merchant.MerchantID,
		Industry:         "hair_salon",
		Address:          "测试路 1 号",
		BusinessHours:    "10:00-21:00",
		Services:         []string{"剪发", "烫染"},
		FAQItems:         []domain.FAQItem{{Question: "几点营业", Answer: "10:00-21:00"}},
		AppointmentRules: "留下称呼、电话和时间。",
	}); err != nil {
		t.Fatal(err)
	}
	profile, err := repo.EnsureMerchantProfile(merchant.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Industry != "hair_salon" || len(profile.Services) != 2 || len(profile.FAQItems) != 1 {
		t.Fatalf("unexpected merchant profile: %#v", profile)
	}

	call, err := repo.UpsertCall(domain.Call{
		CallSID:    "pg-smoke-call",
		CallID:     "sim-pg-smoke-call",
		MerchantID: merchant.MerchantID,
		FromNumber: "+8613811112222",
		ToNumber:   merchant.AccessNumber,
		CallStatus: "completed",
		Direction:  "inbound",
		RawPayload: `{"call_sid":"pg-smoke-call"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertTranscript(domain.Transcript{
		CallSID:    call.CallSID,
		MerchantID: merchant.MerchantID,
		Transcript: "你好，我想预约明天下午三点剪头发。",
		Source:     "simulated",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertSummary(domain.Summary{
		CallSID:           call.CallSID,
		MerchantID:        merchant.MerchantID,
		Summary:           "客户想预约明天下午三点剪头发。",
		Intent:            "appointment",
		AppointmentTime:   "明天下午三点",
		Priority:          "high",
		NeedHumanFollowup: true,
		RawResult:         `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertInboxItem(domain.InboxItem{
		MerchantID:        merchant.MerchantID,
		CallSID:           call.CallSID,
		ItemType:          "call_summary",
		Title:             "预约意向",
		Body:              "客户想预约明天下午三点剪头发。",
		Priority:          "high",
		Status:            "needs_review",
		NeedHumanFollowup: true,
		DigestStatus:      "pending",
	}); err != nil {
		t.Fatal(err)
	}

	foundCall, ok, err := repo.FindCallBySID(call.CallSID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || foundCall.CallSID != call.CallSID {
		t.Fatalf("call not found by sid: ok=%v item=%#v", ok, foundCall)
	}
	foundTranscript, ok, err := repo.FindTranscriptByCallSID(call.CallSID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || foundTranscript.Source != "simulated" {
		t.Fatalf("transcript not found by sid: ok=%v item=%#v", ok, foundTranscript)
	}
	foundSummary, ok, err := repo.FindSummaryByCallSID(call.CallSID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || foundSummary.Intent != "appointment" {
		t.Fatalf("summary not found by sid: ok=%v item=%#v", ok, foundSummary)
	}
	foundInbox, ok, err := repo.FindInboxItemByCallSID(call.CallSID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || foundInbox.Status != "needs_review" {
		t.Fatalf("inbox not found by sid: ok=%v item=%#v", ok, foundInbox)
	}

	user, err := repo.UpsertAppUser(domain.AppUser{
		OpenID:     "pg-openid",
		UnionID:    "pg-unionid",
		SessionKey: "session-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BindMerchantUser(merchant.MerchantID, user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	openID, ok, err := repo.FindPrimaryOpenIDByMerchantID(merchant.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || openID != "pg-openid" {
		t.Fatalf("openid not found by merchant: ok=%v openid=%s", ok, openID)
	}
}
