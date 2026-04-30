const config = require("./config")

function request(path, options = {}) {
  const method = options.method || "GET"
  const data = options.data || undefined
  const header = options.header || {}

  return new Promise((resolve, reject) => {
    wx.request({
      url: `${config.apiBaseURL}${path}`,
      method,
      data,
      header: {
        "Content-Type": "application/json",
        ...header
      },
      success(res) {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.data)
          return
        }
        const detail = res.data && res.data.detail ? res.data.detail : `HTTP ${res.statusCode}`
        const error = new Error(detail)
        error.statusCode = res.statusCode
        reject(error)
      },
      fail(err) {
        const error = new Error(err.errMsg || "request failed")
        error.statusCode = 0
        reject(error)
      }
    })
  })
}

function query(params) {
  const entries = Object.keys(params)
    .filter((key) => params[key] !== undefined && params[key] !== "")
    .map((key) => `${encodeURIComponent(key)}=${encodeURIComponent(params[key])}`)
  return entries.length ? `?${entries.join("&")}` : ""
}

module.exports = {
  config,
  request,
  query,
  getMerchantProfile(merchantId) {
    return request(`/merchant-profile${query({ merchant_id: merchantId })}`)
  },
  updateMerchantProfile(merchantId, data) {
    return request(`/merchant-profile${query({ merchant_id: merchantId })}`, {
      method: "PUT",
      data
    })
  },
  getIndustryTemplates() {
    return request("/industry-templates")
  },
  getValueMetrics(merchantId, period = "month") {
    return request(`/value-metrics${query({ merchant_id: merchantId, period })}`)
  },
  getServiceStatus(merchantId) {
    return request(`/service-status${query({ merchant_id: merchantId })}`)
  },
  activateTrialService(merchantId, data) {
    return request(`/service-status/activate-trial${query({ merchant_id: merchantId })}`, {
      method: "POST",
      data
    })
  },
  getPaymentOrders(merchantId) {
    return request(`/payment-orders${query({ merchant_id: merchantId })}`)
  },
  createPaymentOrder(merchantId, data) {
    return request(`/payment-orders${query({ merchant_id: merchantId })}`, {
      method: "POST",
      data
    })
  },
  getInbox(merchantId) {
    return request(`/inbox${query({ merchant_id: merchantId })}`)
  },
  getCallDetail(callSid) {
    return request(`/calls/${encodeURIComponent(callSid)}`)
  },
  createCallbackRequest(callSid, data) {
    return request(`/calls/${encodeURIComponent(callSid)}/callback-requests`, {
      method: "POST",
      data
    })
  },
  updateCallbackRequestStatus(callSid, callbackId, data) {
    return request(`/calls/${encodeURIComponent(callSid)}/callback-requests/${encodeURIComponent(callbackId)}`, {
      method: "PATCH",
      data
    })
  },
  updateInboxItemStatus(callSid, data) {
    return request(`/calls/${encodeURIComponent(callSid)}/inbox-item`, {
      method: "PATCH",
      data
    })
  },
  getDigests(merchantId) {
    return request(`/digests${query({ merchant_id: merchantId })}`)
  },
  previewDigest(merchantId) {
    return request(`/digests/preview${query({ merchant_id: merchantId })}`)
  },
  generateDigest(merchantId) {
    return request(`/digests/generate${query({ merchant_id: merchantId })}`, { method: "POST" })
  },
  getPreferences(merchantId) {
    return request(`/notification-preferences${query({ merchant_id: merchantId })}`)
  },
  updatePreferences(merchantId, data) {
    return request(`/notification-preferences${query({ merchant_id: merchantId })}`, {
      method: "PUT",
      data
    })
  },
  wechatLogin(code, merchantId) {
    return request("/auth/wechat-login", {
      method: "POST",
      data: {
        code,
        merchant_id: merchantId
      }
    })
  }
}
