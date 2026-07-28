package service

import (
	"bytes"
	"fmt"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"

	"github.com/go-pdf/fpdf"
)

// Seller identity printed on invoices. ApexOne is the sole named subject
// across product copy and legal agreements (see CONTEXT.md "ApexOne as Sole
// Subject"); do not print any other operating-company name here.
const (
	invoiceSellerName  = "ApexOne"
	invoiceSellerEmail = "support@apex1.us"
)

// Invoices are always rendered in English regardless of UI locale, so payment
// method labels use a static map instead of the i18n bundle.
var invoicePaymentMethodNames = map[string]string{
	"alipay":        "Alipay",
	"alipay_direct": "Alipay",
	"wxpay":         "WeChat Pay",
	"wxpay_direct":  "WeChat Pay",
	"stripe":        "Stripe",
	"easypay":       "EasyPay",
	"airwallex":     "Airwallex",
}

func invoicePaymentMethodName(paymentType string) string {
	if name, ok := invoicePaymentMethodNames[paymentType]; ok {
		return name
	}
	return paymentType
}

// invoiceDateFor picks the invoice date: completion time, falling back to
// payment time, then creation time for legacy rows missing timestamps.
func invoiceDateFor(order *dbent.PaymentOrder) time.Time {
	if order.CompletedAt != nil {
		return *order.CompletedAt
	}
	if order.PaidAt != nil {
		return *order.PaidAt
	}
	return order.CreatedAt
}

// invoiceFeeSplit derives the pre-fee base and fee portion from the final
// charged amount. The fee is recomputed from pay_amount rather than stored, so
// the two parts are rounded such that base+fee always equals pay_amount.
func invoiceFeeSplit(payAmount, feeRate float64) (base, fee float64) {
	if feeRate <= 0 || payAmount <= 0 {
		return payAmount, 0
	}
	fee = math.Round((payAmount-payAmount/(1+feeRate/100))*100) / 100
	return payAmount - fee, fee
}

// invoiceLineDescription returns the line-item description and an optional
// note line. Plan names are intentionally not resolved (plans can be renamed
// or retired after purchase); subscription lines stay generic.
func invoiceLineDescription(order *dbent.PaymentOrder) (desc, note string) {
	switch order.OrderType {
	case payment.OrderTypeBalance:
		note = fmt.Sprintf("($%.2f credited to account balance)", order.Amount)
		return "Account Balance Top-up", note
	case payment.OrderTypeSubscription:
		if order.PlanID != nil {
			return fmt.Sprintf("Subscription Plan #%d", *order.PlanID), ""
		}
		return "Subscription Plan", ""
	default:
		return "Service Payment", ""
	}
}

func invoiceAmount(currency string, amount float64) string {
	return fmt.Sprintf("%s %.2f", currency, amount)
}

// drawInvoiceLogo draws the ApexOne mark as native vector operations, mirroring
// apexone-docs/public/favicon.svg (64x64 viewBox: dark rounded square with two
// white bracket glyphs). fpdf cannot embed SVG, and redrawing keeps the mark
// sharp at any zoom without shipping a raster asset.
func drawInvoiceLogo(pdf *fpdf.Fpdf, x, y, size float64) {
	s := size / 64
	pdf.SetFillColor(21, 21, 31)
	pdf.RoundedRect(x, y, size, size, 14*s, "1234", "F")
	pdf.SetDrawColor(255, 255, 255)
	pdf.SetLineWidth(5 * s)
	pdf.SetLineCapStyle("square")
	pdf.MoveTo(x+24*s, y+17*s)
	pdf.LineTo(x+16*s, y+17*s)
	pdf.LineTo(x+16*s, y+47*s)
	pdf.LineTo(x+24*s, y+47*s)
	pdf.DrawPath("D")
	pdf.MoveTo(x+40*s, y+17*s)
	pdf.LineTo(x+48*s, y+17*s)
	pdf.LineTo(x+48*s, y+47*s)
	pdf.LineTo(x+40*s, y+47*s)
	pdf.DrawPath("D")
	// Restore stroke state so later bordered cells keep their default look.
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetLineWidth(0.2)
	pdf.SetLineCapStyle("butt")
}

