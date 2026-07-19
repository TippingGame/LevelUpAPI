package admin

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin/binding"
)

func TestGroupRequestsDecodeExtendedPolicyFields(t *testing.T) {
	body := []byte(`{
		"name":"extended-policy",
		"allow_batch_image_generation":true,
		"batch_image_discount_multiplier":0.55,
		"batch_image_hold_multiplier":0.7,
		"peak_rate_enabled":true,
		"peak_start":"09:00",
		"peak_end":"18:00",
		"peak_rate_multiplier":1.75
	}`)

	var createReq CreateGroupRequest
	if err := json.Unmarshal(body, &createReq); err != nil {
		t.Fatalf("decode create request: %v", err)
	}
	if !createReq.AllowBatchImageGeneration ||
		createReq.BatchImageDiscountMultiplier == nil || *createReq.BatchImageDiscountMultiplier != 0.55 ||
		createReq.BatchImageHoldMultiplier == nil || *createReq.BatchImageHoldMultiplier != 0.7 ||
		!createReq.PeakRateEnabled || createReq.PeakStart != "09:00" || createReq.PeakEnd != "18:00" ||
		createReq.PeakRateMultiplier == nil || *createReq.PeakRateMultiplier != 1.75 {
		t.Fatalf("extended create fields were not decoded: %+v", createReq)
	}

	var updateReq UpdateGroupRequest
	if err := json.Unmarshal(body, &updateReq); err != nil {
		t.Fatalf("decode update request: %v", err)
	}
	if updateReq.AllowBatchImageGeneration == nil || !*updateReq.AllowBatchImageGeneration ||
		updateReq.PeakRateEnabled == nil || !*updateReq.PeakRateEnabled ||
		updateReq.PeakStart == nil || *updateReq.PeakStart != "09:00" ||
		updateReq.PeakEnd == nil || *updateReq.PeakEnd != "18:00" {
		t.Fatalf("extended update fields were not decoded: %+v", updateReq)
	}
}

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
