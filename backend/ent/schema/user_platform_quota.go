package schema

import (
	"fmt"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// UserPlatformQuota holds the schema definition for per-user per-platform quota.
type UserPlatformQuota struct {
	ent.Schema
}

func (UserPlatformQuota) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_platform_quotas"},
	}
}

func (UserPlatformQuota) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (UserPlatformQuota) Fields() []ent.Field {
	return []ent.Field{
		// platform-quota: user_id 改可空——团队 quota 行无单一 user
		// （user_id = NULL, billing_subject_id = 团队主体）。个人主体行 user_id 仍非空。
		field.Int64("user_id").
			Optional().
			Nillable(),
		field.Int64("billing_subject_id").
			Optional().
			Nillable(),
		field.String("platform").
			MaxLen(32).
			NotEmpty().
			Validate(func(s string) error {
				// 注意：平台列表的单一权威源为 service.AllowedQuotaPlatforms；
				// 此处为 ent 构建期约束，需与 service.AllowedQuotaPlatforms 保持同步。
				switch s {
				case "anthropic", "openai", "gemini", "antigravity":
					return nil
				default:
					return fmt.Errorf("platform %q is not allowed", s)
				}
			}),

		// 日 / 周 / 月 USD 上限：
		//   nil / not set → 无限额（完全放行）
		//   0            → 完全禁用（任何请求都会被拒绝，因为 usage >= 0 恒成立）
		//   > 0          → USD 限额上限
		field.Float("daily_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("weekly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("monthly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),

		// 当前窗口已用量（USD，preflight 时与 limit 比较）
		field.Float("daily_usage_usd").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("weekly_usage_usd").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("monthly_usage_usd").
			Default(0).
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),

		// 窗口起点（NULL = 首次还未初始化，由 InitWindowStarts 用 COALESCE 兜底）
		field.Time("daily_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("weekly_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("monthly_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (UserPlatformQuota) Edges() []ent.Edge {
	return []ent.Edge{
		// platform-quota: 去掉 Required()——团队 quota 行 user_id 为空，无 user 边。
		edge.From("user", User.Type).
			Ref("platform_quotas").
			Field("user_id").
			Unique(),
		edge.From("billing_subject", BillingSubject.Type).
			Ref("platform_quotas").
			Field("billing_subject_id").
			Unique(),
	}
}

func (UserPlatformQuota) Indexes() []ent.Index {
	return []ent.Index{
		// 软删除友好：只对未删记录唯一
		index.Fields("user_id", "platform").
			Unique().
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("user_id"),
		// platform-quota: subject 维度部分唯一索引（模型对齐 migration 153）。
		// 软删除友好 + 仅对已设 subject 的行唯一；团队主体多成员共享同一行。
		index.Fields("billing_subject_id", "platform").
			Unique().
			Annotations(entsql.IndexWhere("billing_subject_id IS NOT NULL AND deleted_at IS NULL")),
	}
}
