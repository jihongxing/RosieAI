App({
  globalData: {
    merchantId: "demo-merchant"
  },

  onLaunch() {
    const merchantId = wx.getStorageSync("merchant_id")
    if (merchantId) {
      this.globalData.merchantId = merchantId
    }
  }
})
