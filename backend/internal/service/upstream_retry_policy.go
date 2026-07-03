package service

import "net/http"

const cloudflareOriginTimeoutStatus = 524

// IsUpstreamReplayUnsafeTimeoutStatus reports timeout statuses that may mean
// the upstream request is still running. Retrying on another account can
// duplicate work and usage, so these statuses should be returned to the caller
// instead of triggering same-account retry or route failover.
func IsUpstreamReplayUnsafeTimeoutStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusGatewayTimeout, cloudflareOriginTimeoutStatus:
		return true
	default:
		return false
	}
}

func isDeterministicClientRequestStatus(statusCode int) bool {
	return statusCode >= http.StatusBadRequest &&
		statusCode < http.StatusInternalServerError &&
		statusCode != http.StatusTooManyRequests
}
