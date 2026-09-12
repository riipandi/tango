package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRegistersAllModules(t *testing.T) {
	reg := New(Deps{Config: nil})

	modules := reg.Modules()
	assert.Len(t, modules, 3)

	wantOrder := []string{"auditlog", "identity", "federation"}
	for i, want := range wantOrder {
		assert.Equal(t, want, modules[i].Name())
	}

	assert.NotNil(t, reg.Get("identity"))
	assert.NotNil(t, reg.Get("federation"))
}
