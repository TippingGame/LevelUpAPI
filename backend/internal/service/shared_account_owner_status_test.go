package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sharedOwnerDefRepoStub struct {
	defs []UserAttributeDefinition
}

func (r *sharedOwnerDefRepoStub) Create(_ context.Context, def *UserAttributeDefinition) error {
	def.ID = 1
	r.defs = append(r.defs, *def)
	return nil
}

func (r *sharedOwnerDefRepoStub) GetByID(_ context.Context, id int64) (*UserAttributeDefinition, error) {
	for i := range r.defs {
		if r.defs[i].ID == id {
			return &r.defs[i], nil
		}
	}
	return nil, ErrAttributeDefinitionNotFound
}

func (r *sharedOwnerDefRepoStub) GetByKey(_ context.Context, key string) (*UserAttributeDefinition, error) {
	for i := range r.defs {
		if r.defs[i].Key == key {
			return &r.defs[i], nil
		}
	}
	return nil, ErrAttributeDefinitionNotFound
}

func (r *sharedOwnerDefRepoStub) Update(_ context.Context, def *UserAttributeDefinition) error {
	for i := range r.defs {
		if r.defs[i].ID == def.ID {
			r.defs[i] = *def
			return nil
		}
	}
	return ErrAttributeDefinitionNotFound
}

func (r *sharedOwnerDefRepoStub) Delete(_ context.Context, id int64) error { return nil }

func (r *sharedOwnerDefRepoStub) List(_ context.Context, enabledOnly bool) ([]UserAttributeDefinition, error) {
	out := make([]UserAttributeDefinition, 0, len(r.defs))
	for _, def := range r.defs {
		if enabledOnly && !def.Enabled {
			continue
		}
		out = append(out, def)
	}
	return out, nil
}

func (r *sharedOwnerDefRepoStub) UpdateDisplayOrders(_ context.Context, _ map[int64]int) error {
	return nil
}

func (r *sharedOwnerDefRepoStub) ExistsByKey(_ context.Context, key string) (bool, error) {
	for _, def := range r.defs {
		if def.Key == key {
			return true, nil
		}
	}
	return false, nil
}

type sharedOwnerValueRepoStub struct {
	values map[int64][]UserAttributeValue
}

func (r *sharedOwnerValueRepoStub) GetByUserID(_ context.Context, userID int64) ([]UserAttributeValue, error) {
	return append([]UserAttributeValue(nil), r.values[userID]...), nil
}

func (r *sharedOwnerValueRepoStub) GetByUserIDs(_ context.Context, userIDs []int64) ([]UserAttributeValue, error) {
	var out []UserAttributeValue
	for _, userID := range userIDs {
		out = append(out, r.values[userID]...)
	}
	return out, nil
}

func (r *sharedOwnerValueRepoStub) UpsertBatch(_ context.Context, userID int64, inputs []UpdateUserAttributeInput) error {
	if r.values == nil {
		r.values = map[int64][]UserAttributeValue{}
	}
	existing := r.values[userID]
	for _, input := range inputs {
		updated := false
		for i := range existing {
			if existing[i].AttributeID == input.AttributeID {
				existing[i].Value = input.Value
				existing[i].UpdatedAt = time.Now()
				updated = true
			}
		}
		if !updated {
			existing = append(existing, UserAttributeValue{
				ID:          int64(len(existing) + 1),
				UserID:      userID,
				AttributeID: input.AttributeID,
				Value:       input.Value,
			})
		}
	}
	r.values[userID] = existing
	return nil
}

func (r *sharedOwnerValueRepoStub) DeleteByAttributeID(_ context.Context, _ int64) error { return nil }
func (r *sharedOwnerValueRepoStub) DeleteByUserID(_ context.Context, _ int64) error      { return nil }

func TestResolveSharedAccountOwnerStatus(t *testing.T) {
	def := UserAttributeDefinition{
		ID:      9,
		Key:     "shared_account_owner",
		Name:    "共享号主",
		Type:    AttributeTypeSelect,
		Enabled: true,
		Options: []UserAttributeOption{
			{Value: "true", Label: "手动开启"},
			{Value: "false", Label: "手动关闭"},
		},
	}

	tests := []struct {
		name          string
		total         float64
		attrValue     string
		wantEnabled   bool
		wantMode      string
		wantRemaining float64
	}{
		{
			name:          "below threshold stays locked",
			total:         60,
			wantEnabled:   false,
			wantMode:      SharedAccountOwnerModeNone,
			wantRemaining: 40,
		},
		{
			name:        "threshold unlocks automatically",
			total:       100,
			wantEnabled: true,
			wantMode:    SharedAccountOwnerModeAuto,
		},
		{
			name:          "manual off overrides threshold",
			total:         180,
			attrValue:     "false",
			wantEnabled:   false,
			wantMode:      SharedAccountOwnerModeManualOff,
			wantRemaining: 0,
		},
		{
			name:          "manual on overrides missing threshold",
			total:         12,
			attrValue:     "true",
			wantEnabled:   true,
			wantMode:      SharedAccountOwnerModeManualOn,
			wantRemaining: 88,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valueRepo := &sharedOwnerValueRepoStub{values: map[int64][]UserAttributeValue{}}
			if tt.attrValue != "" {
				valueRepo.values[1] = []UserAttributeValue{{
					ID:          1,
					UserID:      1,
					AttributeID: def.ID,
					Value:       tt.attrValue,
				}}
			}
			svc := NewUserAttributeService(&sharedOwnerDefRepoStub{defs: []UserAttributeDefinition{def}}, valueRepo)
			status, err := svc.ResolveSharedAccountOwnerStatus(context.Background(), &User{
				ID:             1,
				TotalRecharged: tt.total,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantEnabled, status.Enabled)
			require.Equal(t, tt.wantMode, status.Mode)
			require.InDelta(t, tt.wantRemaining, status.Remaining, 0.0001)
		})
	}
}
