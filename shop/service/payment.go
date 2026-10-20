package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/pkg/request"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/gateway/epay"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// maskedSecret is what the API returns instead of a stored merchant key. A
// request that sends it back means "keep the stored value".
const maskedSecret = "********"

// ShopPaymentView is the payment configuration of one shop as the console needs
// it: where the buyers pay, whether that configuration is complete, and how much
// of the platform deposit is still outstanding.
type ShopPaymentView struct {
	// PayMode is "platform" (平台聚合支付) or "custom" (自配支付渠道).
	PayMode model.PayMode `json:"pay_mode"`
	// EpayAPIURL and EpayPID are the merchant credentials of a custom channel.
	EpayAPIURL string `json:"epay_api_url,omitempty"`
	EpayPID    string `json:"epay_pid,omitempty"`
	// EpayKeySet reports whether a merchant key is stored; the key itself is
	// never returned.
	EpayKeySet bool `json:"epay_key_set"`
	// CustomReady reports whether the merchant channel is usable.
	CustomReady bool `json:"custom_ready"`
	// PlatformReady reports whether the platform gateway is configured.
	PlatformReady bool `json:"platform_ready"`
	// Channels are the payment channels offered by the platform.
	Channels []string `json:"channels"`
	// DepositAmount is the deposit (保证金) the platform requires from every
	// shop, DepositPaid what this shop already paid and DepositDue the rest.
	DepositAmount model.Money `json:"deposit_amount"`
	DepositPaid   model.Money `json:"deposit_paid"`
	DepositDue    model.Money `json:"deposit_due"`
	// SettleDelayHours is the unfreeze window applied to the income of an order
	// paid through the platform gateway; zero means the money is available at
	// once.
	SettleDelayHours int `json:"settle_delay_hours"`
}

// PaymentSetting returns the payment configuration of the requester's shop.
func (s *Service) PaymentSetting(c *gin.Context) (*ShopPaymentView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}
	return s.paymentView(shop), nil
}

// UpdatePaymentSetting switches the payment mode of the requester's shop and
// stores the merchant gateway credentials of a custom channel.
func (s *Service) UpdatePaymentSetting(c *gin.Context, req *PaymentSettingRequest) (*ShopPaymentView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	if req.PayMode != nil {
		mode := model.PayMode(strings.TrimSpace(*req.PayMode))
		if !mode.Valid() {
			return nil, serializer.NewError(serializer.CodeParamErr, "Unknown payment mode", nil)
		}
		shop.PayMode = mode
	}

	if req.EpayAPIURL != nil {
		raw := strings.TrimSpace(*req.EpayAPIURL)
		if raw != "" {
			if err := validateGatewayURL(c, raw); err != nil {
				return nil, serializer.NewError(serializer.CodeParamErr, "Invalid gateway URL", err)
			}
		}
		shop.EpayAPIURL = raw
	}
	if req.EpayPID != nil {
		pid := strings.TrimSpace(*req.EpayPID)
		if len(pid) > 64 {
			return nil, serializer.NewError(serializer.CodeParamErr, "The merchant id is too long", nil)
		}
		shop.EpayPID = pid
	}
	// The masked value means "keep the stored key"; an empty string clears it.
	if req.EpayKey != nil && *req.EpayKey != maskedSecret {
		key := strings.TrimSpace(*req.EpayKey)
		if len(key) > 128 {
			return nil, serializer.NewError(serializer.CodeParamErr, "The merchant key is too long", nil)
		}
		shop.EpayKey = key
	}

	// A shop that wants its own channel must provide a complete configuration,
	// otherwise every checkout would fail with a confusing gateway error.
	if shop.PayMode.OrDefault() == model.PayModeCustom && !customGatewayReady(shop) {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"A custom payment channel needs the gateway URL, the merchant id and the key", nil)
	}

	if err := s.store.UpdateShop(c, shop); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to save the payment setting", err)
	}
	return s.paymentView(shop), nil
}

// CreateDepositOrder starts the payment of the outstanding shop deposit
// (保证金). The deposit is always collected through the platform gateway, even
// when the shop configured its own channel: it is the platform that holds it.
func (s *Service) CreateDepositOrder(c *gin.Context) (*OrderView, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}

	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	due := s.DepositDue(shop)
	if due <= 0 {
		return nil, serializer.NewError(serializer.CodeConflict, "The deposit is already paid", nil)
	}
	if s.epayConfig().Key == "" {
		return nil, serializer.NewError(serializer.CodeInternalSetting, "Payment gateway is not configured", nil)
	}

	// Reuse an open deposit order: clicking twice must not create two payable
	// orders for the same deposit.
	if pending, err := s.store.GetPendingOrder(c, shop.UID, model.OrderKindDeposit, shop.ID); err == nil {
		if pending.ExpireAt == 0 || pending.ExpireAt > time.Now().Unix() {
			return s.orderView(c, pending)
		}
	} else if err != store.ErrNotFound {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to check open orders", err)
	}

	order := &model.Order{
		OrderNo:  orderNo(),
		Kind:     model.OrderKindDeposit,
		ShopID:   shop.ID,
		ObjectID: shop.ID,
		BuyerUID: shop.UID,
		Title:    "开店保证金",
		Amount:   due,
		Payable:  due,
		Status:   model.OrderStatusPending,
		Channel:  s.defaultChannel(),
		// The deposit is the platform's own requirement, so it is always paid
		// through the platform gateway.
		PayMode:  model.PayModePlatform,
		ClientIP: c.ClientIP(),
		ExpireAt: time.Now().Unix() + int64(s.Settings().OrderTTL),
	}

	if err := s.store.CreateOrder(c, order); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create the deposit order", err)
	}
	return s.orderView(c, order)
}

