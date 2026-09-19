package appconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleName(t *testing.T) {
	assert.Equal(t, ModuleName, New(nil).Name())
}
