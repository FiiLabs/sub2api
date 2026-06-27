package service

import "context"

type BillingSubject struct {
	ID                         int64
	Type                       string
	UserID                     *int64
	TeamID                     *int64
	Status                     string
	Balance                    float64
	TotalRecharged             float64
	Concurrency                int
	RPMLimit                   int
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailEntry
}

type BillingSubjectRepository interface {
	GetByID(ctx context.Context, id int64) (*BillingSubject, error)
	GetPersonalByUserID(ctx context.Context, userID int64) (*BillingSubject, error)
	EnsurePersonalForUser(ctx context.Context, user *User) (*BillingSubject, error)
	CreateTeamSubject(ctx context.Context, teamID int64, seed BillingSubject) (*BillingSubject, error)
	UpdateBalance(ctx context.Context, subjectID int64, delta float64) error
	DeductBalance(ctx context.Context, subjectID int64, amount float64) error
	UpdateLimits(ctx context.Context, subjectID int64, concurrency, rpmLimit int) error
}
