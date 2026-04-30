const api = require("../../utils/api")

Page({
  data: {
    merchantId: api.config.defaultMerchantId,
    items: [],
    loading: false,
    isEmpty: false,
    errorMessage: "",
    priorityText: {
      urgent: "紧急",
      high: "较高",
      normal: "普通",
      low: "低"
    },
    statusText: {
      needs_review: "待处理",
      filtered: "已过滤",
      archived: "已归档",
      new: "新来电"
    }
  },

  onLoad() {
    const merchantId = wx.getStorageSync("merchant_id") || api.config.defaultMerchantId
    this.setData({ merchantId })
    this.loadInbox()
  },

  onPullDownRefresh() {
    this.loadInbox().finally(() => wx.stopPullDownRefresh())
  },

  onMerchantInput(event) {
    this.setData({ merchantId: event.detail.value })
  },

  saveMerchant() {
    wx.setStorageSync("merchant_id", this.data.merchantId)
    getApp().globalData.merchantId = this.data.merchantId
    this.loadInbox()
  },

  login() {
    wx.login({
      success: (res) => {
        if (!res.code) {
          this.toast("登录失败")
          return
        }
        api.wechatLogin(res.code, this.data.merchantId)
          .then(() => this.toast("已绑定微信"))
          .catch((err) => this.toast(err.message))
      },
      fail: () => this.toast("登录失败")
    })
  },

  loadInbox() {
    this.setData({ loading: true, errorMessage: "" })
    return api.getInbox(this.data.merchantId)
      .then((data) => {
        const items = (data.items || []).map((item) => ({
          ...item,
          priorityLabel: this.data.priorityText[item.priority] || item.priority || "普通",
          statusLabel: this.data.statusText[item.status] || item.status || "新来电",
          createdAtLabel: this.formatTime(item.created_at)
        }))
        this.setData({ items, isEmpty: items.length === 0, errorMessage: "" })
      })
      .catch((err) => {
        this.setData({ errorMessage: err.message, isEmpty: false })
        this.toast(err.message)
      })
      .finally(() => this.setData({ loading: false }))
  },

  openDetail(event) {
    const callSid = event.currentTarget.dataset.callSid
    if (!callSid) return
    wx.navigateTo({
      url: `/pages/call-detail/index?call_sid=${encodeURIComponent(callSid)}`
    })
  },

  toast(title) {
    wx.showToast({ title, icon: "none" })
  },

  formatTime(value) {
    if (!value) return ""
    return String(value).replace("T", " ").replace(/\.\d+Z$/, "").replace("Z", "")
  }
})
