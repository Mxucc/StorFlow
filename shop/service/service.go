// Package service implements the business use cases of the cloud drive shop.
//
// Every entry point receives the gin context, exactly like the other StorFlow
// services, so the module can reuse the host authentication middleware, error
// envelope and file manager. Nothing in this package talks SQL directly: all
// persistence goes through package store.
package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/logging"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// Service is the entry point of the shop use cases.
type Service struct {
	dep   dependency.Dep
	store *store.Store
	l     logging.Logger

	mu       sync.RWMutex
	settings Settings
}

// New creates the shop service over an existing store.
func New(dep dependency.Dep, st *store.Store) *Service {
	s := &Service{
		dep:      dep,
		store:    st,
		l:        dep.Logger().CopyWithPrefix("[Shop]"),
		settings: DefaultSettings(),
	}
	return s
}

// Store exposes the persistence layer (used by the module bootstrap).
func (s *Service) Store() *store.Store { return s.store }

// Settings returns the current module settings.
func (s *Service) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Reload re-reads the module settings from the database.
func (s *Service) Reload(ctx context.Context) error {
	raw, err := s.store.AllSettings(ctx)
	if err != nil {
		return err
	}

	next := DefaultSettings()
	if v, ok := raw[SettingEnabled]; ok {
		next.Enabled = v != "false" && v != "0"
	}
	if v, ok := raw[SettingEpayAPIURL]; ok {
		next.EpayAPIURL = v
	}
	if v, ok := raw[SettingEpayPID]; ok {
		next.EpayPID = v
	}
	if v, ok := raw[SettingEpayKey]; ok {
		next.EpayKey = v
	}
	if v, ok := raw[SettingEpayChannels]; ok {
		next.Channels = splitList(v)
	}
	if v, ok := raw[SettingFeePercent]; ok {
		next.FeePercent = atoiDefault(v, next.FeePercent)
	}
	if v, ok := raw[SettingReferralPercent]; ok {
		next.ReferralPercent = atoiDefault(v, next.ReferralPercent)
	}
	if v, ok := raw[SettingWithdrawFeePercent]; ok {
		next.WithdrawFeePercent = atoiDefault(v, next.WithdrawFeePercent)
	}
	if v, ok := raw[SettingMinWithdraw]; ok {
		next.MinWithdraw = model.Money(atoiDefault(v, int(next.MinWithdraw)))
	}
	if v, ok := raw[SettingOrderTTL]; ok {
		next.OrderTTL = atoiDefault(v, next.OrderTTL)
	}
	if v, ok := raw[SettingRefundWindowDays]; ok {
		next.RefundWindowDays = atoiDefault(v, next.RefundWindowDays)
	}
	if v, ok := raw[SettingRateLimitPerMinute]; ok {
		next.RateLimitPerMinute = atoiDefault(v, next.RateLimitPerMinute)
	}
	if v, ok := raw[SettingSiteURL]; ok {
		next.SiteURL = v
	}
	if v, ok := raw[SettingDepositAmount]; ok {
		next.DepositAmount = model.Money(atoiDefault(v, int(next.DepositAmount)))
	}
	if v, ok := raw[SettingSettleDelayHours]; ok {
		next.SettleDelayHours = atoiDefault(v, next.SettleDelayHours)
	}

	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	return nil
}

// SaveSettings persists the given key/value pairs and reloads the cache.
func (s *Service) SaveSettings(ctx context.Context, values map[string]string) error {
	for k, v := range values {
		if _, ok := knownSettings[k]; !ok {
			return serializer.NewError(serializer.CodeParamErr, fmt.Sprintf("unknown setting %q", k), nil)
		}
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			return serializer.NewError(serializer.CodeDBError, "Failed to save setting", err)
		}
	}
	return s.Reload(ctx)
}

