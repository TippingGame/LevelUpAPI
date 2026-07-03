package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledTestRunnerAccountTesterStub struct {
	result    *ScheduledTestResult
	err       error
	calls     int
	accountID int64
	modelID   string
}

func (s *scheduledTestRunnerAccountTesterStub) RunTestBackground(_ context.Context, accountID int64, modelID string) (*ScheduledTestResult, error) {
	s.calls++
	s.accountID = accountID
	s.modelID = modelID
	return s.result, s.err
}

type scheduledTestRunnerPlanRepoStub struct {
	updateCalls int
	updatedID   int64
	lastRunAt   time.Time
	nextRunAt   time.Time
}

func (s *scheduledTestRunnerPlanRepoStub) Create(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	panic("unexpected Create")
}

func (s *scheduledTestRunnerPlanRepoStub) GetByID(context.Context, int64) (*ScheduledTestPlan, error) {
	panic("unexpected GetByID")
}

func (s *scheduledTestRunnerPlanRepoStub) ListByAccountID(context.Context, int64) ([]*ScheduledTestPlan, error) {
	panic("unexpected ListByAccountID")
}

func (s *scheduledTestRunnerPlanRepoStub) ListDue(context.Context, time.Time) ([]*ScheduledTestPlan, error) {
	panic("unexpected ListDue")
}

func (s *scheduledTestRunnerPlanRepoStub) Update(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	panic("unexpected Update")
}

func (s *scheduledTestRunnerPlanRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete")
}

func (s *scheduledTestRunnerPlanRepoStub) UpdateAfterRun(_ context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	s.updateCalls++
	s.updatedID = id
	s.lastRunAt = lastRunAt
	s.nextRunAt = nextRunAt
	return nil
}

type scheduledTestRunnerResultRepoStub struct {
	createErr  error
	pruneErr   error
	created    []*ScheduledTestResult
	pruneCalls int
	keepCount  int
}

func (s *scheduledTestRunnerResultRepoStub) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	cloned := *result
	cloned.ID = int64(len(s.created) + 1)
	s.created = append(s.created, &cloned)
	return &cloned, nil
}

func (s *scheduledTestRunnerResultRepoStub) ListByPlanID(context.Context, int64, int) ([]*ScheduledTestResult, error) {
	panic("unexpected ListByPlanID")
}

func (s *scheduledTestRunnerResultRepoStub) PruneOldResults(_ context.Context, _ int64, keepCount int) error {
	s.pruneCalls++
	s.keepCount = keepCount
	return s.pruneErr
}

func newScheduledTestRunnerForTest(
	tester scheduledAccountTester,
	planRepo *scheduledTestRunnerPlanRepoStub,
	resultRepo *scheduledTestRunnerResultRepoStub,
) *ScheduledTestRunnerService {
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   NewScheduledTestService(planRepo, resultRepo),
		accountTestSvc: tester,
	}
}

func TestScheduledTestRunner_RunOnePlan_RecordsFailureAndAdvancesOnTesterError(t *testing.T) {
	planRepo := &scheduledTestRunnerPlanRepoStub{}
	resultRepo := &scheduledTestRunnerResultRepoStub{}
	tester := &scheduledTestRunnerAccountTesterStub{err: errors.New("probe transport failed")}
	runner := newScheduledTestRunnerForTest(tester, planRepo, resultRepo)
	plan := &ScheduledTestPlan{
		ID:             12,
		AccountID:      34,
		ModelID:        "gpt-5.1",
		CronExpression: "*/5 * * * *",
		MaxResults:     7,
	}

	runner.runOnePlan(context.Background(), plan)

	require.Equal(t, 1, tester.calls)
	require.Equal(t, plan.AccountID, tester.accountID)
	require.Equal(t, plan.ModelID, tester.modelID)
	require.Len(t, resultRepo.created, 1)
	require.Equal(t, "failed", resultRepo.created[0].Status)
	require.Contains(t, resultRepo.created[0].ErrorMessage, "probe transport failed")
	require.Equal(t, 1, resultRepo.pruneCalls)
	require.Equal(t, plan.MaxResults, resultRepo.keepCount)
	require.Equal(t, 1, planRepo.updateCalls)
	require.Equal(t, plan.ID, planRepo.updatedID)
	require.True(t, planRepo.nextRunAt.After(planRepo.lastRunAt))
}

func TestScheduledTestRunner_RunOnePlan_AdvancesScheduleWhenSavingResultFails(t *testing.T) {
	planRepo := &scheduledTestRunnerPlanRepoStub{}
	resultRepo := &scheduledTestRunnerResultRepoStub{createErr: errors.New("db unavailable")}
	tester := &scheduledTestRunnerAccountTesterStub{result: &ScheduledTestResult{
		Status:     "failed",
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}}
	runner := newScheduledTestRunnerForTest(tester, planRepo, resultRepo)
	plan := &ScheduledTestPlan{
		ID:             56,
		AccountID:      78,
		ModelID:        "claude-sonnet-4-5",
		CronExpression: "*/10 * * * *",
		MaxResults:     0,
	}

	runner.runOnePlan(context.Background(), plan)

	require.Equal(t, 1, tester.calls)
	require.Empty(t, resultRepo.created)
	require.Equal(t, 0, resultRepo.pruneCalls)
	require.Equal(t, 1, planRepo.updateCalls)
	require.Equal(t, plan.ID, planRepo.updatedID)
	require.True(t, planRepo.nextRunAt.After(planRepo.lastRunAt))
}

func TestScheduledTestRunner_RunOnePlan_InvalidCronUsesBackoff(t *testing.T) {
	planRepo := &scheduledTestRunnerPlanRepoStub{}
	resultRepo := &scheduledTestRunnerResultRepoStub{}
	tester := &scheduledTestRunnerAccountTesterStub{result: &ScheduledTestResult{
		Status:     "failed",
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}}
	runner := newScheduledTestRunnerForTest(tester, planRepo, resultRepo)
	plan := &ScheduledTestPlan{
		ID:             90,
		AccountID:      91,
		ModelID:        "gpt-5.1",
		CronExpression: "not-a-cron",
		MaxResults:     5,
	}

	runner.runOnePlan(context.Background(), plan)

	require.Equal(t, 1, tester.calls)
	require.Len(t, resultRepo.created, 1)
	require.Equal(t, 1, planRepo.updateCalls)
	require.Equal(t, plan.ID, planRepo.updatedID)
	require.InDelta(t, float64(scheduledTestInvalidCronBackoff), float64(planRepo.nextRunAt.Sub(planRepo.lastRunAt)), float64(time.Second))
}
