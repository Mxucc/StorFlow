package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WalletBalance is the stored balance of one user's internal wallet, in cents.
//
// It is the snapshot used for the atomic conditional debit (UPDATE ... WHERE
// balance >= amount), so a concurrent checkout can never overdraw the wallet.
// Every change also appends a WalletLedger row, and the ledger is the audit
// trail: sum(delta) must always equal this balance.
type WalletBalance struct {
	ent.Schema
}

// Fields of the WalletBalance.
func (WalletBalance) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").Optional(),
		field.Int64("balance").Default(0), // in cents
	}
}

// Edges of the WalletBalance.
func (WalletBalance) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("wallet_balance").
			Field("user_id").
			Unique(),
	}
}

// Indexes of the WalletBalance: one wallet per user.
func (WalletBalance) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique(),
	}
}

// Mixin of the WalletBalance.
func (WalletBalance) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
