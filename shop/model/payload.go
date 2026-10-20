package model

import (
	"fmt"
	"net/url"
	"strings"
)

// Theme is the decoration payload of a shop. It is stored as JSON and rendered
// by the storefront frontend; the backend only validates it.
type Theme struct {
	// Template selects one of the built-in storefront layouts.
	Template string `json:"template,omitempty"`
	// PrimaryColor is a hex color like "#377dff".
	PrimaryColor string `json:"primary_color,omitempty"`
	// BackgroundColor is a hex color.
	BackgroundColor string `json:"background_color,omitempty"`
	// BackgroundImage is an absolute URL or a site relative path.
	BackgroundImage string `json:"background_image,omitempty"`
	// Banner is the hero image of the storefront.
	Banner string `json:"banner,omitempty"`
	// Logo replaces the site logo inside the storefront.
	Logo string `json:"logo,omitempty"`
	// Slogan is the headline shown under the shop name.
	Slogan string `json:"slogan,omitempty"`
	// Notice is an announcement bar; supports plain text only.
	Notice string `json:"notice,omitempty"`
	// Layout is the ordered list of storefront sections.
	Layout []string `json:"layout,omitempty"`
	// Links are extra navigation entries.
	Links []ThemeLink `json:"links,omitempty"`
	// Footer is the footer text.
	Footer string `json:"footer,omitempty"`
}

// ThemeLink is a custom navigation entry of the storefront.
type ThemeLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

const (
	maxThemeText  = 512
	maxThemeURL   = 2048
	maxThemeLinks = 12
	// MaxProductDescription caps the Markdown product page. Generous on
	// purpose: a tutorial with tables, screenshots and links is still text.
	MaxProductDescription = 200_000
)

// 商品介绍的素材（图片 / 视频）可见范围。卖家在商品编辑页三选一：
//
//	public    任何人，含未登录访客与搜索引擎（默认，介绍页才可能被收录）；
//	login     仅登录用户；
//	purchased 仅已购买该商品的用户（教程、进阶内容放这里）。
const (
	DescriptionVisibilityPublic    = "public"
	DescriptionVisibilityLogin     = "login"
	DescriptionVisibilityPurchased = "purchased"
)

// storefrontSections is the whitelist of layout sections.
var storefrontSections = map[string]struct{}{
	"banner":   {},
	"products": {},
	"about":    {},
	"notice":   {},
	"links":    {},
	"contact":  {},
}

// Normalize validates the theme and trims it into a canonical shape.
func (t *Theme) Normalize() error {
	if t == nil {
		return nil
	}

	textFields := []*string{&t.Template, &t.Slogan, &t.Notice, &t.Footer}
	for _, f := range textFields {
		if len(*f) > maxThemeText {
			return fmt.Errorf("theme field too long")
		}
		*f = strings.TrimSpace(*f)
	}

	if t.Template != "" && !strings.Contains("default minimal magazine", t.Template) {
		return fmt.Errorf("unsupported theme template %q", t.Template)
	}

	for name, value := range map[string]*string{
		"primary_color":    &t.PrimaryColor,
		"background_color": &t.BackgroundColor,
	} {
		v := strings.TrimSpace(*value)
		if v == "" {
			continue
		}
		if !isHexColor(v) {
			return fmt.Errorf("invalid %s: %q", name, v)
		}
		*value = v
	}

	for name, value := range map[string]*string{
		"background_image": &t.BackgroundImage,
		"banner":           &t.Banner,
		"logo":             &t.Logo,
	} {
		if err := validateResourceURL(value); err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
	}

	var layout []string
	for _, section := range t.Layout {
		s := strings.TrimSpace(section)
		if _, ok := storefrontSections[s]; !ok {
			return fmt.Errorf("unknown layout section %q", s)
		}
		layout = append(layout, s)
	}
	t.Layout = layout

	if len(t.Links) > maxThemeLinks {
		return fmt.Errorf("too many theme links")
	}
	links := make([]ThemeLink, 0, len(t.Links))
	for _, link := range t.Links {
		link.Label = strings.TrimSpace(link.Label)
		link.URL = strings.TrimSpace(link.URL)
		if link.Label == "" || len(link.Label) > 32 {
			return fmt.Errorf("invalid link label")
		}
		if err := validateHTTPURL(link.URL); err != nil {
			return fmt.Errorf("invalid link url: %w", err)
		}
		links = append(links, link)
	}
	t.Links = links

	return nil
}

