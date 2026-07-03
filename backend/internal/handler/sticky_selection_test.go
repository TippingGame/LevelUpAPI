package handler

import "testing"

func TestReconcileStickyBoundAccount(t *testing.T) {
	tests := []struct {
		name              string
		previousBoundID   int64
		selectedAccountID int64
		latestCachedID    int64
		wantBoundID       int64
		wantAction        stickyReconcileAction
	}{
		{
			name:              "no previous binding stays unchanged",
			previousBoundID:   0,
			selectedAccountID: 2,
			latestCachedID:    2,
			wantBoundID:       0,
			wantAction:        stickyReconcileUnchanged,
		},
		{
			name:              "selected account already honored",
			previousBoundID:   2,
			selectedAccountID: 2,
			latestCachedID:    2,
			wantBoundID:       2,
			wantAction:        stickyReconcileUnchanged,
		},
		{
			name:              "cache cleared stale binding",
			previousBoundID:   1,
			selectedAccountID: 2,
			latestCachedID:    0,
			wantBoundID:       0,
			wantAction:        stickyReconcileCleared,
		},
		{
			name:              "cache replaced binding with selected account",
			previousBoundID:   1,
			selectedAccountID: 2,
			latestCachedID:    2,
			wantBoundID:       2,
			wantAction:        stickyReconcileReplaced,
		},
		{
			name:              "original binding is still authoritative",
			previousBoundID:   1,
			selectedAccountID: 2,
			latestCachedID:    1,
			wantBoundID:       1,
			wantAction:        stickyReconcileUnchanged,
		},
		{
			name:              "concurrent request moved binding elsewhere",
			previousBoundID:   1,
			selectedAccountID: 2,
			latestCachedID:    3,
			wantBoundID:       3,
			wantAction:        stickyReconcileMoved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBoundID, gotAction := reconcileStickyBoundAccount(tt.previousBoundID, tt.selectedAccountID, tt.latestCachedID)
			if gotBoundID != tt.wantBoundID {
				t.Fatalf("bound id = %d, want %d", gotBoundID, tt.wantBoundID)
			}
			if gotAction != tt.wantAction {
				t.Fatalf("action = %s, want %s", gotAction, tt.wantAction)
			}
		})
	}
}

func TestStickySelectionHonored(t *testing.T) {
	tests := []struct {
		name              string
		sessionKey        string
		boundAccountID    int64
		selectedAccountID int64
		want              bool
	}{
		{name: "selected account matches bound account", sessionKey: "sticky", boundAccountID: 1, selectedAccountID: 1, want: true},
		{name: "selected account differs from stale binding", sessionKey: "sticky", boundAccountID: 1, selectedAccountID: 2, want: false},
		{name: "missing session key is not sticky", sessionKey: "", boundAccountID: 1, selectedAccountID: 1, want: false},
		{name: "missing binding is not sticky", sessionKey: "sticky", boundAccountID: 0, selectedAccountID: 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stickySelectionHonored(tt.sessionKey, tt.boundAccountID, tt.selectedAccountID); got != tt.want {
				t.Fatalf("stickySelectionHonored() = %v, want %v", got, tt.want)
			}
		})
	}
}
