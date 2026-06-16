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

type Team struct {
	ent.Schema
}

func (Team) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "teams"},
	}
}

func (Team) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(120).
			NotEmpty(),
		field.String("slug").
			MaxLen(120).
			NotEmpty(),
		field.Int64("owner_user_id"),
		field.Int64("billing_subject_id").
			Optional().
			Nillable(),
		field.String("status").
			MaxLen(20).
			Default(domain.TeamStatusActive).
			Validate(func(v string) error {
				switch v {
				case domain.TeamStatusActive, domain.TeamStatusDisabled:
					return nil
				default:
					return fmt.Errorf("invalid team status %q", v)
				}
			}),
		field.String("avatar_url").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.Int64("created_by_user_id"),
	}
}

func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).
			Ref("owned_teams").
			Field("owner_user_id").
			Unique().
			Required(),
		edge.From("created_by", User.Type).
			Ref("created_teams").
			Field("created_by_user_id").
			Unique().
			Required(),
		edge.From("billing_subject", BillingSubject.Type).
			Ref("teams").
			Field("billing_subject_id").
			Unique(),
		edge.To("team_subject", BillingSubject.Type),
		edge.To("members", TeamMember.Type),
		edge.To("invitations", TeamInvitation.Type),
		edge.To("api_keys", APIKey.Type),
		edge.To("usage_logs", UsageLog.Type),
		edge.To("payment_orders", PaymentOrder.Type),
	}
}

func (Team) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("slug").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("owner_user_id"),
		index.Fields("billing_subject_id"),
		index.Fields("status"),
		index.Fields("deleted_at"),
	}
}
