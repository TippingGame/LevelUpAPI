package service

import (
	"strconv"
	"strings"
)

func buildLargeOpenAIResponsesImageToolBody(targetBytes int) []byte {
	var builder strings.Builder
	builder.Grow(targetBytes + 1024)
	_, _ = builder.WriteString(`{"model":"gpt-5.4","stream":false,"tools":[{"type":"image_generation","model":"gpt-image-2","size":"2048x1152"}],"input":[`)
	for i := 0; builder.Len() < targetBytes; i++ {
		if i > 0 {
			_ = builder.WriteByte(',')
		}
		_, _ = builder.WriteString(`{"type":"message","role":"user","content":[{"type":"input_text","text":"`)
		_, _ = builder.WriteString(strings.Repeat("openai image billing payload ", 48))
		_, _ = builder.WriteString(strconv.Itoa(i))
		_, _ = builder.WriteString(`"}]}`)
	}
	_, _ = builder.WriteString(`]}`)
	return []byte(builder.String())
}
