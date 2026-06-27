package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// UserAttributeService handles attribute management
type UserAttributeService struct {
	defRepo   UserAttributeDefinitionRepository
	valueRepo UserAttributeValueRepository
}

// NewUserAttributeService creates a new service instance
func NewUserAttributeService(
	defRepo UserAttributeDefinitionRepository,
	valueRepo UserAttributeValueRepository,
) *UserAttributeService {
	return &UserAttributeService{
		defRepo:   defRepo,
		valueRepo: valueRepo,
	}
}

// CreateDefinition creates a new attribute definition
func (s *UserAttributeService) CreateDefinition(ctx context.Context, input CreateAttributeDefinitionInput) (*UserAttributeDefinition, error) {
	// Validate type
	if !isValidAttributeType(input.Type) {
		return nil, ErrInvalidAttributeType
	}

	// Check if key exists
	exists, err := s.defRepo.ExistsByKey(ctx, input.Key)
	if err != nil {
		return nil, fmt.Errorf("check key exists: %w", err)
	}
	if exists {
		return nil, ErrAttributeKeyExists
	}

	def := &UserAttributeDefinition{
		Key:         input.Key,
		Name:        input.Name,
		Description: input.Description,
		Type:        input.Type,
		Options:     input.Options,
		Required:    input.Required,
		Validation:  input.Validation,
		Placeholder: input.Placeholder,
		Enabled:     input.Enabled,
	}

	if err := validateDefinitionPattern(def); err != nil {
		return nil, err
	}

	if err := s.defRepo.Create(ctx, def); err != nil {
		return nil, fmt.Errorf("create definition: %w", err)
	}

	return def, nil
}

// GetDefinition retrieves a definition by ID
func (s *UserAttributeService) GetDefinition(ctx context.Context, id int64) (*UserAttributeDefinition, error) {
	return s.defRepo.GetByID(ctx, id)
}

// ListDefinitions lists all definitions
func (s *UserAttributeService) ListDefinitions(ctx context.Context, enabledOnly bool) ([]UserAttributeDefinition, error) {
	return s.defRepo.List(ctx, enabledOnly)
}

// UpdateDefinition updates an existing definition
func (s *UserAttributeService) UpdateDefinition(ctx context.Context, id int64, input UpdateAttributeDefinitionInput) (*UserAttributeDefinition, error) {
	def, err := s.defRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		def.Name = *input.Name
	}
	if input.Description != nil {
		def.Description = *input.Description
	}
	if input.Type != nil {
		if !isValidAttributeType(*input.Type) {
			return nil, ErrInvalidAttributeType
		}
		def.Type = *input.Type
	}
	if input.Options != nil {
		def.Options = *input.Options
	}
	if input.Required != nil {
		def.Required = *input.Required
	}
	if input.Validation != nil {
		def.Validation = *input.Validation
	}
	if input.Placeholder != nil {
		def.Placeholder = *input.Placeholder
	}
	if input.Enabled != nil {
		def.Enabled = *input.Enabled
	}

	if err := validateDefinitionPattern(def); err != nil {
		return nil, err
	}

	if err := s.defRepo.Update(ctx, def); err != nil {
		return nil, fmt.Errorf("update definition: %w", err)
	}

	return def, nil
}

// DeleteDefinition soft-deletes a definition and hard-deletes associated values
func (s *UserAttributeService) DeleteDefinition(ctx context.Context, id int64) error {
	// Check if definition exists
	_, err := s.defRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// First delete all values (hard delete)
	if err := s.valueRepo.DeleteByAttributeID(ctx, id); err != nil {
		return fmt.Errorf("delete values: %w", err)
	}

	// Then soft-delete the definition
	if err := s.defRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete definition: %w", err)
	}

	return nil
}

// ReorderDefinitions updates display order for multiple definitions
func (s *UserAttributeService) ReorderDefinitions(ctx context.Context, orders map[int64]int) error {
	return s.defRepo.UpdateDisplayOrders(ctx, orders)
}

// GetUserAttributes retrieves all attribute values for a user
func (s *UserAttributeService) GetUserAttributes(ctx context.Context, userID int64) ([]UserAttributeValue, error) {
	return s.valueRepo.GetByUserID(ctx, userID)
}

// GetBatchUserAttributes retrieves attribute values for multiple users
// Returns a map of userID -> map of attributeID -> value
func (s *UserAttributeService) GetBatchUserAttributes(ctx context.Context, userIDs []int64) (map[int64]map[int64]string, error) {
	values, err := s.valueRepo.GetByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	result := make(map[int64]map[int64]string)
	for _, v := range values {
		if result[v.UserID] == nil {
			result[v.UserID] = make(map[int64]string)
		}
		result[v.UserID][v.AttributeID] = v.Value
	}

	return result, nil
}

