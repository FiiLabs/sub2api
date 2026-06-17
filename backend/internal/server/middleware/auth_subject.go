package middleware

import "github.com/gin-gonic/gin"

// AuthSubject is the authenticated identity and resolved billing subject stored in gin context.
type AuthSubject struct {
	UserID           int64
	Concurrency      int
	BillingSubjectID int64
	SubjectType      string
	TeamID           int64
	TeamRole         string
	Permissions      map[string]bool
}

func GetAuthSubjectFromContext(c *gin.Context) (AuthSubject, bool) {
	value, exists := c.Get(string(ContextKeyUser))
	if !exists {
		return AuthSubject{}, false
	}
	subject, ok := value.(AuthSubject)
	return subject, ok
}

func GetUserRoleFromContext(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(ContextKeyUserRole))
	if !exists {
		return "", false
	}
	role, ok := value.(string)
	return role, ok
}
