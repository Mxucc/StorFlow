package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GiftCode is a redeemable code granting points / storage / a user group.
type GiftCode struct {
	ent.Schema
}

// Fields of the GiftCode.
func (GiftCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").Unique().MaxLen(64),
		field.String("product_type").MaxLen(32), // points | storage | group | balance
		field.Int64("num"),                      // points amount / storage bytes / group id
		// Amount is the face value of a balance card, in cents. It is only used
		// when ProductType is "balance"; the other card types grant Num instead.
		field.Int64("amount").Default(0),
		// ExpiresAt is the Unix time after which the card can no longer be
		// redeemed; 0 means the card never expires.
		field.Int64("expires_at").Default(0),
		field.String("name").Optional().MaxLen(255),
		field.Bool("used").Default(false),
		field.Int("used_by").Optional(),
		field.String("used_by_email").Optional().MaxLen(255),
		field.String("created_by_email").Optional().MaxLen(255),
	}
}

// Edges of the GiftCode.
func (GiftCode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("giftcodes").
			Field("used_by").
			Unique(),
	}
}

// Indexes of the GiftCode.
func (GiftCode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code"),
		index.Fields("used"),
	}
}

// Mixin of the GiftCode.
func (GiftCode) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}
