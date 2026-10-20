package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OIDCBinding holds the schema definition for the OIDCBinding entity.
// 它记录第三方 OIDC IdP 账号与本地用户的绑定关系，唯一标识为 (issuer, subject)。
type OIDCBinding struct {
	ent.Schema
}

// Fields of the OIDCBinding.
func (OIDCBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		// issuer 为 IdP 的 issuer（即设置项 oidc_endpoint）。
		field.String("issuer").
			NotEmpty(),
		// subject 为 id_token 中唯一标识用户的 claim（默认 sub）。
		field.String("subject").
			NotEmpty(),
		// email 为绑定时 IdP 返回的邮箱，仅用于展示与排查。
		field.String("email").
			Optional(),
	}
}

// Edges of the OIDCBinding.
func (OIDCBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Field("user_id").
			Ref("oidc_bindings").
			Unique().
			Required(),
	}
}

func (OIDCBinding) Mixin() []ent.Mixin {
	return []ent.Mixin{
		CommonMixin{},
	}
}

// Indexes of the OIDCBinding.
func (OIDCBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("issuer", "subject").
			Unique(),
	}
}
