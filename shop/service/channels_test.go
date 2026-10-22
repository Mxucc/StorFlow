package service

import (
	"encoding/json"
	"testing"

	"github.com/Mxucc/StorFlow/shop/model"
)

// TestParseChannelConfigs covers the three shapes the epay_channels setting has
// had over time: the legacy comma separated list, the per-channel JSON and the
// empty value of the oldest installs.
func TestParseChannelConfigs(t *testing.T) {
	shared := model.PayChannelConfig{APIURL: "https://pay.shared.test/", PID: "1", Key: "shared-key"}

	// Legacy list: the channels are enabled and inherit the shared credentials.
	got := parseChannelConfigs("alipay,wxpay", shared)
	if len(got) != 2 {
		t.Fatalf("expected two channels, got %d (%+v)", len(got), got)
	}
	if got[0].Channel != model.ChannelAlipay || got[1].Channel != model.ChannelWxpay {
		t.Fatalf("unexpected channel order: %+v", got)
	}
	for _, item := range got {
		if !item.Enabled || item.PID != "1" || item.Key != "shared-key" {
			t.Fatalf("the legacy channel must inherit the shared credentials: %+v", item)
		}
	}

	// Per-channel JSON: every channel may point at a different provider, a
	// disabled channel still inherits the shared credentials so the editor can
	// show what would be used.
	raw := `[{"channel":"alipay","api_url":"https://pay.a.test/","pid":"10","key":"ka","enabled":true},` +
		`{"channel":"qqpay","enabled":false}]`
	got = parseChannelConfigs(raw, shared)
	if len(got) != 2 {
		t.Fatalf("expected two channels, got %d (%+v)", len(got), got)
	}
	alipay, ok := model.FindPayChannel(got, model.ChannelAlipay)
	if !ok || alipay.APIURL != "https://pay.a.test" || alipay.PID != "10" || alipay.Key != "ka" {
		t.Fatalf("the alipay entry must keep its own credentials: %+v", alipay)
	}
	qq, ok := model.FindPayChannel(got, model.ChannelQqpay)
	if !ok || qq.Enabled || qq.Key != "shared-key" {
		t.Fatalf("the disabled entry must stay disabled but inherit the key: %+v", qq)
	}

	// Empty value with complete shared credentials: all three channels work.
	got = parseChannelConfigs("", shared)
	if len(got) != len(model.PayChannelOrder) {
		t.Fatalf("expected the three channels, got %+v", got)
	}

	// Empty value without shared credentials: nothing is offered.
	if got := parseChannelConfigs("", model.PayChannelConfig{}); len(got) != 0 {
		t.Fatalf("expected no channel without credentials, got %+v", got)
	}
}

// TestChannelSecretsAreMasked checks that the admin view never returns a merchant
// key and that sending the mask back keeps the stored one.
func TestChannelSecretsAreMasked(t *testing.T) {
	stored := `[{"channel":"alipay","api_url":"https://pay.a.test/","pid":"10","key":"ka","enabled":true}]`

	masked := maskChannelSecrets(stored)
	var list []model.PayChannelConfig
	if err := json.Unmarshal([]byte(masked), &list); err != nil {
		t.Fatalf("maskChannelSecrets() produced invalid JSON: %v", err)
	}
	if len(list) != 1 || list[0].Key != maskedSecret {
		t.Fatalf("the key must be masked, got %+v", list)
	}

	// Sending the mask back keeps the stored secret.
	merged, err := mergeChannelSecrets(stored, masked)
	if err != nil {
		t.Fatalf("mergeChannelSecrets() failed: %v", err)
	}
	var restored []model.PayChannelConfig
	if err := json.Unmarshal([]byte(merged), &restored); err != nil {
		t.Fatalf("mergeChannelSecrets() produced invalid JSON: %v", err)
	}
	if len(restored) != 1 || restored[0].Key != "ka" {
		t.Fatalf("the stored key must survive the round trip, got %+v", restored)
	}

	// A new key replaces the old one.
	updated := `[{"channel":"alipay","api_url":"https://pay.a.test/","pid":"10","key":"kb","enabled":true}]`
	merged, err = mergeChannelSecrets(stored, updated)
	if err != nil {
		t.Fatalf("mergeChannelSecrets() failed: %v", err)
	}
	var replaced []model.PayChannelConfig
	if err := json.Unmarshal([]byte(merged), &replaced); err != nil {
		t.Fatalf("mergeChannelSecrets() produced invalid JSON: %v", err)
	}
	if len(replaced) != 1 || replaced[0].Key != "kb" {
		t.Fatalf("the new key must win, got %+v", replaced)
	}

	// Unknown channels are rejected.
	if _, err := mergeChannelSecrets(stored, `[{"channel":"paypal","enabled":true}]`); err == nil {
		t.Fatal("an unknown channel must be rejected")
	}
}

