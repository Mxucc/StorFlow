// Package epay implements the client side of the "易支付" (epay) payment
// protocol used by most Chinese aggregate payment gateways.
//
// The protocol is deliberately simple:
//
//   - the merchant builds a form/GET request to <gateway>/submit.php with the
//     order parameters and a MD5 signature;
//   - the gateway calls back the notify URL, again signed with the same key;
//   - the merchant verifies the signature and the amount, then settles the
//     order idempotently.
//
// This package only knows about that wire protocol: no database, no HTTP
// framework. Everything is pure functions plus one optional status query.
package epay

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// SignTypeMD5 is the only signature type of the protocol.
	SignTypeMD5 = "MD5"
	// TradeStatusSuccess is the trade_status of a settled payment.
	TradeStatusSuccess = "TRADE_SUCCESS"

	// submitPath is the cashier entry point of an epay gateway.
	submitPath = "/submit.php"
	// apiPath is the JSON API used for status queries.
	apiPath = "/api.php"

	// defaultTimeout bounds every outgoing request.
	defaultTimeout = 10 * time.Second
)

// Errors returned by the package.
var (
	ErrNotConfigured = errors.New("epay: gateway is not configured")
	ErrBadSignature  = errors.New("epay: signature mismatch")
	ErrBadAmount     = errors.New("epay: amount mismatch")
	ErrBadStatus     = errors.New("epay: trade status is not success")
	ErrBadOrder      = errors.New("epay: missing out_trade_no")
)

// Config is the merchant configuration of one gateway.
type Config struct {
	// APIURL is the gateway base URL, e.g. https://pay.example.com/.
	APIURL string
	// PID is the merchant id.
	PID string
	// Key is the merchant secret used for signing.
	Key string
	// SignType defaults to MD5.
	SignType string
	// Client is the HTTP client used by Query; nil means a default client.
	Client *http.Client
}

// Normalize trims the configuration and validates it.
func (c Config) Normalize() (Config, error) {
	c.APIURL = strings.TrimRight(strings.TrimSpace(c.APIURL), "/")
	c.PID = strings.TrimSpace(c.PID)
	c.Key = strings.TrimSpace(c.Key)
	if c.SignType == "" {
		c.SignType = SignTypeMD5
	}
	if c.APIURL == "" || c.PID == "" || c.Key == "" {
		return c, ErrNotConfigured
	}
	if c.SignType != SignTypeMD5 {
		return c, fmt.Errorf("epay: unsupported sign type %q", c.SignType)
	}
	return c, nil
}

// PayRequest describes one checkout.
type PayRequest struct {
	// TradeNo is the merchant order number, must be unique.
	TradeNo string
	// Type is the channel: alipay, wxpay, qqpay...
	Type string
	// Name is the subject shown on the cashier.
	Name string
	// Money is the amount in yuan with two decimals, e.g. "12.30".
	Money string
	// NotifyURL receives the asynchronous, signed confirmation.
	NotifyURL string
	// ReturnURL is where the buyer lands after paying.
	ReturnURL string
	// Param is echoed back by the gateway, used to carry a resume ticket.
	Param string
	// ClientIP is the buyer IP, some gateways require it.
	ClientIP string
}

// Sign computes the MD5 signature of the parameters: every non-empty key except
// sign/sign_type is sorted by ASCII order, joined as k=v&... and suffixed with
// the merchant key.
func Sign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}
	sb.WriteString(key)

	sum := md5.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether params carry a valid signature.
func (c Config) Verify(params map[string]string) error {
	cfg, err := c.Normalize()
	if err != nil {
		return err
	}

	got := params["sign"]
	if got == "" {
		return ErrBadSignature
	}
	if !strings.EqualFold(got, Sign(params, cfg.Key)) {
		return ErrBadSignature
	}
	return nil
}

// PayURL builds the cashier URL for a checkout.
func (c Config) PayURL(req PayRequest) (string, error) {
	cfg, err := c.Normalize()
	if err != nil {
		return "", err
	}
	if req.TradeNo == "" {
		return "", ErrBadOrder
	}
	if req.Money == "" {
		return "", ErrBadAmount
	}
	if _, err := strconv.ParseFloat(req.Money, 64); err != nil {
		return "", fmt.Errorf("epay: invalid amount %q: %w", req.Money, err)
	}

	params := map[string]string{
		"pid":          cfg.PID,
		"type":         req.Type,
		"out_trade_no": req.TradeNo,
		"notify_url":   req.NotifyURL,
		"return_url":   req.ReturnURL,
		"name":         req.Name,
		"money":        req.Money,
		"clientip":     req.ClientIP,
		"param":        req.Param,
	}
	params["sign"] = Sign(params, cfg.Key)
	params["sign_type"] = cfg.SignType

	query := url.Values{}
	for k, v := range params {
		if v != "" {
			query.Set(k, v)
		}
	}
	return cfg.APIURL + submitPath + "?" + query.Encode(), nil
}

