package httputil

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeLenientJSONRequestBodyAcceptsControlCharsInsideStrings(t *testing.T) {
	tests := []struct {
		name    string
		body    []byte
		path    string
		want    string
		wantRaw string
	}{
		{
			name:    "null byte",
			body:    []byte("{\"messages\":[{\"content\":\"hello\x00world\"}]}"),
			path:    "messages.0.content",
			want:    "hello\x00world",
			wantRaw: `"hello\u0000world"`,
		},
		{
			name:    "ANSI escape",
			body:    []byte("{\"messages\":[{\"content\":\"hello\x1b[31mred\x1b[0m\"}]}"),
			path:    "messages.0.content",
			want:    "hello\x1b[31mred\x1b[0m",
			wantRaw: `"hello\u001b[31mred\u001b[0m"`,
		},
		{
			name:    "UTF-8 BOM",
			body:    []byte("\xef\xbb\xbf{\"input\":\"hello\"}"),
			path:    "input",
			want:    "hello",
			wantRaw: `"hello"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gjson.ValidBytes(tt.body) {
				t.Fatalf("test body should be rejected by strict JSON before normalization: %q", tt.body)
			}
			got, err := NormalizeLenientJSONRequestBody(tt.body, 1024)
			if err != nil {
				t.Fatalf("NormalizeLenientJSONRequestBody: %v", err)
			}
			if !gjson.ValidBytes(got) {
				t.Fatalf("normalized body should be valid JSON: %q", got)
			}
			result := gjson.GetBytes(got, tt.path)
			if result.String() != tt.want || result.Raw != tt.wantRaw {
				t.Fatalf("normalized value = %q (%s), want %q (%s)", result.String(), result.Raw, tt.want, tt.wantRaw)
			}
		})
	}
}

func TestNormalizeLenientJSONRequestBodyKeepsInvalidStructureInvalid(t *testing.T) {
	tests := [][]byte{
		[]byte("{\"messages\":[{\"content\":\"hello\"}]"),
		[]byte("{\"input\":\"hello\"}\x00"),
	}

	for _, body := range tests {
		got, err := NormalizeLenientJSONRequestBody(body, 1024)
		if err != nil {
			t.Fatalf("NormalizeLenientJSONRequestBody: %v", err)
		}
		if gjson.ValidBytes(got) {
			t.Fatalf("normalization must not repair invalid JSON structure: %q", got)
		}
	}
}

func TestReadLenientJSONRequestBodyWithPrealloc(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := ReadLenientJSONRequestBodyWithPrealloc(r, 1024)
		if err != nil || !gjson.ValidBytes(body) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	requestBody := []byte("{\"model\":\"gpt-5.5\",\"input\":\"hello\x00world\"}")
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
}

func TestNormalizeLenientJSONRequestBodyRejectsExpansionPastLimit(t *testing.T) {
	body := []byte("{\"input\":\"\x00\x00\"}")
	_, err := NormalizeLenientJSONRequestBody(body, int64(len(body)+5))

	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) {
		t.Fatalf("expected MaxBytesError, got %T %v", err, err)
	}
	if maxErr.Limit != int64(len(body)+5) {
		t.Fatalf("limit = %d, want %d", maxErr.Limit, len(body)+5)
	}
}
