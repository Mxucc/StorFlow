package shop

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes mounts the shop API on the v4 router group (i.e. on
// /api/v4). It is the only function the host router needs to call.
//
// Layout:
//
//	/api/v4/shop/store/:slug            public storefront (read only)
//	/api/v4/shop/store/:slug/product/:id
//	/api/v4/shop/store/:slug/product/:id/preview
//	/api/v4/shop/epay/notify            payment callback (signature verified)
//	/api/v4/shop/...                    seller and buyer endpoints (login)
//	/api/v4/shop/admin/...              administrator endpoints
//
// The public storefront lives under the "store" prefix on purpose: gin cannot
// mix a path parameter with static siblings at the same level, and every other
// route of the module is static.
// The authentication middlewares are passed in by the host router instead of
// being imported: the middleware package already depends on the user service,
// which depends on this module, so importing it here would create a cycle.
func RegisterRoutes(v4 *gin.RouterGroup, loginRequired, adminRequired gin.HandlerFunc, settings func() int) {
	group := v4.Group("shop")

	// Public endpoints are rate limited in process: a fresh deployment is
	// protected without any reverse proxy configuration.
	publicLimiter := newRateLimiter()
	notifyLimiter := newRateLimiter()

	// Public: storefront and payment callbacks.
	storefront := group.Group("store", rateLimit(publicLimiter, settings))
	{
		// 公开店铺目录：首页 / sitemap 使用，只返回上架中的店铺。
		storefront.GET("", ListPublicShops)
		storefront.GET(":slug", GetStorefront)
		storefront.GET(":slug/product/:id", GetProduct)
		storefront.GET(":slug/product/:id/preview", PreviewProduct)
	}

	// Stored storefront images are served to anonymous visitors. They are kept
	// out of the public rate limiter on purpose: one storefront page loads many
	// of them and every response is cached for a year.
	group.GET("asset/:id", ServeShopAsset)

	// Embedded product page assets (tutorial images, demo videos) reference
	// the seller's drive files through a signed token. Like the stored
	// storefront images they stay outside the public limiter: one product page
	// loads many of them and the response is cacheable.
	group.GET("store/:slug/product/:id/asset/:token", ServeProductAsset)

	epayGroup := group.Group("epay", rateLimit(notifyLimiter, func() int {
		// The gateway may retry a notification several times: keep a generous
		// budget so a legitimate retry is never dropped.
		if rate := settings(); rate > 0 && rate < 120 {
			return 120
		}
		return settings()
	}))
	{
		// The gateway may call back with either verb depending on its
		// configuration; both are signature verified inside the service.
		epayGroup.GET("notify", EpayNotify)
		epayGroup.POST("notify", EpayNotify)
	}

	// Seller and buyer endpoints.
	auth := group.Group("")
	auth.Use(loginRequired)
	{
		// Shop profile and decoration.
		auth.POST("", CreateShop)
		auth.GET("mine", GetMyShop)
		auth.PATCH("", UpdateShop)
		auth.POST("status", SetShopStatus)

		// Storefront images: upload one, or snapshot a file of the drive.
		auth.GET("asset", ListShopAssets)
		auth.POST("asset", UploadShopAsset)
		auth.POST("asset/adopt", AdoptShopAsset)

		// Products.
		auth.GET("product", ListProducts)
		auth.POST("product", CreateProduct)
		auth.PATCH("product/:id", UpdateProduct)
		auth.POST("product/:id/status", SetProductStatus)
		// 商品介绍的素材：把卖家网盘里的一个文件签成可写进 Markdown 的直链。
		auth.POST("product/:id/asset", SignProductAsset)

		// Payment configuration: the platform aggregate channel, the merchant's
		// own epay gateway, and the platform deposit (保证金).
		auth.GET("payment", GetPaymentSetting)
		auth.PATCH("payment", UpdatePaymentSetting)
		auth.POST("deposit", CreateDepositOrder)

		// Orders and purchases.
		auth.POST("order", Checkout)
		auth.GET("order", ListMyOrders)
		auth.GET("order/:orderNo", GetOrder)
		auth.GET("sold", ListShopOrders)
		auth.GET("purchase", ListMyPurchases)
		auth.POST("purchase/:id/transfer", RetryDelivery)
		auth.POST("purchase/:id/refund", RequestRefund)

		// Refunds: the buyer asks, the seller (or an administrator) reviews.
		auth.GET("refund", ListMyRefunds)
		auth.GET("refund/received", ListShopRefunds)
		auth.POST("refund/:id", ReviewRefund)

		// Money.
		auth.GET("balance", GetBalance)
		auth.GET("withdraw", ListWithdrawals)
		auth.POST("withdraw", RequestWithdraw)

		// Discounts and referrals.
		auth.GET("coupon", ListCoupons)
		auth.POST("coupon", CreateCoupon)
		auth.PATCH("coupon/:id", UpdateCoupon)
		auth.GET("referral", ListReferrals)
		auth.GET("referral/link", GetReferralLink)

		// Administrator surface.
		admin := auth.Group("admin", adminRequired)
		{
			admin.GET("setting", AdminGetSettings)
			admin.PATCH("setting", AdminSaveSettings)
			admin.GET("withdraw", AdminListWithdrawals)
			admin.POST("withdraw/:id", AdminReviewWithdrawal)
			admin.GET("shop", AdminListShops)
			admin.GET("shop/:id/stats", AdminShopStats)
			admin.POST("order/:orderNo/refund", AdminRefundOrder)
			admin.POST("reconcile", AdminReconcileOrders)
			admin.GET("refund", AdminListRefunds)
			admin.POST("refund/:id", ReviewRefund)
		}
	}
}
