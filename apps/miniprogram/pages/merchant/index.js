const api = require("../../utils/api")

Page({
  data: {
    merchantId: api.config.defaultMerchantId,
    templates: [],
    templateNames: [],
    templateIndex: 0,
    templateName: "",
    merchantName: "",
    accessNumber: "",
    originalNumber: "",
    transferPhone: "",
    industry: "hair_salon",
    address: "",
    businessHours: "",
    servicesText: "",
    appointmentRules: "",
    faqText: "",
    promptPreview: "",
    loading: false,
    saving: false
  },

  onLoad() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    this.setData({ merchantId })
    this.loadAll()
  },

  onShow() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    if (merchantId !== this.data.merchantId) {
      this.setData({ merchantId })
      this.loadAll()
    }
  },

  onPullDownRefresh() {
    this.loadAll().finally(() => wx.stopPullDownRefresh())
  },

  loadAll() {
    this.setData({ loading: true })
    return Promise.all([
      api.getIndustryTemplates(),
      api.getMerchantProfile(this.data.merchantId)
    ])
      .then(([templates, profile]) => {
        const items = templates.items || []
        this.applyProfile(items, profile)
      })
      .catch((err) => wx.showToast({ title: err.message, icon: "none" }))
      .finally(() => this.setData({ loading: false }))
  },

  applyProfile(templates, payload) {
    const merchant = payload.merchant || {}
    const profile = payload.profile || {}
    const templateIndex = Math.max(0, templates.findIndex((item) => item.key === profile.industry))
    this.setData({
      templates,
      templateNames: templates.map((item) => item.name),
      templateIndex,
      templateName: templates[templateIndex] ? templates[templateIndex].name : "",
      merchantName: merchant.merchant_name || "",
      accessNumber: merchant.access_number || "",
      originalNumber: merchant.original_number || "",
      transferPhone: merchant.transfer_phone || "",
      industry: profile.industry || "hair_salon",
      address: profile.address || "",
      businessHours: profile.business_hours || "",
      servicesText: (profile.services || []).join("\n"),
      appointmentRules: profile.appointment_rules || "",
      faqText: this.formatFAQ(profile.faq_items || []),
      promptPreview: payload.system_prompt || ""
    })
  },

  onTemplateChange(event) {
    const index = Number(event.detail.value)
    const template = this.data.templates[index]
    if (!template) return
    this.setData({
      templateIndex: index,
      templateName: template.name,
      industry: template.key
    })
  },

  onInput(event) {
    const key = event.currentTarget.dataset.key
    this.setData({ [key]: event.detail.value })
  },

  save() {
    this.setData({ saving: true })
    api.updateMerchantProfile(this.data.merchantId, {
      merchant_name: this.data.merchantName,
      access_number: this.data.accessNumber,
      original_number: this.data.originalNumber,
      transfer_phone: this.data.transferPhone,
      industry: this.data.industry,
      address: this.data.address,
      business_hours: this.data.businessHours,
      services: this.parseList(this.data.servicesText),
      appointment_rules: this.data.appointmentRules,
      faq_items: this.parseFAQ(this.data.faqText)
    })
      .then((payload) => {
        wx.showToast({ title: "已保存", icon: "success" })
        this.applyProfile(this.data.templates, payload)
      })
      .catch((err) => wx.showToast({ title: err.message, icon: "none" }))
      .finally(() => this.setData({ saving: false }))
  },

  parseList(value) {
    return String(value || "")
      .split(/[\n,，]/)
      .map((item) => item.trim())
      .filter(Boolean)
  },

  parseFAQ(value) {
    return String(value || "")
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => {
        const index = line.indexOf("|")
        if (index < 0) {
          return { question: line, answer: "请人工确认后回复。" }
        }
        return {
          question: line.slice(0, index).trim(),
          answer: line.slice(index + 1).trim()
        }
      })
      .filter((item) => item.question && item.answer)
  },

  formatFAQ(items) {
    return items
      .map((item) => `${item.question}|${item.answer}`)
      .join("\n")
  }
})
