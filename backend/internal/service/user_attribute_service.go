package service

import (
	"context"
	"encoding/json"
	"fmt"
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
		if isSharedAccountOwnerAttributeValue(def, rawValue) {
			return true, nil
		}
	}
	return false, nil
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

func isSharedAccountOwnerAttributeValue(def UserAttributeDefinition, rawValue string) bool {
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

	if keyMatches && isTruthyAttributeValue(value) {
		return true
	}
	return titleFieldMatches && valueMatches
}

func isTruthyAttributeValue(value string) bool {
	switch value {
	case "true", "1", "yes", "y", "on", "enabled", "enable", "开启", "启用", "是", "共享号主":
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
