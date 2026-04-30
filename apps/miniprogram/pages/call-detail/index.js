const api = require("../../utils/api")

Page({
  data: {
    callSid: "",
    detail: null,
    call: {},
    transcript: {},
    summary: {},
    inbox: {},
    callbackRequests: [],
    callbackTargetNumber: "",
    callbackTargetText: "未知",
    canCallback: false,
    callbackLoading: false,
    actionLoading: false,
    hasDetail: false,
    showMissing: false,
    errorMessage: "",
    loading: false
  },

  onLoad(options) {
    const callSid = decodeURIComponent(options.call_sid || "")
    this.setData({ callSid })
    this.loadDetail()
  },

  loadDetail() {
    if (!this.data.callSid) return
    this.setData({ loading: true, errorMessage: "", showMissing: false })
    api.getCallDetail(this.data.callSid)
      .then((detail) => {
        const summary = detail.summary || {}
        const inbox = detail.inbox || {}
        const call = detail.call || {}
        const callbackTargetNumber = this.callbackTarget(summary, call)
        const callbackRequests = this.formatCallbackRequests(detail.callback_requests || [])
        summary.intentText = this.intentText(summary.intent)
        summary.detailTitle = summary.intentText || "通话详情"
        summary.priorityText = this.priorityText(summary.priority)
        summary.summaryText = summary.summary || inbox.body || "暂无摘要"
        summary.appointmentTimeText = summary.appointment_time || "未提及"
        summary.customerNameText = summary.customer_name || "未提及"
        summary.customerPhoneText = callbackTargetNumber || "未知"
        summary.followupText = summary.need_human_followup ? "是" : "否"
        inbox.statusText = this.statusText(inbox.status) || "未进入收件箱"
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
          callbackRequests,
          callbackTargetNumber,
          callbackTargetText: callbackTargetNumber || "未知",
          canCallback: Boolean(callbackTargetNumber) && inbox.status !== "filtered" && summary.intent !== "spam",
          hasDetail: true,
          showMissing: false,
          errorMessage: ""
        })
      })
      .catch((err) => {
        const notFound = err.statusCode === 404
        this.setData({
          detail: null,
          hasDetail: false,
          showMissing: notFound,
          errorMessage: notFound ? "" : err.message
        })
        wx.showToast({ title: err.message, icon: "none" })
      })
      .finally(() => this.setData({ loading: false }))
  },

  onCallbackTap() {
    const phoneNumber = this.data.callbackTargetNumber
    if (!phoneNumber || this.data.callbackLoading) return

    this.setData({ callbackLoading: true })
    api.createCallbackRequest(this.data.callSid, {
      target_number: phoneNumber,
      requested_by: "miniprogram",
      reason: "merchant_manual_call_detail"
    })
      .then((res) => {
        const request = res.callback_request
        if (request) {
          const callbackRequests = this.formatCallbackRequests([request, ...this.data.callbackRequests])
          this.setData({ callbackRequests })
        }
        wx.makePhoneCall({
          phoneNumber,
          success: () => {
            if (request) {
              this.updateCallbackStatus(request.id, "dialed", "wx.makePhoneCall invoked by merchant")
            } else {
              this.setData({ callbackLoading: false })
            }
          },
          fail: (err) => {
            if (request) {
              const status = err.errMsg && err.errMsg.indexOf("cancel") >= 0 ? "canceled" : "failed"
              this.updateCallbackStatus(request.id, status, err.errMsg || "wx.makePhoneCall failed")
            } else {
              this.setData({ callbackLoading: false })
            }
          }
        })
      })
      .catch((err) => {
        wx.showToast({ title: err.message, icon: "none" })
        this.setData({ callbackLoading: false })
      })
  },

  callbackTarget(summary, call) {
    return (summary && summary.customer_phone) || (call && call.from_number) || ""
  },

  updateCallbackStatus(callbackId, status, auditNote) {
    api.updateCallbackRequestStatus(this.data.callSid, callbackId, {
      status,
      audit_note: auditNote
    })
      .then((res) => {
        this.replaceCallbackRequest(res.callback_request)
        if (status === "dialed") {
          wx.showToast({ title: "已记录回拨", icon: "success" })
        }
      })
      .catch((err) => wx.showToast({ title: err.message, icon: "none" }))
      .finally(() => this.setData({ callbackLoading: false }))
  },

  replaceCallbackRequest(nextRequest) {
    if (!nextRequest) return
    const items = this.data.callbackRequests.map((item) => (
      item.id === nextRequest.id ? nextRequest : item
    ))
    this.setData({ callbackRequests: this.formatCallbackRequests(items) })
  },

  updateInboxStatus(status) {
    if (this.data.actionLoading) return
    this.setData({ actionLoading: true })
    api.updateInboxItemStatus(this.data.callSid, { status })
      .then((res) => {
        const inbox = res.inbox || {}
        inbox.statusText = this.statusText(inbox.status) || "未进入收件箱"
        this.setData({ inbox })
        wx.showToast({ title: "已更新", icon: "success" })
      })
      .catch((err) => wx.showToast({ title: err.message, icon: "none" }))
      .finally(() => this.setData({ actionLoading: false }))
  },

  onMarkHandledTap() {
    this.updateInboxStatus("handled")
  },

  onArchiveTap() {
    this.updateInboxStatus("archived")
  },

  onRestoreTap() {
    this.updateInboxStatus("needs_review")
  },

  formatCallbackRequests(items) {
    return items.map((item) => ({
      ...item,
      statusLabel: this.callbackStatusText(item.status),
      createdAtLabel: item.createdAtLabel || this.formatTime(item.created_at)
    }))
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
      handled: "已处理",
      archived: "已归档",
      new: "新来电"
    }
    return map[status] || status
  },

  callbackStatusText(status) {
    const map = {
      requested: "已请求",
      dialed: "已拨出",
      failed: "拨号失败",
      canceled: "已取消"
    }
    return map[status] || status
  },

  formatTime(value) {
    if (!value) return ""
    return String(value).replace("T", " ").replace(/\.\d+Z$/, "").replace("Z", "")
  }
})
