package launcher

import (
	"runtime"
	"strings"
	"testing"

	"github.com/riipandi/tango/internal/config"
)

func runRootCommand(t *testing.T, args ...string) string {
	t.Helper()

	// Reset sticky flag values between runs.
	argVersionShort = false
	argVersionSemantic = false
	healthLive = false

	out := &strings.Builder{}
	rootCmd.SetOut(out)
	rootCmd.SetErr(out)
	rootCmd.SetArgs(args)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v", args, err)
	}

	return out.String()
}

func TestVersionDefault(t *testing.T) {
	out := runRootCommand(t, "version")

	want := config.AppName + " " + config.AppVersion + " " + config.Platform
	if !strings.HasPrefix(out, want) {
		t.Errorf("got %q, want prefix %q", out, want)
	}

	if !strings.Contains(out, "("+config.BuildHash) {
		t.Errorf("expected build hash in output, got %q", out)
	}
}

func TestVersionShort(t *testing.T) {
	out := runRootCommand(t, "version", "--short")

	want := config.AppVersion + " (" + config.BuildHash + ")\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestVersionSemantic(t *testing.T) {
	out := runRootCommand(t, "version", "--semantic")

	if out != config.AppVersion+"\n" {
		t.Errorf("got %q, want %q", out, config.AppVersion+"\n")
	}
}

func TestHealthStatic(t *testing.T) {
	out := runRootCommand(t, "hc")

	if !strings.Contains(out, "status:    healthy") {
		t.Errorf("expected healthy status, got %q", out)
	}

	if !strings.Contains(out, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("expected platform info, got %q", out)
	}
}