// TestShopChannelConfigsResolvePerChannel covers the shop level resolution: an
// epay account usually enables a single channel, so every channel carries its
// own credentials while the cashier only offers the ones that are complete.
func TestShopChannelConfigsResolvePerChannel(t *testing.T) {
	shop := &model.Shop{
		EpayAPIURL: "https://pay.shared.test/",
		EpayPID:    "1",
		EpayKey:    "shared-key",
		EpayChannels: []model.PayChannelConfig{
			{Channel: model.ChannelAlipay, APIURL: "https://pay.a.test/", PID: "10", Key: "ka", Enabled: true},
			// Enabled but without any credential of its own: inherits the shared
			// ones, which keeps the "one account for everything" setup working.
			{Channel: model.ChannelWxpay, Enabled: true},
			{Channel: model.ChannelQqpay, Enabled: false},
		},
	}

	list := shopChannelConfigs(shop)
	alipay, ok := model.FindPayChannel(list, model.ChannelAlipay)
	if !ok || alipay.APIURL != "https://pay.a.test" || alipay.PID != "10" || alipay.Key != "ka" {
		t.Fatalf("alipay must keep its own gateway: %+v", alipay)
	}
	wxpay, ok := model.FindPayChannel(list, model.ChannelWxpay)
	if !ok || wxpay.APIURL != "https://pay.shared.test" || wxpay.PID != "1" || wxpay.Key != "shared-key" {
		t.Fatalf("wxpay must inherit the shared gateway: %+v", wxpay)
	}

	ready := readyChannels(list)
	if len(ready) != 2 || ready[0] != string(model.ChannelAlipay) || ready[1] != string(model.ChannelWxpay) {
		t.Fatalf("the cashier must offer the two enabled channels only, got %+v", ready)
	}

	// A shop that never configured a channel keeps the shared credentials.
	legacy := &model.Shop{EpayAPIURL: "https://pay.shared.test/", EpayPID: "1", EpayKey: "shared-key"}
	if ready := readyChannels(shopChannelConfigs(legacy)); len(ready) != len(model.PayChannelOrder) {
		t.Fatalf("a legacy shop must offer every channel, got %+v", ready)
	}
}

// TestMergeChannelSecretsValidatesGatewayURL rejects a broken per-channel URL so
// the buyer never hits a gateway that cannot be reached.
func TestMergeChannelSecretsValidatesGatewayURL(t *testing.T) {
	stored := `[{"channel":"alipay","api_url":"https://pay.a.test/","pid":"10","key":"ka","enabled":true}]`

	if _, err := mergeChannelSecrets(stored, `[{"channel":"alipay","api_url":"not-a-url","enabled":true}]`); err == nil {
		t.Fatal("an invalid per-channel gateway URL must be rejected")
	}
	if _, err := mergeChannelSecrets(stored, `[{"channel":"alipay","api_url":"https://pay.b.test/","enabled":true}]`); err != nil {
		t.Fatalf("a valid per-channel gateway URL must be accepted: %v", err)
	}
}
