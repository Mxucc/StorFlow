// Package vas implements the value-added service (VAS) system: shop SKUs,
// payments, points and gift codes. Feature-parity with the Pro edition's
// payment system, reimplemented on the community edition.
package vas

import (
	"context"
	"encoding/json"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/group"
	"github.com/Mxucc/StorFlow/ent/setting"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
)

const (
	SettingSKUKey      = "vas_skus"
	SettingProviderKey = "vas_providers"

	ProductTypePoints  = "points"
	ProductTypeStorage = "storage"
	ProductTypeGroup   = "group"

	ChannelEpay   = "epay"
	ChannelCustom = "custom"
	ChannelStripe = "stripe"
	ChannelPoints = "points"

	PaymentStatusUnpaid        = "unpaid"
	PaymentStatusPaid          = "paid"
	PaymentStatusFulfilled     = "fulfilled"
	PaymentStatusFulfillFailed = "fulfill_failed"
)

// SKU is a purchasable product definition, stored as a settings JSON list.
type SKU struct {
	ID           string `json:"id"`
	Type         string `json:"type"` // points | storage | group
	Name         string `json:"name"`
	Price        int64  `json:"price"` // in cents, 0 = points-only
	Currency     string `json:"currency,omitempty"`
	PointsPrice  int64  `json:"points_price,omitempty"`  // price in points
	Num          int64  `json:"num"`                     // points amount / storage bytes / group id
	DurationDays int64  `json:"duration_days,omitempty"` // 会员商品有效期（天）：>0 到期回退原始用户组，0/缺省 = 永久
	Description  string `json:"description,omitempty"`   // 卖家填写的卖点介绍，展示在商店商品卡片上
	OnSale       bool   `json:"on_sale"`
}

// Provider is a payment channel configuration, stored as a settings JSON list.
type Provider struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"` // epay | custom | stripe
	Name   string            `json:"name"`
	OnSale bool              `json:"on_sale"`
	Config map[string]string `json:"config"`
}

// GetSKUs loads the SKU list from settings.
func GetSKUs(ctx context.Context, db *ent.Client) []SKU {
	res := make([]SKU, 0)
	raw := getSettingJSON(ctx, db, SettingSKUKey)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &res)
	}
	return res
}

// SaveSKUs persists the SKU list.
func SaveSKUs(ctx context.Context, db *ent.Client, skus []SKU) error {
	buf, _ := json.Marshal(skus)
	return putSettingJSON(ctx, db, SettingSKUKey, string(buf))
}

// GetProviders loads the payment provider list from settings.
func GetProviders(ctx context.Context, db *ent.Client) []Provider {
	res := make([]Provider, 0)
	raw := getSettingJSON(ctx, db, SettingProviderKey)
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &res)
	}
	return res
}

// SaveProviders persists the payment provider list.
func SaveProviders(ctx context.Context, db *ent.Client, providers []Provider) error {
	buf, _ := json.Marshal(providers)
	return putSettingJSON(ctx, db, SettingProviderKey, string(buf))
}

