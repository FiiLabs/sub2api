//go:build unit

package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveStripeMethodTypesIncludesCrypto(t *testing.T) {
	t.Parallel()

	methods := resolveStripeMethodTypes("card, crypto, link")

	require.Equal(t, []string{"card", "crypto", "link"}, methods)
}

func TestResolveStripeMethodTypesFallsBackToCard(t *testing.T) {
	t.Parallel()

	methods := resolveStripeMethodTypes("unknown")

	require.Equal(t, []string{"card"}, methods)
}
