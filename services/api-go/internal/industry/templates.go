package industry

import (
	"strings"

	"rosie-api/internal/domain"
)

type Template struct {
	Key                    string   `json:"key"`
	Name                   string   `json:"name"`
	Opening                string   `json:"opening"`
	QualificationQuestions []string `json:"qualification_questions"`
	FAQHints               []string `json:"faq_hints"`
	SummaryRules           []string `json:"summary_rules"`
}

func List() []Template {
	return []Template{
		{
			Key:     "hair_salon",
			Name:    "理发店 / 美发店",
			Opening: "您好，这里是{{merchant_name}}的 Rosie AI 前台。老板现在可能在忙，我可以先帮您记录预约或咨询。",
			QualificationQuestions: []string{
				"请问您想剪发、烫染、护理，还是咨询价格和营业时间？",
				"如果要预约，请问您希望哪天几点到店？",
				"方便留下称呼和联系电话吗？我会让老板尽快确认。",
			},
			FAQHints: []string{
				"优先回答营业时间、地址、服务项目和大致价格。",
				"价格不确定时不要编造，记录客户诉求并提示老板确认。",
				"遇到投诉、临近到店、改约取消时标记为需要人工跟进。",
			},
			SummaryRules: []string{
				"摘要必须包含预约时间、服务项目、客户称呼、联系电话。",
				"明确预约或改约时优先级至少为 high。",
				"推销、贷款、发票、POS 机等内容标记为 spam 或 filtered。",
			},
		},
		{
			Key:     "local_service",
			Name:    "本地生活服务",
			Opening: "您好，这里是{{merchant_name}}的 Rosie AI 前台。我可以先帮您记录需求、地址、时间和联系方式。",
			QualificationQuestions: []string{
				"请问您需要哪类服务？",
				"服务地址大概在哪个区域？",
				"希望什么时候上门或回电？",
				"方便留下称呼和联系电话吗？",
			},
			FAQHints: []string{
				"优先收集服务类型、区域、期望时间和紧急程度。",
				"报价不确定时不要承诺最终价格，只记录情况并提示人工确认。",
				"涉及安全、投诉、当天急单时标记为需要人工跟进。",
			},
			SummaryRules: []string{
				"摘要必须包含服务类型、地点、期望时间、客户电话。",
				"当天急单、投诉、无法判断价格时优先级至少为 high。",
				"无明确服务需求的营销电话标记为 spam 或 filtered。",
			},
		},
	}
}

func DefaultKey() string {
	return "hair_salon"
}

func Find(key string) Template {
	for _, item := range List() {
		if item.Key == key {
			return item
		}
	}
	return List()[0]
}

func BuildPrompt(merchant domain.Merchant, profile domain.MerchantProfile) string {
	tmpl := Find(valueOr(profile.Industry, DefaultKey()))
	name := valueOr(merchant.MerchantName, "商家")
	opening := strings.ReplaceAll(tmpl.Opening, "{{merchant_name}}", name)

	var b strings.Builder
	b.WriteString("你是 Rosie AI 电话前台，只处理客户主动打进来的电话，不主动营销外呼。\n")
	b.WriteString("当前商家：" + name + "\n")
	b.WriteString("行业模板：" + tmpl.Name + "\n")
	if profile.Address != "" {
		b.WriteString("地址：" + profile.Address + "\n")
	}
	if profile.BusinessHours != "" {
		b.WriteString("营业时间：" + profile.BusinessHours + "\n")
	}
	if len(profile.Services) > 0 {
		b.WriteString("服务项目：" + strings.Join(profile.Services, "、") + "\n")
	}
	if profile.AppointmentRules != "" {
		b.WriteString("预约规则：" + profile.AppointmentRules + "\n")
	}
	if len(profile.FAQItems) > 0 {
		b.WriteString("常见问答：\n")
		for _, item := range profile.FAQItems {
			if item.Question == "" && item.Answer == "" {
				continue
			}
			b.WriteString("- 问：" + item.Question + "；答：" + item.Answer + "\n")
		}
	}
	b.WriteString("开场话术：" + opening + "\n")
	b.WriteString("追问策略：\n")
	for _, item := range tmpl.QualificationQuestions {
		b.WriteString("- " + item + "\n")
	}
	b.WriteString("回答边界：\n")
	for _, item := range tmpl.FAQHints {
		b.WriteString("- " + item + "\n")
	}
	b.WriteString("摘要规则：\n")
	for _, item := range tmpl.SummaryRules {
		b.WriteString("- " + item + "\n")
	}
	return b.String()
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
