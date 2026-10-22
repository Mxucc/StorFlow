package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/gin-gonic/gin"
)

// AdminSettings returns the module configuration for the admin panel.
func (s *Service) AdminSettings(c *gin.Context) map[string]string {
	values := s.Settings().Values()
	// The merchant keys are secrets; expose only whether one is configured.
	if values[SettingEpayKey] != "" {
		values[SettingEpayKey] = maskedSecret
	}
	values[SettingEpayChannels] = maskChannelSecrets(values[SettingEpayChannels])
	return values
}

// maskChannelSecrets replaces every per-channel merchant key with the mask.
func maskChannelSecrets(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}

	var list []model.PayChannelConfig
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return raw
	}
	for i := range list {
		if list[i].Key != "" {
			list[i].Key = maskedSecret
		}
	}
	out, err := json.Marshal(list)
	if err != nil {
		return raw
	}
	return string(out)
}

// mergeChannelSecrets applies an admin submitted per-channel list on top of the
// stored one: a key sent back as the mask keeps the stored secret.
func mergeChannelSecrets(stored, incoming string) (string, error) {
	var next []model.PayChannelConfig
	if err := json.Unmarshal([]byte(incoming), &next); err != nil {
		return "", fmt.Errorf("the channel list is not valid JSON: %w", err)
	}

	previous := make(map[model.PayChannel]model.PayChannelConfig)
	var prev []model.PayChannelConfig
	if err := json.Unmarshal([]byte(stored), &prev); err == nil {
		for _, item := range prev {
			previous[item.Normalized().Channel] = item.Normalized()
		}
	}

	for i := range next {
		next[i] = next[i].Normalized()
		if !next[i].Channel.Valid() {
			return "", fmt.Errorf("unknown payment channel %q", next[i].Channel)
		}
		if next[i].APIURL != "" && !looksLikeURL(next[i].APIURL) {
			return "", fmt.Errorf("channel %q has an invalid gateway URL", next[i].Channel)
		}
		if next[i].Key == maskedSecret {
			next[i].Key = previous[next[i].Channel].Key
		}
	}
	return marshalChannelConfigs(model.NormalizePayChannels(next)), nil
}

// AdminSaveSettings patches the module configuration.
func (s *Service) AdminSaveSettings(c *gin.Context, req *AdminSettingRequest) error {
	if key, ok := req.Values[SettingEpayKey]; ok && key == "********" {
		delete(req.Values, SettingEpayKey)
	}

	// Validate before writing anything so a bad request cannot leave the module
	// half configured.
	if url, ok := req.Values[SettingEpayAPIURL]; ok && url != "" && !looksLikeURL(url) {
		return serializer.NewError(serializer.CodeParamErr, "Invalid gateway URL", nil)
	}
	if raw, ok := req.Values[SettingEpayChannels]; ok {
		merged, err := mergeChannelSecrets(s.Settings().Values()[SettingEpayChannels], raw)
		if err != nil {
			return serializer.NewError(serializer.CodeParamErr, "Invalid payment channels", err)
		}

		var list []model.PayChannelConfig
		if err := json.Unmarshal([]byte(merged), &list); err == nil {
			for _, item := range list {
				if item.APIURL == "" {
					continue
				}
				if err := validateGatewayURL(c, item.APIURL); err != nil {
					return serializer.NewError(serializer.CodeParamErr, "Invalid gateway URL", err)
				}
			}
		}
		req.Values[SettingEpayChannels] = merged
	}
	for _, key := range []string{SettingFeePercent, SettingReferralPercent, SettingWithdrawFeePercent} {
		if raw, ok := req.Values[key]; ok {
			value := atoiDefault(raw, -1)
			if value < 0 || value > 10000 {
				return serializer.NewError(serializer.CodeParamErr, "Rates are expressed in basis points (0-10000)", nil)
			}
		}
	}
	if raw, ok := req.Values[SettingMinWithdraw]; ok {
		if atoiDefault(raw, -1) < 0 {
			return serializer.NewError(serializer.CodeParamErr, "Invalid minimum withdrawal", nil)
		}
	}
	if raw, ok := req.Values[SettingDepositAmount]; ok {
		if atoiDefault(raw, -1) < 0 {
			return serializer.NewError(serializer.CodeParamErr, "Invalid deposit amount", nil)
		}
	}
	if raw, ok := req.Values[SettingSettleDelayHours]; ok {
		hours := atoiDefault(raw, -1)
		if hours < 0 || hours > 8760 {
			return serializer.NewError(serializer.CodeParamErr,
				"The unfreeze window must be within 0 and 8760 hours", nil)
		}
	}
	if raw, ok := req.Values[SettingOrderTTL]; ok {
		ttl := atoiDefault(raw, -1)
		if ttl < 60 || ttl > 86400 {
			return serializer.NewError(serializer.CodeParamErr, "The order lifetime must be within 60s and 24h", nil)
		}
	}

	return s.SaveSettings(c, req.Values)
}

// AdminShopOverview is the summary row of one shop.
type AdminShopOverview struct {
	Shop         *model.Shop    `json:"shop"`
	Balance      *model.Balance `json:"balance"`
	ProductCount int            `json:"product_count"`
	OrderCount   int            `json:"order_count"`
	SoldCount    int            `json:"sold_count"`
}

// AdminShopStats aggregates the activity of one shop.
func (s *Service) AdminShopStats(c *gin.Context, shopID string) (*AdminShopOverview, error) {
	shop, err := s.store.GetShopByID(c, shopID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
	}

	balance, err := s.store.GetBalance(c, shop.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load balance", err)
	}

	products, err := s.store.ListProductsByShop(c, shop.ID, false, 100, 0)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list products", err)
	}

	orders, err := s.store.CountPaidOrdersByShop(c, shop.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count orders", err)
	}

	sold, err := s.store.CountEntitlementsByShop(c, shop.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to count sales", err)
	}

	return &AdminShopOverview{
		Shop:         shop,
		Balance:      balance,
		ProductCount: len(products),
		OrderCount:   orders,
		SoldCount:    sold,
	}, nil
}

func looksLikeURL(raw string) bool {
	if len(raw) < 8 {
		return false
	}
	return (len(raw) > 7 && (raw[:7] == "http://" || (len(raw) > 8 && raw[:8] == "https://")))
}