// GetProviderByID finds one provider by ID.
func GetProviderByID(ctx context.Context, db *ent.Client, id string) (Provider, bool) {
	for _, p := range GetProviders(ctx, db) {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// GetSKUByID finds one SKU by ID.
func GetSKUByID(ctx context.Context, db *ent.Client, id string) (SKU, bool) {
	for _, s := range GetSKUs(ctx, db) {
		if s.ID == id {
			return s, true
		}
	}
	return SKU{}, false
}

func getSettingJSON(ctx context.Context, db *ent.Client, key string) string {
	s, err := db.Setting.Query().Where(setting.NameEQ(key)).First(ctx)
	if err != nil {
		return ""
	}
	return s.Value
}

func putSettingJSON(ctx context.Context, db *ent.Client, key, value string) error {
	s, err := db.Setting.Query().Where(setting.NameEQ(key)).First(ctx)
	if err == nil {
		return db.Setting.UpdateOneID(s.ID).SetValue(value).Exec(ctx)
	}
	_, err = db.Setting.Create().SetName(key).SetValue(value).Save(ctx)
	return err
}

type (
	// SaveSKUsService persists the SKU list.
	SaveSKUsService struct {
		SKUs []SKU `json:"skus" binding:"required"`
	}
	SaveSKUsParamCtx struct{}

	// SaveProvidersService persists the provider list.
	SaveProvidersService struct {
		Providers []Provider `json:"providers" binding:"required"`
	}
	SaveProvidersParamCtx struct{}
)

// Save saves the SKU list.
func (service *SaveSKUsService) Save(c *gin.Context) error {
	db := dependency.FromContext(c).DBClient()
	return SaveSKUs(c, db, service.SKUs)
}

// Save saves the provider list.
func (service *SaveProvidersService) Save(c *gin.Context) error {
	db := dependency.FromContext(c).DBClient()
	return SaveProviders(c, db, service.Providers)
}

// ListSKUs returns the SKU list (admin view: includes off-sale items).
func ListSKUs(c *gin.Context) serializer.Response {
	db := dependency.FromContext(c).DBClient()
	return serializer.Response{Data: GetSKUs(c, db)}
}

// ListProviders returns the provider list (admin view: includes gateway secrets).
func ListProviders(c *gin.Context) serializer.Response {
	db := dependency.FromContext(c).DBClient()
	return serializer.Response{Data: GetProviders(c, db)}
}

// ShopProvider is the user-facing payment channel view. 网关密钥等配置不返回给买家。
type ShopProvider struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// ShopGroup 是会员类商品的目标用户组权益摘要（用户侧只读）。
type ShopGroup struct {
	Name           string `json:"name"`
	MaxStorage     int64  `json:"max_storage"`
	SpeedLimit     int    `json:"speed_limit"`
	Permission     string `json:"permission,omitempty"`
	TrashRetention int    `json:"trash_retention,omitempty"`
}

// ShopSKU 是用户侧商品视图：在 SKU 之上附带会员商品的目标用户组权益。
type ShopSKU struct {
	SKU
	Group *ShopGroup `json:"group,omitempty"`
}

// ListShopSKUs returns on-sale SKUs for the user-facing store.
func ListShopSKUs(c *gin.Context) serializer.Response {
	db := dependency.FromContext(c).DBClient()
	res := make([]ShopSKU, 0)
	for _, s := range GetSKUs(c, db) {
		if !s.OnSale {
			continue
		}
		item := ShopSKU{SKU: s}
		if s.Type == ProductTypeGroup {
			item.Group = shopGroup(c, db, int(s.Num))
		}
		res = append(res, item)
	}
	return serializer.Response{Data: res}
}

// shopGroup 读取会员商品对应的用户组权益；用户组已被删除时返回 nil，
// 由前端退化为「仅展示商品名」。
func shopGroup(ctx context.Context, db *ent.Client, id int) *ShopGroup {
	g, err := db.Group.Query().Where(group.IDEQ(id)).First(ctx)
	if err != nil {
		return nil
	}
	res := &ShopGroup{
		Name:       g.Name,
		MaxStorage: g.MaxStorage,
		SpeedLimit: g.SpeedLimit,
	}
	if g.Permissions != nil {
		if perm, err := g.Permissions.String(); err == nil {
			res.Permission = perm
		}
	}
	if g.Settings != nil {
		res.TrashRetention = g.Settings.TrashRetention
	}
	return res
}

// ListShopProviders returns on-sale payment channels for the user-facing store.
func ListShopProviders(c *gin.Context) serializer.Response {
	db := dependency.FromContext(c).DBClient()
	res := make([]ShopProvider, 0)
	for _, p := range GetProviders(c, db) {
		if p.OnSale {
			res = append(res, ShopProvider{ID: p.ID, Type: p.Type, Name: p.Name})
		}
	}
	return serializer.Response{Data: res}
}
