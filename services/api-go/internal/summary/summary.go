package summary

import (
	"regexp"
	"strings"

	"rosie-api/internal/domain"
)

var phonePattern = regexp.MustCompile(`1[3-9]\d{9}`)
var digitTimePattern = regexp.MustCompile(`(今天|明天|后天)?\s*(上午|下午|晚上)?\s*\d{1,2}\s*[点:：]\s*(\d{1,2}分?)?`)
var chineseTimePattern = regexp.MustCompile(`(今天|明天|后天)?\s*(上午|下午|晚上)?\s*(十一|十二|十|零|一|二|两|三|四|五|六|七|八|九)\s*点\s*(半|一刻|三刻)?`)

func Fallback(transcript string) domain.Summary {
	text := strings.TrimSpace(transcript)
	summaryText := customerOnlyText(text)
	if summaryText == "" {
		summaryText = text
	}
	lower := strings.ToLower(text)
	intent := "inquiry"
	priority := "normal"
	needFollowup := true

	if containsAny(lower, []string{"贷款", "pos", "发票", "代开", "推广", "营销", "信用卡"}) {
		intent = "spam"
		priority = "low"
		needFollowup = false
	} else if containsAny(text, []string{"投诉", "紧急", "急", "马上", "尽快", "严重"}) {
		intent = "urgent"
		priority = "urgent"
	} else if containsAny(text, []string{"预约", "约", "明天", "今天", "下午", "上午", "晚上", "几点"}) {
		intent = "appointment"
		priority = "high"
	}

	return domain.Summary{
		Summary:           truncateRunes(summaryText, 120),
		CustomerPhone:     firstMatch(phonePattern, text),
		Intent:            intent,
		AppointmentTime:   timeHint(text),
		Priority:          priority,
		NeedHumanFollowup: needFollowup,
		RawResult:         "{}",
	}
}

func customerOnlyText(text string) string {
	lines := strings.Split(text, "\n")
	customerLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{"客户：", "客户:", "Customer:", "customer:"} {
			if strings.HasPrefix(line, prefix) {
				customerLines = append(customerLines, strings.TrimSpace(strings.TrimPrefix(line, prefix)))
				break
			}
		}
	}
	return strings.Join(customerLines, "\n")
}

func InboxTitle(s domain.Summary) string {
	switch s.Intent {
	case "spam":
		return "疑似骚扰来电"
	case "appointment":
		return "预约意向"
	case "urgent":
		return "紧急事项"
	default:
		return "有效来电"
	}
}

func InboxStatus(s domain.Summary) string {
	if s.Intent == "spam" {
		return "filtered"
	}
	if s.NeedHumanFollowup {
		return "needs_review"
	}
	return "archived"
}

func DigestText(items []domain.InboxItem) string {
	if len(items) == 0 {
		return "Rosie 今日暂无需要汇总的漏接电话。"
	}

	appointmentCount := 0
	urgentCount := 0
	spamCount := 0
	followupItems := make([]domain.InboxItem, 0)
	for _, item := range items {
		if item.Title == "预约意向" {
			appointmentCount++
		}
		if item.Priority == "urgent" {
			urgentCount++
		}
		if item.Status == "filtered" {
			spamCount++
		}
		if item.NeedHumanFollowup && item.Status != "filtered" {
			followupItems = append(followupItems, item)
		}
	}

	lines := []string{
		"Rosie 今日帮你整理了 " + itoa(len(items)) + " 通漏接电话：",
		"- " + itoa(appointmentCount) + " 个预约意向",
		"- " + itoa(urgentCount) + " 个紧急事项",
		"- " + itoa(spamCount) + " 个疑似骚扰",
		"- " + itoa(len(followupItems)) + " 个建议处理",
	}
	if len(followupItems) > 0 {
		lines = append(lines, "", "建议优先处理：")
		max := len(followupItems)
		if max > 5 {
			max = 5
		}
		for i := 0; i < max; i++ {
			item := followupItems[i]
			lines = append(lines, itoa(i+1)+". "+item.Title+"："+item.Body)
		}
	}
	return strings.Join(lines, "\n")
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) || strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func firstMatch(pattern *regexp.Regexp, text string) string {
	return pattern.FindString(text)
}

func timeHint(text string) string {
	if match := digitTimePattern.FindString(text); match != "" {
		return strings.TrimSpace(match)
	}
	if match := chineseTimePattern.FindString(text); match != "" {
		return strings.TrimSpace(match)
	}
	return ""
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
