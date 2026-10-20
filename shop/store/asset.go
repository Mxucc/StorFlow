package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Mxucc/StorFlow/shop/model"
)

// assetMetaColumns lists the metadata columns, i.e. everything but the bytes.
// Listings never load the payload so a gallery of a hundred images stays cheap.
const assetMetaColumns = `id, uid, name, mime, size, created_at`

// CreateAsset stores an image.
func (s *Store) CreateAsset(ctx context.Context, a *model.Asset) error {
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	_, err := s.conn().exec(ctx, `INSERT INTO crshop_asset (id, uid, name, mime, size, data, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.UID, a.Name, a.Mime, a.Size, a.Data, a.CreatedAt)
	return wrapErr("shop: failed to save asset", err)
}

// GetAsset loads one asset including its bytes.
func (s *Store) GetAsset(ctx context.Context, id string) (*model.Asset, error) {
	a := &model.Asset{}
	err := s.conn().queryRow(ctx, `SELECT `+assetMetaColumns+`, data FROM crshop_asset WHERE id = ?`, id).
		Scan(&a.ID, &a.UID, &a.Name, &a.Mime, &a.Size, &a.CreatedAt, &a.Data)
	if err != nil {
		return nil, wrapErr("shop: failed to load asset", err)
	}
	return a, nil
}

// ListAssetsByUID lists the metadata of the assets of one user, newest first.
func (s *Store) ListAssetsByUID(ctx context.Context, uid, limit int) ([]*model.Asset, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.conn().query(ctx, `SELECT `+assetMetaColumns+` FROM crshop_asset
		WHERE uid = ? ORDER BY created_at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, wrapErr("shop: failed to list assets", err)
	}
	defer rows.Close()

	out := make([]*model.Asset, 0, limit)
	for rows.Next() {
		a := &model.Asset{}
		if err := rows.Scan(&a.ID, &a.UID, &a.Name, &a.Mime, &a.Size, &a.CreatedAt); err != nil {
			return nil, wrapErr("shop: failed to scan asset", err)
		}
		out = append(out, a)
	}
	return out, wrapErr("shop: failed to list assets", rows.Err())
}

// ListStaleAssets returns the metadata of assets created before the given
// timestamp. It is used by the garbage collector of the module.
func (s *Store) ListStaleAssets(ctx context.Context, before int64, limit int) ([]*model.Asset, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.conn().query(ctx, `SELECT `+assetMetaColumns+` FROM crshop_asset
		WHERE created_at < ? ORDER BY created_at ASC LIMIT ?`, before, limit)
	if err != nil {
		return nil, wrapErr("shop: failed to list stale assets", err)
	}
	defer rows.Close()

	out := make([]*model.Asset, 0, limit)
	for rows.Next() {
		a := &model.Asset{}
		if err := rows.Scan(&a.ID, &a.UID, &a.Name, &a.Mime, &a.Size, &a.CreatedAt); err != nil {
			return nil, wrapErr("shop: failed to scan asset", err)
		}
		out = append(out, a)
	}
	return out, wrapErr("shop: failed to list stale assets", rows.Err())
}

// CountAssetReferences counts how many shops and products still point at the
// given asset. The identifiers are random, so a LIKE match is exact enough and
// works on every supported dialect without a second join table.
func (s *Store) CountAssetReferences(ctx context.Context, id string) (int, error) {
	needle := "%" + id + "%"
	var shops, products int
	err := s.conn().queryRow(ctx,
		`SELECT COUNT(*) FROM crshop_shop WHERE avatar LIKE ? OR cover LIKE ? OR theme LIKE ?`,
		needle, needle, needle).Scan(&shops)
	if err != nil {
		return 0, wrapErr("shop: failed to count asset references", err)
	}
	err = s.conn().queryRow(ctx, `SELECT COUNT(*) FROM crshop_product WHERE cover LIKE ?`, needle).Scan(&products)
	if err != nil {
		return 0, wrapErr("shop: failed to count asset references", err)
	}
	return shops + products, nil
}

// DeleteAsset removes one asset.
func (s *Store) DeleteAsset(ctx context.Context, id string) error {
	_, err := s.conn().exec(ctx, `DELETE FROM crshop_asset WHERE id = ?`, id)
	return wrapErr(fmt.Sprintf("shop: failed to delete asset %s", id), err)
}
