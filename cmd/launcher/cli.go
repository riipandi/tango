// package launcher implements the tango command line.
//
// The command surface is declared once as a struct (kong); runtime
// configuration is layered separately by internal/config (koanf).
// Kong flags stay zero-valued — config defaults never live here.
package launcher

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
)

// ANSI colors, matching the shell scripts output style. Shared by
// every command that prints status output (secrets, migrate).
const (
	colorRed   = "\033[0;31m"
	colorGreen = "\033[0;32m"
	colorCyan  = "\033[0;36m"
	colorBold  = "\033[1m"
	colorReset = "\033[0m"
)

// stdinReader is the input source for confirmation prompts; tests
// override it the same way captureStdout swaps os.Stdout.
var stdinReader io.Reader = os.Stdin

// stdinIsInteractive reports whether stdin is a terminal. Pipes and
// /dev/null cannot confirm a prompt.
var stdinIsInteractive = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// versionVars builds the kong interpolation vars: the --version
// flag (kong.VersionFlag) prints the "version" variable verbatim.
func versionVars() kong.Vars {
	return kong.Vars{
		"version": fmt.Sprintf("%s %s %s (%s %s)", config.AppName, config.AppVersion, config.Platform, config.BuildHash, config.BuildDate),
	}
}

// CLI is the command-line grammar. Subcommand Run methods receive
// the parsed CLI via kong's type-based binding (ctx.Run(cli)).
type CLI struct {
	// EnvFile loads a dotenv file into the config layering, below the system environment.
	EnvFile string `help:"Load environment variables from a dotenv file"`

	// DataDir overrides the app data directory (app.data_dir):
	// the single root for logs, backups, and generated keys.
	// Wins over APP_DATA_DIR and the env file.
	DataDir string `name:"data-dir" help:"Application data directory for on-disk runtime state"`

	// Version prints the "version" variable and exits.
	Version kong.VersionFlag `short:"V" help:"Show the application version"`

	Serve   ServeCmd   `cmd:"" help:"Start the application server"`
	DB      DBCmd      `cmd:"" help:"Database backup, restore, and migration commands"`
	Secrets SecretsCmd `cmd:"" help:"Generate application secrets"`
	Health  HealthCmd  `cmd:"" help:"Check application health" aliases:"hc"`
}

// globalOverrides resolves the global CLI flags into config
// overrides: --data-dir wins over every other layer (it is the
// most explicit statement of intent).
func globalOverrides(cli *CLI) map[string]any {
	overrides := map[string]any{}
	if cli.DataDir != "" {
		overrides["app.data_dir"] = cli.DataDir
	}
	return overrides
}

// confirmDestructive gates destructive operations: unless --force,
// it prompts on stdin and refuses in non-interactive sessions (CI,
// scripts), where nobody can answer the prompt.
func confirmDestructive(force bool) error {
	if force {
		return nil
	}
	if !stdinIsInteractive() {
		return fmt.Errorf("refusing destructive operation in a non-interactive session (pass --force to proceed)")
	}

	fmt.Print("This operation is destructive. Proceed? [y/N] ")
	answer, err := bufio.NewReader(stdinReader).ReadString('\n')
	if err != nil && answer == "" {
		return fmt.Errorf("aborted")
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "y" || answer == "yes" {
		return nil
	}
	return fmt.Errorf("aborted")
}

// loadConfig loads the layered configuration for any command:
// global CLI flags (--data-dir, --env-file) plus optional
// command-specific overrides. Global flags win on key conflict.
func loadConfig(cli *CLI, extra map[string]any) (*config.Config, error) {
	overrides := map[string]any{}
	for key, value := range extra {
		overrides[key] = value
	}
	for key, value := range globalOverrides(cli) {
		overrides[key] = value
	}
	cfg, err := config.Load(config.LoadOptions{EnvFile: cli.EnvFile, Overrides: overrides})
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// RunCLI parses args and runs the selected command. The db
// command surface is wired per-variant (see db_debug.go and
// db_release.go); everything else is static grammar on CLI.
func RunCLI(args []string, opts ...kong.Option) error {
	cli := &CLI{}
	base := []kong.Option{
		kong.Name("tango"),
		kong.Description("A fullstack web application built with Go, Chi, and React."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		versionVars(),
	}
	parser, err := kong.New(cli, append(base, opts...)...)
	if err != nil {
		return err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	return kctx.Run(cli)
}

// HealthCmd checks application health.
type HealthCmd struct {
	Addr string `help:"Server health endpoint URL (default: from config)"`
	Live bool   `help:"Check live server via HTTP"`
}

// Run prints static binary info, or probes a live server.
func (h *HealthCmd) Run(cli *CLI) error {
	addr := h.Addr
	if h.Live {
		if addr == "" {
			cfg, err := loadConfig(cli, nil)
			if err != nil {
				return err
			}
			addr = fmt.Sprintf("http://%s:%d/api/healthz", cfg.Host, cfg.Port)
		}
		return checkLive(addr)
	}
	checkStatic()
	return nil
}

// checkStatic prints build and runtime information.
func checkStatic() {
	exe, _ := os.Executable()
	info, err := os.Stat(exe)
	var size string
	if err == nil {
		size = formatSize(info.Size())
	}
	fmt.Printf("runtime:   %s\n", runtime.Version())
	fmt.Printf("platform:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("binary:    %s\n", exe)
	fmt.Printf("size:      %s\n", size)
	fmt.Println("status:    healthy")
}

// checkLive probes the configured endpoint; an unhealthy result
// terminates the process with exit code 1.
func checkLive(addr string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(addr)
	if err != nil {
		fmt.Printf("unhealthy: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("unhealthy: status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	fmt.Println("ok")
	return nil
}

func formatSize(bytes int64) string {
	const mb = 1024 * 1024
	if bytes >= mb {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(mb))
	}
	const kb = 1024
	return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
}
