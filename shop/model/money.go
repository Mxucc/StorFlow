package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Money is an amount in the smallest currency unit (cents / 分).
//
// The shop never uses floating point for money. All arithmetic happens on
// int64 cents so that repeated discounts cannot drift.
type Money int64

const (
	// MinPrice is the smallest price a seller may set and the smallest payable
	// amount of an order (epay gateways reject zero-amount orders).
	MinPrice Money = 1
	// MaxPrice caps user supplied prices at 1,000,000.00 to keep operational
	// blast radius small and to catch obvious input mistakes.
	MaxPrice Money = 100_000_000
)

// PercentOff returns the amount after taking percent off, rounded half up.
// A percent outside (0, 100) leaves the amount untouched.
func PercentOff(amount Money, percent int) Money {
	if percent <= 0 || percent >= 100 || amount <= 0 {
		return amount
	}

	kept := amount * Money(100-percent)
	return Money(math.Round(float64(kept) / 100))
}

// FixedOff subtracts a fixed amount, never returning a negative value.
func FixedOff(amount, off Money) Money {
	if off <= 0 {
		return amount
	}
	if off >= amount {
		return 0
	}
	return amount - off
}

// ApplyRate applies a basis-point rate (1/10000) to the amount, rounded half up.
// It is used for platform fees and referral rebates.
func ApplyRate(amount Money, bps int) Money {
	if bps <= 0 || amount <= 0 {
		return 0
	}
	return Money(math.Round(float64(amount) * float64(bps) / 10000))
}

// ClampPayable raises the amount to MinPrice and caps it at MaxPrice.
func ClampPayable(amount Money) Money {
	if amount < MinPrice {
		return MinPrice
	}
	if amount > MaxPrice {
		return MaxPrice
	}
	return amount
}

// String renders the amount as a decimal string, e.g. "12.30".
func (m Money) String() string {
	neg := m < 0
	v := m
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%02d", v/100, v%100)
	if neg {
		return "-" + s
	}
	return s
}

// Yuan renders the amount as a decimal string that epay gateways accept
// (plain decimal with two fraction digits, no separators).
func (m Money) Yuan() string { return m.String() }

// ParseYuan parses a decimal amount like "12.3" or "12.30" into cents.
func ParseYuan(raw string) (Money, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}

	intPart, fracPart, found := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > 2 {
		// Truncate (rather than reject) sub-cent precision coming from gateways.
		fracPart = fracPart[:2]
	}
	fracPart = fracPart + strings.Repeat("0", 2-len(fracPart))

	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", raw, err)
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", raw, err)
	}
	if !found && strings.Contains(raw, ".") {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}

	res := Money(whole*100 + frac)
	if neg {
		res = -res
	}
	return res, nil
}