// DepositDue returns the part of the platform deposit a shop still owes.
func (s *Service) DepositDue(shop *model.Shop) model.Money {
	required := s.Settings().DepositAmount
	if shop == nil || required <= shop.DepositPaid {
		return 0
	}
	return required - shop.DepositPaid
}

// paymentView renders the payment configuration of a shop.
func (s *Service) paymentView(shop *model.Shop) *ShopPaymentView {
	settings := s.Settings()
	return &ShopPaymentView{
		PayMode:          shop.PayMode.OrDefault(),
		EpayAPIURL:       shop.EpayAPIURL,
		EpayPID:          shop.EpayPID,
		EpayKeySet:       strings.TrimSpace(shop.EpayKey) != "",
		CustomReady:      customGatewayReady(shop),
		PlatformReady:    s.epayConfig().Key != "",
		Channels:         settings.Channels,
		DepositAmount:    settings.DepositAmount,
		DepositPaid:      shop.DepositPaid,
		DepositDue:       s.DepositDue(shop),
		SettleDelayHours: settings.SettleDelayHours,
	}
}

// customGatewayReady reports whether a shop stored a usable merchant gateway.
func customGatewayReady(shop *model.Shop) bool {
	if shop == nil {
		return false
	}
	return strings.TrimSpace(shop.EpayAPIURL) != "" &&
		strings.TrimSpace(shop.EpayPID) != "" &&
		strings.TrimSpace(shop.EpayKey) != ""
}

// defaultChannel returns the first channel the administrator enabled, falling
// back to alipay when the list is empty.
func (s *Service) defaultChannel() model.PayChannel {
	for _, raw := range s.Settings().Channels {
		if channel := model.PayChannel(strings.TrimSpace(raw)); channel.Valid() {
			return channel
		}
	}
	return model.ChannelAlipay
}

// gatewayFor resolves the gateway a new order of this shop has to use, together
// with the payment mode it implies.
func (s *Service) gatewayFor(shop *model.Shop) (epay.Config, model.PayMode, error) {
	if shop.PayMode.OrDefault() == model.PayModeCustom {
		cfg := epay.Config{APIURL: shop.EpayAPIURL, PID: shop.EpayPID, Key: shop.EpayKey}
		normalized, err := cfg.Normalize()
		if err != nil {
			return cfg, model.PayModeCustom, serializer.NewError(serializer.CodeParamErr,
				"This shop has not configured its payment channel yet", err)
		}
		return normalized, model.PayModeCustom, nil
	}

	cfg := s.epayConfig()
	if cfg.Key == "" {
		return cfg, model.PayModePlatform, serializer.NewError(serializer.CodeInternalSetting,
			"Payment gateway is not configured", nil)
	}
	return cfg, model.PayModePlatform, nil
}

// gatewayForOrder resolves the gateway of an existing order. The payment mode is
// read from the snapshot taken at checkout, so an order keeps working when the
// shop reconfigures its channel afterwards.
func (s *Service) gatewayForOrder(ctx context.Context, order *model.Order) (epay.Config, error) {
	if order.PayMode.OrDefault() != model.PayModeCustom {
		return s.epayConfig(), nil
	}

	shop, err := s.store.GetShopByID(ctx, order.ShopID)
	if err != nil {
		return epay.Config{}, fmt.Errorf("shop: failed to load the shop of order %s: %w", order.OrderNo, err)
	}

	cfg := epay.Config{APIURL: shop.EpayAPIURL, PID: shop.EpayPID, Key: shop.EpayKey}
	normalized, err := cfg.Normalize()
	if err != nil {
		return cfg, fmt.Errorf("shop: the merchant channel of shop %s is not usable: %w", shop.ID, err)
	}
	return normalized, nil
}

// validateGatewayURL checks a merchant supplied gateway URL. The module calls it
// back for status queries, so an internal address would turn the reconciliation
// job into a server side request forgery vector.
func validateGatewayURL(ctx context.Context, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("the gateway URL must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("the gateway URL must contain a host")
	}
	return request.ValidateExternalURL(ctx, raw, request.SSRFOptions{})
}
