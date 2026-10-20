package model

import "testing"

func TestPercentOff(t *testing.T) {
	cases := []struct {
		amount  Money
		percent int
		want    Money
	}{
		{1000, 0, 1000},   // no promotion
		{1000, 100, 1000}, // invalid promotion is ignored
		{1000, 10, 900},   // 10% off
		{999, 50, 500},    // rounds half up
		{1, 50, 1},        // never drops below one cent
		{0, 50, 0},
	}
	for _, c := range cases {
		if got := PercentOff(c.amount, c.percent); got != c.want {
			t.Fatalf("PercentOff(%d, %d) = %d, want %d", c.amount, c.percent, got, c.want)
		}
	}
}

func TestFixedOff(t *testing.T) {
	if got := FixedOff(1000, 300); got != 700 {
		t.Fatalf("FixedOff(1000, 300) = %d", got)
	}
	if got := FixedOff(100, 500); got != 0 {
		t.Fatalf("a coupon larger than the order must clamp to zero, got %d", got)
	}
	if got := FixedOff(100, 0); got != 100 {
		t.Fatalf("FixedOff(100, 0) = %d", got)
	}
}

func TestApplyRate(t *testing.T) {
	cases := []struct {
		amount Money
		bps    int
		want   Money
	}{
		{10000, 300, 300}, // 3% platform fee
		{10000, 1000, 1000},
		{1, 5000, 1}, // rounds up to one cent
		{10000, 0, 0},
		{0, 300, 0},
	}
	for _, c := range cases {
		if got := ApplyRate(c.amount, c.bps); got != c.want {
			t.Fatalf("ApplyRate(%d, %d) = %d, want %d", c.amount, c.bps, got, c.want)
		}
	}
}

func TestClampPayable(t *testing.T) {
	if got := ClampPayable(0); got != MinPrice {
		t.Fatalf("ClampPayable(0) = %d, want %d", got, MinPrice)
	}
	if got := ClampPayable(MaxPrice + 1); got != MaxPrice {
		t.Fatalf("ClampPayable(over) = %d, want %d", got, MaxPrice)
	}
	if got := ClampPayable(1234); got != 1234 {
		t.Fatalf("ClampPayable(1234) = %d", got)
	}
}

func TestMoneyStringAndParse(t *testing.T) {
	for _, c := range []struct {
		cents Money
		text  string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{1230, "12.30"},
		{-1230, "-12.30"},
	} {
		if got := c.cents.String(); got != c.text {
			t.Fatalf("%d renders as %q, want %q", c.cents, got, c.text)
		}

		parsed, err := ParseYuan(c.text)
		if err != nil {
			t.Fatalf("ParseYuan(%q) failed: %v", c.text, err)
		}
		if parsed != c.cents {
			t.Fatalf("ParseYuan(%q) = %d, want %d", c.text, parsed, c.cents)
		}
	}

	if _, err := ParseYuan(""); err == nil {
		t.Fatal("an empty amount must not parse")
	}
	if _, err := ParseYuan("abc"); err == nil {
		t.Fatal("a non numeric amount must not parse")
	}
}

func TestProductEffectivePrice(t *testing.T) {
	product := &Product{Price: 1000, Discount: 20, DiscountEnd: 100}
	if got := product.EffectivePrice(50); got != 800 {
		t.Fatalf("promoted price = %d, want 800", got)
	}
	if got := product.EffectivePrice(200); got != 1000 {
		t.Fatalf("an expired promotion must fall back to the base price, got %d", got)
	}

	product.DiscountEnd = 0
	if got := product.EffectivePrice(1 << 40); got != 800 {
		t.Fatalf("a promotion without an end date must stay active, got %d", got)
	}
}

func TestThemeNormalize(t *testing.T) {
	valid := &Theme{
		Template:        "default",
		PrimaryColor:    "#377dff",
		BackgroundImage: "/static/bg.png",
		Layout:          []string{"banner", "products"},
		Links:           []ThemeLink{{Label: "Docs", URL: "https://docs.example.com"}},
	}
	if err := valid.Normalize(); err != nil {
		t.Fatalf("a valid theme must normalize: %v", err)
	}

	bad := []*Theme{
		{PrimaryColor: "red"},
		{BackgroundImage: "javascript:alert(1)"},
		{Layout: []string{"unknown"}},
		{Links: []ThemeLink{{Label: "x", URL: "ftp://example.com"}}},
		{Links: []ThemeLink{{Label: "", URL: "https://example.com"}}},
	}
	for i, theme := range bad {
		if err := theme.Normalize(); err == nil {
			t.Fatalf("case %d must be rejected", i)
		}
	}
}

func TestProductPropsNormalize(t *testing.T) {
	props := &ProductProps{Summary: "  hi  ", Tags: []string{" a ", "", "b"}}
	if err := props.Normalize(); err != nil {
		t.Fatalf("Normalize() failed: %v", err)
	}
	if props.Summary != "hi" || len(props.Tags) != 2 {
		t.Fatalf("unexpected normalization result %+v", props)
	}

	limit := &ProductProps{PreviewLimit: 5000}
	if got := limit.EffectivePreviewLimit(); got != 500 {
		t.Fatalf("preview limit = %d, want the 500 cap", got)
	}

	disabled := false
	props.AllowPreview = &disabled
	if props.PreviewAllowed() {
		t.Fatal("preview must be disabled when explicitly switched off")
	}
	if (&ProductProps{}).PreviewAllowed() != true {
		t.Fatal("preview must default to enabled")
	}
}

func TestWithdrawalAccountNormalize(t *testing.T) {
	alipay := &WithdrawalAccount{Channel: "alipay", Name: "张三", Account: "a@b.com"}
	if err := alipay.Normalize(); err != nil {
		t.Fatalf("a valid alipay account must normalize: %v", err)
	}

	bank := &WithdrawalAccount{Channel: "bank", Name: "张三", Account: "6222", Bank: "ICBC"}
	if err := bank.Normalize(); err != nil {
		t.Fatalf("a valid bank account must normalize: %v", err)
	}

	for _, bad := range []*WithdrawalAccount{
		{Channel: "paypal", Name: "x", Account: "y"},
		{Channel: "bank", Name: "x", Account: "y"},
		{Channel: "alipay", Name: "", Account: "y"},
	} {
		if err := bad.Normalize(); err == nil {
			t.Fatalf("account %+v must be rejected", bad)
		}
	}
}
