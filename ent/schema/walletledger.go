package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WalletLedger records every movement of a user's wallet (recharge card,
// purchase, refund, manual adjustment), in cents.
type WalletLedger struct {
	ent.Schema
}

// Fields of the WalletLedger.
func (WalletLedger) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").Optional(),
		field.Int64("delta"),         // in cents, positive = credit
		field.Int64("balance_after"), // in cents, snapshot after the change
		// recharge | purchase | shop_pay | refund | manual
		field.String("reason").MaxLen(64),
		field.String("note").Optional().MaxLen(512),
		// ref points at what caused the change: an order number or a card code.
		field.String("ref").Optional().MaxLen(128),
	}
}

// Edges of the WalletLedger.
func (WalletLedger) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("wallet_ledgers").
			Field("user_id").
			Unique(),
	}
}

// Indexes of the WalletLedger.
func (WalletLedger) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "created_at"),
		index.Fields("reason"),
	}
}

// Mixin of the WalletLedger.
func (WalletLedger) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
