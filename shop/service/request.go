package service

import (
	"github.com/Mxucc/StorFlow/shop/model"
)

// CreateShopRequest opens a shop.
type CreateShopRequest struct {
	Name        string       `json:"name"`
	Slug        string       `json:"slug"`
	Description string       `json:"description"`
	Avatar      string       `json:"avatar"`
	Cover       string       `json:"cover"`
	Theme       *model.Theme `json:"theme"`
}

// UpdateShopRequest patches a shop; nil fields stay untouched.
type UpdateShopRequest struct {
	Name         *string      `json:"name"`
	Slug         *string      `json:"slug"`
	Description  *string      `json:"description"`
	Avatar       *string      `json:"avatar"`
	Cover        *string      `json:"cover"`
	Theme        *model.Theme `json:"theme"`
	ReferrerRate *int         `json:"referrer_rate"`
}

// ShopStatusRequest toggles the storefront.
type ShopStatusRequest struct {
	Enabled bool `json:"enabled"`
}

// CreateProductRequest lists a folder of the owner's drive for sale.
type CreateProductRequest struct {
	// Uri points at a folder in the owner's own file system.
	Uri         string             `json:"uri" binding:"required"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Cover       string             `json:"cover"`
	Price       int64              `json:"price" binding:"required,min=1,max=100000000"`
	Status      string             `json:"status"`
	Props       model.ProductProps `json:"props"`
}

// UpdateProductRequest patches a product.
type UpdateProductRequest struct {
	Name        *string             `json:"name"`
	Description *string             `json:"description"`
	Cover       *string             `json:"cover"`
	Price       *int64              `json:"price"`
	Discount    *int                `json:"discount"`
	DiscountEnd *int64              `json:"discount_end"`
	Status      *string             `json:"status"`
	Props       *model.ProductProps `json:"props"`
}

// ProductStatusRequest changes the sale state of a product.
type ProductStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// ListRequest is the shared pagination payload.
type ListRequest struct {
	Limit  int `json:"limit" form:"limit"`
	Offset int `json:"offset" form:"offset"`
}

// CreateOrderRequest starts a checkout.
type CreateOrderRequest struct {
	// Kind is "product" or "share".
	Kind string `json:"kind" binding:"required,oneof=product share"`
	// ObjectID is the product id or the share hash id.
	ObjectID string `json:"object_id" binding:"required"`
	// Channel selects the payment channel.
	Channel string `json:"channel"`
	// Coupon is an optional discount code.
	Coupon string `json:"coupon"`
	// SharePassword is required when buying a password protected share.
	SharePassword string `json:"share_password"`
	// Ref carries the inviter hash id captured from a share link, used when the
	// buyer registered through a resource page.
	Ref string `json:"ref"`
}

// NotifyRequest is unused for GET callbacks but kept for POST ones.
type NotifyRequest struct {
	OrderNo string `json:"order_no"`
}

// WithdrawRequest asks for a payout.
type WithdrawRequest struct {
	Amount  int64                   `json:"amount" binding:"required,min=1"`
	Account model.WithdrawalAccount `json:"account" binding:"required"`
}

// ReviewWithdrawalRequest settles a payout.
type ReviewWithdrawalRequest struct {
	Action string `json:"action" binding:"required,oneof=approve reject paid"`
	Remark string `json:"remark"`
}

// CouponRequest creates or updates a coupon.
type CouponRequest struct {
	Code      string `json:"code"`
	Kind      string `json:"kind" binding:"required,oneof=percent fixed"`
	Value     int64  `json:"value" binding:"required,min=1"`
	MinAmount int64  `json:"min_amount"`
	ProductID string `json:"product_id"`
	MaxUses   int    `json:"max_uses"`
	PerUser   int    `json:"per_user"`
	Status    string `json:"status"`
	StartAt   int64  `json:"start_at"`
	EndAt     int64  `json:"end_at"`
}

// RequestRefundRequest asks for a refund of one purchase.
type RequestRefundRequest struct {
	Reason string `json:"reason"`
}

// ReviewRefundRequest drives a refund request through its state machine:
//
//	approve / reject  reviewer of the request (seller or administrator)
//	evidence          the seller documents a refund it carried out itself
//	confirm / return  administrator verification of that proof
type ReviewRefundRequest struct {
	Action   string `json:"action" binding:"required,oneof=approve reject evidence confirm return"`
	Remark   string `json:"remark"`
	Evidence string `json:"evidence"`
}

// RefundOrderRequest refunds a settled order.
type RefundOrderRequest struct {
	Reason string `json:"reason"`
}

// AdminSettingRequest patches the module configuration.
type AdminSettingRequest struct {
	Values map[string]string `json:"values" binding:"required"`
}

// PaymentSettingRequest patches the payment configuration of a shop. A nil field
// stays untouched; the masked key value keeps the stored secret.
type PaymentSettingRequest struct {
	// PayMode is "platform" (平台聚合支付) or "custom" (自配支付渠道).
	PayMode *string `json:"pay_mode"`
	// EpayAPIURL / EpayPID / EpayKey are the merchant gateway credentials of a
	// custom channel. An empty EpayKey clears the stored secret.
	EpayAPIURL *string `json:"epay_api_url"`
	EpayPID    *string `json:"epay_pid"`
	EpayKey    *string `json:"epay_key"`
	// EpayChannels replaces the per-channel gateway list. A nil field keeps the
	// stored list; sending an empty array clears it (all channels then share the
	// credentials above). key == "********" keeps the stored secret of that
	// channel.
	EpayChannels *[]PayChannelRequest `json:"epay_channels"`
}

// PayChannelRequest is one row of the per-channel gateway editor.
type PayChannelRequest struct {
	Channel string `json:"channel"`
	APIURL  string `json:"api_url"`
	PID     string `json:"pid"`
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}
