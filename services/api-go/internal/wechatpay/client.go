package wechatpay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"rosie-api/internal/config"
)

type Client struct {
	appID      string
	cfg        config.WeChatPayConfig
	httpClient *http.Client
	privateKey *rsa.PrivateKey
}

type JSAPIOrderRequest struct {
	OrderNo     string
	Description string
	AmountCents int
	Currency    string
	OpenID      string
}

type JSAPIOrder struct {
	PrepayID      string            `json:"prepay_id"`
	RequestParams map[string]string `json:"request_params"`
}

type Notification struct {
	OutTradeNo    string
	TransactionID string
	TradeState    string
	SuccessTime   time.Time
}

func Configured(appID string, cfg config.WeChatPayConfig) bool {
	return appID != "" &&
		cfg.MchID != "" &&
		cfg.NotifyURL != "" &&
		cfg.MerchantSerialNo != "" &&
		cfg.PrivateKeyPath != "" &&
		cfg.APIv3Key != ""
}

func NotifyConfigured(cfg config.WeChatPayConfig) bool {
	return cfg.APIv3Key != "" && cfg.PlatformSerialNo != "" && cfg.PlatformKeyPath != ""
}

func NewClient(appID string, cfg config.WeChatPayConfig, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	privateKey, err := loadPrivateKey(cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	return &Client{appID: appID, cfg: cfg, httpClient: httpClient, privateKey: privateKey}, nil
}

func (c *Client) CreateJSAPIOrder(ctx context.Context, req JSAPIOrderRequest) (JSAPIOrder, error) {
	if req.OrderNo == "" {
		return JSAPIOrder{}, errors.New("missing order no")
	}
	if req.OpenID == "" {
		return JSAPIOrder{}, errors.New("missing payer openid")
	}
	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		currency = "CNY"
	}
	payload := map[string]any{
		"appid":        c.appID,
		"mchid":        c.cfg.MchID,
		"description":  valueOr(req.Description, "Rosie AI 试点套餐续费"),
		"out_trade_no": req.OrderNo,
		"notify_url":   c.cfg.NotifyURL,
		"amount": map[string]any{
			"total":    req.AmountCents,
			"currency": currency,
		},
		"payer": map[string]any{
			"openid": req.OpenID,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return JSAPIOrder{}, err
	}
	endpoint := strings.TrimRight(valueOr(c.cfg.APIBaseURL, "https://api.mch.weixin.qq.com"), "/") + "/v3/pay/transactions/jsapi"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return JSAPIOrder{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.signRequest(httpReq, body); err != nil {
		return JSAPIOrder{}, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return JSAPIOrder{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return JSAPIOrder{}, fmt.Errorf("wechat pay jsapi order failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	var result struct {
		PrepayID string `json:"prepay_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return JSAPIOrder{}, err
	}
	if result.PrepayID == "" {
		return JSAPIOrder{}, errors.New("wechat pay response missing prepay_id")
	}
	params, err := c.RequestPaymentParams(result.PrepayID, time.Now())
	if err != nil {
		return JSAPIOrder{}, err
	}
	return JSAPIOrder{PrepayID: result.PrepayID, RequestParams: params}, nil
}

func (c *Client) RequestPaymentParams(prepayID string, now time.Time) (map[string]string, error) {
	timeStamp := strconv.FormatInt(now.Unix(), 10)
	nonce := randomHex(16)
	packageValue := "prepay_id=" + prepayID
	signature, err := c.signString(c.appID + "\n" + timeStamp + "\n" + nonce + "\n" + packageValue + "\n")
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"timeStamp": timeStamp,
		"nonceStr":  nonce,
		"package":   packageValue,
		"signType":  "RSA",
		"paySign":   signature,
	}, nil
}

func (c *Client) signRequest(req *http.Request, body []byte) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomHex(16)
	canonicalURL := req.URL.EscapedPath()
	if req.URL.RawQuery != "" {
		canonicalURL += "?" + req.URL.RawQuery
	}
	message := req.Method + "\n" + canonicalURL + "\n" + timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	signature, err := c.signString(message)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		c.cfg.MchID, nonce, signature, timestamp, c.cfg.MerchantSerialNo,
	))
	return nil
}

func (c *Client) signString(message string) (string, error) {
	hash := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func ParseNotification(cfg config.WeChatPayConfig, header http.Header, body []byte) (Notification, error) {
	if !NotifyConfigured(cfg) {
		return Notification{}, errors.New("missing wechat pay notification verification config")
	}
	serial := strings.TrimSpace(header.Get("Wechatpay-Serial"))
	if serial == "" {
		return Notification{}, errors.New("missing wechat pay platform serial header")
	}
	if serial != cfg.PlatformSerialNo {
		return Notification{}, fmt.Errorf("wechat pay platform serial mismatch: %s", serial)
	}
	timestamp := header.Get("Wechatpay-Timestamp")
	nonce := header.Get("Wechatpay-Nonce")
	signature := header.Get("Wechatpay-Signature")
	if timestamp == "" || nonce == "" || signature == "" {
		return Notification{}, errors.New("missing wechat pay signature headers")
	}
	publicKey, err := loadPublicKey(cfg.PlatformKeyPath)
	if err != nil {
		return Notification{}, err
	}
	if err := verifySignature(publicKey, timestamp+"\n"+nonce+"\n"+string(body)+"\n", signature); err != nil {
		return Notification{}, err
	}

	var envelope struct {
		Resource struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			Nonce          string `json:"nonce"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Notification{}, err
	}
	if envelope.Resource.Ciphertext == "" || envelope.Resource.Nonce == "" {
		return Notification{}, errors.New("missing encrypted resource")
	}
	if envelope.Resource.Algorithm != "" && envelope.Resource.Algorithm != "AEAD_AES_256_GCM" {
		return Notification{}, fmt.Errorf("unsupported wechat pay resource algorithm: %s", envelope.Resource.Algorithm)
	}
	plain, err := decryptResource(cfg.APIv3Key, envelope.Resource.Nonce, envelope.Resource.AssociatedData, envelope.Resource.Ciphertext)
	if err != nil {
		return Notification{}, err
	}
	var transaction struct {
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		SuccessTime   string `json:"success_time"`
	}
	if err := json.Unmarshal(plain, &transaction); err != nil {
		return Notification{}, err
	}
	item := Notification{
		OutTradeNo:    transaction.OutTradeNo,
		TransactionID: transaction.TransactionID,
		TradeState:    transaction.TradeState,
	}
	if transaction.SuccessTime != "" {
		parsed, err := time.Parse(time.RFC3339, transaction.SuccessTime)
		if err != nil {
			return Notification{}, fmt.Errorf("invalid success_time: %w", err)
		}
		item.SuccessTime = parsed.UTC()
	}
	return item, nil
}

func VerifyNotificationSignature(cfg config.WeChatPayConfig, timestamp string, nonce string, body []byte, signature string) error {
	publicKey, err := loadPublicKey(cfg.PlatformKeyPath)
	if err != nil {
		return err
	}
	return verifySignature(publicKey, timestamp+"\n"+nonce+"\n"+string(body)+"\n", signature)
}

func decryptResource(apiV3Key string, nonce string, associatedData string, ciphertext string) ([]byte, error) {
	key := []byte(apiV3Key)
	if len(key) != 32 {
		return nil, errors.New("wechat pay api v3 key must be 32 bytes")
	}
	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(nonce), ciphertextBytes, []byte(associatedData))
}

func verifySignature(publicKey *rsa.PublicKey, message string, signatureValue string) error {
	signature, err := base64.StdEncoding.DecodeString(signatureValue)
	if err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature)
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return nil, errors.New("missing private key path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return key, nil
}

func loadPublicKey(path string) (*rsa.PublicKey, error) {
	if path == "" {
		return nil, errors.New("missing platform public key path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid public key pem")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if publicKey, ok := key.(*rsa.PublicKey); ok {
			return publicKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("platform public key is not RSA")
	}
	return publicKey, nil
}

func randomHex(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}

func valueOr(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
