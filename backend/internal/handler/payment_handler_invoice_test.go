//go:build unit

package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func setupInvoiceTest(t *testing.T) (*dbent.Client, *PaymentHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	paymentSvc := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, nil, nil, nil, nil)
	return client, NewPaymentHandler(paymentSvc, nil, nil)
}

var invoiceTestOrderSeq atomic.Int64

func createInvoiceTestOrder(t *testing.T, client *dbent.Client, userID int64, email, status string) *dbent.PaymentOrder {
	t.Helper()
	completed := time.Now().Add(-time.Hour)
	order, err := client.PaymentOrder.Create().
		SetUserID(userID).
		SetUserEmail(email).
		SetUserName("invoice-user").
		SetAmount(50).
		SetPayAmount(368).
		SetFeeRate(2).
		SetRechargeCode("INVOICE-TEST").
		SetOutTradeNo(fmt.Sprintf("INV%d", invoiceTestOrderSeq.Add(1))).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo(fmt.Sprintf("trade-inv-%d", invoiceTestOrderSeq.Load())).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(status).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetCompletedAt(completed).
		Save(context.Background())
	require.NoError(t, err)
	return order
}

func invokeDownloadInvoice(h *PaymentHandler, userID, orderID int64) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/payment/orders/%d/invoice", orderID), nil)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(orderID)}}
	ctx.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: userID})

	h.DownloadInvoice(ctx)
	return recorder
}

func TestDownloadInvoiceCompletedOrder(t *testing.T) {
	t.Parallel()
	client, h := setupInvoiceTest(t)

	user, err := client.User.Create().
		SetEmail("invoice-owner@example.com").
		SetPasswordHash("hash").
		SetUsername("invoice-owner").
		Save(context.Background())
	require.NoError(t, err)

	order := createInvoiceTestOrder(t, client, user.ID, user.Email, payment.OrderStatusCompleted)

	recorder := invokeDownloadInvoice(h, user.ID, order.ID)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "application/pdf", recorder.Header().Get("Content-Type"))
	require.Contains(t, recorder.Header().Get("Content-Disposition"), order.OutTradeNo)
	require.True(t, strings.HasPrefix(recorder.Body.String(), "%PDF-"), "body should be a PDF")
}

func TestDownloadInvoiceRejectsNonCompletedOrder(t *testing.T) {
	t.Parallel()
	client, h := setupInvoiceTest(t)

	user, err := client.User.Create().
		SetEmail("invoice-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("invoice-pending").
		Save(context.Background())
	require.NoError(t, err)

	for _, status := range []string{
		payment.OrderStatusPending,
		payment.OrderStatusRefunded,
		payment.OrderStatusPartiallyRefunded,
	} {
		order := createInvoiceTestOrder(t, client, user.ID, user.Email, status)
		recorder := invokeDownloadInvoice(h, user.ID, order.ID)
		require.Equal(t, http.StatusNotFound, recorder.Code, "status %s should not be invoice-eligible", status)
	}
}

func TestDownloadInvoiceRejectsOtherUsersOrder(t *testing.T) {
	t.Parallel()
	client, h := setupInvoiceTest(t)

	owner, err := client.User.Create().
		SetEmail("invoice-real-owner@example.com").
		SetPasswordHash("hash").
		SetUsername("invoice-real-owner").
		Save(context.Background())
	require.NoError(t, err)

	intruder, err := client.User.Create().
		SetEmail("invoice-intruder@example.com").
		SetPasswordHash("hash").
		SetUsername("invoice-intruder").
		Save(context.Background())
	require.NoError(t, err)

	order := createInvoiceTestOrder(t, client, owner.ID, owner.Email, payment.OrderStatusCompleted)

	recorder := invokeDownloadInvoice(h, intruder.ID, order.ID)
	require.Equal(t, http.StatusForbidden, recorder.Code)
}