// ProductProps is the auxiliary payload of a product.
type ProductProps struct {
	// Summary is a short marketing sentence.
	Summary string `json:"summary,omitempty"`
	// Tags are free-form labels, at most 8 entries of 16 chars.
	Tags []string `json:"tags,omitempty"`
	// TransferName overrides the folder name created in the buyer's drive.
	TransferName string `json:"transfer_name,omitempty"`
	// AllowPreview disables the public folder preview when false.
	AllowPreview *bool `json:"allow_preview,omitempty"`
	// PreviewLimit caps the number of entries returned by one preview page.
	PreviewLimit int `json:"preview_limit,omitempty"`
	// UpdateNotice is shown to buyers whenever the folder content changes.
	UpdateNotice string `json:"update_notice,omitempty"`
	// Sort orders products inside one shop, larger first.
	Sort int `json:"sort,omitempty"`
	// ForceRefresh is a monotonically increasing revision bumped by the owner to
	// notify buyers that the folder changed.
	Revision int `json:"revision,omitempty"`
	// DescriptionVisibility gates who may load the assets referenced by the
	// product description (see Product.Description): the Markdown tutorial
	// images and demo videos signed by shop/service/productasset.go.
	// One of the DescriptionVisibility* constants.
	DescriptionVisibility string `json:"description_visibility,omitempty"`
}

// PreviewAllowed reports whether the folder structure may be listed publicly.
func (p *ProductProps) PreviewAllowed() bool {
	if p == nil || p.AllowPreview == nil {
		return true
	}
	return *p.AllowPreview
}

// EffectivePreviewLimit returns the page size used by the preview endpoint.
func (p *ProductProps) EffectivePreviewLimit() int {
	if p == nil || p.PreviewLimit <= 0 {
		return 200
	}
	if p.PreviewLimit > 500 {
		return 500
	}
	return p.PreviewLimit
}

// EffectiveDescriptionVisibility returns the asset visibility of the product
// page, defaulting to public so an unset payload keeps the SEO friendly
// behaviour.
func (p *ProductProps) EffectiveDescriptionVisibility() string {
	if p == nil {
		return DescriptionVisibilityPublic
	}
	switch p.DescriptionVisibility {
	case DescriptionVisibilityLogin, DescriptionVisibilityPurchased:
		return p.DescriptionVisibility
	default:
		return DescriptionVisibilityPublic
	}
}

// Normalize validates the payload.
func (p *ProductProps) Normalize() error {
	if p == nil {
		return nil
	}
	p.Summary = strings.TrimSpace(p.Summary)
	p.TransferName = strings.TrimSpace(p.TransferName)
	p.UpdateNotice = strings.TrimSpace(p.UpdateNotice)
	if len(p.Summary) > 500 {
		return fmt.Errorf("summary too long")
	}
	if len(p.TransferName) > 100 {
		return fmt.Errorf("transfer name too long")
	}
	if len(p.UpdateNotice) > 500 {
		return fmt.Errorf("update notice too long")
	}
	p.DescriptionVisibility = strings.TrimSpace(p.DescriptionVisibility)
	switch p.DescriptionVisibility {
	case "", DescriptionVisibilityPublic, DescriptionVisibilityLogin, DescriptionVisibilityPurchased:
	default:
		return fmt.Errorf("unknown description visibility %q", p.DescriptionVisibility)
	}
	if len(p.Tags) > 8 {
		return fmt.Errorf("too many tags")
	}
	tags := make([]string, 0, len(p.Tags))
	for _, tag := range p.Tags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		if len(t) > 16 {
			return fmt.Errorf("tag too long")
		}
		tags = append(tags, t)
	}
	p.Tags = tags
	return nil
}

// WithdrawalAccount describes where the money should be sent.
type WithdrawalAccount struct {
	// Channel is one of "alipay", "wxpay", "bank".
	Channel string `json:"channel"`
	// Name is the real name of the payee.
	Name string `json:"name"`
	// Account is the alipay account / bank card number.
	Account string `json:"account"`
	// Bank is the bank name, required for the "bank" channel.
	Bank string `json:"bank,omitempty"`
	// Remark is an optional note from the shop owner.
	Remark string `json:"remark,omitempty"`
}

// Normalize validates the withdrawal account.
func (a *WithdrawalAccount) Normalize() error {
	a.Channel = strings.TrimSpace(a.Channel)
	a.Name = strings.TrimSpace(a.Name)
	a.Account = strings.TrimSpace(a.Account)
	a.Bank = strings.TrimSpace(a.Bank)
	a.Remark = strings.TrimSpace(a.Remark)

	switch a.Channel {
	case "alipay", "wxpay":
	case "bank":
		if a.Bank == "" {
			return fmt.Errorf("bank name is required")
		}
	default:
		return fmt.Errorf("unsupported withdrawal channel %q", a.Channel)
	}
	if a.Name == "" || len(a.Name) > 64 {
		return fmt.Errorf("invalid payee name")
	}
	if a.Account == "" || len(a.Account) > 128 {
		return fmt.Errorf("invalid payee account")
	}
	if len(a.Remark) > 200 {
		return fmt.Errorf("remark too long")
	}
	return nil
}

func isHexColor(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func validateResourceURL(v *string) error {
	s := strings.TrimSpace(*v)
	*v = s
	if s == "" {
		return nil
	}
	if len(s) > maxThemeURL {
		return fmt.Errorf("url too long")
	}
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") {
		return nil
	}
	return validateHTTPURL(s)
}

func validateHTTPURL(raw string) error {
	if raw == "" || len(raw) > maxThemeURL {
		return fmt.Errorf("invalid url length")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("url host is required")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("url must not contain line breaks")
	}
	return nil
}
