package epay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testConfig() Config {
	return Config{APIURL: "https://pay.example.com/", PID: "1001", Key: "secret"}
}

func TestSignMatchesProtocolOrdering(t *testing.T) {
	// The signature is the MD5 of the ASCII sorted "k=v" pairs with the
	// merchant key appended. The expected value below is precomputed so that a
	// regression in the ordering or in the exclusions fails the test.
	params := map[string]string{
		"pid":          "1001",
		"out_trade_no": "T1",
		"money":        "1.00",
		// Excluded by the protocol.
		"sign":      "ignored",
		"sign_type": "MD5",
		"empty":     "",
	}

	if got, want := Sign(params, "secret"), "5db577b883bc92b98c337321356ab0d5"; got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
}

func TestSignIsStableForDifferentMapOrder(t *testing.T) {
	first := map[string]string{"b": "2", "a": "1", "c": "3"}
	second := map[string]string{"c": "3", "a": "1", "b": "2"}
	if Sign(first, "k") != Sign(second, "k") {
		t.Fatal("Sign() must not depend on map iteration order")
	}
}

func TestVerify(t *testing.T) {
	cfg := testConfig()

	params := map[string]string{"pid": "1001", "out_trade_no": "T1", "money": "1.00"}
	params["sign"] = Sign(params, cfg.Key)
	params["sign_type"] = SignTypeMD5

	if err := cfg.Verify(params); err != nil {
		t.Fatalf("Verify() rejected a valid signature: %v", err)
	}

	tampered := map[string]string{"pid": "1001", "out_trade_no": "T1", "money": "9.99"}
	tampered["sign"] = params["sign"]
	if err := cfg.Verify(tampered); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify() accepted a tampered payload: %v", err)
	}

	if err := cfg.Verify(map[string]string{"pid": "1001"}); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify() accepted a payload without sign: %v", err)
	}
}

