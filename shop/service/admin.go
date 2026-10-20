package service

import (
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/gin-gonic/gin"
)

// AdminSettings returns the module configuration for the admin panel.
func (s *Service) AdminSettings(c *gin.Context) map[string]string {
	values := s.Settings().Values()
	// The merchant key is a secret; expose only whether it is configured.
	if values[SettingEpayKey] != "" {
		values[SettingEpayKey] = "********"
	}
	return values
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
