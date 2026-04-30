package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"rosie-api/internal/domain"
)

type AgentExtractor struct {
	baseURL string
	client  *http.Client
}

func NewAgentExtractor(baseURL string, timeout time.Duration) *AgentExtractor {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &AgentExtractor{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

func (e *AgentExtractor) Health(ctx context.Context) (map[string]any, error) {
	if e == nil || e.baseURL == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai-agent health returned %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

type agentExtractRequest struct {
	MerchantName string `json:"merchant_name"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Transcript   string `json:"transcript"`
}

type agentExtractResponse struct {
	Model  string `json:"model"`
	Result string `json:"result"`
}

type agentSummaryResult struct {
	Summary           *string `json:"summary"`
	CustomerName      *string `json:"customer_name"`
	CustomerPhone     *string `json:"customer_phone"`
	Intent            *string `json:"intent"`
	AppointmentTime   *string `json:"appointment_time"`
	Service           *string `json:"service"`
	Priority          *string `json:"priority"`
	NeedHumanFollowup *bool   `json:"need_human_followup"`
}

func (e *AgentExtractor) Extract(
	ctx context.Context,
	merchantName string,
	systemPrompt string,
	transcript string,
	fallback domain.Summary,
) (domain.Summary, bool, error) {
	if e == nil || e.baseURL == "" || strings.TrimSpace(transcript) == "" {
		return domain.Summary{}, false, nil
	}
	body, err := json.Marshal(agentExtractRequest{
		MerchantName: merchantName,
		SystemPrompt: systemPrompt,
		Transcript:   transcript,
	})
	if err != nil {
		return domain.Summary{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/extract", bytes.NewReader(body))
	if err != nil {
		return domain.Summary{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return domain.Summary{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.Summary{}, false, nil
	}

	var extract agentExtractResponse
	if err := json.NewDecoder(resp.Body).Decode(&extract); err != nil {
		return domain.Summary{}, false, err
	}
	item, ok := parseAgentSummary(extract.Result, fallback)
	if !ok {
		return domain.Summary{}, false, nil
	}
	raw, _ := json.Marshal(map[string]string{
		"source": "ai_agent",
		"model":  extract.Model,
		"result": extract.Result,
	})
	item.RawResult = string(raw)
	return item, true, nil
}

func parseAgentSummary(result string, fallback domain.Summary) (domain.Summary, bool) {
	cleaned := cleanAgentJSON(result)
	var parsed agentSummaryResult
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return domain.Summary{}, false
	}

	item := fallback
	if value := stringPtrValue(parsed.Summary); value != "" {
		item.Summary = truncateRunes(value, 160)
	}
	if value := stringPtrValue(parsed.CustomerName); value != "" {
		item.CustomerName = value
	}
	if value := stringPtrValue(parsed.CustomerPhone); value != "" {
		item.CustomerPhone = value
	}
	if value := stringPtrValue(parsed.Intent); value != "" {
		item.Intent = value
	}
	if value := stringPtrValue(parsed.AppointmentTime); value != "" {
		item.AppointmentTime = value
	}
	if value := stringPtrValue(parsed.Service); value != "" {
		item.Service = value
	}
	if value := stringPtrValue(parsed.Priority); validPriority(value) {
		item.Priority = value
	}
	if parsed.NeedHumanFollowup != nil {
		item.NeedHumanFollowup = *parsed.NeedHumanFollowup
	}
	if strings.TrimSpace(item.Summary) == "" {
		return domain.Summary{}, false
	}
	return item, true
}

func cleanAgentJSON(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	return strings.TrimSpace(value)
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func validPriority(value string) bool {
	switch value {
	case "low", "normal", "high", "urgent":
		return true
	default:
		return false
	}
}