// RenderOrderInvoicePDF renders the fixed English invoice template for a
// completed order. It is a pure function over the order row and buyer email;
// eligibility (ownership, COMPLETED status) is enforced by the caller.
func RenderOrderInvoicePDF(order *dbent.PaymentOrder, buyerEmail string) ([]byte, error) {
	currency := PaymentOrderCurrency(order)
	base, fee := invoiceFeeSplit(order.PayAmount, order.FeeRate)
	desc, note := invoiceLineDescription(order)

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("Invoice "+order.OutTradeNo, true)
	pdf.SetAutoPageBreak(true, 20)
	pdf.AddPage()

	// Header: title left, brand logo right
	pageW, _ := pdf.GetPageSize()
	_, topMargin, rightMargin, _ := pdf.GetMargins()
	logoSize := 12.0
	// MoveTo/LineTo in the logo path move fpdf's cursor; save and restore it
	// so the title cell still starts at the left margin.
	headerX, headerY := pdf.GetXY()
	drawInvoiceLogo(pdf, pageW-rightMargin-logoSize, topMargin, logoSize)
	pdf.SetXY(headerX, headerY)
	pdf.SetFont("Helvetica", "B", 24)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(95, 12, "INVOICE", "", 1, "L", false, 0, "")
	pdf.Ln(6)

	// Meta block
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(75, 85, 99)
	pdf.CellFormat(30, 6, "Invoice No:", "", 0, "L", false, 0, "")
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(0, 6, order.OutTradeNo, "", 1, "L", false, 0, "")
	pdf.SetTextColor(75, 85, 99)
	pdf.CellFormat(30, 6, "Date:", "", 0, "L", false, 0, "")
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(0, 6, invoiceDateFor(order).UTC().Format("Jan 2, 2006"), "", 1, "L", false, 0, "")
	pdf.Ln(8)

	// From / Bill To columns
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(75, 85, 99)
	pdf.CellFormat(95, 6, "From", "", 0, "L", false, 0, "")
	pdf.CellFormat(95, 6, "Bill To", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(95, 6, invoiceSellerName, "", 0, "L", false, 0, "")
	pdf.CellFormat(95, 6, buyerEmail, "", 1, "L", false, 0, "")
	pdf.CellFormat(95, 6, invoiceSellerEmail, "", 1, "L", false, 0, "")
	pdf.Ln(10)

	// Line items table
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(243, 244, 246)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(140, 8, "Description", "B", 0, "L", true, 0, "")
	pdf.CellFormat(50, 8, "Amount", "B", 1, "R", true, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(140, 8, desc, "", 0, "L", false, 0, "")
	pdf.CellFormat(50, 8, invoiceAmount(currency, base), "", 1, "R", false, 0, "")
	if note != "" {
		pdf.SetTextColor(107, 114, 128)
		pdf.SetFont("Helvetica", "", 9)
		pdf.CellFormat(140, 5, note, "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.SetTextColor(17, 24, 39)
	}
	if fee > 0 {
		pdf.CellFormat(140, 8, fmt.Sprintf("Processing Fee (%.4g%%)", order.FeeRate), "", 0, "L", false, 0, "")
		pdf.CellFormat(50, 8, invoiceAmount(currency, fee), "", 1, "R", false, 0, "")
	}

	// Total
	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(140, 10, "Total Paid", "T", 0, "L", false, 0, "")
	pdf.CellFormat(50, 10, invoiceAmount(currency, order.PayAmount), "T", 1, "R", false, 0, "")
	pdf.Ln(4)

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(75, 85, 99)
	pdf.CellFormat(0, 6, "Payment Method: "+invoicePaymentMethodName(order.PaymentType), "", 1, "L", false, 0, "")
	pdf.Ln(12)

	// Footer disclaimer
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(107, 114, 128)
	pdf.MultiCell(0, 4, fmt.Sprintf(
		"This document is a payment record issued by %s and is not a tax invoice. "+
			"For questions about this order, contact %s.",
		invoiceSellerName, invoiceSellerEmail), "", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render invoice pdf: %w", err)
	}
	return buf.Bytes(), nil
}
