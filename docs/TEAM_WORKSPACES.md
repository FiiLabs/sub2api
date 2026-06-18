# Team Workspaces

Sub2API supports personal and team workspaces.

- A user is always the login identity.
- A billing subject owns balance, subscriptions, API keys, usage, orders, and platform quotas.
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
