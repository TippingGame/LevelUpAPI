package service

import (
	"context"
	"time"
)

const (
	openAIAccountStateUpdateTimeout    = 5 * time.Second
	openAIStopSchedulingBridgeCooldown = 2 * time.Minute
)

func openAIAccountStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, openAIAccountStateUpdateTimeout)
}

func isOpenAIAccount(account *Account) bool {
	return account != nil && (account.Platform == PlatformOpenAI || account.Platform == PlatformGrok)
}

// BlockAccountScheduling installs an immediate in-process scheduling fence.
// Durable state is persisted separately; this bridge closes the window before
// the database/outbox update reaches all scheduler snapshots.
func (s *OpenAIGatewayService) BlockAccountScheduling(account *Account, until time.Time, _ string) {
	if s == nil || !isOpenAIAccount(account) || account.ID <= 0 {
		return
	}
	now := time.Now()
	blockUntil := until
	if !blockUntil.After(now) {
		blockUntil = now.Add(openAIStopSchedulingBridgeCooldown)
	}

	for {
		current, loaded := s.openaiAccountRuntimeBlockUntil.Load(account.ID)
		if !loaded {
			actual, stored := s.openaiAccountRuntimeBlockUntil.LoadOrStore(account.ID, blockUntil)
			if !stored {
				return
			}
			current = actual
		}
		currentUntil, ok := current.(time.Time)
		if !ok || !currentUntil.After(blockUntil) {
			if s.openaiAccountRuntimeBlockUntil.CompareAndSwap(account.ID, current, blockUntil) {
				return
			}
			continue
		}
		return
	}
}

func (s *OpenAIGatewayService) ClearAccountSchedulingBlock(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.openaiAccountRuntimeBlockUntil.Delete(accountID)
}

func (s *OpenAIGatewayService) isOpenAIAccountRuntimeBlocked(account *Account) bool {
	if s == nil || !isOpenAIAccount(account) || account.ID <= 0 {
		return false
	}
	value, ok := s.openaiAccountRuntimeBlockUntil.Load(account.ID)
	if !ok {
		return false
	}
	until, ok := value.(time.Time)
	if !ok || !until.After(time.Now()) {
		s.openaiAccountRuntimeBlockUntil.Delete(account.ID)
		return false
	}
	return true
}
