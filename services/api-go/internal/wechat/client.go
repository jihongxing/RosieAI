package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rosie-api/internal/config"
	"rosie-api/internal/domain"
)

type Client struct {
	cfg        config.WeChatConfig
	httpClient *http.Client
	token      string
	tokenUntil time.Time
}

type LoginSession struct {
	OpenID     string
	UnionID    string
	SessionKey string
}

func NewClient(cfg config.WeChatConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{cfg: cfg, httpClient: httpClient}
}

func (c *Client) SendSubscribe(ctx context.Context, log domain.NotificationLog, openID string) error {
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" || c.cfg.TemplateID == "" {
		return errors.New("missing wechat app id, app secret or subscribe template id")
	}
	if openID == "" {
		return errors.New("missing wechat openid")
	}

	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"touser":      openID,
		"template_id": c.cfg.TemplateID,
		"page":        c.cfg.Page,
		"data":        c.templateData(log),
	}
	if c.cfg.MiniprogramState != "" {
		payload["miniprogram_state"] = c.cfg.MiniprogramState
	}
	if c.cfg.Lang != "" {
		payload["lang"] = c.cfg.Lang
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return err
	}
	endpoint := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/cgi-bin/message/subscribe/send?access_token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result wechatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wechat subscribe send failed: %d %s", result.ErrCode, result.ErrMsg)
	}
	return nil
}

func (c *Client) CodeToSession(ctx context.Context, code string) (LoginSession, error) {
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		return LoginSession{}, errors.New("missing wechat app id or app secret")
	}
	if code == "" {
		return LoginSession{}, errors.New("missing js_code")
	}

	values := url.Values{}
	values.Set("appid", c.cfg.AppID)
	values.Set("secret", c.cfg.AppSecret)
	values.Set("js_code", code)
	values.Set("grant_type", "authorization_code")
	endpoint := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/sns/jscode2session?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return LoginSession{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return LoginSession{}, err
	}
	defer resp.Body.Close()

	var result codeSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return LoginSession{}, err
	}
	if result.ErrCode != 0 {
		return LoginSession{}, fmt.Errorf("wechat code2session failed: %d %s", result.ErrCode, result.ErrMsg)
	}
	if result.OpenID == "" || result.SessionKey == "" {
		return LoginSession{}, errors.New("wechat code2session response missing openid or session_key")
	}
	return LoginSession{
		OpenID:     result.OpenID,
		UnionID:    result.UnionID,
		SessionKey: result.SessionKey,
	}, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		return c.token, nil
	}

	values := url.Values{}
	values.Set("grant_type", "client_credential")
	values.Set("appid", c.cfg.AppID)
	values.Set("secret", c.cfg.AppSecret)
	endpoint := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/cgi-bin/token?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat token failed: %d %s", result.ErrCode, result.ErrMsg)
	}
	if result.AccessToken == "" {
		return "", errors.New("wechat token response missing access_token")
	}
	c.token = result.AccessToken
	expires := time.Duration(result.ExpiresIn-300) * time.Second
	if expires <= 0 {
		expires = time.Hour
	}
	c.tokenUntil = time.Now().Add(expires)
	return c.token, nil
}

func (c *Client) templateData(log domain.NotificationLog) map[string]map[string]string {
	data := map[string]map[string]string{}
	if c.cfg.TitleKey != "" {
		data[c.cfg.TitleKey] = map[string]string{"value": fieldValue(c.cfg.TitleKey, valueOr(log.Subject, "Rosie 来电汇总"))}
	}
	if c.cfg.SummaryKey != "" {
		data[c.cfg.SummaryKey] = map[string]string{"value": fieldValue(c.cfg.SummaryKey, log.Body)}
	}
	if c.cfg.TimeKey != "" {
		data[c.cfg.TimeKey] = map[string]string{"value": time.Now().Format("2006-01-02 15:04")}
	}
	return data
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

type wechatResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type codeSessionResponse struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

func fieldValue(key string, value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(key, "thing") {
		return truncateRunes(value, 20)
	}
	return truncateRunes(value, 200)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