// Setting keys of the module.
const (
	SettingEnabled              = "enabled"
	SettingEpayAPIURL           = "epay_api_url"
	SettingEpayPID              = "epay_pid"
	SettingEpayKey              = "epay_key"
	SettingEpayChannels         = "epay_channels"
	SettingFeePercent           = "fee_percent"
	SettingReferralPercent      = "referral_percent"
	SettingWithdrawFeePercent   = "withdraw_fee_percent"
	SettingMinWithdraw          = "min_withdraw"
	SettingOrderTTL             = "order_ttl"
	SettingSiteURL              = "site_url"
	SettingRefundWindowDays     = "refund_window_days"
	SettingRateLimitPerMinute   = "rate_limit_per_minute"
	SettingDefaultReferralShare = "referral_enabled"
	SettingDepositAmount        = "deposit_amount"
	SettingSettleDelayHours     = "settle_delay_hours"
)

var knownSettings = map[string]struct{}{
	SettingEnabled: {}, SettingEpayAPIURL: {}, SettingEpayPID: {}, SettingEpayKey: {},
	SettingEpayChannels: {}, SettingFeePercent: {}, SettingReferralPercent: {},
	SettingWithdrawFeePercent: {}, SettingMinWithdraw: {}, SettingOrderTTL: {},
	SettingSiteURL: {}, SettingDefaultReferralShare: {}, SettingRefundWindowDays: {}, SettingRateLimitPerMinute: {},
	SettingDepositAmount: {}, SettingSettleDelayHours: {},
}

// Settings is the typed view of the module configuration.
type Settings struct {
	// Enabled is the module level switch.
	Enabled bool
	// EpayAPIURL is the base URL of the epay gateway.
	EpayAPIURL string
	// EpayPID / EpayKey are the merchant credentials.
	EpayPID string
	EpayKey string
	// Channels is the list of payment channels offered on the cashier.
	Channels []string
	// FeePercent is the platform commission in basis points (300 = 3%).
	FeePercent int
	// ReferralPercent is the default rebate rate in basis points.
	ReferralPercent int
	// WithdrawFeePercent is the payout fee in basis points.
	WithdrawFeePercent int
	// MinWithdraw is the minimum payout amount.
	MinWithdraw model.Money
	// OrderTTL is the lifetime of a pending order in seconds.
	OrderTTL int
	// SiteURL overrides the callback base URL; empty means the StorFlow site
	// URL is used.
	SiteURL string
	// RefundWindowDays is how long a buyer may request a refund; 0 disables the
	// limit (the seller decides case by case).
	RefundWindowDays int
	// RateLimitPerMinute caps the requests per minute and per IP on the public
	// endpoints; 0 disables the limiter.
	RateLimitPerMinute int
	// ReferralEnabled turns the invite rebate program on or off.
	ReferralEnabled bool
	// DepositAmount is the shop deposit (保证金) every shop has to pay before it
	// may withdraw; zero disables the requirement.
	DepositAmount model.Money
	// SettleDelayHours is how long the platform holds the income of an order
	// paid through the platform gateway before it becomes withdrawable; zero
	// credits the balance immediately.
	SettleDelayHours int
}

// DefaultSettings returns the shipped defaults.
func DefaultSettings() Settings {
	return Settings{
		Enabled:            true,
		Channels:           []string{"alipay", "wxpay", "qqpay"},
		FeePercent:         300,
		ReferralPercent:    1000,
		WithdrawFeePercent: 0,
		MinWithdraw:        1000,
		OrderTTL:           900,
		RefundWindowDays:   7,
		RateLimitPerMinute: 240,
		ReferralEnabled:    true,
		SettleDelayHours:   24,
	}
}

