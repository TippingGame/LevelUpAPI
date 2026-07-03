package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSubscriptionType(t *testing.T) {
	require.Equal(t, SubscriptionTypeStandard, NormalizeSubscriptionType(""))
	require.Equal(t, SubscriptionTypeStandard, NormalizeSubscriptionType(" STANDARD "))
	require.Equal(t, SubscriptionTypeSubscription, NormalizeSubscriptionType(" Subscription "))
	require.True(t, IsStandardSubscriptionType(" standard "))
	require.True(t, (&Group{SubscriptionType: " subscription "}).IsSubscriptionType())
}
