package user

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Mxucc/StorFlow/application/dependency"
	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/ent/user"
	"github.com/Mxucc/StorFlow/inventory"
	"github.com/Mxucc/StorFlow/pkg/auth"
	"github.com/Mxucc/StorFlow/pkg/cluster/routes"
	"github.com/Mxucc/StorFlow/pkg/email"
	"github.com/Mxucc/StorFlow/pkg/hashid"
	"github.com/Mxucc/StorFlow/pkg/serializer"
	"github.com/Mxucc/StorFlow/pkg/setting"
	"github.com/Mxucc/StorFlow/pkg/shoplink"
	"github.com/Mxucc/StorFlow/pkg/util"
	"github.com/gin-gonic/gin"
)

// RegisterParameterCtx define key fore UserRegisterService
type RegisterParameterCtx struct{}

// UserRegisterService 管理用户注册的服务
type UserRegisterService struct {
	UserName string `form:"email" json:"email" binding:"required,email"`
	Password string `form:"password" json:"password" binding:"required,min=6,max=128"`
	Language string `form:"language" json:"language"`
	// Ref is the inviter hash id carried by a share link (网盘小店返利).
	Ref string `form:"ref" json:"ref"`
}

// Register 新用户注册
func (service *UserRegisterService) Register(c *gin.Context) serializer.Response {
	dep := dependency.FromContext(c)
	settings := dep.SettingProvider()

	isEmailRequired := settings.EmailActivationEnabled(c)
	args := &inventory.NewUserArgs{
		Email:         strings.ToLower(service.UserName),
		PlainPassword: service.Password,
		Status:        user.StatusActive,
		GroupID:       settings.DefaultGroup(c),
		Language:      service.Language,
	}

	// Pro：注册邮箱域过滤与子地址限制，仅作用于注册流程；SSO 登录、重置密码不受影响。
	if settings.DisableSubAddressEmail(c) && strings.Contains(args.Email, "+") {
		return serializer.ErrWithDetails(c, serializer.CodeEmailProviderBaned,
			"Email addresses containing '+' are not allowed for registration", nil)
	}
	if mode := settings.FilterEmailProvider(c); mode != setting.EmailProviderFilterDisabled {
		if rule := settings.FilterEmailProviderRule(c); len(rule) > 0 {
			domain := ""
			if at := strings.LastIndex(args.Email, "@"); at >= 0 {
				domain = args.Email[at+1:]
			}
			matched := slices.Contains(rule, domain)
			if (mode == setting.EmailProviderFilterWhitelist && !matched) ||
				(mode == setting.EmailProviderFilterBlacklist && matched) {
				return serializer.ErrWithDetails(c, serializer.CodeEmailProviderBaned,
					"Registration with this email domain is not allowed", nil)
			}
		}
	}
	if isEmailRequired {
		args.Status = user.StatusInactive
	}
	// 站点级「初始分享快捷方式」：注册时把这些分享写进新用户的侧边栏置顶。
	if pined := ResolveSiteDefaultPins(c, dep, settings.DefaultSymbolics(c)); len(pined) > 0 {
		args.Pined = pined
	}

	userClient := dep.UserClient()
	uc, tx, _, err := inventory.WithTx(c, userClient)
	if err != nil {
		return serializer.DBErr(c, "Failed to start transaction", err)
	}

	expectedUser, err := uc.Create(c, args)
	if expectedUser != nil {
		util.WithValue(c, inventory.UserCtx{}, expectedUser)
	}

	if err != nil {
		_ = inventory.Rollback(tx)
		if errors.Is(err, inventory.ErrUserEmailExisted) {
			return serializer.ErrWithDetails(c, serializer.CodeEmailExisted, "Email already in use", err)
		}

		if errors.Is(err, inventory.ErrInactiveUserExisted) {
			if err := sendActivationEmail(c, dep, expectedUser); err != nil {
				return serializer.ErrWithDetails(c, serializer.CodeNotSet, "", err)
			}

			return serializer.ErrWithDetails(c, serializer.CodeEmailSent, "User is not activated, activation email has been resent", nil)
		}

		return serializer.DBErr(c, "Failed to insert user row", err)
	}

	if err := inventory.Commit(tx); err != nil {
		return serializer.DBErr(c, "Failed to commit user row", err)
	}

	// 网盘小店：记录邀请关系（资源页注册的用户视为邀请，享受后续返利）。
	// 通过 shoplink 钩子转发，避免 service/user 反向依赖 shop 包；
	// 未安装钩子（未启用小店）时为空操作。
	shoplink.BindReferral(c, service.Ref, expectedUser.ID)

	if isEmailRequired {
		if err := sendActivationEmail(c, dep, expectedUser); err != nil {
			return serializer.ErrWithDetails(c, serializer.CodeNotSet, "", err)
		}
		return serializer.Response{Code: serializer.CodeNotFullySuccess}
	}

	return serializer.Response{Data: BuildUser(expectedUser, dep.HashIDEncoder())}
}

func sendActivationEmail(ctx context.Context, dep dependency.Dep, newUser *ent.User) error {
	base := dep.SettingProvider().SiteURL(ctx)
	userID := hashid.EncodeUserID(dep.HashIDEncoder(), newUser.ID)
	ttl := time.Now().Add(time.Duration(24) * time.Hour)
	activateURL, err := auth.SignURI(ctx, dep.GeneralAuth(), routes.MasterUserActivateAPIUrl(base, userID).String(), &ttl)
	if err != nil {
		return serializer.NewError(serializer.CodeEncryptError, "Failed to sign the activation link", err)
	}

	// 取得签名
	credential := activateURL.Query().Get("sign")

	// 生成对用户访问的激活地址
	finalURL := routes.MasterUserActivateUrl(base)
	queries := finalURL.Query()
	queries.Add("id", userID)
	queries.Add("sign", credential)
	finalURL.RawQuery = queries.Encode()

	// 返送激活邮件
	title, body, err := email.NewActivationEmail(ctx, dep.SettingProvider(), newUser, finalURL.String())
	if err != nil {
		return serializer.NewError(serializer.CodeFailedSendEmail, "Failed to send activation email", err)
	}

	if err := dep.EmailClient(ctx).Send(ctx, newUser.Email, title, body); err != nil {
		return serializer.NewError(serializer.CodeFailedSendEmail, "Failed to send activation email", err)
	}

	return nil
}

// ActivateUser 激活用户
func ActivateUser(c *gin.Context) serializer.Response {
	uid := hashid.FromContext(c)
	dep := dependency.FromContext(c)
	userClient := dep.UserClient()

	// 查找待激活用户
	inactiveUser, err := userClient.GetByID(c, uid)
	if err != nil {
		return serializer.ErrWithDetails(c, serializer.CodeUserNotFound, "User not fount", err)
	}

	// 检查状态
	if inactiveUser.Status != user.StatusInactive {
		return serializer.ErrWithDetails(c, serializer.CodeUserCannotActivate, "This user cannot be activated", nil)
	}

	// 激活用户
	activeUser, err := userClient.SetStatus(c, inactiveUser, user.StatusActive)
	if err != nil {
		return serializer.DBErr(c, "Failed to update user", err)
	}

	util.WithValue(c, inventory.UserCtx{}, activeUser)
	return serializer.Response{Data: BuildUser(activeUser, dep.HashIDEncoder())}
}
