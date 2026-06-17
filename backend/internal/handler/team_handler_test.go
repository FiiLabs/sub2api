package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTeamHandlerListWorkspaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &TeamHandler{teamService: teamHandlerServiceAdapter{workspaces: []service.WorkspaceSubject{
		{BillingSubjectID: 1, Type: "user", UserID: 9, Name: "Personal", Balance: 3.25},
	}}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 9})
		c.Next()
	})
	router.GET("/api/v1/workspaces", h.ListWorkspaces)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"billing_subject_id":1`)
	require.Contains(t, rec.Body.String(), `"Personal"`)
}

type teamHandlerServiceAdapter struct {
	workspaces []service.WorkspaceSubject
}

func (a teamHandlerServiceAdapter) ListWorkspaces(_ context.Context, _ int64) ([]service.WorkspaceSubject, error) {
	return a.workspaces, nil
}
