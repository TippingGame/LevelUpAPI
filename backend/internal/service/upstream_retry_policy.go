package service

import "net/http"

const cloudflareOriginTimeoutStatus = 524

// IsUpstreamReplayUnsafeTimeoutStatus reports timeout statuses that may mean
// the upstream request is still running. Retrying on another account can
// duplicate work and usage, so these statuses should be returned to the caller
// instead of triggering account or route failover.
func IsUpstreamReplayUnsafeTimeoutStatus(statusCode int) bool {
	return statusCode == http.StatusGatewayTimeout || statusCode == cloudflareOriginTimeoutStatus
}
