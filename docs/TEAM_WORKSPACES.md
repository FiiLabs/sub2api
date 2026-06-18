# Team Workspaces

Sub2API supports personal and team workspaces.

- A user is always the login identity.
- A billing subject owns balance, subscriptions, API keys, usage, and orders.
- A personal workspace maps one user to one billing subject.
- A team workspace maps one team to one billing subject.
- The dashboard, keys, usage, payment, subscriptions, redeem, and orders pages use the active workspace.
- The Members page appears when the active workspace is a team.

Team roles:

| Role | Usage | API keys | Members | Billing | Settings | Delete |
|---|---|---|---|---|---|---|
| owner | yes | yes | yes | yes | yes | yes |
| admin | yes | yes | yes | yes | yes | no |
| billing | yes | no | no | yes | no | no |
| developer | yes | yes | no | no | no | no |
| viewer | yes | no | no | no | no | no |

## Known limitations

- **Per-platform USD quotas are not yet team-scoped.** Daily/weekly/monthly platform
  quota enforcement (`user_platform_quotas`) is still keyed by `user_id`, so it applies
  per acting user rather than per team billing subject. The table carries a
  `billing_subject_id` column and the cache layer exposes subject-keyed helpers, but the
  storage identity (`UNIQUE (user_id, platform)`, non-null `user_id`) and the enforcement
  and flush paths remain user-keyed. Personal workspaces are unaffected. Making platform
  quotas team-wide requires a schema-identity change (nullable `user_id`, a unique
  `(billing_subject_id, platform)` index, rewritten conflict targets, subject-scoped admin
  management, and a data backfill) and is deferred to a later release.
