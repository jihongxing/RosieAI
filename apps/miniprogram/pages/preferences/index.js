const api = require("../../utils/api")
const subscription = require("../../utils/subscription")

Page({
  data: {
    merchantId: api.config.defaultMerchantId,
    digestMode: "daily",
    digestTimesText: "20:00",
    urgentRealtimeEnabled: true,
    teamWecomEnabled: false,
    smsFallbackEnabled: false,
    dailyModeClass: "segment active",
    twiceDailyModeClass: "segment",
    manualModeClass: "segment",
    loading: false,
    saving: false,
    subscribing: false,
    errorMessage: "",
    subscribeStatusText: "未授权",
    subscribeStatusClass: "badge low"
  },

  onLoad() {
    this.setData({ merchantId: wx.getStorageSync("merchant_id") || api.config.defaultMerchantId })
    this.loadPreferences()
  },

  onShow() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    if (merchantId !== this.data.merchantId) {
      this.setData({ merchantId })
      this.loadPreferences()
    }
  },

  loadPreferences() {
    this.setData({ loading: true, errorMessage: "" })
    api.getPreferences(this.data.merchantId)
      .then((prefs) => {
        this.setData(this.withModeFlags({
          digestMode: prefs.digest_mode,
          digestTimesText: (prefs.digest_times || []).join(","),
          urgentRealtimeEnabled: prefs.urgent_realtime_enabled,
          teamWecomEnabled: prefs.team_wecom_enabled,
          smsFallbackEnabled: prefs.sms_fallback_enabled,
          errorMessage: ""
        }))
      })
      .catch((err) => {
        this.setData({ errorMessage: err.message })
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ loading: false }))
  },

  onModeTap(event) {
    const mode = event.currentTarget.dataset.mode
    const times = mode === "twice_daily" ? "12:00,20:00" : "20:00"
    this.setData(this.withModeFlags({
      digestMode: mode,
      digestTimesText: mode === "manual" ? "20:00" : times
    }))
  },

  onToggle(event) {
    const key = event.currentTarget.dataset.key
    this.setData({ [key]: event.detail.value })
  },

  onDigestTimesInput(event) {
    this.setData({ digestTimesText: event.detail.value })
  },

  save() {
    this.setData({ saving: true })
    const digestTimes = this.data.digestTimesText
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean)
    api.updatePreferences(this.data.merchantId, {
      digest_mode: this.data.digestMode,
      digest_times: digestTimes.length ? digestTimes : ["20:00"],
      realtime_enabled: false,
      urgent_realtime_enabled: this.data.urgentRealtimeEnabled,
      team_wecom_enabled: this.data.teamWecomEnabled,
      sms_fallback_enabled: this.data.smsFallbackEnabled
    })
      .then(() => wx.showToast({ title: "已保存", icon: "success" }))
      .catch((err) => {
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ saving: false }))
  },

  authorizeSubscribe() {
    this.setData({ subscribing: true })
    subscription.requestNotificationSubscription()
      .then((result) => {
        this.setData({
          subscribeStatusText: subscription.subscriptionStatusText(result),
          subscribeStatusClass: result.status === "accepted" ? "badge" : "badge low"
        })
        if (result.status === "accepted") {
          wx.showToast({ title: "已授权提醒", icon: "success" })
        } else if (result.status === "missing_config") {
          wx.showToast({ title: "未配置模板 ID", icon: "none" })
        } else {
          wx.showToast({ title: result.message || "未完成授权", icon: "none" })
        }
      })
      .finally(() => this.setData({ subscribing: false }))
  },

  withModeFlags(nextData) {
    const mode = nextData.digestMode || this.data.digestMode
    return {
      ...nextData,
      dailyModeClass: mode === "daily" ? "segment active" : "segment",
      twiceDailyModeClass: mode === "twice_daily" ? "segment active" : "segment",
      manualModeClass: mode === "manual" ? "segment active" : "segment"
    }
  }
})
