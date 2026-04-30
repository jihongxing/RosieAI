package config

import (
	"os"
	"strconv"
	"time"

	"rosie-api/internal/domain"
)

type Config struct {
	Addr                 string
	DatabaseURL          string
	DefaultAccessNumber  string
	DefaultMerchantID    string
	DefaultMerchantName  string
	DefaultTransferPhone string
	AIAgentURL           string
	AISummaryEnabled     bool
	AISummaryTimeout     time.Duration
	Jambonz              JambonzConfig
	WeChat               WeChatConfig
	WeChatPay            WeChatPayConfig
}

type JambonzConfig struct {
	ExpectedCallHookURL   string
	ExpectedStatusHookURL string
	APIBaseURL            string
	APIToken              string
	ApplicationsPath      string
	PhoneNumbersPath      string
}

type WeChatConfig struct {
	APIBaseURL       string
	AppID            string
	AppSecret        string
	TemplateID       string
	Page             string
	MiniprogramState string
	Lang             string
	DefaultOpenID    string
	TitleKey         string
	SummaryKey       string
	TimeKey          string
}

type WeChatPayConfig struct {
	APIBaseURL       string
	MchID            string
	NotifyURL        string
	MerchantSerialNo string
	PrivateKeyPath   string
	APIv3Key         string
	PlatformSerialNo string
	PlatformKeyPath  string
}

func FromEnv() Config {
	return Config{
		Addr:                 env("ROSIE_API_ADDR", "127.0.0.1:8030"),
		DatabaseURL:          os.Getenv("ROSIE_DATABASE_URL"),
		DefaultAccessNumber:  env("ROSIE_DEFAULT_ACCESS_NUMBER", "8613736849910"),
		DefaultMerchantID:    env("ROSIE_DEFAULT_MERCHANT_ID", "demo-merchant"),
		DefaultMerchantName:  env("ROSIE_DEFAULT_MERCHANT_NAME", "测试商家"),
		DefaultTransferPhone: os.Getenv("ROSIE_DEFAULT_TRANSFER_PHONE"),
		AIAgentURL:           os.Getenv("ROSIE_AI_AGENT_URL"),
		AISummaryEnabled:     envBool("ROSIE_AI_SUMMARY_ENABLED", true),
		AISummaryTimeout:     envDurationSeconds("ROSIE_AI_SUMMARY_TIMEOUT_SECONDS", 3*time.Second),
		Jambonz: JambonzConfig{
			ExpectedCallHookURL:   os.Getenv("ROSIE_JAMBONZ_EXPECTED_CALL_HOOK_URL"),
			ExpectedStatusHookURL: os.Getenv("ROSIE_JAMBONZ_EXPECTED_STATUS_HOOK_URL"),
			APIBaseURL:            os.Getenv("ROSIE_JAMBONZ_API_BASE_URL"),
			APIToken:              os.Getenv("ROSIE_JAMBONZ_API_TOKEN"),
			ApplicationsPath:      env("ROSIE_JAMBONZ_APPLICATIONS_PATH", "/Applications"),
			PhoneNumbersPath:      env("ROSIE_JAMBONZ_PHONE_NUMBERS_PATH", "/PhoneNumbers"),
		},
		WeChat: WeChatConfig{
			APIBaseURL:       env("ROSIE_WECHAT_API_BASE_URL", "https://api.weixin.qq.com"),
			AppID:            os.Getenv("ROSIE_WECHAT_APP_ID"),
			AppSecret:        os.Getenv("ROSIE_WECHAT_APP_SECRET"),
			TemplateID:       os.Getenv("ROSIE_WECHAT_SUBSCRIBE_TEMPLATE_ID"),
			Page:             env("ROSIE_WECHAT_SUBSCRIBE_PAGE", "pages/inbox/index"),
			MiniprogramState: env("ROSIE_WECHAT_MINIPROGRAM_STATE", "formal"),
			Lang:             env("ROSIE_WECHAT_LANG", "zh_CN"),
			DefaultOpenID:    os.Getenv("ROSIE_WECHAT_DEFAULT_OPENID"),
			TitleKey:         env("ROSIE_WECHAT_DIGEST_TITLE_KEY", "thing1"),
			SummaryKey:       env("ROSIE_WECHAT_DIGEST_SUMMARY_KEY", "thing2"),
			TimeKey:          env("ROSIE_WECHAT_DIGEST_TIME_KEY", "time3"),
		},
		WeChatPay: WeChatPayConfig{
			APIBaseURL:       env("ROSIE_WECHAT_PAY_API_BASE_URL", "https://api.mch.weixin.qq.com"),
			MchID:            os.Getenv("ROSIE_WECHAT_PAY_MCH_ID"),
			NotifyURL:        os.Getenv("ROSIE_WECHAT_PAY_NOTIFY_URL"),
			MerchantSerialNo: os.Getenv("ROSIE_WECHAT_PAY_MERCHANT_SERIAL_NO"),
			PrivateKeyPath:   os.Getenv("ROSIE_WECHAT_PAY_PRIVATE_KEY_PATH"),
			APIv3Key:         os.Getenv("ROSIE_WECHAT_PAY_API_V3_KEY"),
			PlatformSerialNo: os.Getenv("ROSIE_WECHAT_PAY_PLATFORM_SERIAL_NO"),
			PlatformKeyPath:  os.Getenv("ROSIE_WECHAT_PAY_PLATFORM_KEY_PATH"),
		},
	}
}

func (c Config) DefaultMerchant() domain.Merchant {
	return domain.Merchant{
		MerchantID:    c.DefaultMerchantID,
		MerchantName:  c.DefaultMerchantName,
		AccessNumber:  c.DefaultAccessNumber,
		TransferPhone: c.DefaultTransferPhone,
		Enabled:       true,
	}
}

func env(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationSeconds(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return time.Duration(parsed * float64(time.Second))
}
