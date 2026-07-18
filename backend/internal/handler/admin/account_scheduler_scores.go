package admin

import (
	"context"
	"log/slog"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type AccountSchedulerScore struct {
	BaseScore             float64 `json:"base_score"`
	StickyScore           float64 `json:"sticky_score"`
	StickyScoreInfinity   bool    `json:"sticky_score_infinity"`
	StickyWeightedEnabled bool    `json:"sticky_weighted_enabled"`
}

type AccountSchedulerGroupScore struct {
	GroupID       *int64 `json:"group_id"`
	GroupName     string `json:"group_name,omitempty"`
	GroupPriority *int   `json:"group_priority,omitempty"`
	AccountSchedulerScore
}

type openAIAccountSchedulerScorePoolLister interface {
	ListOpenAISchedulableAccountsForSchedulerScore(ctx context.Context, groupID *int64) ([]service.Account, error)
}

type accountSchedulerScoreFilterPoolLister interface {
	ListAccountsForSchedulerScoreFilter(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]service.Account, error)
}

func (h *AccountHandler) scoreOpenAIAccountSchedulerPool(ctx context.Context, accounts []service.Account) map[int64]AccountSchedulerScore {
	if len(accounts) == 0 {
		return nil
	}
	openAIAccounts := make([]*service.Account, 0, len(accounts))
	loadReq := make([]service.AccountWithConcurrency, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != service.PlatformOpenAI {
			continue
		}
		openAIAccounts = append(openAIAccounts, account)
		loadReq = append(loadReq, service.AccountWithConcurrency{ID: account.ID, MaxConcurrency: account.EffectiveLoadFactor()})
	}
	if len(openAIAccounts) == 0 {
		return nil
	}

	loadMap := map[int64]*service.AccountLoadInfo{}
	if h.concurrencyService != nil {
		if batchLoad, err := h.concurrencyService.GetAccountsLoadBatch(ctx, loadReq); err == nil && batchLoad != nil {
			loadMap = batchLoad
		}
	}
	var scores map[int64]service.OpenAIAccountSchedulerScoreSnapshot
	if h.rateLimitService != nil {
		scores = h.rateLimitService.BuildOpenAIAccountSchedulerScoreSnapshot(ctx, openAIAccounts, loadMap)
	} else {
		scores = service.BuildOpenAIAccountSchedulerScoreSnapshot(openAIAccounts, loadMap)
	}
	result := make(map[int64]AccountSchedulerScore, len(scores))
	for accountID, score := range scores {
		result[accountID] = AccountSchedulerScore{
			BaseScore:             score.BaseScore,
			StickyScore:           score.StickyScore,
			StickyScoreInfinity:   score.StickyScoreInfinity,
			StickyWeightedEnabled: score.StickyWeightedEnabled,
		}
	}
	return result
}

func (h *AccountHandler) buildOpenAIAccountSchedulerScores(
	ctx context.Context,
	accounts []service.Account,
	filterPool []service.Account,
) (map[int64]*AccountSchedulerScore, map[int64][]AccountSchedulerGroupScore) {
	if len(accounts) == 0 {
		return nil, nil
	}
	if len(filterPool) == 0 {
		filterPool = accounts
	}
	baseScores := make(map[int64]*AccountSchedulerScore)
	for accountID, score := range h.scoreOpenAIAccountSchedulerPool(ctx, filterPool) {
		copiedScore := score
		baseScores[accountID] = &copiedScore
	}

	pageOpenAIAccountIDs := make(map[int64]struct{})
	groupIDs := make(map[int64]struct{})
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != service.PlatformOpenAI {
			continue
		}
		pageOpenAIAccountIDs[account.ID] = struct{}{}
		for _, accountGroup := range account.AccountGroups {
			if accountGroup.GroupID > 0 {
				groupIDs[accountGroup.GroupID] = struct{}{}
			}
		}
		for _, groupID := range account.GroupIDs {
			if groupID > 0 {
				groupIDs[groupID] = struct{}{}
			}
		}
	}
	if len(pageOpenAIAccountIDs) == 0 {
		return baseScores, nil
	}

	groupScoresByAccount := make(map[int64][]AccountSchedulerGroupScore)
	scoreGroupPool := func(groupID *int64, groupNameByID map[int64]string, groupPriorityByAccount map[int64]int, pool []service.Account) {
		for accountID, schedulerScore := range h.scoreOpenAIAccountSchedulerPool(ctx, pool) {
			if _, ok := pageOpenAIAccountIDs[accountID]; !ok {
				continue
			}
			groupScore := AccountSchedulerGroupScore{GroupID: groupID, AccountSchedulerScore: schedulerScore}
			if groupID != nil {
				groupScore.GroupName = groupNameByID[*groupID]
				if priority, ok := groupPriorityByAccount[accountID]; ok {
					groupScore.GroupPriority = &priority
				}
			}
			groupScoresByAccount[accountID] = append(groupScoresByAccount[accountID], groupScore)
		}
	}

	if lister, ok := h.adminService.(openAIAccountSchedulerScorePoolLister); ok {
		groupIDList := make([]int64, 0, len(groupIDs))
		for groupID := range groupIDs {
			groupIDList = append(groupIDList, groupID)
		}
		sort.Slice(groupIDList, func(i, j int) bool { return groupIDList[i] < groupIDList[j] })
		for _, groupID := range groupIDList {
			gid := groupID
			pool, err := lister.ListOpenAISchedulableAccountsForSchedulerScore(ctx, &gid)
			if err != nil {
				slog.Warn("openai_scheduler_group_score_pool_failed", "group_id", gid, "error", err)
				continue
			}
			groupNameByID := make(map[int64]string)
			groupPriorityByAccount := make(map[int64]int)
			for i := range pool {
				for _, accountGroup := range pool[i].AccountGroups {
					if accountGroup.GroupID != gid {
						continue
					}
					groupPriorityByAccount[pool[i].ID] = accountGroup.Priority
					if accountGroup.Group != nil {
						groupNameByID[gid] = accountGroup.Group.Name
					}
				}
			}
			scoreGroupPool(&gid, groupNameByID, groupPriorityByAccount, pool)
		}
	}
	for accountID := range groupScoresByAccount {
		sort.SliceStable(groupScoresByAccount[accountID], func(i, j int) bool {
			return *groupScoresByAccount[accountID][i].GroupID < *groupScoresByAccount[accountID][j].GroupID
		})
	}
	return baseScores, groupScoresByAccount
}

func (h *AccountHandler) listAccountSchedulerScoreFilterPool(
	ctx context.Context,
	platform, accountType, status, search string,
	groupID int64,
	privacyMode string,
) []service.Account {
	if h.adminService == nil || (platform != "" && platform != service.PlatformOpenAI) {
		return nil
	}
	lister, ok := h.adminService.(accountSchedulerScoreFilterPoolLister)
	if !ok {
		return nil
	}
	accounts, err := lister.ListAccountsForSchedulerScoreFilter(ctx, platform, accountType, status, search, groupID, privacyMode)
	if err != nil {
		slog.Warn("openai_scheduler_filter_score_pool_failed", "error", err)
		return nil
	}
	return accounts
}
