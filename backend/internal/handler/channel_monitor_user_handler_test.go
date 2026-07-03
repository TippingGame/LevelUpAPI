package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFilterUserMonitorViewsByVisibleGroups(t *testing.T) {
	visible := visibleGroupNameSet([]service.Group{
		{Name: " OpenAI Pro "},
		{Name: "claude-shared"},
	})
	views := []*service.UserMonitorView{
		{ID: 1, GroupName: "openai pro"},
		{ID: 2, GroupName: "CLAUDE-SHARED"},
		{ID: 3, GroupName: "other-group"},
		{ID: 4, GroupName: ""},
		nil,
	}

	filtered := filterUserMonitorViewsByVisibleGroups(views, visible)

	require.Len(t, filtered, 2)
	require.Equal(t, int64(1), filtered[0].ID)
	require.Equal(t, int64(2), filtered[1].ID)
}

func TestMonitorGroupVisibleRejectsBlankOrUnknownGroup(t *testing.T) {
	visible := visibleGroupNameSet([]service.Group{{Name: "openai-pro"}})

	require.True(t, monitorGroupVisible(" OPENAI-PRO ", visible))
	require.False(t, monitorGroupVisible("", visible))
	require.False(t, monitorGroupVisible("other", visible))
	require.False(t, monitorGroupVisible("openai-pro", nil))
}
