package service

import "testing"

func TestOpenAIImageOutputCounterTextOnlyResponsesDoNotCountImages(t *testing.T) {
	sseBody := `data: {"type":"response.output_item.done","item":{"id":"item_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}

data: {"type":"response.completed","response":{"id":"resp_1","output":[{"id":"item_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}

data: [DONE]`

	if got := countOpenAIImageOutputsFromSSEBody(sseBody); got != 0 {
		t.Fatalf("countOpenAIImageOutputsFromSSEBody(text-only) = %d, want 0", got)
	}

	jsonBody := []byte(`{"id":"resp_1","output":[{"id":"item_1","type":"message","content":[{"type":"output_text","text":"hello"}]}],"data":[{"id":"not-image","status":"done"}]}`)
	if got := countOpenAIResponseImageOutputsFromJSONBytes(jsonBody); got != 0 {
		t.Fatalf("countOpenAIResponseImageOutputsFromJSONBytes(text-only data) = %d, want 0", got)
	}
}

func TestOpenAIImageOutputCounterCountsOnlyDataArrayImages(t *testing.T) {
	jsonBody := []byte(`{"data":[{"id":"not-image"},{"url":"https://example.com/a.png"},{"b64_json":"abc"}]}`)

	if got := countOpenAIResponseImageOutputsFromJSONBytes(jsonBody); got != 2 {
		t.Fatalf("countOpenAIResponseImageOutputsFromJSONBytes(image data) = %d, want 2", got)
	}
}

func TestOpenAIImageOutputCounterIgnoresEmptyImageGenerationCompleted(t *testing.T) {
	body := `data: {"type":"image_generation.completed","item":{"id":"call_1","type":"image_generation.completed"}}

data: [DONE]`

	if got := countOpenAIImageOutputsFromSSEBody(body); got != 0 {
		t.Fatalf("countOpenAIImageOutputsFromSSEBody(empty image_generation.completed) = %d, want 0", got)
	}
}
