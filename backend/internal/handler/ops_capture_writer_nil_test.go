package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpsCaptureWriter_NilInnerWriter_NoPanic(t *testing.T) {
	w := &opsCaptureWriter{}
	w.ResponseWriter = nil

	assert.NotPanics(t, func() {
		assert.Equal(t, 0, w.Status())
	}, "Status() on released writer must not panic")

	assert.NotPanics(t, func() {
		assert.Equal(t, -1, w.Size())
	}, "Size() on released writer must not panic")

	assert.NotPanics(t, func() {
		assert.False(t, w.Written())
	}, "Written() on released writer must not panic")

	assert.NotPanics(t, func() {
		n, err := w.Write([]byte("test"))
		assert.Equal(t, 0, n)
		assert.NoError(t, err)
	}, "Write() on released writer must not panic")

	assert.NotPanics(t, func() {
		n, err := w.WriteString("test")
		assert.Equal(t, 0, n)
		assert.NoError(t, err)
	}, "WriteString() on released writer must not panic")

	assert.NotPanics(t, func() {
		assert.Equal(t, http.Header{}, w.Header())
	})
	assert.NotPanics(t, func() { w.WriteHeader(http.StatusOK) })
	assert.NotPanics(t, w.WriteHeaderNow)
	assert.NotPanics(t, w.Flush)
	assert.NotPanics(t, func() {
		conn, rw, err := w.Hijack()
		assert.Nil(t, conn)
		assert.Nil(t, rw)
		assert.Error(t, err)
	})
	assert.NotPanics(t, func() {
		ch := w.CloseNotify()
		assert.NotNil(t, ch)
	})
	assert.NotPanics(t, func() { assert.Nil(t, w.Pusher()) })
}
