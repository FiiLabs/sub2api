package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RedeemAuditLog 兑换码入账审计流水：记录 who(actor)/when/code/amount/subject，供资金可审计。
type RedeemAuditLog struct {
	ent.Schema
}

func (RedeemAuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "redeem_audit_logs"},
	}
}

func (RedeemAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("redeem_code_id"),
		field.String("code").MaxLen(64),
		field.Int64("actor_user_id"),
		field.Int64("billing_subject_id").Optional(),
		field.String("code_type").MaxLen(20),
		field.Float("amount").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (RedeemAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("redeem_code_id"),
		index.Fields("actor_user_id", "created_at"),
		index.Fields("billing_subject_id", "created_at"),
	}
}
