package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type httpStatusCodeRange struct {
	Start int
	End   int
}

func parseHTTPStatusCodeRangesValue(value any) []httpStatusCodeRange {
	switch v := value.(type) {
	case nil:
		return nil
	case int:
		return statusCodeExactRange(v)
	case int64:
		return statusCodeExactRange(int(v))
	case float64:
		return statusCodeExactRange(int(v))
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return statusCodeExactRange(int(i))
		}
	case string:
		return parseHTTPStatusCodeRangesString(v)
	case []int:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			ranges = append(ranges, statusCodeExactRange(item)...)
		}
		return normalizeHTTPStatusCodeRanges(ranges)
	case []float64:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			ranges = append(ranges, statusCodeExactRange(int(item))...)
		}
		return normalizeHTTPStatusCodeRanges(ranges)
	case []string:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			ranges = append(ranges, parseHTTPStatusCodeRangesString(item)...)
		}
		return normalizeHTTPStatusCodeRanges(ranges)
	case []any:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			ranges = append(ranges, parseHTTPStatusCodeRangesValue(item)...)
		}
		return normalizeHTTPStatusCodeRanges(ranges)
	}
	return nil
}

func ParseHTTPStatusCodesValue(value any) ([]int, error) {
	ranges, err := parseHTTPStatusCodeRangesValueStrict(value)
	if err != nil {
		return nil, err
	}
	return expandHTTPStatusCodeRanges(ranges), nil
}

func parseHTTPStatusCodeRangesValueStrict(value any) ([]httpStatusCodeRange, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case int:
		return strictStatusCodeExactRange(v)
	case int64:
		return strictStatusCodeExactRange(int(v))
	case float64:
		if v != float64(int(v)) {
			return nil, fmt.Errorf("status code must be an integer: %v", v)
		}
		return strictStatusCodeExactRange(int(v))
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return nil, fmt.Errorf("invalid status code: %s", v.String())
		}
		return strictStatusCodeExactRange(int(i))
	case string:
		return parseHTTPStatusCodeRangesStringStrict(v)
	case []int:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			next, err := strictStatusCodeExactRange(item)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, next...)
		}
		return normalizeHTTPStatusCodeRanges(ranges), nil
	case []int64:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			next, err := strictStatusCodeExactRange(int(item))
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, next...)
		}
		return normalizeHTTPStatusCodeRanges(ranges), nil
	case []float64:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			if item != float64(int(item)) {
				return nil, fmt.Errorf("status code must be an integer: %v", item)
			}
			next, err := strictStatusCodeExactRange(int(item))
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, next...)
		}
		return normalizeHTTPStatusCodeRanges(ranges), nil
	case []string:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			next, err := parseHTTPStatusCodeRangesStringStrict(item)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, next...)
		}
		return normalizeHTTPStatusCodeRanges(ranges), nil
	case []any:
		ranges := make([]httpStatusCodeRange, 0, len(v))
		for _, item := range v {
			next, err := parseHTTPStatusCodeRangesValueStrict(item)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, next...)
		}
		return normalizeHTTPStatusCodeRanges(ranges), nil
	default:
		return nil, fmt.Errorf("unsupported status code value %T", value)
	}
}

func parseHTTPStatusCodeRangesString(input string) []httpStatusCodeRange {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	input = strings.NewReplacer("，", ",").Replace(input)
	segments := strings.Split(input, ",")
	ranges := make([]httpStatusCodeRange, 0, len(segments))
	for _, segment := range segments {
		if r, ok := parseHTTPStatusCodeRangeToken(segment); ok {
			ranges = append(ranges, r)
		}
	}
	return normalizeHTTPStatusCodeRanges(ranges)
}

func parseHTTPStatusCodeRangesStringStrict(input string) ([]httpStatusCodeRange, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}
	input = strings.NewReplacer("，", ",").Replace(input)
	segments := strings.Split(input, ",")
	ranges := make([]httpStatusCodeRange, 0, len(segments))
	invalid := make([]string, 0)
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if r, ok := parseHTTPStatusCodeRangeToken(segment); ok {
			ranges = append(ranges, r)
			continue
		}
		invalid = append(invalid, segment)
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid http status code rules: %s", strings.Join(invalid, ", "))
	}
	return normalizeHTTPStatusCodeRanges(ranges), nil
}

func parseHTTPStatusCodeRangeToken(token string) (httpStatusCodeRange, bool) {
	token = strings.TrimSpace(strings.ReplaceAll(token, " ", ""))
	if token == "" {
		return httpStatusCodeRange{}, false
	}
	if strings.Contains(token, "-") {
		parts := strings.Split(token, "-")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return httpStatusCodeRange{}, false
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return httpStatusCodeRange{}, false
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return httpStatusCodeRange{}, false
		}
		if start > end || !isHTTPStatusCode(start) || !isHTTPStatusCode(end) {
			return httpStatusCodeRange{}, false
		}
		return httpStatusCodeRange{Start: start, End: end}, true
	}

	code, err := strconv.Atoi(token)
	if err != nil || !isHTTPStatusCode(code) {
		return httpStatusCodeRange{}, false
	}
	return httpStatusCodeRange{Start: code, End: code}, true
}

func statusCodeExactRange(code int) []httpStatusCodeRange {
	if !isHTTPStatusCode(code) {
		return nil
	}
	return []httpStatusCodeRange{{Start: code, End: code}}
}

func strictStatusCodeExactRange(code int) ([]httpStatusCodeRange, error) {
	if !isHTTPStatusCode(code) {
		return nil, fmt.Errorf("status code out of bounds: %d", code)
	}
	return []httpStatusCodeRange{{Start: code, End: code}}, nil
}

func normalizeHTTPStatusCodeRanges(ranges []httpStatusCodeRange) []httpStatusCodeRange {
	if len(ranges) == 0 {
		return nil
	}
	normalized := make([]httpStatusCodeRange, 0, len(ranges))
	for _, r := range ranges {
		if !isHTTPStatusCode(r.Start) || !isHTTPStatusCode(r.End) || r.Start > r.End {
			continue
		}
		normalized = append(normalized, r)
	}
	if len(normalized) == 0 {
		return nil
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Start == normalized[j].Start {
			return normalized[i].End < normalized[j].End
		}
		return normalized[i].Start < normalized[j].Start
	})

	merged := []httpStatusCodeRange{normalized[0]}
	for _, r := range normalized[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End+1 {
			if r.End > last.End {
				last.End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func httpStatusCodeRangesContain(ranges []httpStatusCodeRange, code int) bool {
	if !isHTTPStatusCode(code) {
		return false
	}
	for _, r := range ranges {
		if code < r.Start {
			return false
		}
		if code <= r.End {
			return true
		}
	}
	return false
}

func expandHTTPStatusCodeRanges(ranges []httpStatusCodeRange) []int {
	ranges = normalizeHTTPStatusCodeRanges(ranges)
	if len(ranges) == 0 {
		return nil
	}
	codes := make([]int, 0, len(ranges))
	for _, r := range ranges {
		for code := r.Start; code <= r.End; code++ {
			codes = append(codes, code)
		}
	}
	return codes
}

func isHTTPStatusCode(code int) bool {
	return code >= 100 && code <= 599
}
