package service

import (
	"context"
	"time"
)

const (
	securitySwitchesCacheTTL  = 5 * time.Second
	securitySwitchesErrorTTL  = time.Second
	securitySwitchesDBTimeout = 2 * time.Second
)

type cachedSecuritySwitches struct {
	sessionBindingEnabled bool
	stepUpEnabled         bool
	expiresAt             int64
}

func (s *SettingService) getSecuritySwitches(ctx context.Context) cachedSecuritySwitches {
	if s == nil || s.settingRepo == nil {
		return cachedSecuritySwitches{}
	}
	if cached := s.loadSecuritySwitchesCache(); cached != nil {
		return *cached
	}

	s.securitySwitchesMu.Lock()
	defer s.securitySwitchesMu.Unlock()

	if cached := s.loadSecuritySwitchesCache(); cached != nil {
		return *cached
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), securitySwitchesDBTimeout)
	defer cancel()

	values, err := s.settingRepo.GetMultiple(dbCtx, []string{
		SettingKeySessionBindingEnabled,
		SettingKeyStepUpEnabled,
	})
	ttl := securitySwitchesCacheTTL
	entry := &cachedSecuritySwitches{}
	if err != nil {
		ttl = securitySwitchesErrorTTL
	} else {
		entry.sessionBindingEnabled = values[SettingKeySessionBindingEnabled] == "true"
		entry.stepUpEnabled = values[SettingKeyStepUpEnabled] == "true"
	}
	entry.expiresAt = time.Now().Add(ttl).UnixNano()
	s.securitySwitchesCache.Store(entry)
	return *entry
}

func (s *SettingService) loadSecuritySwitchesCache() *cachedSecuritySwitches {
	cached, _ := s.securitySwitchesCache.Load().(*cachedSecuritySwitches)
	if cached == nil || time.Now().UnixNano() >= cached.expiresAt {
		return nil
	}
	return cached
}

func (s *SettingService) refreshSecuritySwitchesCache(sessionBindingEnabled, stepUpEnabled bool) {
	if s == nil {
		return
	}
	s.securitySwitchesMu.Lock()
	s.securitySwitchesCache.Store(&cachedSecuritySwitches{
		sessionBindingEnabled: sessionBindingEnabled,
		stepUpEnabled:         stepUpEnabled,
		expiresAt:             time.Now().Add(securitySwitchesCacheTTL).UnixNano(),
	})
	s.securitySwitchesMu.Unlock()
}
