package service

import (
	"context"
	"time"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/inventory/types"
	"github.com/Mxucc/StorFlow/pkg/fileguard"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/sharegate"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/Mxucc/StorFlow/shop/store"
)

const (
	// RetractionWindow is how long a seller may still take back a file that was
	// added to a folder which was already sold. After that window the file has
	// been synchronised by the buyers and can no longer be removed.
	RetractionWindow = 24 * time.Hour

	// guardDescendDepth / guardDescendBudget bound the subtree scan that runs
	// when a folder is deleted: a folder that contains a sold product must not
	// disappear either.
	guardDescendDepth  = 4
	guardDescendBudget = 300
)

// GuardDeletion implements pkg/fileguard: it keeps the promise made to buyers
// that the content they paid for stays available.
//
// Rules, in order:
//
//  1. a folder that was sold can never be deleted (the seller stops selling by
//     switching the product off, which keeps every buyer working);
//  2. a file that already existed when the product was first sold can never be
//     deleted: it was delivered to the buyers;
//  3. a file added after the first sale can be taken back within
//     RetractionWindow, and is protected afterwards;
//  4. a folder that (transitively, within a small depth) contains a sold product
//     cannot be deleted.
func (s *Service) GuardDeletion(ctx context.Context, file *ent.File, hard bool, ancestors []*ent.File) *fileguard.Decision {
	if file == nil {
		return nil
	}

	// 1. the folder itself was sold (as a product, or as a paid share with at
	// least one buyer).
	if sold, err := s.soldContent(ctx, file.ID); err != nil {
		s.l.Warning("Deletion guard: failed to inspect file %d: %s", file.ID, err)
		return nil
	} else if sold {
		return &fileguard.Decision{
			Allowed: false,
			Reason:  "该文件夹已经售出，不能删除。如需停止销售请在「我的小店」中下架，已购用户仍可继续访问。",
		}
	}

	// 2./3. the file lives inside content that was sold.
	for _, ancestor := range ancestors {
		sold, err := s.soldContent(ctx, ancestor.ID)
		if err != nil {
			s.l.Warning("Deletion guard: failed to inspect file %d: %s", ancestor.ID, err)
			continue
		}
		if !sold {
			continue
		}

		firstSale, err := s.firstSaleAt(ctx, ancestor.ID)
		if err != nil || firstSale == 0 {
			// Unknown history: refuse rather than risk taking content away from
			// a buyer.
			return &fileguard.Decision{
				Allowed: false,
				Reason:  "该文件夹已经售出，其中的内容不能删除。",
			}
		}

		// Timestamps have a one second resolution: anything created in the same
		// second as the first sale is treated as "existed before the sale",
		// which is the safe side of the trade off.
		if file.CreatedAt.Unix() <= firstSale {
			return &fileguard.Decision{
				Allowed: false,
				Reason:  "该内容在首次售出前就已存在，已经交付给买家，不能删除。",
			}
		}

		if time.Since(file.CreatedAt) > RetractionWindow {
			return &fileguard.Decision{
				Allowed: false,
				Reason:  "新增内容已超过 24 小时撤回时效，买家可能已经同步，不能删除。",
			}
		}

		// Inside the retraction window: the seller may take the addition back.
		return &fileguard.Decision{Allowed: true}
	}

	// 4. a folder that contains sold content.
	if types.FileType(file.Type) == types.FileTypeFolder && s.subtreeHasSoldContent(ctx, file.ID) {
		return &fileguard.Decision{
			Allowed: false,
			Reason:  "该文件夹中包含已售出的商品，不能删除。请先下架对应商品。",
		}
	}

	return &fileguard.Decision{Allowed: true}
}

// soldContent reports whether the folder is the source of something that was
// already paid for: a product with sales, or a paid share with a buyer.
func (s *Service) soldContent(ctx context.Context, fileID int) (bool, error) {
	if fileID == 0 {
		return false, nil
	}

	if product, err := s.store.GetProductByFileIDAnyShop(ctx, fileID); err == nil {
		if product.Sales > 0 {
			return true, nil
		}
	} else if err != store.ErrNotFound {
		return false, err
	}

	return s.shareHasBuyer(ctx, fileID)
}

