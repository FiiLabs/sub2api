package service

import (
	"context"
	"fmt"
	"strings"
)

// teamInviteEmailNotifier is the production TeamInviteNotifier: it sends a
// transactional invitation email via EmailService and resolves the accept-link
// base URL via SettingService (frontend_url first, falling back to api_base_url).
//
// It also exposes AcceptBaseURL so TeamService can build the same link it returns
// to the API caller (the copyable-link fallback) without duplicating the base-URL
// resolution.
type teamInviteEmailNotifier struct {
	emailService   *EmailService
	settingService *SettingService
}

// ProvideTeamInviteNotifier builds the production invitation notifier from the
// existing EmailService + SettingService. Returned as the TeamInviteNotifier
// interface so wire injects it via TeamService.SetInviteNotifier.
func ProvideTeamInviteNotifier(emailService *EmailService, settingService *SettingService) TeamInviteNotifier {
	return &teamInviteEmailNotifier{emailService: emailService, settingService: settingService}
}

// AcceptBaseURL returns the base URL for the accept link: frontend_url first,
// then api_base_url. Trailing slashes are trimmed; may be empty when neither is
// configured (callers then fall back to a relative path).
func (n *teamInviteEmailNotifier) AcceptBaseURL(ctx context.Context) string {
	if n == nil || n.settingService == nil {
		return ""
	}
	if v := strings.TrimRight(strings.TrimSpace(n.settingService.GetFrontendURL(ctx)), "/"); v != "" {
		return v
	}
	return strings.TrimRight(strings.TrimSpace(n.settingService.GetAPIBaseURL(ctx)), "/")
}

// SendInvite sends the invitation email. Errors are returned to the caller, which
// treats delivery as best-effort (it logs and never fails the invite).
func (n *teamInviteEmailNotifier) SendInvite(ctx context.Context, toEmail, acceptLink, teamName string) error {
	if n == nil || n.emailService == nil {
		return ErrEmailNotConfigured
	}
	siteName := defaultSiteName
	if n.settingService != nil {
		if s := strings.TrimSpace(n.settingService.GetSiteName(ctx)); s != "" {
			siteName = s
		}
	}
	teamLabel := strings.TrimSpace(teamName)
	if teamLabel == "" {
		teamLabel = "a team"
	}
	subject := fmt.Sprintf("%s — team invitation", siteName)
	body := fmt.Sprintf(
		"You have been invited to join %s on %s.\n\nAccept the invitation:\n%s\n\n"+
			"If you do not have an account, sign up first with this email address, then open the link again.\n"+
			"This invitation may expire; if the link no longer works, ask the team to send a new one.",
		teamLabel, siteName, acceptLink,
	)
	return n.emailService.SendEmail(ctx, toEmail, subject, body)
}
