package service

import (
	"context"
	"strings"

	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/shop/model"
	"github.com/gin-gonic/gin"
)

// BindReferralFromRegister is called by the host registration flow. ref is the
// inviter hash id carried by the share link a visitor registered through; it
// may be empty, in which case nothing happens.
//
// The function never fails the registration: a broken referral link must not
// prevent a user from signing up, so errors are only logged.
func (s *Service) BindReferralFromRegister(ctx context.Context, ref string, inviteeUID int) {
	ref = strings.TrimSpace(ref)
	if ref == "" || inviteeUID <= 0 {
		return
	}
	if !s.Settings().ReferralEnabled {
		return
	}

	inviter, err := s.Hasher().Decode(ref, hashid.UserID)
	if err != nil {
		s.l.Debug("Ignoring invalid referral code %q: %s", ref, err)
		return
	}
	if inviter == inviteeUID {
		return
	}

	if _, err := s.store.GetReferralByInvitee(ctx, inviteeUID); err == nil {
		// Already bound (for example by an earlier checkout).
		return
	}

	if err := s.bindReferral(ctx, inviter, inviteeUID, "register", ""); err != nil {
		s.l.Warning("Failed to bind referral %d -> %d: %s", inviter, inviteeUID, err)
	}
}

// bindReferral writes the invite relation. The unique key on the invitee makes
// the call idempotent.
func (s *Service) bindReferral(ctx context.Context, inviter, invitee int, source, objectID string) error {
	rate := s.referralRate(ctx, inviter)

	referral := &model.Referral{
		ID:         newID(),
		InviterUID: inviter,
		InviteeUID: invitee,
		Source:     source,
		ObjectID:   objectID,
		Rate:       rate,
	}
	return s.store.CreateReferral(ctx, referral)
}

// referralRate returns the rebate rate of an inviter: the shop level override
// when the inviter owns a shop, the module default otherwise.
func (s *Service) referralRate(ctx context.Context, inviterUID int) int {
	if shop, err := s.store.GetShopByUID(ctx, inviterUID); err == nil && shop.ReferrerRate > 0 {
		return shop.ReferrerRate
	}
	return s.Settings().ReferralPercent
}

// SetReferralRate changes the default rebate rate of the requester's shop.
func (s *Service) SetReferralRate(c *gin.Context, rate int) (*ShopView, error) {
	if rate < 0 || rate > 5000 {
		return nil, serializer.NewError(serializer.CodeParamErr, "Referral rate must be within 0%-50%", nil)
	}

	shop, err := s.OwnShop(c, false)
	if err != nil {
		return nil, err
	}

	shop.ReferrerRate = rate
	if err := s.store.UpdateShop(c, shop); err != nil {
		return nil, serializer.NewError(serializer.CodeDBError, "Failed to update the referral rate", err)
	}
	return s.buildShopView(c, shop)
}

// ReferralLink returns the share link suffix a seller should append so that new
// visitors are bound to the shop.
func (s *Service) ReferralLink(c *gin.Context) (string, error) {
	shop, err := s.OwnShop(c, false)
	if err != nil {
		return "", err
	}

	code := hashid.EncodeUserID(s.Hasher(), shop.UID)
	return s.BaseURL(c) + "/shop/" + shop.Slug + "?ref=" + code, nil
}