// ResolveSharedAccountOwnerStatus resolves the shared account owner feature gate.
// Manual off overrides the automatic threshold; manual on grants access directly.
func (s *UserAttributeService) ResolveSharedAccountOwnerStatus(ctx context.Context, user *User) (SharedAccountOwnerStatus, error) {
	status := SharedAccountOwnerStatusFromUser(user)
	if user == nil || user.ID <= 0 || s == nil || s.defRepo == nil || s.valueRepo == nil {
		return status, nil
	}

	override, attributeID, err := s.sharedAccountOwnerOverride(ctx, user.ID)
	if err != nil {
		return status, err
	}
	if attributeID != nil {
		status.AttributeID = attributeID
	}
	if override != nil {
		status.ManualOverride = override
		if *override {
			status.Enabled = true
			status.Mode = SharedAccountOwnerModeManualOn
			status.Reasons = []string{"manual_on"}
			return status, nil
		}
		status.Enabled = false
		status.Mode = SharedAccountOwnerModeManualOff
		status.Reasons = []string{"manual_off"}
		return status, nil
	}

	if status.TotalRecharged+1e-9 >= status.Threshold {
		status.Enabled = true
		status.Mode = SharedAccountOwnerModeAuto
		status.Reasons = []string{"recharge_threshold_met"}
	}
	return status, nil
}

// UserHasSharedAccountOwnerTitle reports whether the user has been granted the
// shared account owner title via the existing custom user attributes system.
func (s *UserAttributeService) UserHasSharedAccountOwnerTitle(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.defRepo == nil || s.valueRepo == nil || userID <= 0 {
		return false, nil
	}

	defs, err := s.defRepo.List(ctx, true)
	if err != nil {
		return false, err
	}
	if len(defs) == 0 {
		return false, nil
	}

	values, err := s.valueRepo.GetByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	valueByAttributeID := make(map[int64]string, len(values))
	for _, value := range values {
		valueByAttributeID[value.AttributeID] = value.Value
	}

	for _, def := range defs {
		rawValue, ok := valueByAttributeID[def.ID]
		if !ok || strings.TrimSpace(rawValue) == "" {
			continue
		}
		override, matched := sharedAccountOwnerOverrideFromValue(def, rawValue)
		if matched && override != nil && *override {
			return true, nil
		}
	}
	return false, nil
}

func (s *UserAttributeService) SetSharedAccountOwnerOverride(ctx context.Context, userID int64, override *bool) (SharedAccountOwnerStatus, error) {
	status := SharedAccountOwnerStatusFromUser(&User{ID: userID})
	if userID <= 0 {
		return status, nil
	}
	if s == nil || s.defRepo == nil || s.valueRepo == nil {
		return status, fmt.Errorf("user attribute service is unavailable")
	}

	def, err := s.ensureSharedAccountOwnerDefinition(ctx)
	if err != nil {
		return status, err
	}

	value := ""
	if override != nil {
		if *override {
			value = "true"
		} else {
			value = "false"
		}
	}
	if err := s.valueRepo.UpsertBatch(ctx, userID, []UpdateUserAttributeInput{{
		AttributeID: def.ID,
		Value:       value,
	}}); err != nil {
		return status, err
	}

	status.AttributeID = &def.ID
	status.ManualOverride = override
	if override != nil {
		if *override {
			status.Enabled = true
			status.Mode = SharedAccountOwnerModeManualOn
		} else {
			status.Enabled = false
			status.Mode = SharedAccountOwnerModeManualOff
		}
	}
	return status, nil
}

// UpdateUserAttributes batch updates attribute values for a user
func (s *UserAttributeService) UpdateUserAttributes(ctx context.Context, userID int64, inputs []UpdateUserAttributeInput) error {
	// Validate all values before updating
	defs, err := s.defRepo.List(ctx, true)
	if err != nil {
		return fmt.Errorf("list definitions: %w", err)
	}

	defMap := make(map[int64]*UserAttributeDefinition, len(defs))
	for i := range defs {
		defMap[defs[i].ID] = &defs[i]
	}

	for _, input := range inputs {
		def, ok := defMap[input.AttributeID]
		if !ok {
			return ErrAttributeDefinitionNotFound
		}

		if err := s.validateValue(def, input.Value); err != nil {
			return err
		}
	}

	return s.valueRepo.UpsertBatch(ctx, userID, inputs)
}

