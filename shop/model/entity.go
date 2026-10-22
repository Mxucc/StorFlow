package model

// Shop is a storefront owned by one site user.
type Shop struct {
	ID          string     `json:"id"`
	UID         int        `json:"uid"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Avatar      string     `json:"avatar"`
	Cover       string     `json:"cover"`
	Theme       Theme      `json:"theme"`
	Status      ShopStatus `json:"status"`
	// ReferrerRate is the default rebate rate in basis points for invitees of
	// this shop. Zero falls back to the module level default.
	ReferrerRate int `json:"referrer_rate"`
	// PayMode selects where the buyers of this shop pay: the platform
	// aggregate gateway (default) or the merchant's own epay gateway.
	PayMode PayMode `json:"pay_mode"`
	// EpayAPIURL, EpayPID and EpayKey are the merchant's own epay credentials.
	// They are only used when PayMode is custom; the key is never serialized.
	EpayAPIURL string `json:"epay_api_url,omitempty"`
	EpayPID    string `json:"epay_pid,omitempty"`
	EpayKey    string `json:"-"`
	// EpayChannels are the merchant's own per-channel gateway credentials. When
	// empty the shared EpayAPIURL/EpayPID/EpayKey above apply to every channel;
	// an entry may itself leave a field empty to inherit the shared value.
	EpayChannels []PayChannelConfig `json:"-"`
	// PayChannels is the list of channels a buyer can actually pick at the
	// cashier. It is computed on read (never persisted) so the storefront does
	// not have to know anything about the gateway configuration.
	PayChannels []string `json:"pay_channels,omitempty"`
	// DepositPaid is how much of the platform deposit (保证金) this shop has
	// already paid, so it owes DepositAmount - DepositPaid.
	DepositPaid   Money `json:"deposit_paid"`
	DepositPaidAt int64 `json:"deposit_paid_at,omitempty"`
	CreatedAt     int64 `json:"created_at"`
	UpdatedAt     int64 `json:"updated_at"`
}

// Balance is the independent wallet of a shop.
//
// Available + Frozen is the shop's total claimable balance. Every mutation goes
// through store methods that also append a Ledger row inside the same
// transaction.
type Balance struct {
	ShopID    string `json:"shop_id"`
	Available Money  `json:"available"`
	Frozen    Money  `json:"frozen"`
	// Pending is income that was earned but not released yet: the platform
	// holds a settled order for the configured unfreeze window before the money
	// becomes withdrawable.
	Pending       Money `json:"pending"`
	TotalIncome   Money `json:"total_income"`
	TotalWithdraw Money `json:"total_withdraw"`
	UpdatedAt     int64 `json:"updated_at"`
}

// Ledger is one immutable balance movement.
type Ledger struct {
	ID        string     `json:"id"`
	ShopID    string     `json:"shop_id"`
	Kind      LedgerKind `json:"kind"`
	Amount    Money      `json:"amount"`
	Balance   Money      `json:"balance"`
	RefID     string     `json:"ref_id,omitempty"`
	Remark    string     `json:"remark,omitempty"`
	CreatedAt int64      `json:"created_at"`
}

// Product is a priced folder of the owner's drive.
type Product struct {
	ID     string `json:"id"`
	ShopID string `json:"shop_id"`
	// FileID is the id of the folder in the host application's file table.
	FileID int `json:"file_id"`
	// ShareID and SharePassword identify the internal share used to read the
	// folder. They are secret and never serialized to API responses.
	ShareID       int    `json:"-"`
	SharePassword string `json:"-"`

	Name        string        `json:"name"`
	Description string        `json:"description"`
	Cover       string        `json:"cover"`
	Price       Money         `json:"price"`
	Discount    int           `json:"discount"`
	DiscountEnd int64         `json:"discount_end"`
	Status      ProductStatus `json:"status"`
	Sales       int           `json:"sales"`
	Views       int           `json:"views"`
	Props       ProductProps  `json:"props"`
	CreatedAt   int64         `json:"created_at"`
	UpdatedAt   int64         `json:"updated_at"`
}

// EffectivePrice returns the price after the time limited promotion, if any.
func (p *Product) EffectivePrice(now int64) Money {
	if p.Discount > 0 && (p.DiscountEnd == 0 || p.DiscountEnd > now) {
		return PercentOff(p.Price, p.Discount)
	}
	return p.Price
}

// Order is a pending or settled purchase.
type Order struct {
	OrderNo  string      `json:"order_no"`
	Kind     OrderKind   `json:"kind"`
	ShopID   string      `json:"shop_id,omitempty"`
	ObjectID string      `json:"object_id"`
	BuyerUID int         `json:"buyer_uid"`
	Title    string      `json:"title"`
	Amount   Money       `json:"amount"`
	Discount Money       `json:"discount"`
	Payable  Money       `json:"payable"`
	Paid     Money       `json:"paid"`
	Status   OrderStatus `json:"status"`
	Channel  PayChannel  `json:"channel"`
	// PayMode is a snapshot of where the money went when the order was created:
	// a later reconfiguration of the shop must not change how an existing order
	// is verified, settled or refunded.
	PayMode PayMode `json:"pay_mode,omitempty"`
	// SettleAt is when a held sale income becomes withdrawable, zero when
	// nothing is held. SettleState is "held" while the platform keeps the money
	// and "released" once it moved to the available balance.
	SettleAt    int64  `json:"settle_at,omitempty"`
	SettleState string `json:"settle_state,omitempty"`
	CouponID    string `json:"coupon_id,omitempty"`
	// ReferrerUID is the invitee's inviter at the time of checkout, it is
	// snapshotted so later referral changes do not rewrite history.
	ReferrerUID int `json:"referrer_uid,omitempty"`
	// TransferState records whether the symbolic folder was created.
	TransferState string `json:"transfer_state,omitempty"`
	TradeNo       string `json:"trade_no,omitempty"`
	ClientIP      string `json:"client_ip,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	PaidAt        int64  `json:"paid_at,omitempty"`
	ExpireAt      int64  `json:"expire_at"`
}

