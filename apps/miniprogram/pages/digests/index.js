const api = require("../../utils/api")

Page({
  data: {
    merchantId: api.config.defaultMerchantId,
    preview: {
      total: 0,
      followup_count: 0,
      spam_count: 0,
      digest_text: "暂无预览"
    },
    items: [],
    isEmpty: false,
    loading: false,
    errorMessage: ""
  },

  onLoad() {
    this.setData({ merchantId: wx.getStorageSync("merchant_id") || api.config.defaultMerchantId })
    this.loadAll()
  },

  onShow() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    if (merchantId !== this.data.merchantId) {
      this.setData({ merchantId })
      this.loadAll()
    }
  },

  loadAll() {
    this.setData({ loading: true, errorMessage: "" })
    return Promise.all([
      api.previewDigest(this.data.merchantId),
      api.getDigests(this.data.merchantId)
    ])
      .then(([preview, digests]) => {
        const items = (digests.items || []).map((item) => ({
          ...item,
          digestTypeLabel: this.digestTypeText(item.digest_type),
          createdAtLabel: this.formatTime(item.created_at)
        }))
        this.setData({
          preview: {
            ...preview,
            digestText: preview.digest_text || "暂无预览"
          },
          items,
          isEmpty: items.length === 0,
          errorMessage: ""
        })
      })
      .catch((err) => {
        this.setData({ errorMessage: err.message, isEmpty: false })
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ loading: false }))
  },

  generateDigest() {
    api.generateDigest(this.data.merchantId)
      .then(() => {
        wx.showToast({ title: "已生成", icon: "success" })
        this.loadAll()
      })
      .catch((err) => {
        this.setData({ errorMessage: err.message })
        wx.showToast({ title: err.message, icon: "none" })
      })
  },

  digestTypeText(value) {
    const map = {
      daily: "每日",
      twice_daily: "定时",
      hourly: "每小时",
      manual: "手动"
    }
    return map[value] || value || "来电"
  },

  formatTime(value) {
    if (!value) return ""
    return String(value).replace("T", " ").replace(/\.\d+Z$/, "").replace("Z", "")
  }
})
