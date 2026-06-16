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

type TeamInvitation struct {
	ent.Schema
}

func (TeamInvitation) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "team_invitations"},
	}
}

func (TeamInvitation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (TeamInvitation) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("team_id"),
		field.String("email").
			MaxLen(255).
			NotEmpty(),
		field.String("role").
			MaxLen(20).
			Validate(func(v string) error {
				switch v {
				case domain.TeamRoleAdmin, domain.TeamRoleBilling, domain.TeamRoleDeveloper, domain.TeamRoleViewer:
					return nil
				default:
					return fmt.Errorf("invalid invitation role %q", v)
				}
			}),
		field.String("token_hash").
			MaxLen(128).
			NotEmpty(),
		field.String("status").
			MaxLen(20).
			Default(domain.TeamInvitationStatusPending).
			Validate(func(v string) error {
				switch v {
				case domain.TeamInvitationStatusPending, domain.TeamInvitationStatusAccepted, domain.TeamInvitationStatusExpired, domain.TeamInvitationStatusRevoked:
					return nil
				default:
					return fmt.Errorf("invalid invitation status %q", v)
				}
			}),
		field.Int64("invited_by_user_id"),
		field.Int64("accepted_by_user_id").
			Optional().
			Nillable(),
		field.Time("expires_at"),
	}
}

func (TeamInvitation) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).
			Ref("invitations").
			Field("team_id").
			Unique().
			Required(),
		edge.From("invited_by", User.Type).
			Ref("sent_team_invitations").
			Field("invited_by_user_id").
			Unique().
			Required(),
		edge.From("accepted_by", User.Type).
			Ref("accepted_team_invitations").
			Field("accepted_by_user_id").
			Unique(),
	}
}

func (TeamInvitation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("team_id", "status"),
		index.Fields("email", "status"),
		index.Fields("deleted_at"),
	}
}