// validateValue validates a value against its definition
func (s *UserAttributeService) validateValue(def *UserAttributeDefinition, value string) error {
	// Skip validation for empty non-required fields
	if value == "" && !def.Required {
		return nil
	}

	// Required check
	if def.Required && value == "" {
		return validationError(fmt.Sprintf("%s is required", def.Name))
	}

	v := def.Validation

	// String length validation
	if v.MinLength != nil && len(value) < *v.MinLength {
		return validationError(fmt.Sprintf("%s must be at least %d characters", def.Name, *v.MinLength))
	}
	if v.MaxLength != nil && len(value) > *v.MaxLength {
		return validationError(fmt.Sprintf("%s must be at most %d characters", def.Name, *v.MaxLength))
	}

	// Number validation
	if def.Type == AttributeTypeNumber && value != "" {
		num, err := strconv.Atoi(value)
		if err != nil {
			return validationError(fmt.Sprintf("%s must be a number", def.Name))
		}
		if v.Min != nil && num < *v.Min {
			return validationError(fmt.Sprintf("%s must be at least %d", def.Name, *v.Min))
		}
		if v.Max != nil && num > *v.Max {
			return validationError(fmt.Sprintf("%s must be at most %d", def.Name, *v.Max))
		}
	}

	// Pattern validation
	if v.Pattern != nil && *v.Pattern != "" && value != "" {
		re, err := regexp.Compile(*v.Pattern)
		if err != nil {
			return validationError(def.Name + " has an invalid pattern")
		}
		if !re.MatchString(value) {
			msg := def.Name + " format is invalid"
			if v.Message != nil && *v.Message != "" {
				msg = *v.Message
			}
			return validationError(msg)
		}
	}

	// Select validation
	if def.Type == AttributeTypeSelect && value != "" {
		found := false
		for _, opt := range def.Options {
			if opt.Value == value {
				found = true
				break
			}
		}
		if !found {
			return validationError(fmt.Sprintf("%s: invalid option", def.Name))
		}
	}

	// Multi-select validation (stored as JSON array)
	if def.Type == AttributeTypeMultiSelect && value != "" {
		var values []string
		if err := json.Unmarshal([]byte(value), &values); err != nil {
			// Try comma-separated fallback
			values = strings.Split(value, ",")
		}
		for _, val := range values {
			val = strings.TrimSpace(val)
			found := false
			for _, opt := range def.Options {
				if opt.Value == val {
					found = true
					break
				}
			}
			if !found {
				return validationError(fmt.Sprintf("%s: invalid option %s", def.Name, val))
			}
		}
	}

	return nil
}

func SharedAccountOwnerStatusFromUser(user *User) SharedAccountOwnerStatus {
	total := 0.0
	if user != nil {
		total = math.Max(0, user.TotalRecharged)
	}
	threshold := SharedAccountOwnerRechargeThreshold
	progress := 0.0
	if threshold > 0 {
		progress = math.Min(1, total/threshold)
	}
	status := SharedAccountOwnerStatus{
		Enabled:        false,
		Mode:           SharedAccountOwnerModeNone,
		Threshold:      threshold,
		TotalRecharged: total,
		Progress:       progress,
		Remaining:      math.Max(0, threshold-total),
		Reasons:        []string{"recharge_threshold_pending"},
	}
	if total+1e-9 >= threshold {
		status.Enabled = true
		status.Mode = SharedAccountOwnerModeAuto
		status.Reasons = []string{"recharge_threshold_met"}
	}
	return status
}

