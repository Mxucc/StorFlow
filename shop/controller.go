package shop

import (
	"errors"
	"net/http"

	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/gateway/epay"
	"github.com/Mxucc/StorFlow/shop/service"
	"github.com/gin-gonic/gin"
)

// svc resolves the module service of the running instance.
func svc(c *gin.Context) (*service.Service, bool) {
	s := Service()
	if s == nil {
		c.JSON(http.StatusOK, serializer.Err(c, serializer.NewError(serializer.CodeInternalSetting,
			"Shop module is not initialized", nil)))
		return nil, false
	}
	return s, true
}

// paramErr renders a malformed request.
func paramErr(c *gin.Context, msg string, err error) {
	c.JSON(http.StatusOK, serializer.ParamErr(c, msg, err))
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		c.JSON(http.StatusOK, serializer.Err(c, err))
		return
	}
	c.JSON(http.StatusOK, serializer.Response{Data: data})
}

// ─── storefront (public) ────────────────────────────────────────────────────

// ListPublicShops lists active shops for the public site (directory / sitemap).
func ListPublicShops(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.PublicShops(c, req.Limit)
	respond(c, data, err)
}

// GetStorefront renders the public storefront of a shop.

func GetStorefront(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.Storefront(c, c.Param("slug"), req.Limit, req.Offset)
	respond(c, data, err)
}

// GetProduct returns the public detail page of a product.
func GetProduct(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.ProductDetailFor(c, c.Param("slug"), c.Param("id"))
	respond(c, data, err)
}

// PreviewProduct lists the folder of a product without exposing any download.
func PreviewProduct(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.Preview(c, c.Param("slug"), c.Param("id"), c.Query("path"))
	respond(c, data, err)
}

// ─── seller ─────────────────────────────────────────────────────────────────

// CreateShop opens a shop for the requester.
func CreateShop(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.CreateShopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid shop", err)
		return
	}
	data, err := s.CreateShop(c, &req)
	respond(c, data, err)
}

// GetMyShop returns the requester's shop.
func GetMyShop(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.GetMyShop(c)
	respond(c, data, err)
}

// UpdateShop patches the requester's shop.
func UpdateShop(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.UpdateShopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid shop", err)
		return
	}
	data, err := s.UpdateShop(c, &req)
	respond(c, data, err)
}

// SetShopStatus toggles the storefront.
func SetShopStatus(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ShopStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid request", err)
		return
	}
	data, err := s.SetShopStatus(c, req.Enabled)
	respond(c, data, err)
}

// ListProducts lists the products of the requester's shop.
func ListProducts(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.ListMyProducts(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// CreateProduct lists a folder for sale.
func CreateProduct(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid product", err)
		return
	}
	data, err := s.CreateProduct(c, &req)
	respond(c, data, err)
}

// UpdateProduct patches a product.
func UpdateProduct(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid product", err)
		return
	}
	data, err := s.UpdateProduct(c, c.Param("id"), &req)
	respond(c, data, err)
}

// SignProductAsset turns one drive file of the seller into a stable URL that
// can be embedded in the product page (tutorial images, demo videos).
func SignProductAsset(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ProductAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid asset", err)
		return
	}
	data, err := s.SignProductAsset(c, c.Param("id"), &req)
	respond(c, data, err)
}

// ServeProductAsset streams an embedded product asset to a storefront visitor.
func ServeProductAsset(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	if err := s.ServeProductAsset(c, c.Param("slug"), c.Param("id"), c.Param("token")); err != nil {
		// The URL is consumed by an <img> or <video> tag: answer with a real
		// HTTP status so the browser (and any proxy cache) treats a missing or
		// forbidden asset as such instead of caching an empty body.
		status := http.StatusNotFound
		var appErr serializer.AppError
		if errors.As(err, &appErr) && appErr.Code == serializer.CodeNoPermissionErr {
			status = http.StatusForbidden
		}
		c.JSON(status, serializer.Err(c, err))
	}
}

