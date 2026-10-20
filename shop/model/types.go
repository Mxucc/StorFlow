// Package model contains the plain data types of the cloud drive shop module.
//
// The shop module keeps its own persistence layer (see package store) and never
// touches the ent schema of the host application. All types in this package are
// free of database or HTTP concerns so they can be shared by the store, the
// service layer and the router.
package model

// ShopStatus is the lifecycle state of a shop.
type ShopStatus string

const (
	// ShopStatusActive means the shop is publicly reachable.
	ShopStatusActive ShopStatus = "active"
	// ShopStatusDisabled means the shop owner switched the shop off. Public
	// storefront endpoints return 404 while disabled, purchased entitlements
	// stay valid.
	ShopStatusDisabled ShopStatus = "disabled"
)

// Valid reports whether s is a known shop status.
func (s ShopStatus) Valid() bool {
	return s == ShopStatusActive || s == ShopStatusDisabled
}

// ProductStatus is the lifecycle state of a product (a priced folder).
type ProductStatus string

const (
	// ProductStatusDraft is only visible to the owner.
	ProductStatusDraft ProductStatus = "draft"
	// ProductStatusOnSale is purchasable.
	ProductStatusOnSale ProductStatus = "onsale"
	// ProductStatusOffSale is hidden from the storefront but keeps every
	// entitlement, the internal share and the symbolic folders of buyers alive.
	ProductStatusOffSale ProductStatus = "offsale"
	// ProductStatusRemoved is a terminal state, nothing is deleted physically so
	// that order history and entitlements stay readable.
	ProductStatusRemoved ProductStatus = "removed"
)

// Valid reports whether s is a known product status.
func (s ProductStatus) Valid() bool {
	switch s {
	case ProductStatusDraft, ProductStatusOnSale, ProductStatusOffSale, ProductStatusRemoved:
		return true
	}
	return false
}

// Purchasable reports whether a product in this state accepts new orders.
func (s ProductStatus) Purchasable() bool { return s == ProductStatusOnSale }

// OrderKind distinguishes the sellable object types handled by the shop.
type OrderKind string

const (
	// OrderKindProduct is an order for a shop product (a folder).
	OrderKindProduct OrderKind = "product"
	// OrderKindShare is an order for a paid share link (Pro feature: 收费分享).
	OrderKindShare OrderKind = "share"
	// OrderKindDeposit is the seller paying the shop deposit (保证金) to the
	// platform. Nothing is delivered and no wallet is credited: the order only
	// records the payment that unlocks withdrawals.
	OrderKindDeposit OrderKind = "deposit"
)

// Valid reports whether k is a known order kind.
func (k OrderKind) Valid() bool {
	return k == OrderKindProduct || k == OrderKindShare || k == OrderKindDeposit
}

// OrderStatus is the payment state machine of an order.
//
//	pending --(epay notify)--> paid
//	pending --(expire sweep)---> closed
//	paid -----------------------> refunded (manual)
type OrderStatus string

const (
	OrderStatusPending  OrderStatus = "pending"
	OrderStatusPaid     OrderStatus = "paid"
	OrderStatusClosed   OrderStatus = "closed"
	OrderStatusRefunded OrderStatus = "refunded"
)

// Valid reports whether s is a known order status.
func (s OrderStatus) Valid() bool {
	switch s {
	case OrderStatusPending, OrderStatusPaid, OrderStatusClosed, OrderStatusRefunded:
		return true
	}
	return false
}

// SettleState values of a paid order: the platform holds the income of a
// platform channel order until the unfreeze window of the module has passed.
const (
	// SettleStateHeld means the income is held back and not withdrawable yet.
	SettleStateHeld = "held"
	// SettleStateReleased means the held income reached the available balance.
	SettleStateReleased = "released"
)

// PayChannel is the payment channel passed to the epay gateway.
type PayChannel string

const (
	ChannelAlipay PayChannel = "alipay"
	ChannelWxpay  PayChannel = "wxpay"
	ChannelQqpay  PayChannel = "qqpay"
)

// Valid reports whether c is a channel supported by the epay v1 protocol.
func (c PayChannel) Valid() bool {
	return c == ChannelAlipay || c == ChannelWxpay || c == ChannelQqpay
}

// PayMode selects where the buyers of a shop actually pay.
type PayMode string

const (
	// PayModePlatform means the platform aggregate gateway (平台聚合支付) is
	// used: the money is collected by the platform, held for the configured
	// unfreeze window and paid out through a withdrawal request.
	PayModePlatform PayMode = "platform"
	// PayModeCustom means the merchant configured their own epay gateway
	// (自配支付渠道): buyers pay the merchant directly and the platform only
	// records the order and delivers the content. No balance is credited, no
	// commission is charged and a refund is carried out by the merchant.
	PayModeCustom PayMode = "custom"
)

