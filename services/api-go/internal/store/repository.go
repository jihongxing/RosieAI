package store

import "rosie-api/internal/domain"

type Repository interface {
	UpsertMerchant(domain.Merchant) (domain.Merchant, error)
	ListMerchants() ([]domain.Merchant, error)
	FindMerchantByAccessNumber(string) (domain.Merchant, bool, error)
	FindMerchantByID(string) (domain.Merchant, bool, error)
	EnsureMerchantProfile(string) (domain.MerchantProfile, error)
	UpdateMerchantProfile(domain.MerchantProfile) (domain.MerchantProfile, error)
	UpsertCall(domain.Call) (domain.Call, error)
	FindCallBySID(string) (domain.Call, bool, error)
	ListCalls(int) ([]domain.Call, error)
	UpsertTranscript(domain.Transcript) (domain.Transcript, error)
	FindTranscriptByCallSID(string) (domain.Transcript, bool, error)
	UpsertSummary(domain.Summary) (domain.Summary, error)
	FindSummaryByCallSID(string) (domain.Summary, bool, error)
	UpsertInboxItem(domain.InboxItem) (domain.InboxItem, error)
	FindInboxItemByCallSID(string) (domain.InboxItem, bool, error)
	ListInboxItems(string, int) ([]domain.InboxItem, error)
	ListPendingDigestItems(string, int) ([]domain.InboxItem, error)
	InsertDigest(domain.Digest) (domain.Digest, error)
	MarkInboxItemsDigested([]int64) error
	ListDigests(string, int) ([]domain.Digest, error)
	EnsurePreferences(string) (domain.NotificationPreferences, error)
	UpdatePreferences(domain.NotificationPreferences) (domain.NotificationPreferences, error)
	InsertNotificationLog(domain.NotificationLog) (domain.NotificationLog, error)
	FindNotificationLogByKey(string) (domain.NotificationLog, bool, error)
	ListNotificationLogs(string, string, int) ([]domain.NotificationLog, error)
	UpdateNotificationLogStatus(int64, string, int, string) (domain.NotificationLog, error)
	UpsertAppUser(domain.AppUser) (domain.AppUser, error)
	BindMerchantUser(string, int64, string) (domain.MerchantUserBinding, error)
	FindPrimaryOpenIDByMerchantID(string) (string, bool, error)
}
