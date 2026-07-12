package admin

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
)

func TestGroupRequestsAllowGrokPlatform(t *testing.T) {
	tests := []struct {
		name    string
		request any
	}{
		{
			name: "create",
			request: CreateGroupRequest{
				Name:     "grok-pool",
				Platform: "grok",
			},
		},
		{
			name: "update",
			request: UpdateGroupRequest{
				Platform: "grok",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := binding.Validator.ValidateStruct(tt.request); err != nil {
				t.Fatalf("expected Grok platform to pass request validation: %v", err)
			}
		})
	}
}

func TestGroupRequestsRejectUnknownPlatform(t *testing.T) {
	tests := []struct {
		name    string
		request any
	}{
		{
			name: "create",
			request: CreateGroupRequest{
				Name:     "unknown-pool",
				Platform: "unknown",
			},
		},
		{
			name: "update",
			request: UpdateGroupRequest{
				Platform: "unknown",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := binding.Validator.ValidateStruct(tt.request); err == nil {
				t.Fatal("expected an unknown platform to fail request validation")
			}
		})
	}
}
