package service

import (
	"fmt"
	"strings"

	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// ShopView is the seller facing projection of a shop.
type ShopView struct {
	*model.Shop
	// URL is the public storefront link.
	URL string `json:"url"`
	// ProductCount / Sales are the headline statistics.
	ProductCount int `json:"product_count"`
	Sales        int `json:"sales"`
	// ReferralCode is the code appended to a share link so that a visitor who
	// registers through it becomes an invitee of this shop owner.
	ReferralCode string `json:"referral_code"`
	// DepositDue is the outstanding platform deposit (保证金): while it is
	// positive the console asks the owner to pay it and withdrawals are blocked.
	DepositDue model.Money `json:"deposit_due"`
}

// Storefront is the public projection of a shop.
type Storefront struct {
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Avatar      string            `json:"avatar"`
	Cover       string            `json:"cover"`
	Theme       model.Theme       `json:"theme"`
	URL         string            `json:"url"`
	Owner       StorefrontOwner   `json:"owner"`
	Products    []*ProductSummary `json:"products"`
}

// StorefrontOwner is the public identity of the shop owner.
type StorefrontOwner struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

// CreateShop opens a shop for the requester.
func (s *Service) CreateShop(c *gin.Context, req *CreateShopRequest) (*ShopView, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}

	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	existing, err := s.store.GetShopByUID(c, user.ID)
	if err == nil {
		// Idempotent: opening a shop twice returns the existing one.
		return s.buildShopView(c, existing)
	}
	if err != store.ErrNotFound {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load shop", err)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = user.Nick
	}
	if name == "" {
		name = fmt.Sprintf("Shop %d", user.ID)
	}
	if len([]rune(name)) > 40 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Shop name is too long", nil)
	}

	slug := slugify(req.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	if slug == "" {
		slug = fmt.Sprintf("shop-%s", newToken(4))
	}

	shop := &model.Shop{
		ID:           newID(),
		UID:          user.ID,
		Slug:         slug,
		Name:         name,
		Description:  strings.TrimSpace(req.Description),
		Avatar:       strings.TrimSpace(req.Avatar),
		Cover:        strings.TrimSpace(req.Cover),
		Status:       model.ShopStatusActive,
		ReferrerRate: s.Settings().ReferralPercent,
	}
	if req.Theme != nil {
		theme := *req.Theme
		if err := theme.Normalize(); err != nil {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid theme", err)
		}
		shop.Theme = theme
	}

	// Slugs are unique across the instance; append a random suffix until the
	// insert succeeds so that two shops never collide.
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			shop.Slug = fmt.Sprintf("%s-%s", slug, newToken(3))
		}
		err = s.store.CreateShop(c, shop)
		if err == nil {
			return s.buildShopView(c, shop)
		}
		if !store.IsConflict(err) {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to create shop", err)
		}
	}

	return nil, serializer.NewError(serializer.CodeConflict, "Shop slug is already taken", nil)
}

// GetMyShop returns the shop of the requester.
func (s *Service) GetMyShop(c *gin.Context) (*ShopView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}
	return s.buildShopView(c, shop)
}

// UpdateShop updates the profile and the decoration of the requester's shop.
func (s *Service) UpdateShop(c *gin.Context, req *UpdateShopRequest) (*ShopView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 40 {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid shop name", nil)
		}
		shop.Name = name
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		if len([]rune(desc)) > 500 {
			return nil, serializer.NewError(serializer.CodeParamErr, "Description is too long", nil)
		}
		shop.Description = desc
	}
	if req.Avatar != nil {
		shop.Avatar = strings.TrimSpace(*req.Avatar)
	}
	if req.Cover != nil {
		shop.Cover = strings.TrimSpace(*req.Cover)
	}
	if req.Theme != nil {
		if err := req.Theme.Normalize(); err != nil {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid theme", err)
		}
		shop.Theme = *req.Theme
	}
	if req.ReferrerRate != nil {
		rate := *req.ReferrerRate
		if rate < 0 || rate > 5000 {
			return nil, serializer.NewError(serializer.CodeParamErr, "Referral rate must be within 0%-50%", nil)
		}
		shop.ReferrerRate = rate
	}
	if req.Slug != nil {
		slug := slugify(*req.Slug)
		if slug == "" {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid shop slug", nil)
		}
		shop.Slug = slug
	}

	if err := s.store.UpdateShop(c, shop); err != nil {
		if store.IsConflict(err) {
			return nil, serializer.NewError(serializer.CodeConflict, "Shop slug is already taken", err)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update shop", err)
	}
	return s.buildShopView(c, shop)
}

