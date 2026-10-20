package email

import (
	"context"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/Mxucc/StorFlow/ent"
	"github.com/Mxucc/StorFlow/pkg/setting"
)

type CommonContext struct {
	SiteBasic *setting.SiteBasic
	Logo      *setting.Logo
	SiteUrl   string
}

// ResetContext used for variables in reset email
type ResetContext struct {
	*CommonContext
	User *ent.User
	Url  string
}

// NewResetEmail generates reset email from template
func NewResetEmail(ctx context.Context, settings setting.Provider, user *ent.User, url string) (string, string, error) {
	templates := settings.ResetEmailTemplate(ctx)
	if len(templates) == 0 {
		return "", "", fmt.Errorf("reset email template not configured")
	}

	selected := selectTemplate(templates, user)
	resetCtx := ResetContext{
		CommonContext: commonContext(ctx, settings),
		User:          user,
		Url:           url,
	}

	tmplTitle, err := template.New("resetTitle").Parse(selected.Title)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse email title: %w", err)
	}

	var resTitle strings.Builder
	err = tmplTitle.Execute(&resTitle, resetCtx.templateData())
	if err != nil {
		return "", "", fmt.Errorf("failed to execute email title: %w", err)
	}

	tmplBody, err := template.New("resetBody").Parse(selected.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse email template: %w", err)
	}

	var resBody strings.Builder
	err = tmplBody.Execute(&resBody, resetCtx.templateData())
	if err != nil {
		return "", "", fmt.Errorf("failed to execute email template: %w", err)
	}

	return resTitle.String(), resBody.String(), nil
}

// ActivationContext used for variables in activation email
type ActivationContext struct {
	*CommonContext
	User *ent.User
	Url  string
}

// NewActivationEmail generates activation email from template
func NewActivationEmail(ctx context.Context, settings setting.Provider, user *ent.User, url string) (string, string, error) {
	templates := settings.ActivationEmailTemplate(ctx)
	if len(templates) == 0 {
		return "", "", fmt.Errorf("activation email template not configured")
	}

	selected := selectTemplate(templates, user)
	activationCtx := ActivationContext{
		CommonContext: commonContext(ctx, settings),
		User:          user,
		Url:           url,
	}

	tmplTitle, err := template.New("activationTitle").Parse(selected.Title)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse email title: %w", err)
	}

	var resTitle strings.Builder
	err = tmplTitle.Execute(&resTitle, activationCtx.templateData())
	if err != nil {
		return "", "", fmt.Errorf("failed to execute email title: %w", err)
	}

	tmplBody, err := template.New("activationBody").Parse(selected.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse email template: %w", err)
	}

	var resBody strings.Builder
	err = tmplBody.Execute(&resBody, activationCtx.templateData())
	if err != nil {
		return "", "", fmt.Errorf("failed to execute email template: %w", err)
	}

	return resTitle.String(), resBody.String(), nil
}

func commonContext(ctx context.Context, settings setting.Provider) *CommonContext {
	logo := settings.Logo(ctx)
	siteUrl := settings.SiteURL(ctx)
	resolvedLogo := *logo

	// Add site url if logo is not an url
	if !strings.HasPrefix(logo.Light, "http") {
		logoPath, _ := url.Parse(logo.Light)
		resolvedLogo.Light = siteUrl.ResolveReference(logoPath).String()
	}

	if !strings.HasPrefix(logo.Normal, "http") {
		logoPath, _ := url.Parse(logo.Normal)
		resolvedLogo.Normal = siteUrl.ResolveReference(logoPath).String()
	}

	return &CommonContext{
		SiteBasic: settings.SiteBasic(ctx),
		Logo:      &resolvedLogo,
		SiteUrl:   siteUrl.String(),
	}
}

func (c *CommonContext) templateData() map[string]any {
	return map[string]any{
		"SiteBasic": map[string]string{
			"Name":        c.SiteBasic.Name,
			"Title":       c.SiteBasic.Title,
			"ID":          c.SiteBasic.ID,
			"Description": c.SiteBasic.Description,
			"Script":      c.SiteBasic.Script,
		},
		"Logo": map[string]string{
			"Normal": c.Logo.Normal,
			"Light":  c.Logo.Light,
		},
		"SiteUrl": c.SiteUrl,
	}
}

func (c ResetContext) templateData() map[string]any {
	return map[string]any{
		"CommonContext": c.CommonContext.templateData(),
		"User":          c.User,
		"Url":           c.Url,
	}
}

func (c ActivationContext) templateData() map[string]any {
	return map[string]any{
		"CommonContext": c.CommonContext.templateData(),
		"User":          c.User,
		"Url":           c.Url,
	}
}

