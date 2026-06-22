# Team Workspaces

Sub2API supports personal and team workspaces.

- A user is always the login identity.
- A billing subject owns balance, subscriptions, API keys, usage, and orders.
- A personal workspace maps one user to one billing subject.
- A team workspace maps one team to one billing subject.
- The dashboard, keys, usage, payment, subscriptions, redeem, and orders pages use the active workspace.
- The Members page appears when the active workspace is a team.

## Membership lifecycle

- **Create a team:** any user can create a team from the workspace switcher (they become
  the owner); a platform admin can create a team for any user from the admin console.
- **Invite members:** owners and admins invite by email. The invitee receives an email
  with an accept link, and the inviter also gets a copyable accept link. An invitation is
  a pending record only — it never creates an account or sets a password.
- **Accept an invitation:** the invitee opens the link and signs in (new invitees register
  normally first, choosing their own password with email verification). Acceptance is
  **bound to the invited email** — the accepting account's email must match the invitation
  — then an active membership is created.
- **Manage members:** owners/admins can change a member's role, suspend/reactivate, or
  remove members (the owner cannot be modified or removed here).
- **Transfer ownership:** the owner can transfer ownership to another active member; the
  previous owner is demoted to `admin`. The owner cannot leave or be removed until they
  transfer ownership.

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
