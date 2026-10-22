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

// ShopChannelView is one row of the per-channel gateway editor.
type ShopChannelView struct {
	// Channel is the epay channel this row configures.
	Channel string `json:"channel"`
	// APIURL and PID are the credentials in effect for the channel.
	APIURL string `json:"api_url,omitempty"`
	PID    string `json:"pid,omitempty"`
	// KeySet reports whether a merchant key is stored; the key itself is never
	// returned.
	KeySet bool `json:"key_set"`
	// Enabled reports whether the cashier offers the channel.
	Enabled bool `json:"enabled"`
	// Ready reports whether the channel is enabled and fully configured, i.e.
	// whether a buyer can actually pay with it.
	Ready bool `json:"ready"`
}

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
	// Channels are the channels the cashier may offer for this shop: enabled and
	// with usable credentials (per-channel credentials, falling back to the
	// shared ones).
	Channels []string `json:"channels"`
	// ChannelConfigs is the editable per-channel table. It always contains the
	// three epay channels, with the credentials that are actually in effect and
	// the key masked behind KeySet.
	ChannelConfigs []ShopChannelView `json:"channel_configs"`
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

	// 逐通道覆盖：每个通道可以有自己的收银台地址 / 商户号 / 密钥。
	// 与共享凭据相同的值不重复落库，留空即表示「沿用共享凭据」。
	if req.EpayChannels != nil {
		current := make(map[model.PayChannel]model.PayChannelConfig)
		for _, item := range shopChannelConfigs(shop) {
			current[item.Channel] = item
		}

		next := make([]model.PayChannelConfig, 0, len(*req.EpayChannels))
		for _, row := range *req.EpayChannels {
			channel := model.PayChannel(strings.ToLower(strings.TrimSpace(row.Channel)))
			if !channel.Valid() {
				return nil, serializer.NewError(serializer.CodeParamErr, "Unknown payment channel", nil)
			}

			apiURL := strings.TrimSpace(row.APIURL)
			if apiURL != "" {
				if err := validateGatewayURL(c, apiURL); err != nil {
					return nil, serializer.NewError(serializer.CodeParamErr, "Invalid gateway URL", err)
				}
			}
			pid := strings.TrimSpace(row.PID)
			if len(pid) > 64 {
				return nil, serializer.NewError(serializer.CodeParamErr, "The merchant id is too long", nil)
			}

			key := strings.TrimSpace(row.Key)
			// 哨兵值表示沿用该通道已保存的密钥。
			if key == maskedSecret {
				key = current[channel].Key
			}
			if len(key) > 128 {
				return nil, serializer.NewError(serializer.CodeParamErr, "The merchant key is too long", nil)
			}

			// 与共享凭据相同的值不重复落库：留空即表示「沿用共享凭据」。
			if apiURL == strings.TrimRight(strings.TrimSpace(shop.EpayAPIURL), "/") {
				apiURL = ""
			}
			if pid == strings.TrimSpace(shop.EpayPID) {
				pid = ""
			}
			if key == strings.TrimSpace(shop.EpayKey) {
				key = ""
			}

			next = append(next, model.PayChannelConfig{
				Channel: channel,
				APIURL:  apiURL,
				PID:     pid,
				Key:     key,
				Enabled: row.Enabled,
			})
		}
		shop.EpayChannels = model.NormalizePayChannels(next)
	}

	// 自配渠道必须至少有一个通道配齐，否则买家点付款只会拿到网关报错。
	if shop.PayMode.OrDefault() == model.PayModeCustom && !anyReadyChannel(shopChannelConfigs(shop)) {
		return nil, serializer.NewError(serializer.CodeParamErr,
			"A custom payment channel needs at least one enabled channel with the gateway URL, the merchant id and the key", nil)
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
	channels := shopChannelConfigs(shop)
	view := &ShopPaymentView{
		PayMode:          shop.PayMode.OrDefault(),
		EpayAPIURL:       shop.EpayAPIURL,
		EpayPID:          shop.EpayPID,
		EpayKeySet:       strings.TrimSpace(shop.EpayKey) != "",
		CustomReady:      anyReadyChannel(channels),
		PlatformReady:    anyReadyChannel(settings.ChannelConfigs),
		DepositAmount:    settings.DepositAmount,
		DepositPaid:      shop.DepositPaid,
		DepositDue:       s.DepositDue(shop),
		SettleDelayHours: settings.SettleDelayHours,
		ChannelConfigs:   channelViews(channels),
	}

	// 收银台只展示当前收款方式下真正能用的通道。
	if shop.PayMode.OrDefault() == model.PayModeCustom {
		view.Channels = readyChannels(channels)
	} else {
		view.Channels = settings.Channels()
	}
	return view
}

