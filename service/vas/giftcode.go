package vas

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/giftcode"
	"github.com/Mxucc/StorFlow/ent/pointsledger"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/eventtype"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/gin-gonic/gin"
	"time"
)

// AdminCreateGiftCodesParamCtx defines the context key.
type AdminCreateGiftCodesParamCtx struct{}

// AdminCreateGiftCodesService is the payload for batch gift code creation.
type AdminCreateGiftCodesService struct {
	Count int `json:"count" binding:"min=1,max=1000"`
	// ProductType is "points" | "storage" | "group" | "balance".
	ProductType string `json:"product_type" binding:"required,eq=points|eq=storage|eq=group|eq=balance"`
	// Num is the amount granted by points/storage/group cards.
	Num int64 `json:"num"`
	// Amount is the face value of a balance card, in cents.
	Amount int64 `json:"amount"`
	// ExpiresAt is the Unix time the cards expire; 0 means "never".
	ExpiresAt int64  `json:"expires_at"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
}

// AdminCreateGiftCodes generates gift codes in batch.
func AdminCreateGiftCodes(c *gin.Context) serializer.Response {
	dep := dependency.FromContext(c)
	db := dep.DBClient()
	user := inventory.UserFromContext(c)

	var req AdminCreateGiftCodesService
	if err := c.ShouldBindJSON(&req); err != nil {
		return serializer.ParamErr(c, "Invalid request", err)
	}

	// A balance card carries its face value in Amount, the other card types use
	// Num: accepting both would silently create worthless cards.
	if req.ProductType == ProductTypeBalance {
		if req.Amount <= 0 {
			return serializer.Err(c, serializer.NewError(serializer.CodeParamErr, "卡密面额必须大于 0", nil))
		}
	} else if req.Num <= 0 {
		return serializer.Err(c, serializer.NewError(serializer.CodeParamErr, "请填写卡密的数量或容量", nil))
	}
	if req.ExpiresAt > 0 && req.ExpiresAt <= time.Now().Unix() {
		return serializer.Err(c, serializer.NewError(serializer.CodeParamErr, "有效期必须晚于当前时间", nil))
	}

	created := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code := req.Prefix + genCode(12)
		creator := db.GiftCode.Create().
			SetCode(code).
			SetProductType(req.ProductType).
			SetNum(req.Num)
		if req.Amount > 0 {
			creator = creator.SetAmount(req.Amount)
		}
		if req.ExpiresAt > 0 {
			creator = creator.SetExpiresAt(req.ExpiresAt)
		}
		if req.Name != "" {
			creator = creator.SetName(req.Name)
		}
		if user != nil {
			creator = creator.SetCreatedByEmail(user.Email)
		}
		if _, err := creator.Save(c); err != nil {
			continue
		}
		created = append(created, code)
	}

	return serializer.Response{Data: map[string]interface{}{
		"codes": created,
		"count": len(created),
	}}
}

// AdminListGiftCodes lists gift codes.
func AdminListGiftCodes(c *gin.Context) serializer.Response {
	dep := dependency.FromContext(c)
	db := dep.DBClient()
	page, size := pageParams(c)

	q := db.GiftCode.Query()
	if used := c.Query("used"); used == "true" || used == "false" {
		q = q.Where(giftcode.UsedEQ(used == "true"))
	}
	total, _ := q.Clone().Count(c)
	codes, err := q.Order(ent.Desc(giftcode.FieldCreatedAt)).
		Limit(size).Offset((page - 1) * size).All(c)
	if err != nil {
		return serializer.DBErr(c, "Failed to list gift codes", err)
	}
	return serializer.Response{Data: map[string]interface{}{
		"gift_codes": codes,
		"pagination": map[string]interface{}{"total_items": total, "page": page - 1, "page_size": size},
	}}
}

// AdminDeleteGiftCodesParamCtx defines the context key.
type AdminDeleteGiftCodesParamCtx struct{}

// AdminDeleteGiftCodesService is the payload for batch deletion.
type AdminDeleteGiftCodesService struct {
	IDs []int `json:"ids" binding:"min=1"`
}

// AdminDeleteGiftCodesParamCtx defines the context key.

// Delete batch deletes gift codes（IDs 由路由层 FromJSON 绑定——勿在此重复消费 body）。
func (service *AdminDeleteGiftCodesService) Delete(c *gin.Context) serializer.Response {
	dep := dependency.FromContext(c)
	db := dep.DBClient()
	if _, err := db.GiftCode.Delete().Where(giftcode.IDIn(service.IDs...)).Exec(c); err != nil {
		return serializer.DBErr(c, "Failed to delete gift codes", err)
	}
	return serializer.Response{}
}

// Redeem redeems a gift code for the current user.
func Redeem(c *gin.Context) serializer.Response {
	dep := dependency.FromContext(c)
	db := dep.DBClient()
	user := inventory.UserFromContext(c)
	if user == nil {
		return serializer.Err(c, serializer.NewError(serializer.CodeNoPermissionErr, "Login required", nil))
	}

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return serializer.ParamErr(c, "Invalid request", err)
	}

	code, err := db.GiftCode.Query().Where(giftcode.CodeEQ(req.Code)).First(c)
	if err != nil {
		return serializer.Err(c, serializer.NewError(serializer.CodeNotFound, "兑换码不存在", err))
	}
	if code.Used {
		return serializer.Err(c, serializer.NewError(serializer.CodeParamErr, "兑换码已被使用", nil))
	}
	// An expired card stays unused in the database (an administrator can extend
	// its validity), it simply cannot be redeemed any more.
	if giftCodeExpired(code, time.Now().Unix()) {
		return serializer.Err(c, serializer.NewError(serializer.CodeParamErr, "兑换码已过期", nil))
	}

	// Fulfill + mark used in one transaction.
	tx, txErr := db.Tx(c)
	if txErr != nil {
		return serializer.DBErr(c, "Failed to start transaction", txErr)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Mark used with a conditional update: two concurrent redemptions of the same
	// card must not both succeed, which matters a lot more for a balance card
	// than for a points code.
	affected, err := tx.GiftCode.Update().
		Where(giftcode.IDEQ(code.ID), giftcode.UsedEQ(false)).
		SetUsed(true).
		SetUsedBy(user.ID).
		SetUsedByEmail(user.Email).
		Save(c)
	if err != nil {
		return serializer.DBErr(c, "Failed to redeem", err)
	}
	if affected == 0 {
		return serializer.Err(c, serializer.NewError(serializer.CodeParamErr, "兑换码已被使用", nil))
	}

	if err := applyGiftCodeReward(c, tx, user, code); err != nil {
		return serializer.Err(c, err)
	}

	if err := tx.Commit(); err != nil {
		return serializer.DBErr(c, "Failed to commit redemption", err)
	}
	committed = true

	auditWriter(c, db, user.ID, user.Email, eventtype.RedeemGiftCode, code.ProductType,
		code.Name, map[string]interface{}{"code": code.Code, "num": code.Num})

	reward := code.Num
	data := map[string]interface{}{
		"product_type": code.ProductType,
		"num":          reward,
		"name":         code.Name,
	}
	if code.ProductType == ProductTypeBalance {
		// A balance card reports the credited amount plus the balance after the
		// redemption, so the console can show "到账 ¥X，余额 ¥Y".
		data["amount"] = code.Amount
		data["balance"] = GetWalletBalance(c, db, user.ID)
	}
	return serializer.Response{Data: data}
}

// genCode generates a random alphanumeric code.
func genCode(length int) string {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, 0, length)
	for _, b := range buf {
		out = append(out, charset[int(b)%len(charset)])
	}
	return string(out)
}

// GiftCodeRewards exposes reward resolution for gift code redemption API.
func GiftCodeRewards(c *gin.Context, code string) (string, string, int64, bool) {
	dep := dependency.FromContext(c)
	db := dep.DBClient()
	gc, err := db.GiftCode.Query().Where(giftcode.CodeEQ(code)).First(c)
	if err != nil {
		return "", "", 0, false
	}
	return gc.ProductType, gc.Name, gc.Num, !gc.Used
}

var _ = context.Background()
var _ = strconv.Itoa
var _ = fmt.Sprintf

type (
	// RedeemService redeems a gift code for the current user.
	RedeemService struct {
		Code string `json:"code" binding:"required"`
	}
	RedeemParamCtx struct{}
)

// PointsSummary returns the user's points balance + recent history.
func PointsSummary(c *gin.Context) serializer.Response {
	dep := dependency.FromContext(c)
	db := dep.DBClient()
	user := inventory.UserFromContext(c)
	if user == nil {
		return serializer.Err(c, serializer.NewError(serializer.CodeNoPermissionErr, "Login required", nil))
	}
	balance := GetPointsBalance(c, db, user.ID)
	history, err := db.PointsLedger.Query().
		Where(pointsledger.UserIDEQ(user.ID)).
		Order(ent.Desc(pointsledger.FieldCreatedAt)).
		Limit(50).
		All(c)
	if err != nil {
		return serializer.DBErr(c, "Failed to load points history", err)
	}
	return serializer.Response{Data: map[string]interface{}{
		"balance": balance,
		"history": history,
	}}
}

// giftCodeExpired reports whether the card can no longer be redeemed at now
// (Unix seconds). A zero expiry never expires.
func giftCodeExpired(code *ent.GiftCode, now int64) bool {
	return code != nil && code.ExpiresAt > 0 && now > code.ExpiresAt
}

// applyGiftCodeReward grants the value of a redeemed card inside the redemption
// transaction, so a card is either fully applied or not consumed at all.
func applyGiftCodeReward(ctx context.Context, tx *ent.Tx, user *ent.User, code *ent.GiftCode) error {
	switch code.ProductType {
	case ProductTypePoints:
		_, err := GainPointsTx(ctx, tx, user.ID, code.Num, "redeem", code.Code)
		return err
	case ProductTypeStorage:
		return addExtraStorage(ctx, tx.Client(), user.ID, code.Num)
	case ProductTypeGroup:
		_, err := tx.User.UpdateOneID(user.ID).SetGroupUsers(int(code.Num)).Save(ctx)
		return err
	case ProductTypeBalance:
		_, err := CreditWalletTx(ctx, tx, user.ID, code.Amount, WalletReasonRecharge, code.Name, code.Code)
		return err
	default:
		return serializer.NewError(serializer.CodeParamErr, "未知的卡密类型", nil)
	}
}
