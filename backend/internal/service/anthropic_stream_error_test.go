package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnthropicStreamErrorStatusBillingTextIsConservative(t *testing.T) {
	status, _ := anthropicStreamErrorStatusAndMessage([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Please include the credit balance field in the report"}}`))
	require.Equal(t, http.StatusBadRequest, status)

	status, _ = anthropicStreamErrorStatusAndMessage([]byte(`{"type":"error","error":{"type":"billing_error","message":"Billing check failed"}}`))
	require.Equal(t, http.StatusPaymentRequired, status)

	status, _ = anthropicStreamErrorStatusAndMessage([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low"}}`))
	require.Equal(t, http.StatusPaymentRequired, status)
}
