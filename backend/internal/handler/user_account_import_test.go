package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestValidateOpenAIImportTargetLevelRejectsProWithoutProxy(t *testing.T) {
	_, err := validateOpenAIImportTargetLevel(importUserAccountCredentialsRequest{
		AccountLevel: service.AccountLevelPro,
	})

	require.ErrorIs(t, err, service.ErrOwnedOpenAIAccountProxyRequired)
}

func TestValidateOpenAIImportTargetLevelAllowsProWithProxy(t *testing.T) {
	proxyID := int64(10)

	level, err := validateOpenAIImportTargetLevel(importUserAccountCredentialsRequest{
		AccountLevel: service.AccountLevelPro,
		ProxyID:      &proxyID,
	})

	require.NoError(t, err)
	require.Equal(t, service.AccountLevelPro, level)
}
