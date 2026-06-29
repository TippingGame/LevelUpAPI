//go:build unit

package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSchedulerGroupPayloadEmptyGroupsDedupesLikeLiteralNil(t *testing.T) {
	accountID := int64(42)

	keyLiteralNil := schedulerOutboxDedupKey("account_changed", &accountID, nil, nil)

	emptyGroupsPayload := buildSchedulerGroupPayload(nil)
	require.Nil(t, emptyGroupsPayload)

	var payloadJSON []byte
	if emptyGroupsPayload != nil {
		t.Fatalf("empty scheduler group payload must be untyped nil")
	}
	keyEmptyGroups := schedulerOutboxDedupKey("account_changed", &accountID, nil, payloadJSON)

	require.Equal(t, keyLiteralNil, keyEmptyGroups)
}