func TestPayURL(t *testing.T) {
	cfg := testConfig()
	raw, err := cfg.PayURL(PayRequest{
		TradeNo:   "T1",
		Type:      "alipay",
		Name:      "A folder",
		Money:     "12.30",
		NotifyURL: "https://site.example.com/api/v4/shop/epay/notify",
		ReturnURL: "https://site.example.com/shop/order/T1",
		Param:     "ticket",
		ClientIP:  "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("PayURL() failed: %v", err)
	}

	if !strings.HasPrefix(raw, "https://pay.example.com/submit.php?") {
		t.Fatalf("unexpected cashier URL %q", raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("failed to parse cashier URL: %v", err)
	}
	query := parsed.Query()
	for k, want := range map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": "T1",
		"money":        "12.30",
		"name":         "A folder",
		"sign_type":    SignTypeMD5,
	} {
		if got := query.Get(k); got != want {
			t.Fatalf("param %s = %q, want %q", k, got, want)
		}
	}

	// The generated signature must verify against the very same parameters.
	if err := cfg.Verify(CollectParams(query, nil)); err != nil {
		t.Fatalf("generated signature does not verify: %v", err)
	}
}

func TestPayURLRejectsIncompleteRequests(t *testing.T) {
	cfg := testConfig()
	if _, err := cfg.PayURL(PayRequest{Money: "1.00"}); !errors.Is(err, ErrBadOrder) {
		t.Fatalf("expected ErrBadOrder, got %v", err)
	}
	if _, err := cfg.PayURL(PayRequest{TradeNo: "T1", Money: "abc"}); err == nil {
		t.Fatal("expected an error for a non numeric amount")
	}

	if _, err := (Config{}).PayURL(PayRequest{TradeNo: "T1", Money: "1.00"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestParseNotify(t *testing.T) {
	cfg := testConfig()

	base := func() map[string]string {
		params := map[string]string{
			"pid":          "1001",
			"out_trade_no": "20240101",
			"trade_no":     "EP1",
			"name":         "Test",
			"money":        "12.30",
			"trade_status": TradeStatusSuccess,
			"type":         "alipay",
			"param":        "ticket",
		}
		params["sign"] = Sign(params, cfg.Key)
		params["sign_type"] = SignTypeMD5
		return params
	}

	notify, err := cfg.ParseNotify(base(), 1230)
	if err != nil {
		t.Fatalf("ParseNotify() failed: %v", err)
	}
	if notify.OutTradeNo != "20240101" || notify.TradeNo != "EP1" || notify.Param != "ticket" {
		t.Fatalf("unexpected notify payload %+v", notify)
	}
	if notify.Raw["pid"] != "1001" {
		t.Fatal("raw parameters must be preserved")
	}
	if cents, err := notify.AmountCents(); err != nil || cents != 1230 {
		t.Fatalf("AmountCents() = %d, %v", cents, err)
	}

	// Amount mismatch: the gateway paid less than the order asked for.
	if _, err := cfg.ParseNotify(base(), 9999); !errors.Is(err, ErrBadAmount) {
		t.Fatalf("expected ErrBadAmount, got %v", err)
	}

	// Unpaid notification.
	unpaid := base()
	unpaid["trade_status"] = "TRADE_CLOSED"
	unpaid["sign"] = Sign(unpaid, cfg.Key)
	if _, err := cfg.ParseNotify(unpaid, 0); !errors.Is(err, ErrBadStatus) {
		t.Fatalf("expected ErrBadStatus, got %v", err)
	}

	// Tampered amount invalidates the signature.
	tampered := base()
	tampered["money"] = "0.01"
	if _, err := cfg.ParseNotify(tampered, 0); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("expected ErrBadSignature, got %v", err)
	}

	// Missing order number.
	missing := base()
	delete(missing, "out_trade_no")
	missing["sign"] = Sign(missing, cfg.Key)
	if _, err := cfg.ParseNotify(missing, 0); !errors.Is(err, ErrBadOrder) {
		t.Fatalf("expected ErrBadOrder, got %v", err)
	}
}

func TestParseYuan(t *testing.T) {
	cases := map[string]int64{
		"0":      0,
		"1":      100,
		"1.5":    150,
		"1.50":   150,
		"12.345": 1234,
		"100.00": 10000,
		".5":     50,
	}
	for raw, want := range cases {
		got, err := ParseYuan(raw)
		if err != nil {
			t.Fatalf("ParseYuan(%q) failed: %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseYuan(%q) = %d, want %d", raw, got, want)
		}
	}

	for _, raw := range []string{"", "abc", "1.2.3"} {
		if _, err := ParseYuan(raw); err == nil {
			t.Fatalf("ParseYuan(%q) should fail", raw)
		}
	}
}

func TestQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiPath {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("act") != "order" || q.Get("pid") != "1001" || q.Get("key") != "secret" ||
			q.Get("out_trade_no") != "T1" {
			t.Errorf("unexpected query %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"msg":"ok","status":1,"money":"1.00","out_trade_no":"T1","trade_no":"EP1"}`))
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.APIURL = server.URL

	res, err := cfg.Query(context.Background(), "T1")
	if err != nil {
		t.Fatalf("Query() failed: %v", err)
	}
	if !res.Paid() || res.TradeNo != "EP1" {
		t.Fatalf("unexpected query result %+v", res)
	}
}

func TestNormalizeRejectsUnsupportedSignType(t *testing.T) {
	cfg := testConfig()
	cfg.SignType = "RSA"
	if _, err := cfg.Normalize(); err == nil {
		t.Fatal("expected an error for an unsupported sign type")
	}
}

func TestCollectParamsPrefersFormValues(t *testing.T) {
	query := url.Values{"a": {"1"}, "b": {"2"}}
	form := url.Values{"b": {"3"}}
	params := CollectParams(query, form)
	if params["a"] != "1" || params["b"] != "3" {
		t.Fatalf("unexpected params %v", params)
	}
}