// Valid reports whether m is a known payment mode.
func (m PayMode) Valid() bool { return m == PayModePlatform || m == PayModeCustom }

// OrDefault returns m, or the platform mode when m is unset or unknown.
func (m PayMode) OrDefault() PayMode {
	if m.Valid() {
		return m
	}
	return PayModePlatform
}

// EntitlementStatus tracks whether the buyer already received the content.
type EntitlementStatus string

const (
	// EntitlementStatusPending means the payment is settled but the symbolic
	// folder could not be created yet, the buyer (or a retry) can fix it.
	EntitlementStatusPending EntitlementStatus = "pending"
	// EntitlementStatusGranted means the content was delivered.
	EntitlementStatusGranted EntitlementStatus = "granted"
	// EntitlementStatusRevoked is set when an order is refunded.
	EntitlementStatusRevoked EntitlementStatus = "revoked"
)

// WithdrawalStatus is the review state machine of a withdrawal request.
type WithdrawalStatus string

const (
	WithdrawalStatusPending  WithdrawalStatus = "pending"
	WithdrawalStatusApproved WithdrawalStatus = "approved"
	WithdrawalStatusRejected WithdrawalStatus = "rejected"
	WithdrawalStatusPaid     WithdrawalStatus = "paid"
)

// Valid reports whether s is a known withdrawal status.
func (s WithdrawalStatus) Valid() bool {
	switch s {
	case WithdrawalStatusPending, WithdrawalStatusApproved, WithdrawalStatusRejected, WithdrawalStatusPaid:
		return true
	}
	return false
}

// LedgerKind classifies a balance movement.
type LedgerKind string

const (
	LedgerKindSale           LedgerKind = "sale"
	LedgerKindReferral       LedgerKind = "referral"
	LedgerKindWithdrawFreeze LedgerKind = "withdraw_freeze"
	LedgerKindWithdrawRefund LedgerKind = "withdraw_refund"
	LedgerKindWithdrawSettle LedgerKind = "withdraw_settle"
	LedgerKindAdjust         LedgerKind = "adjust"
	// LedgerKindSalePending is the income of a platform channel order: it is
	// credited to the pending bucket of the wallet and only released to the
	// available balance once the platform unfreeze window has passed.
	LedgerKindSalePending LedgerKind = "sale_pending"
	// LedgerKindSaleRelease moves pending income into the available balance.
	LedgerKindSaleRelease LedgerKind = "sale_release"
)

// CouponKind is the discount formula of a coupon.
type CouponKind string

const (
	// CouponKindPercent takes Value percent off (1..99).
	CouponKindPercent CouponKind = "percent"
	// CouponKindFixed subtracts Value (in cents).
	CouponKindFixed CouponKind = "fixed"
)

// Valid reports whether k is a known coupon kind.
func (k CouponKind) Valid() bool { return k == CouponKindPercent || k == CouponKindFixed }

// CouponStatus is the lifecycle state of a coupon.
type CouponStatus string

const (
	CouponStatusActive  CouponStatus = "active"
	CouponStatusStopped CouponStatus = "stopped"
)

// RefundStatus is the review state of a buyer initiated refund request.
//
// A refund of an order paid through the platform channel is settled by the
// platform itself, so "approved" is final there. A refund of an order paid
// through the merchant's own channel (PayMode custom) is really carried out by
// the merchant, so the seller submits the proof of the refund and an
// administrator verifies it before the case is closed:
//
//	pending --(seller/admin)---> approved --(seller: 凭证)--> refunded
//	refunded --(admin: 核验)--> confirmed
//	refunded --(admin: 退回)--> approved
type RefundStatus string

const (
	RefundStatusPending  RefundStatus = "pending"
	RefundStatusApproved RefundStatus = "approved"
	RefundStatusRejected RefundStatus = "rejected"
	// RefundStatusRefunded means the merchant refunded the buyer through their
	// own channel and submitted the proof; the platform has not verified it.
	RefundStatusRefunded RefundStatus = "refunded"
	// RefundStatusConfirmed means an administrator verified the proof.
	RefundStatusConfirmed RefundStatus = "confirmed"
)

// Valid reports whether s is a known refund status.
func (s RefundStatus) Valid() bool {
	switch s {
	case RefundStatusPending, RefundStatusApproved, RefundStatusRejected,
		RefundStatusRefunded, RefundStatusConfirmed:
		return true
	}
	return false
}
