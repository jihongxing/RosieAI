const config = require("./config")

function configuredTemplateIds() {
  const ids = config.subscribeTemplateIds || {}
  return [ids.digest, ids.realtime]
    .map((item) => String(item || "").trim())
    .filter(Boolean)
}

function requestNotificationSubscription() {
  const tmplIds = configuredTemplateIds()
  if (!tmplIds.length) {
    return Promise.resolve({
      status: "missing_config",
      accepted: [],
      rejected: [],
      tmplIds
    })
  }
  if (!wx.requestSubscribeMessage) {
    return Promise.resolve({
      status: "unsupported",
      accepted: [],
      rejected: tmplIds,
      tmplIds
    })
  }

  return new Promise((resolve) => {
    wx.requestSubscribeMessage({
      tmplIds,
      success(res) {
        const accepted = tmplIds.filter((id) => res[id] === "accept")
        const rejected = tmplIds.filter((id) => res[id] && res[id] !== "accept")
        resolve({
          status: accepted.length ? "accepted" : "rejected",
          accepted,
          rejected,
          tmplIds
        })
      },
      fail(err) {
        resolve({
          status: "failed",
          accepted: [],
          rejected: tmplIds,
          tmplIds,
          message: err.errMsg || "requestSubscribeMessage failed"
        })
      }
    })
  })
}

function subscriptionStatusText(result) {
  if (!result) return "未授权"
  const total = result.tmplIds ? result.tmplIds.length : 0
  switch (result.status) {
    case "accepted":
      return `已授权 ${result.accepted.length}/${total}`
    case "missing_config":
      return "未配置模板"
    case "unsupported":
      return "当前微信版本不支持"
    case "failed":
      return "授权失败"
    default:
      return "未授权"
  }
}

module.exports = {
  configuredTemplateIds,
  requestNotificationSubscription,
  subscriptionStatusText
}
