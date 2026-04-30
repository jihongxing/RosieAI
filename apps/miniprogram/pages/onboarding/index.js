const api = require("../../utils/api")

Page({
  data: {
    merchantId: api.config.defaultMerchantId,
    loading: false,
    activating: false,
    paying: false,
    errorMessage: "",
    status: null,
    subscription: {},
    plan: {},
    addOns: [],
    metrics: {},
    onboardingSteps: [],
    forwardingInstructions: [],
    paymentOrders: [],
    statusText: "未开通",
    trialText: "",
    valueText: "0 分钟",
    canActivateTrial: false
  },

  onLoad() {
    this.setData({ merchantId: wx.getStorageSync("merchant_id") || api.config.defaultMerchantId })
    this.loadStatus()
  },

  onShow() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    if (merchantId !== this.data.merchantId) {
      this.setData({ merchantId })
      this.loadStatus()
    }
  },

  onPullDownRefresh() {
    this.loadStatus().finally(() => wx.stopPullDownRefresh())
  },

  loadStatus() {
    this.setData({ loading: true, errorMessage: "" })
    return Promise.all([
      api.getServiceStatus(this.data.merchantId),
      api.getPaymentOrders(this.data.merchantId)
    ])
      .then(([status, orders]) => {
        this.applyStatus(status)
        this.setData({ paymentOrders: this.formatOrders(orders.items || []) })
      })
      .catch((err) => {
        this.setData({ errorMessage: err.message })
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ loading: false }))
  },

  activateTrial() {
    if (this.data.activating) return
    this.setData({ activating: true })
    api.activateTrialService(this.data.merchantId, { plan_code: "pilot_basic" })
      .then((status) => {
        this.applyStatus(status)
        wx.showToast({ title: "试用已开通", icon: "success" })
      })
      .catch((err) => wx.showToast({ title: err.message, icon: "none" }))
      .finally(() => this.setData({ activating: false }))
  },

  createRenewalOrder() {
    if (this.data.paying) return
    this.setData({ paying: true })
    api.createPaymentOrder(this.data.merchantId, {
      order_type: "renewal",
      plan_code: "pilot_basic"
    })
      .then((res) => {
        const order = res.order
        if (res.payment && res.payment.configured && res.payment.request_params) {
          wx.requestPayment({
            ...res.payment.request_params,
            success: () => this.loadStatus(),
            fail: (err) => wx.showToast({ title: err.errMsg || "支付未完成", icon: "none" })
          })
        } else {
          const paymentOrders = this.formatOrders([order, ...this.data.paymentOrders])
          this.setData({ paymentOrders })
          wx.showToast({ title: "已生成待支付订单", icon: "none" })
        }
      })
      .catch((err) => wx.showToast({ title: err.message, icon: "none" }))
      .finally(() => this.setData({ paying: false }))
  },

  applyStatus(status) {
    const metrics = status.metrics || {}
    const trialDays = Number(status.trial_days_remaining || 0)
    this.setData({
      status,
      subscription: status.subscription || {},
      plan: status.plan || {},
      addOns: status.add_ons || [],
      metrics,
      onboardingSteps: status.onboarding_steps || [],
      forwardingInstructions: status.call_forwarding_instructions || [],
      statusText: status.status_text || "未开通",
      trialText: trialDays ? `剩余 ${trialDays} 天` : "",
      valueText: this.savedTimeText(metrics.estimated_saved_minutes),
      canActivateTrial: (status.subscription || {}).status === "not_started",
      errorMessage: ""
    })
  },

  formatOrders(items) {
    return items.map((item) => ({
      ...item,
      statusText: this.orderStatusText(item.status),
      amountText: this.priceText(item.amount_cents),
      createdAtText: this.formatTime(item.created_at)
    }))
  },

  orderStatusText(status) {
    const map = {
      pending: "待支付",
      paid: "已支付",
      closed: "已关闭",
      failed: "支付失败"
    }
    return map[status] || status
  },

  priceText(cents) {
    return `${(Number(cents || 0) / 100).toFixed(2)} 元`
  },

  formatTime(value) {
    if (!value) return ""
    return String(value).replace("T", " ").replace(/\.\d+Z$/, "").replace("Z", "")
  },

  savedTimeText(minutes) {
    const value = Number(minutes || 0)
    if (value < 60) {
      return `${value} 分钟`
    }
    const hours = Math.floor(value / 60)
    const rest = value % 60
    return rest ? `${hours} 小时 ${rest} 分钟` : `${hours} 小时`
  }
})
