const api = require("../../utils/api")

Page({
  data: {
    callSid: "",
    detail: null,
    call: {},
    transcript: {},
    summary: {},
    inbox: {},
    hasDetail: false,
    showMissing: false,
    loading: false
  },

  onLoad(options) {
    const callSid = decodeURIComponent(options.call_sid || "")
    this.setData({ callSid })
    this.loadDetail()
  },

  loadDetail() {
    if (!this.data.callSid) return
    this.setData({ loading: true })
    api.getCallDetail(this.data.callSid)
      .then((detail) => {
        const summary = detail.summary || {}
        const inbox = detail.inbox || {}
        summary.intentText = this.intentText(summary.intent)
        summary.detailTitle = summary.intentText || "通话详情"
        summary.priorityText = this.priorityText(summary.priority)
        summary.summaryText = summary.summary || inbox.body || "暂无摘要"
        summary.appointmentTimeText = summary.appointment_time || "未提及"
        summary.customerNameText = summary.customer_name || "未提及"
        summary.customerPhoneText = summary.customer_phone || (detail.call && detail.call.from_number) || "未知"
        summary.followupText = summary.need_human_followup ? "是" : "否"
        inbox.statusText = this.statusText(inbox.status) || "未进入收件箱"
        const call = detail.call || {}
        this.setData({
          detail,
          call: {
            ...call,
            fromNumberText: call.from_number || "未知号码",
            toNumberText: call.to_number || "未知接入号",
            createdAtLabel: this.formatTime(call.created_at)
          },
          transcript: {
            ...(detail.transcript || {}),
            transcriptText: (detail.transcript && detail.transcript.transcript) || "暂无转写"
          },
          summary,
          inbox,
          hasDetail: true,
          showMissing: false
        })
      })
      .catch((err) => {
        this.setData({ detail: null, hasDetail: false, showMissing: true })
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ loading: false }))
  },

  intentText(intent) {
    const map = {
      appointment: "预约意向",
      urgent: "紧急事项",
      spam: "疑似骚扰",
      inquiry: "有效咨询"
    }
    return map[intent] || "通话详情"
  },

  priorityText(priority) {
    const map = {
      urgent: "紧急",
      high: "较高",
      normal: "普通",
      low: "低"
    }
    return map[priority] || "普通"
  },

  statusText(status) {
    const map = {
      needs_review: "待处理",
      filtered: "已过滤",
      archived: "已归档",
      new: "新来电"
    }
    return map[status] || status
  },

  formatTime(value) {
    if (!value) return ""
    return String(value).replace("T", " ").replace(/\.\d+Z$/, "").replace("Z", "")
  }
})