// Entitlement is the buyer's proof of purchase. It survives product status
// changes and drives both the symbolic folder and the update feed.
type Entitlement struct {
	ID       string            `json:"id"`
	UID      int               `json:"uid"`
	Kind     OrderKind         `json:"kind"`
	ObjectID string            `json:"object_id"`
	OrderNo  string            `json:"order_no"`
	ShopID   string            `json:"shop_id,omitempty"`
	Title    string            `json:"title"`
	Status   EntitlementStatus `json:"status"`
	// FolderID is the id of the symbolic folder created in the buyer's drive.
	FolderID  int   `json:"folder_id,omitempty"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// Withdrawal is a payout request against the shop balance.
type Withdrawal struct {
	ID        string            `json:"id"`
	ShopID    string            `json:"shop_id"`
	Amount    Money             `json:"amount"`
	Fee       Money             `json:"fee"`
	Actual    Money             `json:"actual"`
	Account   WithdrawalAccount `json:"account"`
	Status    WithdrawalStatus  `json:"status"`
	Remark    string            `json:"remark,omitempty"`
	Operator  int               `json:"operator,omitempty"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
}

// Refund is a buyer initiated refund request that the seller (or an
// administrator) has to approve.
type Refund struct {
	ID            string       `json:"id"`
	EntitlementID string       `json:"entitlement_id"`
	OrderNo       string       `json:"order_no"`
	UID           int          `json:"uid"`
	ShopID        string       `json:"shop_id,omitempty"`
	Amount        Money        `json:"amount"`
	Reason        string       `json:"reason,omitempty"`
	Status        RefundStatus `json:"status"`
	Operator      int          `json:"operator,omitempty"`
	Remark        string       `json:"remark,omitempty"`
	// Evidence is the proof the seller submits when the refund was really
	// carried out through their own payment channel (交易号、退款截图地址等).
	Evidence string `json:"evidence,omitempty"`
	// EvidenceAt is when the seller submitted that proof.
	EvidenceAt int64 `json:"evidence_at,omitempty"`
	// Verifier and VerifiedAt record the administrator who checked the proof.
	Verifier   int   `json:"verifier,omitempty"`
	VerifiedAt int64 `json:"verified_at,omitempty"`
	CreatedAt  int64 `json:"created_at"`
	UpdatedAt  int64 `json:"updated_at"`
}

// Coupon is a discount code created by a shop owner.
type Coupon struct {
	ID        string       `json:"id"`
	ShopID    string       `json:"shop_id"`
	Code      string       `json:"code"`
	Kind      CouponKind   `json:"kind"`
	Value     int64        `json:"value"`
	MinAmount Money        `json:"min_amount"`
	ProductID string       `json:"product_id,omitempty"`
	MaxUses   int          `json:"max_uses"`
	Used      int          `json:"used"`
	PerUser   int          `json:"per_user"`
	Status    CouponStatus `json:"status"`
	StartAt   int64        `json:"start_at,omitempty"`
	EndAt     int64        `json:"end_at,omitempty"`
	CreatedAt int64        `json:"created_at"`
}

// Available reports whether the coupon can be used right now.
func (c *Coupon) Available(now int64) bool {
	if c.Status != CouponStatusActive {
		return false
	}
	if c.StartAt > 0 && now < c.StartAt {
		return false
	}
	if c.EndAt > 0 && now > c.EndAt {
		return false
	}
	if c.MaxUses > 0 && c.Used >= c.MaxUses {
		return false
	}
	return true
}

// Referral links an invitee (a user registered through a resource page) to the
// shop owner who invited them.
type Referral struct {
	ID          string `json:"id"`
	InviterUID  int    `json:"inviter_uid"`
	InviteeUID  int    `json:"invitee_uid"`
	Source      string `json:"source,omitempty"`
	ObjectID    string `json:"object_id,omitempty"`
	Rate        int    `json:"rate"`
	TotalRebate Money  `json:"total_rebate"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}