// SetShopStatus enables or disables the storefront.
func (s *Service) SetShopStatus(c *gin.Context, enabled bool) (*ShopView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	if enabled {
		shop.Status = model.ShopStatusActive
	} else {
		shop.Status = model.ShopStatusDisabled
	}

	if err := s.store.UpdateShop(c, shop); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update shop", err)
	}
	return s.buildShopView(c, shop)
}

// PublicShopSummary is the minimal shop projection used by the public site
// (shop directory and sitemap): identity plus decoration images only.
type PublicShopSummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Cover       string `json:"cover,omitempty"`
	UpdatedAt   int64  `json:"updated_at"`
}

// PublicShops lists active shops for the public site.
//
// store.ListShops caps one page at 100 rows, so the requested limit is filled by
// paging instead of asking for one oversized query.
func (s *Service) PublicShops(c *gin.Context, limit int) ([]*PublicShopSummary, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	res := make([]*PublicShopSummary, 0, limit)
	for offset := 0; len(res) < limit; offset += 100 {
		page, err := s.store.ListShops(c, 100, offset)
		if err != nil {
			return nil, serializer.NewError(serializer.CodeDBError, "Failed to list shops", err)
		}
		for _, shop := range page {
			if shop.Status != model.ShopStatusActive {
				continue
			}
			res = append(res, &PublicShopSummary{
				Slug:        shop.Slug,
				Name:        shop.Name,
				Description: shop.Description,
				Avatar:      shop.Avatar,
				Cover:       shop.Cover,
				UpdatedAt:   shop.UpdatedAt,
			})
		}
		if len(page) < 100 {
			break
		}
	}
	return res, nil
}

// Storefront renders the public storefront of a shop. limit/offset paginate the
// product list (a shop may list hundreds of folders).
func (s *Service) Storefront(c *gin.Context, slug string, limit, offset int) (*Storefront, error) {
	shop, err := s.PublicShop(c, slug)
	if err != nil {
		return nil, err
	}

	owner, err := s.dep.UserClient().GetByID(c, shop.UID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load shop owner", err)
	}

	products, err := s.store.ListProductsByShop(c, shop.ID, true, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list products", err)
	}

	// Ownership is resolved with a single query: the storefront may list many
	// products and must not turn into one query per card.
	owned := map[string]struct{}{}
	if user := CurrentUser(c); user != nil && user.ID != 0 {
		owned, err = s.store.ListEntitledObjectIDs(c, user.ID, model.OrderKindProduct)
		if err != nil {
			s.l.Warning("Failed to resolve the owned products of user %d: %s", user.ID, err)
			owned = map[string]struct{}{}
		}
	}

	hasher := s.Hasher()
	summaries := make([]*ProductSummary, 0, len(products))
	for _, p := range products {
		summary := s.productSummary(c, p, hasher)
		if _, ok := owned[p.ID]; ok {
			summary.Owned = true
		}
		summaries = append(summaries, summary)
	}

	return &Storefront{
		Slug:        shop.Slug,
		Name:        shop.Name,
		Description: shop.Description,
		Avatar:      shop.Avatar,
		Cover:       shop.Cover,
		Theme:       shop.Theme,
		URL:         s.shopURL(c, shop.Slug),
		Owner: StorefrontOwner{
			ID:       hashid.EncodeUserID(hasher, shop.UID),
			Nickname: owner.Nick,
			Avatar:   owner.Avatar,
		},
		Products: summaries,
	}, nil
}

