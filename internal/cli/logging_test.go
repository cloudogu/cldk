package cli

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownPC returns a program counter inside this test package, i.e. a cldk package.
func ownPC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	return pcs[0]
}

// foreignPC returns a program counter of a function outside cldk.
func foreignPC() uintptr {
	return reflect.ValueOf(strings.ToUpper).Pointer()
}

func TestOwnDebugHandler(t *testing.T) {
	tests := []struct {
		name  string
		level slog.Level
		pc    uintptr
		want  bool
	}{
		{name: "debug of cldk passes", level: slog.LevelDebug, pc: ownPC(), want: true},
		{name: "debug of a library is dropped", level: slog.LevelDebug, pc: foreignPC(), want: false},
		{name: "debug without caller passes", level: slog.LevelDebug, pc: 0, want: true},
		{name: "info of a library passes", level: slog.LevelInfo, pc: foreignPC(), want: true},
		{name: "warning of a library passes", level: slog.LevelWarn, pc: foreignPC(), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			handler := libraryDebugFilter{Handler: slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})}

			err := handler.Handle(context.Background(), slog.NewRecord(time.Now(), tt.level, "hello", tt.pc))

			require.NoError(t, err)
			assert.Equal(t, tt.want, strings.Contains(out.String(), "msg=hello"))
		})
	}

	t.Run("derived loggers keep the filter", func(t *testing.T) {
		var out bytes.Buffer
		var handler slog.Handler = libraryDebugFilter{Handler: slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})}
		handler = handler.WithAttrs([]slog.Attr{slog.String("a", "b")}).WithGroup("g")

		err := handler.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelDebug, "hello", foreignPC()))

		require.NoError(t, err)
		assert.Empty(t, out.String())
	})
}
