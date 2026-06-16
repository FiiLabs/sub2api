package schema

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TeamMember struct {
	ent.Schema
}

func (TeamMember) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "team_members"},
	}
}

func (TeamMember) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (TeamMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("team_id"),
		field.Int64("user_id"),
		field.String("role").
			MaxLen(20).
			Validate(func(v string) error {
				switch v {
				case domain.TeamRoleOwner, domain.TeamRoleAdmin, domain.TeamRoleBilling, domain.TeamRoleDeveloper, domain.TeamRoleViewer:
					return nil
				default:
					return fmt.Errorf("invalid team role %q", v)
				}
			}),
		field.String("status").
			MaxLen(20).
			Default(domain.TeamMemberStatusActive).
			Validate(func(v string) error {
				switch v {
				case domain.TeamMemberStatusActive, domain.TeamMemberStatusSuspended, domain.TeamMemberStatusLeft:
					return nil
				default:
					return fmt.Errorf("invalid team member status %q", v)
				}
			}),
		field.Int64("invited_by_user_id").
			Optional().
			Nillable(),
		field.Time("joined_at").
			Optional().
			Nillable(),
		field.Time("last_active_at").
			Optional().
			Nillable(),
	}
}

func (TeamMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("members").
			Field("team_id").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("team_memberships").
			Field("user_id").
			Unique().
			Required(),
		edge.From("invited_by", User.Type).
			Ref("team_member_invites").
			Field("invited_by_user_id").
			Unique(),
	}
}

func (TeamMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "user_id").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("user_id", "status"),
		index.Fields("team_id", "status"),
		index.Fields("deleted_at"),
	}
}