func (s *UserAttributeService) sharedAccountOwnerOverride(ctx context.Context, userID int64) (*bool, *int64, error) {
	defs, err := s.defRepo.List(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	if len(defs) == 0 {
		return nil, nil, nil
	}

	values, err := s.valueRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	valueByAttributeID := make(map[int64]string, len(values))
	for _, value := range values {
		valueByAttributeID[value.AttributeID] = value.Value
	}

	var matchedAttributeID *int64
	for _, def := range defs {
		rawValue, ok := valueByAttributeID[def.ID]
		if !ok {
			continue
		}
		override, matched := sharedAccountOwnerOverrideFromValue(def, rawValue)
		if !matched {
			continue
		}
		id := def.ID
		matchedAttributeID = &id
		if override != nil {
			return override, matchedAttributeID, nil
		}
	}
	return nil, matchedAttributeID, nil
}

func (s *UserAttributeService) ensureSharedAccountOwnerDefinition(ctx context.Context) (*UserAttributeDefinition, error) {
	def, err := s.defRepo.GetByKey(ctx, "shared_account_owner")
	if err == nil {
		changed := false
		if !def.Enabled {
			def.Enabled = true
			changed = true
		}
		if def.Type != AttributeTypeSelect {
			def.Type = AttributeTypeSelect
			changed = true
		}
		if !hasAttributeOption(def.Options, "true") || !hasAttributeOption(def.Options, "false") {
			def.Options = []UserAttributeOption{
				{Value: "true", Label: "手动开启"},
				{Value: "false", Label: "手动关闭"},
			}
			changed = true
		}
		if changed {
			if err := s.defRepo.Update(ctx, def); err != nil {
				return nil, err
			}
		}
		return def, nil
	}
	if !errors.Is(err, ErrAttributeDefinitionNotFound) {
		return nil, err
	}

	def = &UserAttributeDefinition{
		Key:         "shared_account_owner",
		Name:        "共享号主",
		Description: "留空时按历史兑换满 100 自动开启；手动关闭会覆盖自动开启。",
		Type:        AttributeTypeSelect,
		Options: []UserAttributeOption{
			{Value: "true", Label: "手动开启"},
			{Value: "false", Label: "手动关闭"},
		},
		Required: false,
		Enabled:  true,
	}
	if err := s.defRepo.Create(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

func hasAttributeOption(options []UserAttributeOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func sharedAccountOwnerOverrideFromValue(def UserAttributeDefinition, rawValue string) (*bool, bool) {
	key := normalizeAttributeMatchText(def.Key)
	name := normalizeAttributeMatchText(def.Name)
	description := normalizeAttributeMatchText(def.Description)
	value := normalizeAttributeMatchText(rawValue)

	keyMatches := key == "sharedaccountowner" ||
		key == "accountshareowner" ||
		key == "accountowner" ||
		key == "accountownertitle" ||
		key == "sharedowner" ||
		strings.Contains(key, "sharedaccountowner") ||
		strings.Contains(key, "共享号主")
	valueMatches := value == "sharedaccountowner" ||
		value == "shareowner" ||
		value == "accountshareowner" ||
		value == "accountowner" ||
		strings.Contains(value, "共享号主")
	titleFieldMatches := strings.Contains(name, "共享号主") ||
		strings.Contains(description, "共享号主") ||
		strings.Contains(key, "共享号主") ||
		strings.Contains(key, "title") ||
		strings.Contains(key, "role") ||
		strings.Contains(name, "头衔") ||
		strings.Contains(name, "身份")
	strictOwnerFieldMatches := keyMatches ||
		strings.Contains(name, "共享号主") ||
		strings.Contains(description, "共享号主") ||
		strings.Contains(key, "共享号主")

	if !keyMatches && !titleFieldMatches {
		return nil, false
	}
	if strictOwnerFieldMatches && (strings.TrimSpace(rawValue) == "" || value == "auto" || value == "自动" || value == "跟随自动") {
		return nil, true
	}
	if (strictOwnerFieldMatches && isTruthyAttributeValue(value)) || (titleFieldMatches && valueMatches) {
		out := true
		return &out, true
	}
	if strictOwnerFieldMatches && isFalseyAttributeValue(value) {
		out := false
		return &out, true
	}
	return nil, strictOwnerFieldMatches
}

func isTruthyAttributeValue(value string) bool {
	switch value {
	case "true", "1", "yes", "y", "on", "enabled", "enable", "开启", "启用", "是", "共享号主":
		return true
	default:
		return false
	}
}

func isFalseyAttributeValue(value string) bool {
	switch value {
	case "false", "0", "no", "n", "off", "disabled", "disable", "关闭", "禁用", "否", "普通用户":
		return true
	default:
		return false
	}
}

func normalizeAttributeMatchText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", "\t", "", "\n", "", "\r", "")
	return replacer.Replace(value)
}

// validationError creates a validation error with a custom message
func validationError(msg string) error {
	return infraerrors.BadRequest("ATTRIBUTE_VALIDATION_FAILED", msg)
}

func isValidAttributeType(t UserAttributeType) bool {
	switch t {
	case AttributeTypeText, AttributeTypeTextarea, AttributeTypeNumber,
		AttributeTypeEmail, AttributeTypeURL, AttributeTypeDate,
		AttributeTypeSelect, AttributeTypeMultiSelect:
		return true
	}
	return false
}

func validateDefinitionPattern(def *UserAttributeDefinition) error {
	if def == nil {
		return nil
	}
	if def.Validation.Pattern == nil {
		return nil
	}
	pattern := strings.TrimSpace(*def.Validation.Pattern)
	if pattern == "" {
		return nil
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return infraerrors.BadRequest("INVALID_ATTRIBUTE_PATTERN", fmt.Sprintf("invalid pattern for %s: %v", def.Name, err))
	}
	return nil
}
