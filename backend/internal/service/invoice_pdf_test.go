package service

import (
	"bytes"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestInvoiceDateFor(t *testing.T) {
	created := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	paid := created.Add(5 * time.Minute)
	completed := created.Add(10 * time.Minute)

	order := &dbent.PaymentOrder{CreatedAt: created}
	if got := invoiceDateFor(order); !got.Equal(created) {
		t.Errorf("expected created_at fallback, got %v", got)
	}

	order.PaidAt = &paid
	if got := invoiceDateFor(order); !got.Equal(paid) {
		t.Errorf("expected paid_at fallback, got %v", got)
	}

	order.CompletedAt = &completed
	if got := invoiceDateFor(order); !got.Equal(completed) {
		t.Errorf("expected completed_at, got %v", got)
	}
}

func TestInvoiceFeeSplit(t *testing.T) {
	tests := []struct {
		name      string
		payAmount float64
		feeRate   float64
		wantBase  float64
		wantFee   float64
	}{
		{"no fee", 100, 0, 100, 0},
		{"negative fee rate treated as none", 100, -1, 100, 0},
		{"two percent", 368, 2, 360.78, 7.22},
		{"zero amount", 0, 2, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, fee := invoiceFeeSplit(tt.payAmount, tt.feeRate)
			if base != tt.wantBase || fee != tt.wantFee {
				t.Errorf("invoiceFeeSplit(%v, %v) = (%v, %v), want (%v, %v)",
					tt.payAmount, tt.feeRate, base, fee, tt.wantBase, tt.wantFee)
			}
			if base+fee != tt.payAmount {
				t.Errorf("base+fee = %v, must equal pay_amount %v", base+fee, tt.payAmount)
			}
		})
	}
}

func TestInvoiceLineDescription(t *testing.T) {
	planID := int64(7)

	balance := &dbent.PaymentOrder{OrderType: payment.OrderTypeBalance, Amount: 50}
	desc, note := invoiceLineDescription(balance)
	if desc != "Account Balance Top-up" {
		t.Errorf("balance desc = %q", desc)
	}
	if note != "($50.00 credited to account balance)" {
		t.Errorf("balance note = %q", note)
	}

	sub := &dbent.PaymentOrder{OrderType: payment.OrderTypeSubscription, PlanID: &planID}
	desc, note = invoiceLineDescription(sub)
	if desc != "Subscription Plan #7" || note != "" {
		t.Errorf("subscription with plan = (%q, %q)", desc, note)
	}

	sub.PlanID = nil
	desc, _ = invoiceLineDescription(sub)
	if desc != "Subscription Plan" {
		t.Errorf("subscription without plan = %q", desc)
	}

	other := &dbent.PaymentOrder{OrderType: "mystery"}
	desc, _ = invoiceLineDescription(other)
	if desc != "Service Payment" {
		t.Errorf("unknown order type desc = %q", desc)
	}
}

func TestInvoicePaymentMethodName(t *testing.T) {
	if got := invoicePaymentMethodName("wxpay_direct"); got != "WeChat Pay" {
		t.Errorf("wxpay_direct = %q", got)
	}
	if got := invoicePaymentMethodName("custom_gateway"); got != "custom_gateway" {
		t.Errorf("unknown type should fall back to raw value, got %q", got)
	}
}

func TestRenderOrderInvoicePDF(t *testing.T) {
	completed := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	order := &dbent.PaymentOrder{
		ID:          123,
		UserID:      42,
		Amount:      50,
		PayAmount:   368,
		FeeRate:     2,
		PaymentType: "alipay",
		OutTradeNo:  "ORD20260701123456",
		Status:      payment.OrderStatusCompleted,
		OrderType:   payment.OrderTypeBalance,
		CreatedAt:   completed.Add(-10 * time.Minute),
		CompletedAt: &completed,
	}

	pdfBytes, err := RenderOrderInvoicePDF(order, "user@example.com")
	if err != nil {
		t.Fatalf("RenderOrderInvoicePDF: %v", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Errorf("output does not start with PDF magic bytes")
	}
	if len(pdfBytes) < 1000 {
		t.Errorf("suspiciously small PDF: %d bytes", len(pdfBytes))
	}
}

func TestRenderOrderInvoicePDF_NoFee(t *testing.T) {
	order := &dbent.PaymentOrder{
		ID:          124,
		UserID:      42,
		Amount:      100,
		PayAmount:   100,
		FeeRate:     0,
		PaymentType: "stripe",
		OutTradeNo:  "ORD20260701123457",
		Status:      payment.OrderStatusCompleted,
		OrderType:   payment.OrderTypeSubscription,
		CreatedAt:   time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
	}

	pdfBytes, err := RenderOrderInvoicePDF(order, "user@example.com")
	if err != nil {
		t.Fatalf("RenderOrderInvoicePDF: %v", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Errorf("output does not start with PDF magic bytes")
	}
}
