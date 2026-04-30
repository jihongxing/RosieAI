package config

import (
	"os"

	"rosie-api/internal/domain"
)

type Config struct {
	Addr                 string
	DatabaseURL          string
	DefaultAccessNumber  string
	DefaultMerchantID    string
	DefaultMerchantName  string
	DefaultTransferPhone string
	WeChat               WeChatConfig
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

func FromEnv() Config {
	return Config{
		Addr:                 env("ROSIE_API_ADDR", "127.0.0.1:8030"),
		DatabaseURL:          os.Getenv("ROSIE_DATABASE_URL"),
		DefaultAccessNumber:  env("ROSIE_DEFAULT_ACCESS_NUMBER", "8613736849910"),
		DefaultMerchantID:    env("ROSIE_DEFAULT_MERCHANT_ID", "demo-merchant"),
		DefaultMerchantName:  env("ROSIE_DEFAULT_MERCHANT_NAME", "测试商家"),
		DefaultTransferPhone: os.Getenv("ROSIE_DEFAULT_TRANSFER_PHONE"),
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
