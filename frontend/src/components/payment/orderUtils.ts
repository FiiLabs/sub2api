/**
 * Shared utility functions for payment order display.
 * Used by AdminOrderDetail, AdminOrderTable, AdminRefundDialog, AdminOrdersView, etc.
 */

const STATUS_BADGE_MAP: Record<string, string> = {
  PENDING: 'badge-warning',
  PAID: 'badge-info',
  RECHARGING: 'badge-info',
  COMPLETED: 'badge-success',
  EXPIRED: 'badge-secondary',
  CANCELLED: 'badge-secondary',
  FAILED: 'badge-danger',
  REFUND_REQUESTED: 'badge-warning',
  REFUNDING: 'badge-warning',
  PARTIALLY_REFUNDED: 'badge-warning',
  REFUNDED: 'badge-info',
  REFUND_FAILED: 'badge-danger',
}

const REFUNDABLE_STATUSES = ['COMPLETED', 'PARTIALLY_REFUNDED', 'REFUND_REQUESTED', 'REFUND_FAILED']

export function statusBadgeClass(status: string): string {
  return STATUS_BADGE_MAP[status] || 'badge-secondary'
}

export function canRefund(status: string): boolean {
  return REFUNDABLE_STATUSES.includes(status)
}

export function formatOrderDateTime(dateStr: string): string {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString()
}

/**
 * Format a monetary amount using the order's ISO currency code via Intl,
 * falling back to a "CODE 12.34" prefix when the code is unknown to Intl.
 * Currency defaults to CNY, matching the backend's DefaultPaymentCurrency
 * for legacy orders that predate per-order currency.
 */
export function formatCurrencyAmount(amount: number, currency?: string | null, locale?: string): string {
  const code = (currency || 'CNY').toUpperCase()
  try {
    return new Intl.NumberFormat(locale, { style: 'currency', currency: code }).format(amount)
  } catch {
    return `${code} ${amount.toFixed(2)}`
  }
}

/**
 * Currency the credited amount (`order.amount`) is denominated in:
 * balance top-ups credit USD to the account; subscription/other orders use
 * CNY-denominated plan pricing.
 */
export function creditedCurrency(orderType: string): string {
  return orderType === 'balance' ? 'USD' : 'CNY'
}