// SetProductStatus changes the sale state of a product.
func SetProductStatus(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ProductStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid status", err)
		return
	}
	data, err := s.UpdateProduct(c, c.Param("id"), &service.UpdateProductRequest{Status: &req.Status})
	respond(c, data, err)
}

// GetBalance returns the wallet of the requester's shop.
func GetBalance(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.Balance(c)
	respond(c, data, err)
}

// ListShopOrders lists the orders received by the requester's shop.
func ListShopOrders(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.ShopOrders(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// ListCoupons lists the coupons of the requester's shop.
func ListCoupons(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.ListCoupons(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// CreateCoupon adds a coupon.
func CreateCoupon(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.CouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid coupon", err)
		return
	}
	data, err := s.CreateCoupon(c, &req)
	respond(c, data, err)
}

// UpdateCoupon patches a coupon.
func UpdateCoupon(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.CouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid coupon", err)
		return
	}
	data, err := s.UpdateCoupon(c, c.Param("id"), &req)
	respond(c, data, err)
}

// ListReferrals lists the invitees of the requester's shop.
func ListReferrals(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.Referrals(c)
	respond(c, data, err)
}

// GetReferralLink returns the referral storefront link of the seller.
func GetReferralLink(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.ReferralLink(c)
	respond(c, data, err)
}

// ListWithdrawals lists the payout requests of the requester's shop.
func ListWithdrawals(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.MyWithdrawals(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// RequestWithdraw creates a payout request.
func RequestWithdraw(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid withdrawal", err)
		return
	}
	data, err := s.RequestWithdraw(c, &req)
	respond(c, data, err)
}

// ─── buyer ──────────────────────────────────────────────────────────────────

// Checkout creates an order and returns the cashier URL.
func Checkout(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid order", err)
		return
	}
	data, err := s.Checkout(c, &req)
	respond(c, data, err)
}

// GetOrder returns the state of an order.
func GetOrder(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.Order(c, c.Param("orderNo"))
	respond(c, data, err)
}

// ListMyOrders lists the orders of the requester.
func ListMyOrders(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.MyOrders(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// ListMyPurchases lists the purchases of the requester.
func ListMyPurchases(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.MyPurchases(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// RetryDelivery re-runs the delivery of a purchase.
func RetryDelivery(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.RetryDelivery(c, c.Param("id"))
	respond(c, data, err)
}

// ─── payment callbacks ──────────────────────────────────────────────────────

// EpayNotify handles the asynchronous epay notification.
//
// The gateway expects the literal body "success" for every accepted
// notification, including the ones that are ignored on purpose, otherwise it
// keeps retrying.
func EpayNotify(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}

	params := epay.CollectParams(c.Request.URL.Query(), nil)
	if c.Request.Method == http.MethodPost {
		_ = c.Request.ParseForm()
		params = epay.CollectParams(c.Request.URL.Query(), c.Request.PostForm)
	}

	if err := s.Notify(c, params); err != nil {
		s.Logger().Warning("Rejected epay notification: %s", err)
		// Unknown orders are acknowledged to stop retries; anything else is
		// reported as a failure so the gateway tries again.
		c.String(http.StatusOK, "fail")
		return
	}

	c.String(http.StatusOK, "success")
}

// ─── admin ──────────────────────────────────────────────────────────────────

// AdminGetSettings returns the module configuration.
func AdminGetSettings(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	respond(c, s.AdminSettings(c), nil)
}

// AdminSaveSettings patches the module configuration.
func AdminSaveSettings(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.AdminSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid settings", err)
		return
	}
	// Save first: the response must reflect the stored configuration, not the
	// values the request arrived with.
	if err := s.AdminSaveSettings(c, &req); err != nil {
		respond(c, nil, err)
		return
	}
	respond(c, s.AdminSettings(c), nil)
}

// AdminListWithdrawals lists payout requests of every shop.
func AdminListWithdrawals(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.AdminListWithdrawals(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// AdminReviewWithdrawal settles a payout request.
func AdminReviewWithdrawal(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ReviewWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid review", err)
		return
	}
	data, err := s.ReviewWithdraw(c, c.Param("id"), &req)
	respond(c, data, err)
}

// AdminShopStats aggregates the activity of one shop.
func AdminShopStats(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.AdminShopStats(c, c.Param("id"))
	respond(c, data, err)
}

// AdminRefundOrder refunds a settled order and revokes the buyer's entitlement.
func AdminRefundOrder(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.RefundOrderRequest
	_ = c.ShouldBindJSON(&req)
	data, err := s.AdminRefundOrder(c, c.Param("orderNo"), req.Reason)
	respond(c, data, err)
}

// AdminListShops lists every shop with its wallet summary.
func AdminListShops(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.AdminListShops(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// AdminReconcileOrders asks the payment gateway about pending orders and
// settles the ones that were paid but whose notification never arrived.
func AdminReconcileOrders(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	// An explicit administrator request has no grace period: settle whatever the
	// gateway reports as paid right now.
	settled, err := s.ReconcilePendingOrders(c, 50, 0)
	respond(c, gin.H{"settled": settled}, err)
}

// RequestRefund lets a buyer ask for a refund of one purchase.
func RequestRefund(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.RequestRefundRequest
	_ = c.ShouldBindJSON(&req)
	data, err := s.RequestRefund(c, c.Param("id"), req.Reason)
	respond(c, data, err)
}

// ListMyRefunds lists the refund requests of the requester.
func ListMyRefunds(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.MyRefunds(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// ListShopRefunds lists the refund requests received by the requester's shop.
func ListShopRefunds(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.ShopRefunds(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// ReviewRefund approves or rejects a refund request (seller or administrator).
func ReviewRefund(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ReviewRefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid review", err)
		return
	}
	data, err := s.ReviewRefund(c, c.Param("id"), &req)
	respond(c, data, err)
}

// AdminListRefunds lists refund requests of every shop.
func AdminListRefunds(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.ListRequest
	_ = c.ShouldBindQuery(&req)
	data, err := s.AdminRefunds(c, req.Limit, req.Offset)
	respond(c, data, err)
}

// ─── assets (storefront images) ─────────────────────────────────────────────

// UploadShopAsset stores an uploaded image (shop avatar/cover, product cover).
func UploadShopAsset(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.UploadAsset(c)
	respond(c, data, err)
}

// AdoptShopAsset snapshots an image of the seller's drive as a stored asset.
func AdoptShopAsset(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.AdoptAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid image", err)
		return
	}
	data, err := s.AdoptAsset(c, &req)
	respond(c, data, err)
}

// ListShopAssets lists the images the requester has stored before.
func ListShopAssets(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.ListAssets(c)
	respond(c, data, err)
}

// ServeShopAsset streams a stored image to anyone: the identifier is random and
// an asset only exists after a seller published it.
func ServeShopAsset(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	if err := s.ServeAsset(c, c.Param("id")); err != nil {
		// An image URL is consumed by an <img> tag: answer with a real HTTP
		// status so the browser (and any proxy cache) treats it as missing.
		c.JSON(http.StatusNotFound, serializer.Err(c, err))
	}
}

// ─── shop payment configuration ─────────────────────────────────────────────

// GetPaymentSetting returns the payment configuration of the requester's shop.
func GetPaymentSetting(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.PaymentSetting(c)
	respond(c, data, err)
}

// UpdatePaymentSetting patches the payment configuration of the requester's shop:
// the platform aggregate channel or the merchant's own epay gateway.
func UpdatePaymentSetting(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	var req service.PaymentSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paramErr(c, "Invalid payment setting", err)
		return
	}
	data, err := s.UpdatePaymentSetting(c, &req)
	respond(c, data, err)
}

// CreateDepositOrder starts the payment of the outstanding shop deposit
// (保证金) through the platform gateway.
func CreateDepositOrder(c *gin.Context) {
	s, ok := svc(c)
	if !ok {
		return
	}
	data, err := s.CreateDepositOrder(c)
	respond(c, data, err)
}