// buildShopView renders the seller facing shop projection.
func (s *Service) buildShopView(c *gin.Context, shop *model.Shop) (*ShopView, error) {
	products, err := s.store.ListProductsByShop(c, shop.ID, false, 100, 0)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list products", err)
	}

	sales := 0
	for _, p := range products {
		sales += p.Sales
	}

	return &ShopView{
		Shop:         shop,
		URL:          s.shopURL(c, shop.Slug),
		ProductCount: len(products),
		Sales:        sales,
		ReferralCode: hashid.EncodeUserID(s.Hasher(), shop.UID),
		DepositDue:   s.DepositDue(shop),
	}, nil
}

// BalanceView is the seller facing wallet projection.
type BalanceView struct {
	*model.Balance
	Ledger []*model.Ledger `json:"ledger"`
	// DepositAmount / DepositPaid / DepositDue describe the platform deposit
	// (保证金) of the shop: withdrawals stay blocked while DepositDue is positive.
	DepositAmount model.Money `json:"deposit_amount"`
	DepositPaid   model.Money `json:"deposit_paid"`
	DepositDue    model.Money `json:"deposit_due"`
	// SettleDelayHours is the unfreeze window applied to the income of a
	// platform channel order, so the console can explain the pending balance.
	SettleDelayHours int `json:"settle_delay_hours"`
}

// Balance returns the wallet of the requester's shop.
func (s *Service) Balance(c *gin.Context) (*BalanceView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	balance, err := s.store.GetBalance(c, shop.ID)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load balance", err)
	}

	ledger, err := s.store.ListLedger(c, shop.ID, 50, 0)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to load ledger", err)
	}

	settings := s.Settings()
	return &BalanceView{
		Balance:          balance,
		Ledger:           ledger,
		DepositAmount:    settings.DepositAmount,
		DepositPaid:      shop.DepositPaid,
		DepositDue:       s.DepositDue(shop),
		SettleDelayHours: settings.SettleDelayHours,
	}, nil
}

// ReferralView describes one invitee of the shop owner.
type ReferralView struct {
	InviteeUID  string          `json:"invitee_uid"`
	Nickname    string          `json:"nickname"`
	Source      string          `json:"source,omitempty"`
	Rate        int             `json:"rate"`
	TotalRebate model.Money     `json:"total_rebate"`
	CreatedAt   int64           `json:"created_at"`
	Rebate      []*model.Ledger `json:"-"`
}

// Referrals lists the invitees of the requester's shop.
func (s *Service) Referrals(c *gin.Context) ([]*ReferralView, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	referrals, err := s.store.ListReferrals(c, shop.UID, 100, 0)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list referrals", err)
	}

	hasher := s.Hasher()
	res := make([]*ReferralView, 0, len(referrals))
	for _, r := range referrals {
		view := &ReferralView{
			InviteeUID:  hashid.EncodeUserID(hasher, r.InviteeUID),
			Source:      r.Source,
			Rate:        r.Rate,
			TotalRebate: r.TotalRebate,
			CreatedAt:   r.CreatedAt,
		}
		if invitee, err := s.dep.UserClient().GetByID(c, r.InviteeUID); err == nil {
			view.Nickname = invitee.Nick
		}
		res = append(res, view)
	}
	return res, nil
}

// OwnerUIDOfShop resolves the site user owning a shop.
func (s *Service) OwnerUIDOfShop(c *gin.Context, shopID string) (int, error) {
	shop, err := s.store.GetShopByID(c, shopID)
	if err != nil {
		return 0, serializer.NewError(serializer.CodeNotFound, "Shop not found", err)
	}
	return shop.UID, nil
}