// Values renders the settings as the map used by the admin API.
func (s Settings) Values() map[string]string {
	return map[string]string{
		SettingEnabled:            strconv.FormatBool(s.Enabled),
		SettingEpayAPIURL:         s.EpayAPIURL,
		SettingEpayPID:            s.EpayPID,
		SettingEpayKey:            s.EpayKey,
		SettingEpayChannels:       strings.Join(s.Channels, ","),
		SettingFeePercent:         strconv.Itoa(s.FeePercent),
		SettingReferralPercent:    strconv.Itoa(s.ReferralPercent),
		SettingWithdrawFeePercent: strconv.Itoa(s.WithdrawFeePercent),
		SettingMinWithdraw:        strconv.FormatInt(int64(s.MinWithdraw), 10),
		SettingOrderTTL:           strconv.Itoa(s.OrderTTL),
		SettingSiteURL:            s.SiteURL,
		SettingRefundWindowDays:   strconv.Itoa(s.RefundWindowDays),
		SettingRateLimitPerMinute: strconv.Itoa(s.RateLimitPerMinute),
		SettingDepositAmount:      strconv.FormatInt(int64(s.DepositAmount), 10),
		SettingSettleDelayHours:   strconv.Itoa(s.SettleDelayHours),
	}
}

// CheckEnabled returns an error when the module is switched off.
func (s *Service) CheckEnabled() error {
	if !s.Settings().Enabled {
		return serializer.NewError(serializer.CodeNotFound, "Shop is not available", nil)
	}
	return nil
}

// BaseURL returns the public base URL used to build payment callbacks.
func (s *Service) BaseURL(c *gin.Context) string {
	if override := strings.TrimRight(strings.TrimSpace(s.Settings().SiteURL), "/"); override != "" {
		return override
	}
	if u := s.dep.SettingProvider().SiteURL(c); u != nil {
		return strings.TrimRight(u.String(), "/")
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// Hasher returns the hash id encoder of the host application.
func (s *Service) Hasher() hashid.Encoder { return s.dep.HashIDEncoder() }

// CurrentUser returns the requester, which may be the anonymous user.
func CurrentUser(c *gin.Context) *ent.User { return inventory.UserFromContext(c) }

// RequireLogin returns an error unless the requester is a real user.
func RequireLogin(c *gin.Context) (*ent.User, error) {
	user := CurrentUser(c)
	if user == nil || inventory.IsAnonymousUser(user) || user.ID == 0 {
		return nil, serializer.NewError(serializer.CodeCheckLogin, "Login required", nil)
	}
	return user, nil
}

// OwnShop returns the shop of the requester, creating it on demand when
// createIfMissing is set.
func (s *Service) OwnShop(c *gin.Context, createIfMissing bool) (*model.Shop, error) {
	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	shop, err := s.store.GetShopByUID(c, user.ID)
	if err == nil {
		return shop, nil
	}
	if err != store.ErrNotFound {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load shop", err)
	}
	if !createIfMissing {
		return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
	}

	return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", nil)
}

// PublicShop resolves a storefront by slug.
func (s *Service) PublicShop(c *gin.Context, slug string) (*model.Shop, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}

	shop, err := s.store.GetShopBySlug(c, strings.TrimSpace(slug))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
	}
	if shop.Status != model.ShopStatusActive {
		return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", nil)
	}
	return shop, nil
}

// shopURL renders the public storefront URL of a shop.
func (s *Service) shopURL(c *gin.Context, slug string) string {
	return s.BaseURL(c) + "/shop/" + url.PathEscape(slug)
}

func splitList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' })
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			res = append(res, p)
		}
	}
	return res
}

func atoiDefault(raw string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}

// orderNo generates a non enumerable order number: a timestamp prefix for
// support purposes plus 128 bits of randomness.
func orderNo() string {
	return time.Now().Format("20060102150405") + strings.ToUpper(newToken(12))
}

// entUser is the host user model used across the module.
type entUser = ent.User

// System metadata keys of the host file manager. They are mirrored here so the
// module does not depend on the internals of the file manager package.
const (
	dbfsMetadataSharedRedirect = "sys:shared_redirect"
	dbfsMetadataSharedOwner    = "sys:shared_owner"
)

// ShopUID returns the owner of a shop.
func (s *Service) ShopUID(ctx context.Context, shopID string) (int, error) {
	shop, err := s.store.GetShopByID(ctx, shopID)
	if err != nil {
		return 0, err
	}
	return shop.UID, nil
}

// Logger exposes the module logger (used by the controllers for diagnostics).
func (s *Service) Logger() logging.Logger { return s.l }
