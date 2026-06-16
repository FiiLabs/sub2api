package schema

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type BillingSubject struct {
	ent.Schema
}

func (BillingSubject) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "billing_subjects"},
	}
}

func (BillingSubject) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (BillingSubject) Fields() []ent.Field {
	return []ent.Field{
		field.String("type").
			MaxLen(20).
			Validate(func(v string) error {
				if v == domain.BillingSubjectTypeUser || v == domain.BillingSubjectTypeTeam {
					return nil
				}
				return fmt.Errorf("invalid billing subject type %q", v)
			}),
		field.Int64("user_id").
			Optional().
			Nillable(),
		field.Int64("team_id").
			Optional().
			Nillable(),
		field.String("status").
			MaxLen(20).
			Default(domain.StatusActive).
			Validate(func(v string) error {
				switch v {
				case domain.StatusActive, domain.StatusDisabled:
					return nil
				default:
					return fmt.Errorf("invalid billing subject status %q", v)
				}
			}),
		field.Float("balance").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		field.Float("total_recharged").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		field.Int("concurrency").
			Default(5),
		field.Int("rpm_limit").
			Default(0),
		field.Bool("balance_notify_enabled").
			Default(true),
		field.String("balance_notify_threshold_type").
			Default("fixed"),
		field.Float("balance_notify_threshold").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Optional().
			Nillable(),
		field.String("balance_notify_extra_emails").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default("[]"),
	}
}

func (BillingSubject) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("billing_subjects").
			Field("user_id").
			Unique(),
		edge.From("team", Team.Type).
			Ref("team_subject").
			Field("team_id").
			Unique(),
		edge.To("teams", Team.Type),
		edge.To("api_keys", APIKey.Type),
		edge.To("usage_logs", UsageLog.Type),
		edge.To("payment_orders", PaymentOrder.Type),
		edge.To("subscriptions", UserSubscription.Type),
		edge.To("platform_quotas", UserPlatformQuota.Type),
	}
}

func (BillingSubject) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique().
			Annotations(entsql.IndexWhere("type = 'user' AND deleted_at IS NULL")),
		index.Fields("team_id").
			Unique().
			Annotations(entsql.IndexWhere("type = 'team' AND deleted_at IS NULL")),
		index.Fields("status"),
		index.Fields("deleted_at"),
	}
}
