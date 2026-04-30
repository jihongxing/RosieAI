const api = require("../../utils/api")

Page({
  data: {
    merchantId: api.config.defaultMerchantId,
    period: "month",
    monthClass: "segment active",
    sevenDayClass: "segment",
    thirtyDayClass: "segment",
    loading: false,
    errorMessage: "",
    metrics: {
      total_calls: 0,
      effective_calls: 0,
      appointment_count: 0,
      spam_count: 0,
      followup_count: 0,
      urgent_count: 0,
      handled_count: 0,
      callback_requested_count: 0,
      callback_dialed_count: 0,
      estimated_saved_minutes: 0
    },
    savedTimeText: "0 分钟",
    appointmentRateText: "0%",
    callbackRateText: "0%",
    periodText: "本月"
  },

  onLoad() {
    this.setData({ merchantId: wx.getStorageSync("merchant_id") || api.config.defaultMerchantId })
    this.loadMetrics()
  },

  onShow() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    if (merchantId !== this.data.merchantId) {
      this.setData({ merchantId })
      this.loadMetrics()
    }
  },

  onPullDownRefresh() {
    this.loadMetrics().finally(() => wx.stopPullDownRefresh())
  },

  onPeriodTap(event) {
    const period = event.currentTarget.dataset.period
    this.setData(this.withPeriodFlags({ period }))
    this.loadMetrics()
  },

  openOnboarding() {
    wx.navigateTo({ url: "/pages/onboarding/index" })
  },

  loadMetrics() {
    this.setData({ loading: true, errorMessage: "" })
    return api.getValueMetrics(this.data.merchantId, this.data.period)
      .then((metrics) => {
        this.setData({
          metrics,
          savedTimeText: this.savedTimeText(metrics.estimated_saved_minutes),
          appointmentRateText: this.percentText(metrics.appointment_rate),
          callbackRateText: this.percentText(metrics.callback_completion_rate),
          periodText: this.periodText(metrics.period || this.data.period),
          errorMessage: ""
        })
      })
      .catch((err) => {
        this.setData({ errorMessage: err.message })
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ loading: false }))
  },

  withPeriodFlags(nextData) {
    const period = nextData.period || this.data.period
    return {
      ...nextData,
      monthClass: period === "month" ? "segment active" : "segment",
      sevenDayClass: period === "7d" ? "segment active" : "segment",
      thirtyDayClass: period === "30d" ? "segment active" : "segment"
    }
  },

  periodText(period) {
    const map = {
      month: "本月",
      "7d": "近 7 天",
      "30d": "近 30 天",
      custom: "自定义"
    }
    return map[period] || "本月"
  },

  savedTimeText(minutes) {
    const value = Number(minutes || 0)
    if (value < 60) {
      return `${value} 分钟`
    }
    const hours = Math.floor(value / 60)
    const rest = value % 60
    return rest ? `${hours} 小时 ${rest} 分钟` : `${hours} 小时`
  },

  percentText(value) {
    const rate = Number(value || 0)
    return `${Math.round(rate * 100)}%`
  }
})
