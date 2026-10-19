package logging

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComponentLogValuer(t *testing.T) {
	value := slog.AnyValue(ComponentDatabase).Resolve()

	assert.Equal(t, slog.KindString, value.Kind())
	assert.Equal(t, string(ComponentDatabase), value.String())
}

func TestOperationLogValuer(t *testing.T) {
	value := slog.AnyValue(OperationSubmit).Resolve()

	assert.Equal(t, slog.KindString, value.Kind())
	assert.Equal(t, string(OperationSubmit), value.String())
}