// ReceiptContext used for variables in payment receipt email
type ReceiptContext struct {
	*CommonContext
	User  *ent.User
	Url   string
	Order *ReceiptOrder
}

// ReceiptOrder is the order summary rendered inside a receipt email.
type ReceiptOrder struct {
	OrderNo     string
	ProductName string
	Amount      string
	Quantity    int64
	PaidAt      string
}

func (c ReceiptContext) templateData() map[string]any {
	order := c.Order
	if order == nil {
		order = &ReceiptOrder{}
	}

	return map[string]any{
		"CommonContext": c.CommonContext.templateData(),
		"User":          c.User,
		"Url":           c.Url,
		"Order": map[string]any{
			"OrderNo":     order.OrderNo,
			"ProductName": order.ProductName,
			"Amount":      order.Amount,
			"Quantity":    order.Quantity,
			"PaidAt":      order.PaidAt,
		},
	}
}

// QuotaContext used for variables in storage quota exceed email
type QuotaContext struct {
	*CommonContext
	User  *ent.User
	Url   string
	Quota *QuotaDetail
}

// QuotaDetail describes the storage usage rendered inside a quota email.
type QuotaDetail struct {
	Used    string
	Total   string
	Percent string
}

func (c QuotaContext) templateData() map[string]any {
	quota := c.Quota
	if quota == nil {
		quota = &QuotaDetail{}
	}

	return map[string]any{
		"CommonContext": c.CommonContext.templateData(),
		"User":          c.User,
		"Url":           c.Url,
		"Quota": map[string]any{
			"Used":    quota.Used,
			"Total":   quota.Total,
			"Percent": quota.Percent,
		},
	}
}

// NewReceiptEmail generates the payment receipt email from template.
func NewReceiptEmail(
	ctx context.Context,
	settings setting.Provider,
	user *ent.User,
	url string,
	order *ReceiptOrder,
) (string, string, error) {
	templates := settings.ReceiptEmailTemplate(ctx)
	if len(templates) == 0 {
		return "", "", fmt.Errorf("receipt email template not configured")
	}

	receiptCtx := ReceiptContext{
		CommonContext: commonContext(ctx, settings),
		User:          user,
		Url:           url,
		Order:         order,
	}

	return renderEmail(templates, user, "receipt", receiptCtx.templateData())
}

// NewExceedQuotaEmail generates the storage quota exceed email from template.
func NewExceedQuotaEmail(
	ctx context.Context,
	settings setting.Provider,
	user *ent.User,
	url string,
	used, total int64,
) (string, string, error) {
	templates := settings.ExceedQuotaEmailTemplate(ctx)
	if len(templates) == 0 {
		return "", "", fmt.Errorf("quota exceed email template not configured")
	}

	quotaCtx := QuotaContext{
		CommonContext: commonContext(ctx, settings),
		User:          user,
		Url:           url,
		Quota: &QuotaDetail{
			Used:    FormatSize(used),
			Total:   FormatSize(total),
			Percent: UsedPercent(used, total),
		},
	}

	return renderEmail(templates, user, "quota", quotaCtx.templateData())
}

// renderEmail selects the template matching the user's language and renders it.
func renderEmail(templates []setting.EmailTemplate, user *ent.User, name string, data map[string]any) (string, string, error) {
	selected := selectTemplate(templates, user)

	tmplTitle, err := template.New(name + "Title").Parse(selected.Title)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse email title: %w", err)
	}

	var resTitle strings.Builder
	if err := tmplTitle.Execute(&resTitle, data); err != nil {
		return "", "", fmt.Errorf("failed to execute email title: %w", err)
	}

	tmplBody, err := template.New(name + "Body").Parse(selected.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse email template: %w", err)
	}

	var resBody strings.Builder
	if err := tmplBody.Execute(&resBody, data); err != nil {
		return "", "", fmt.Errorf("failed to execute email template: %w", err)
	}

	return resTitle.String(), resBody.String(), nil
}

// FormatSize formats a byte size into a human readable string.
func FormatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

// UsedPercent returns the storage usage percentage capped at 100%.
func UsedPercent(used, total int64) string {
	if total <= 0 {
		return "100%"
	}

	percent := used * 100 / total
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}

	return fmt.Sprintf("%d%%", percent)
}

func selectTemplate(templates []setting.EmailTemplate, u *ent.User) setting.EmailTemplate {
	selected := templates[0]
	if u != nil && u.Settings != nil {
		for _, t := range templates {
			if strings.EqualFold(t.Language, u.Settings.Language) {
				selected = t
				break
			}
		}
	}

	return selected
}