// channelViews converts the effective configuration into editable rows.
func channelViews(list []model.PayChannelConfig) []ShopChannelView {
	byChannel := make(map[model.PayChannel]model.PayChannelConfig, len(list))
	for _, item := range list {
		byChannel[item.Normalized().Channel] = item
	}

	res := make([]ShopChannelView, 0, len(model.PayChannelOrder))
	for _, channel := range model.PayChannelOrder {
		item, ok := byChannel[channel]
		if !ok {
			res = append(res, ShopChannelView{Channel: string(channel)})
			continue
		}
		res = append(res, ShopChannelView{
			Channel: string(channel),
			APIURL:  item.APIURL,
			PID:     item.PID,
			KeySet:  item.Key != "",
			Enabled: item.Enabled,
			Ready:   item.Ready(),
		})
	}
	return res
}

// readyChannels lists the channel names a buyer can actually use.
func readyChannels(list []model.PayChannelConfig) []string {
	res := make([]string, 0, len(list))
	for _, item := range list {
		if item.Ready() {
			res = append(res, string(item.Channel))
		}
	}
	return res
}

// anyReadyChannel reports whether at least one channel can be paid with.
func anyReadyChannel(list []model.PayChannelConfig) bool {
	return len(readyChannels(list)) > 0
}

// shopChannelConfigs returns the effective per-channel gateway configuration of
// a shop: the per-channel entries merged with the shared credentials of the
// shop. A shop that never configured per-channel entries falls back to "the
// shared credentials apply to every channel", which keeps old shops working.
func shopChannelConfigs(shop *model.Shop) []model.PayChannelConfig {
	if shop == nil {
		return nil
	}

	shared := model.PayChannelConfig{APIURL: shop.EpayAPIURL, PID: shop.EpayPID, Key: shop.EpayKey}
	if len(shop.EpayChannels) == 0 {
		if shared.APIURL == "" || shared.PID == "" || shared.Key == "" {
			return nil
		}
		list := make([]model.PayChannelConfig, 0, len(model.PayChannelOrder))
		for _, channel := range model.PayChannelOrder {
			list = append(list, model.PayChannelConfig{Channel: channel, Enabled: true}.Merge(shared))
		}
		return list
	}

	list := model.NormalizePayChannels(shop.EpayChannels)
	for i := range list {
		list[i] = list[i].Merge(shared)
	}
	return list
}

// defaultChannel returns the first channel the administrator enabled, falling
// back to alipay when the list is empty.
func (s *Service) defaultChannel() model.PayChannel {
	if channels := s.Settings().Channels(); len(channels) > 0 {
		return model.PayChannel(channels[0])
	}
	return model.ChannelAlipay
}

// gatewayFor resolves the gateway a new order of this shop has to use for the
// requested channel, together with the payment mode it implies.
//
// 每个通道都可能指向不同的易支付服务商（地址 / 商户号 / 密钥各一套），
// 所以这里按通道取配置，而不是把一份凭据套到三个通道上。
func (s *Service) gatewayFor(shop *model.Shop, channel model.PayChannel) (epay.Config, model.PayMode, error) {
	mode := shop.PayMode.OrDefault()
	list := s.Settings().ChannelConfigs
	if mode == model.PayModeCustom {
		list = shopChannelConfigs(shop)
	}

	channel = model.PayChannel(strings.ToLower(strings.TrimSpace(string(channel))))
	if !channel.Valid() {
		channel = s.defaultChannel()
	}

	item, ok := model.FindPayChannel(list, channel)
	if !ok || !item.Enabled {
		return epay.Config{}, mode, serializer.NewError(serializer.CodeParamErr,
			"This payment channel is not enabled", nil)
	}

	cfg, err := (epay.Config{APIURL: item.APIURL, PID: item.PID, Key: item.Key}).Normalize()
	if err != nil {
		code := serializer.CodeParamErr
		if mode == model.PayModePlatform {
			code = serializer.CodeInternalSetting
		}
		return cfg, mode, serializer.NewError(code, "The payment channel is not configured yet", err)
	}
	return cfg, mode, nil
}

// gatewayForOrder resolves the gateway of an existing order by the channel that
// was chosen at checkout. The payment mode comes from the snapshot taken then,
// so an order keeps working when the shop reconfigures its channels afterwards.
func (s *Service) gatewayForOrder(ctx context.Context, order *model.Order) (epay.Config, error) {
	list := s.Settings().ChannelConfigs
	if order.PayMode.OrDefault() == model.PayModeCustom {
		shop, err := s.store.GetShopByID(ctx, order.ShopID)
		if err != nil {
			return epay.Config{}, fmt.Errorf("shop: failed to load the shop of order %s: %w", order.OrderNo, err)
		}
		list = shopChannelConfigs(shop)
	}

	item, ok := model.FindPayChannel(list, order.Channel)
	if !ok {
		return epay.Config{}, fmt.Errorf("shop: channel %q of order %s is no longer configured", order.Channel, order.OrderNo)
	}

	cfg, err := (epay.Config{APIURL: item.APIURL, PID: item.PID, Key: item.Key}).Normalize()
	if err != nil {
		return cfg, fmt.Errorf("shop: channel %q of order %s is not usable: %w", order.Channel, order.OrderNo, err)
	}
	return cfg, nil
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