// firstSaleAt returns when the content of the folder was first paid for.
func (s *Service) firstSaleAt(ctx context.Context, fileID int) (int64, error) {
	if product, err := s.store.GetProductByFileIDAnyShop(ctx, fileID); err == nil && product.Sales > 0 {
		return s.store.FirstSaleAt(ctx, product.ID)
	} else if err != nil && err != store.ErrNotFound {
		return 0, err
	}

	shares, err := s.sharesOf(ctx, fileID)
	if err != nil {
		return 0, err
	}
	first := int64(0)
	for _, entity := range shares {
		objectID := hashid.EncodeShareID(s.Hasher(), entity.ID)
		at, err := s.store.FirstEntitlementAt(ctx, model.OrderKindShare, objectID)
		if err != nil {
			return 0, err
		}
		if at > 0 && (first == 0 || at < first) {
			first = at
		}
	}
	return first, nil
}

// shareHasBuyer reports whether any paid share pointing at the folder was bought.
func (s *Service) shareHasBuyer(ctx context.Context, fileID int) (bool, error) {
	shares, err := s.sharesOf(ctx, fileID)
	if err != nil {
		return false, err
	}

	for _, entity := range shares {
		if !sharegate.Payable(entity) {
			continue
		}
		objectID := hashid.EncodeShareID(s.Hasher(), entity.ID)
		at, err := s.store.FirstEntitlementAt(ctx, model.OrderKindShare, objectID)
		if err != nil {
			return false, err
		}
		if at > 0 {
			return true, nil
		}
	}
	return false, nil
}

// sharesOf lists the shares that point at a folder.
func (s *Service) sharesOf(ctx context.Context, fileID int) ([]*ent.Share, error) {
	res, err := s.dep.ShareClient().List(ctx, &inventory.ListShareArgs{
		PaginationArgs: &inventory.PaginationArgs{PageSize: 20, Page: 0},
		FileID:         fileID,
	})
	if err != nil {
		return nil, err
	}
	return res.Shares, nil
}

// subtreeHasSoldContent reports whether the folder contains sold content within
// a bounded depth. The scan is skipped entirely when nothing was ever sold,
// which is the common case.
func (s *Service) subtreeHasSoldContent(ctx context.Context, rootID int) bool {
	products, err := s.store.ListProductsWithSales(ctx, 100)
	if err != nil {
		s.l.Warning("Deletion guard: failed to list sold products: %s", err)
		return false
	}

	soldFolders := make(map[int]struct{}, len(products))
	for _, product := range products {
		soldFolders[product.FileID] = struct{}{}
	}

	fileClient := s.dep.FileClient()
	frontier := []int{rootID}
	visited := 1

	for depth := 0; depth < guardDescendDepth && len(frontier) > 0; depth++ {
		next := make([]int, 0, len(frontier))
		for _, id := range frontier {
			root, err := fileClient.GetByID(ctx, id)
			if err != nil {
				continue
			}

			res, err := fileClient.GetChildFiles(ctx, &inventory.ListFileParameters{
				PaginationArgs: &inventory.PaginationArgs{PageSize: 100, Page: 0},
			}, 0, root)
			if err != nil || res == nil {
				continue
			}

			for _, child := range res.Files {
				visited++
				if visited > guardDescendBudget {
					// Give up on a huge tree rather than blocking the request:
					// the sold product itself is still protected by rule 1.
					return false
				}
				if types.FileType(child.Type) != types.FileTypeFolder {
					continue
				}
				if _, ok := soldFolders[child.ID]; ok {
					return true
				}
				// A folder that a paid share points at counts as sold content
				// only when somebody actually bought it.
				if hasBuyer, err := s.shareHasBuyer(ctx, child.ID); err == nil && hasBuyer {
					return true
				}
				next = append(next, child.ID)
			}
		}
		frontier = next
	}

	return false
}
