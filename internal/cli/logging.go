package cli

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
)

// cldkPackagePrefix is the import path prefix of all cldk packages.
const cldkPackagePrefix = "github.com/cloudogu/cldk/"

// libraryDebugFilter drops the debug records of libraries; all records from info level on pass.
type libraryDebugFilter struct {
	slog.Handler
}

// Handle drops debug records that were not logged by a cldk package.
func (h libraryDebugFilter) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < slog.LevelInfo && !loggedByCldk(record.PC) {
		return nil
	}

	return h.Handler.Handle(ctx, record)
}

// WithAttrs keeps the filter for loggers derived with With.
func (h libraryDebugFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return libraryDebugFilter{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup keeps the filter for loggers derived with WithGroup.
func (h libraryDebugFilter) WithGroup(name string) slog.Handler {
	return libraryDebugFilter{Handler: h.Handler.WithGroup(name)}
}

// loggedByCldk reports whether the program counter of a record belongs to a cldk package.
func loggedByCldk(pc uintptr) bool {
	if pc == 0 {
		return true
	}

	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()

	return strings.HasPrefix(frame.Function, cldkPackagePrefix)
}
