package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMigrateSubcommands(t *testing.T) {
	out := runRootCommand(t, "migrate", "up")
	assert.Contains(t, out, "")

	out = runRootCommand(t, "migrate", "down")
	assert.Contains(t, out, "")

	out = runRootCommand(t, "migrate", "status")
	assert.Contains(t, out, "")
}

func TestServeHelpListsCommand(t *testing.T) {
	out := runRootCommand(t, "--help")
	assert.Contains(t, out, "serve")
	assert.Contains(t, out, "migrate")
	assert.Contains(t, out, "health")
}
