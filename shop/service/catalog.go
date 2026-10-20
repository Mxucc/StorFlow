package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/filemanager/fs"
	"github.com/Mxucc/StorFlow/pkg/filemanager/manager"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/pkg/sharegate"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
	"github.com/gin-gonic/gin"
)

// ProductSummary is the storefront projection of a product.
type ProductSummary struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Summary     string      `json:"summary,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Cover       string      `json:"cover,omitempty"`
	Price       model.Money `json:"price"`
	FinalPrice  model.Money `json:"final_price"`
	Discount    int         `json:"discount,omitempty"`
	DiscountEnd int64       `json:"discount_end,omitempty"`
	Sales       int         `json:"sales"`
	Views       int         `json:"views"`
	Status      string      `json:"status"`
	CreatedAt   int64       `json:"created_at"`
	Shop        string      `json:"shop,omitempty"`
	ShopName    string      `json:"shop_name,omitempty"`
	Previewable bool        `json:"previewable"`
	Owned       bool        `json:"owned"`
	URL         string      `json:"url,omitempty"`
}

// ProductDetail is the buyer facing projection of a product.
type ProductDetail struct {
	*ProductSummary
	Description string `json:"description"`
	// DescriptionVisibility tells the page who may load the images and videos
	// embedded in Description: public, login or purchased.
	DescriptionVisibility string `json:"description_visibility"`
	FileCount             int    `json:"-"`
	CanPreview            bool   `json:"can_preview"`
	OwnerShopUID          string `json:"-"`
}

// CreateProduct lists a folder of the owner's drive.
func (s *Service) CreateProduct(c *gin.Context, req *CreateProductRequest) (*ProductSummary, error) {
	if err := s.CheckEnabled(); err != nil {
		return nil, err
	}

	user, err := RequireLogin(c)
	if err != nil {
		return nil, err
	}

	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	uri, err := fs.NewUriFromString(strings.TrimSpace(req.Uri))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Unknown folder", err)
	}

	m := manager.NewFileManager(s.dep, user)
	defer m.Recycle()

	folder, err := m.Get(c, uri)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Folder not found", err)
	}
	if folder.Type() != types.FileTypeFolder {
		return nil, serializer.NewError(serializer.CodeParamErr, "Only folders can be sold", nil)
	}
	if folder.OwnerID() != user.ID {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "You can only sell your own folder", nil)
	}
	if folder.IsRootFolder() {
		return nil, serializer.NewError(serializer.CodeParamErr, "The root folder cannot be sold", nil)
	}

	if _, err := s.store.GetProductByFileID(c, shop.ID, folder.ID()); err == nil {
		return nil, serializer.NewError(serializer.CodeConflict, "This folder is already on sale", nil)
	} else if err != store.ErrNotFound {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to check product", err)
	}

	if req.Price < int64(model.MinPrice) || req.Price > int64(model.MaxPrice) {
		return nil, serializer.NewError(serializer.CodeParamErr, "Price out of range", nil)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = folder.Name()
	}
	if name == "" || len([]rune(name)) > 100 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid product name", nil)
	}

	props := req.Props
	if err := props.Normalize(); err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid product props", err)
	}

	// The internal share is the only credential able to read the sold folder.
	// Its random password never leaves the server: buyers receive a linked
	// folder instead (see deliverProduct).
	//
	// The share is created as a PAID share on purpose: the address of the
	// folder is stored in the buyer's drive metadata, so it is readable by the
	// buyer and could be leaked. Marking it paid makes the share gate require
	// an entitlement even when somebody knows the address, which is what keeps
	// a buyer from republishing the folder for free.
	password := newToken(8)
	share, err := m.CreateOrUpdateShare(c, uri, &manager.CreateShareArgs{
		IsPrivate:  true,
		Password:   password,
		ShareView:  true,
		Permission: types.SharePermissionRead,
		PayEnabled: true,
		Price:      req.Price,
		Internal:   true,
	})
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to prepare the folder", err)
	}

	status := model.ProductStatusOnSale
	if strings.TrimSpace(req.Status) != "" {
		candidate := model.ProductStatus(strings.TrimSpace(req.Status))
		if !candidate.Valid() || candidate == model.ProductStatusRemoved {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid product status", nil)
		}
		status = candidate
	}

	product := &model.Product{
		ID:            newID(),
		ShopID:        shop.ID,
		FileID:        folder.ID(),
		ShareID:       share.ID,
		SharePassword: password,
		Name:          name,
		Description:   strings.TrimSpace(req.Description),
		Cover:         strings.TrimSpace(req.Cover),
		Price:         model.Money(req.Price),
		Status:        status,
		Props:         props,
	}

	if err := s.store.CreateProduct(c, product); err != nil {
		// Roll the internal share back so a failed listing leaves no trace.
		_ = s.dep.ShareClient().Delete(c, share.ID)
		if store.IsConflict(err) {
			return nil, serializer.NewError(serializer.CodeConflict, "This folder is already on sale", err)
		}
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to create product", err)
	}

	return s.productSummary(c, product, s.Hasher()), nil
}

// UpdateProduct patches a product.
func (s *Service) UpdateProduct(c *gin.Context, id string, req *UpdateProductRequest) (*ProductSummary, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	product, err := s.ownProduct(c, shop.ID, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 100 {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid product name", nil)
		}
		product.Name = name
	}
	if req.Description != nil {
		// The description is a Markdown product page: tutorials with headings,
		// tables and screenshots, so the plain-text cap of the early version no
		// longer fits.
		desc := strings.TrimSpace(*req.Description)
		if len(desc) > model.MaxProductDescription {
			return nil, serializer.NewError(serializer.CodeParamErr, "Description is too long", nil)
		}
		product.Description = desc
	}
	if req.Cover != nil {
		product.Cover = strings.TrimSpace(*req.Cover)
	}
	if req.Price != nil {
		if *req.Price < int64(model.MinPrice) || *req.Price > int64(model.MaxPrice) {
			return nil, serializer.NewError(serializer.CodeParamErr, "Price out of range", nil)
		}
		product.Price = model.Money(*req.Price)
	}
	if req.Discount != nil {
		if *req.Discount < 0 || *req.Discount >= 100 {
			return nil, serializer.NewError(serializer.CodeParamErr, "Discount must be within 0%-99%", nil)
		}
		product.Discount = *req.Discount
	}
	if req.DiscountEnd != nil {
		product.DiscountEnd = *req.DiscountEnd
	}
	if req.Status != nil {
		status := model.ProductStatus(strings.TrimSpace(*req.Status))
		if !status.Valid() {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid product status", nil)
		}
		// Off-sale keeps every entitlement and the internal share alive, so
		// existing buyers keep receiving updates. Removing a product is only
		// allowed once nothing was sold, to protect the buyers.
		if status == model.ProductStatusRemoved && product.Sales > 0 {
			return nil, serializer.NewError(serializer.CodeParamErr,
				"This product already has sales, switch it off instead of removing it", nil)
		}
		product.Status = status
	}
	if req.Props != nil {
		props := *req.Props
		if err := props.Normalize(); err != nil {
			return nil, serializer.NewError(serializer.CodeParamErr, "Invalid product props", err)
		}
		product.Props = props
	}

	if err := s.store.UpdateProduct(c, product); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update product", err)
	}

	// The share carries the price of the product for the paywall; keep it in
	// sync. A failure here is not fatal: the gate only needs PayEnabled, and
	// the entitlement lookup never depends on the price.
	s.syncProductShare(c, product)

	return s.productSummary(c, product, s.Hasher()), nil
}

// syncProductShare refreshes the internal share of a product after the seller
// changed its price or its description.
func (s *Service) syncProductShare(ctx context.Context, product *model.Product) {
	loadCtx := context.WithValue(ctx, inventory.LoadShareFile{}, true)
	entity, err := s.dep.ShareClient().GetByID(loadCtx, product.ShareID)
	if err != nil {
		s.l.Warning("Failed to load the internal share of product %s: %s", product.ID, err)
		return
	}

	props := &types.ShareProps{
		ShareView:  true,
		PayEnabled: true,
		Price:      int64(product.Price),
		Internal:   true,
	}
	if entity.Props != nil {
		props.ShowReadMe = entity.Props.ShowReadMe
		props.Permission = entity.Props.Permission
		props.Grants = entity.Props.Grants
		props.PayCurrency = entity.Props.PayCurrency
	}

	if _, err := s.dep.ShareClient().Upsert(ctx, &inventory.CreateShareParams{
		Existed: entity,
		Props:   props,
	}); err != nil {
		s.l.Warning("Failed to update the internal share of product %s: %s", product.ID, err)
	}
}

// ListMyProducts returns the products of the requester's shop.
func (s *Service) ListMyProducts(c *gin.Context, limit, offset int) ([]*ProductSummary, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	products, err := s.store.ListProductsByShop(c, shop.ID, false, limit, offset)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to list products", err)
	}

	hasher := s.Hasher()
	res := make([]*ProductSummary, 0, len(products))
	for _, p := range products {
		res = append(res, s.productSummary(c, p, hasher))
	}
	return res, nil
}

// ProductDetailFor returns the public detail page of a product.
func (s *Service) ProductDetailFor(c *gin.Context, slug, id string) (*ProductDetail, error) {
	shop, err := s.PublicShop(c, slug)
	if err != nil {
		return nil, err
	}

	product, err := s.store.GetProductByID(c, id)
	if err != nil || product.ShopID != shop.ID {
		return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", err)
	}

	user := CurrentUser(c)
	owned := false
	if user != nil && user.ID != 0 {
		// A revoked entitlement (a refunded order) must not look like ownership:
		// the buyer has to be able to buy the product again.
		if entitlement, err := s.store.GetEntitlement(c, user.ID, model.OrderKindProduct, product.ID); err == nil &&
			entitlement.Status != model.EntitlementStatusRevoked {
			owned = true
		}
	}

	// A product that is not on sale is only visible to its owner and to the
	// buyers who already paid for it.
	if !product.Status.Purchasable() && !owned && (user == nil || user.ID != shop.UID) {
		return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", nil)
	}

	summary := s.productSummary(c, product, s.Hasher())
	summary.Owned = owned

	return &ProductDetail{
		ProductSummary:        summary,
		Description:           product.Description,
		DescriptionVisibility: product.Props.EffectiveDescriptionVisibility(),
		CanPreview:            product.Status == model.ProductStatusOnSale || owned,
		OwnerShopUID:          hashid.EncodeUserID(s.Hasher(), shop.UID),
	}, nil
}

// PreviewEntry is one row of the folder preview.
type PreviewEntry struct {
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Size      int64       `json:"size"`
	UpdatedAt int64       `json:"updated_at"`
	Path      string      `json:"path,omitempty"`
	Children  int         `json:"children,omitempty"`
	Price     model.Money `json:"-"`
	Ext       string      `json:"ext,omitempty"`
}

// Preview lists the folder content of a product without exposing any way to
// download or save it: only metadata plus opaque path tokens are returned.
func (s *Service) Preview(c *gin.Context, slug, id, pathToken string) ([]*PreviewEntry, error) {
	product, shop, err := s.publicProduct(c, slug, id)
	if err != nil {
		return nil, err
	}

	user := CurrentUser(c)
	owned := false
	if user != nil && user.ID != 0 {
		// A revoked entitlement (a refunded order) must not look like ownership:
		// the buyer has to be able to buy the product again.
		if entitlement, err := s.store.GetEntitlement(c, user.ID, model.OrderKindProduct, product.ID); err == nil &&
			entitlement.Status != model.EntitlementStatusRevoked {
			owned = true
		}
	}

	if !owned && !product.Props.PreviewAllowed() {
		return nil, serializer.NewError(serializer.CodeNoPermissionErr, "Preview is disabled for this product", nil)
	}

	sub, err := decodePathToken(pathToken)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "Invalid path", err)
	}

	uri, err := fs.NewUriFromString(fs.NewShareUri(hashid.EncodeShareID(s.Hasher(), product.ShareID), product.SharePassword))
	if err != nil {
		return nil, serializer.NewError(serializer.CodeInternalSetting, "Invalid folder", err)
	}
	if sub != "" {
		uri = uri.Join(sub)
	}

	viewer, err := s.previewUser(c)
	if err != nil {
		return nil, err
	}

	m := manager.NewFileManager(s.dep, viewer)
	defer m.Recycle()

	// The storefront preview is metadata only and is scoped to this very share
	// and request: it lets the paid-share gate open for a listing without
	// granting any download.
	ctx := sharegate.WithPreviewAllowance(c, product.ShareID)

	_, listing, err := m.List(ctx, uri, &manager.ListArgs{Page: 1, PageSize: product.Props.EffectivePreviewLimit()})
	if err != nil {
		return nil, serializer.NewError(serializer.CodeNotFound, "Folder is not available", err)
	}

	// Count the visit once, and only for the root of the product, so a deep
	// walk cannot be used to hammer the database with writes.
	if sub == "" {
		_ = s.store.IncreaseProductViews(c, product.ID, 1)
	}
	_ = shop

	entries := make([]*PreviewEntry, 0, len(listing.Files))
	for _, f := range listing.Files {
		entry := &PreviewEntry{
			Name:      f.Name(),
			Type:      "file",
			Size:      f.Size(),
			UpdatedAt: f.UpdatedAt().Unix(),
			Ext:       f.Ext(),
		}
		if f.Type() == types.FileTypeFolder {
			entry.Type = "folder"
			entry.Path = encodePathToken(strings.Trim(sub+"/"+f.Name(), "/"))
			if summary := f.FolderSummary(); summary != nil {
				entry.Children = summary.Folders + summary.Files
			}
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// deliverProduct materialises a purchase into the buyer's drive as a linked
// folder: a symbolic folder whose only content is the address of the product
// share. Nothing is copied, so the seller's later additions show up for the
// buyer immediately, and an off-sale product keeps working for its buyers.
func (s *Service) deliverProduct(ctx context.Context, buyer *entUser, shopID string, product *model.Product, orderNo string) (int, error) {
	ownerUID, err := s.ShopUID(ctx, shopID)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve the shop owner: %w", err)
	}

	redirect := fs.NewShareUri(hashid.EncodeShareID(s.Hasher(), product.ShareID), product.SharePassword)
	ownerHash := hashid.EncodeUserID(s.Hasher(), ownerUID)

	name := strings.TrimSpace(product.Props.TransferName)
	if name == "" {
		name = product.Name
	}

	m := manager.NewFileManager(s.dep, buyer)
	defer m.Recycle()

	root, err := fs.NewUriFromString(fs.NewMyUri(hashid.EncodeUserID(s.Hasher(), buyer.ID)))
	if err != nil {
		return 0, fmt.Errorf("failed to build the buyer root: %w", err)
	}

	created, err := m.Create(ctx, root.Join(name), types.FileTypeFolder, fs.WithMetadata(map[string]string{
		dbfsMetadataSharedRedirect: redirect,
		dbfsMetadataSharedOwner:    ownerHash,
	}))
	if err != nil {
		return 0, fmt.Errorf("failed to create the delivery folder: %w", err)
	}

	_ = orderNo
	return created.ID(), nil
}

// previewUser returns the user object used to walk a product folder: the
// requester when logged in, the anonymous user otherwise. The shop owner is
// never impersonated, the internal share already carries the read permission.
func (s *Service) previewUser(c *gin.Context) (*entUser, error) {
	user := CurrentUser(c)
	if user != nil && user.ID != 0 {
		return user, nil
	}

	anonymous, err := s.dep.UserClient().AnonymousUser(c)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to resolve the visitor", err)
	}
	return anonymous, nil
}

// publicProduct resolves a product that is visible on a storefront.
func (s *Service) publicProduct(c *gin.Context, slug, id string) (*model.Product, *model.Shop, error) {
	shop, err := s.PublicShop(c, slug)
	if err != nil {
		return nil, nil, err
	}

	product, err := s.store.GetProductByID(c, id)
	if err != nil || product.ShopID != shop.ID {
		return nil, nil, serializer.NewError(serializer.CodeNotFound, "Product not found", err)
	}
	if product.Status != model.ProductStatusOnSale {
		user := CurrentUser(c)
		if user == nil || user.ID != shop.UID {
			return nil, nil, serializer.NewError(serializer.CodeNotFound, "Product not found", nil)
		}
	}
	return product, shop, nil
}

// ownProduct loads a product and checks that it belongs to the given shop.
func (s *Service) ownProduct(c *gin.Context, shopID, id string) (*model.Product, error) {
	product, err := s.store.GetProductByID(c, id)
	if err != nil || product.ShopID != shopID {
		return nil, serializer.NewError(serializer.CodeNotFound, "Product not found", err)
	}
	return product, nil
}

// productSummary projects a product for the storefront.
func (s *Service) productSummary(c *gin.Context, p *model.Product, hasher hashid.Encoder) *ProductSummary {
	now := time.Now().Unix()
	return &ProductSummary{
		ID:          p.ID,
		Name:        p.Name,
		Summary:     p.Props.Summary,
		Tags:        p.Props.Tags,
		Cover:       p.Cover,
		Price:       p.Price,
		FinalPrice:  p.EffectivePrice(now),
		Discount:    p.Discount,
		DiscountEnd: p.DiscountEnd,
		Sales:       p.Sales,
		Views:       p.Views,
		Status:      string(p.Status),
		CreatedAt:   p.CreatedAt,
		Previewable: p.Props.PreviewAllowed(),
	}
}