// Notify is a parsed and validated asynchronous payment notification.
type Notify struct {
	TradeNo     string
	OutTradeNo  string
	Type        string
	Name        string
	Money       string
	TradeStatus string
	Param       string
	Raw         map[string]string
}

// AmountCents parses the notified amount into cents.
func (n Notify) AmountCents() (int64, error) {
	return ParseYuan(n.Money)
}

// ParseYuan converts a decimal amount string into cents without using floats.
func ParseYuan(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, ErrBadAmount
	}

	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > 2 {
		fracPart = fracPart[:2]
	}
	fracPart += strings.Repeat("0", 2-len(fracPart))

	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("epay: invalid amount %q: %w", raw, err)
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("epay: invalid amount %q: %w", raw, err)
	}
	return whole*100 + frac, nil
}

// ParseNotify validates a notification payload: signature first, then the
// trade status, the order number and the amount.
func (c Config) ParseNotify(params map[string]string, expectedCents int64) (*Notify, error) {
	if err := c.Verify(params); err != nil {
		return nil, err
	}
	if params["trade_status"] != TradeStatusSuccess {
		return nil, fmt.Errorf("%w: %s", ErrBadStatus, params["trade_status"])
	}

	orderNo := params["out_trade_no"]
	if orderNo == "" {
		return nil, ErrBadOrder
	}

	notify := &Notify{
		TradeNo:     params["trade_no"],
		OutTradeNo:  orderNo,
		Type:        params["type"],
		Name:        params["name"],
		Money:       params["money"],
		TradeStatus: params["trade_status"],
		Param:       params["param"],
		Raw:         params,
	}

	if expectedCents > 0 {
		paid, err := notify.AmountCents()
		if err != nil {
			return nil, err
		}
		if paid != expectedCents {
			return nil, fmt.Errorf("%w: notified %d, expected %d", ErrBadAmount, paid, expectedCents)
		}
	}

	return notify, nil
}

// QueryResult is the payload of the api.php order query.
type QueryResult struct {
	Code        int    `json:"code"`
	Msg         string `json:"msg"`
	TradeNo     string `json:"trade_no"`
	OutTradeNo  string `json:"out_trade_no"`
	Type        string `json:"type"`
	PID         string `json:"pid"`
	AddTime     string `json:"addtime"`
	EndTime     string `json:"endtime"`
	Name        string `json:"name"`
	Money       string `json:"money"`
	Status      int    `json:"status"` // 1 = settled
	Param       string `json:"param"`
	Buyer       string `json:"buyer"`
	ClientIP    string `json:"clientip"`
	APIQueryErr string `json:"-"`
}

// Paid reports whether the gateway considers the order settled.
func (r *QueryResult) Paid() bool { return r.Code == 1 && r.Status == 1 }

// Query asks the gateway about the current state of an order. It is used for
// reconciliation when a notification was lost.
func (c Config) Query(ctx context.Context, tradeNo string) (*QueryResult, error) {
	cfg, err := c.Normalize()
	if err != nil {
		return nil, err
	}
	if tradeNo == "" {
		return nil, ErrBadOrder
	}

	query := url.Values{}
	query.Set("act", "order")
	query.Set("pid", cfg.PID)
	query.Set("key", cfg.Key)
	query.Set("out_trade_no", tradeNo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.APIURL+apiPath+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("epay: failed to build query request: %w", err)
	}

	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("epay: query request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("epay: failed to read query response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("epay: query returned HTTP %d", resp.StatusCode)
	}

	var res QueryResult
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("epay: invalid query response %q: %w", string(body), err)
	}
	return &res, nil
}

// CollectParams flattens the query string and the form body of an incoming
// notification into a single parameter map.
func CollectParams(values url.Values, form url.Values) map[string]string {
	params := make(map[string]string, len(values)+len(form))
	for k, v := range values {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	for k, v := range form {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	return params
}
