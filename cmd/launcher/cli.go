// package launcher implements the application command line.
//
// Grammar is declared once as a struct (kong); config is layered
// separately by internal/config. Kong flags stay zero-valued.
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

// ANSI colors, shared by every command printing status output.
const (
	colorRed   = "\033[0;31m"
	colorGreen = "\033[0;32m"
	colorCyan  = "\033[0;36m"
	colorBold  = "\033[1m"
	colorReset = "\033[0m"
)

// CLI is the command-line grammar. Subcommands receive it via
// kong's type-based binding (ctx.Run(cli)).
type CLI struct {
	// EnvFile loads a dotenv file, below the system environment.
	// Empty defaults to .env.local when it exists.
	EnvFile string `help:"Load environment variables from a dotenv file (default: .env.local when present)"`

	// DataDir is the single root for logs, backups, keys.
	// Wins over APP_DATA_DIR and the env file.
	DataDir string `name:"data-dir" help:"Application data directory for on-disk runtime state"`

	// Version prints the "version" variable and exits.
	Version kong.VersionFlag `short:"V" help:"Show the application version"`

	Serve   ServeCmd   `cmd:"" help:"Start the application server"`
	DB      DBCmd      `cmd:"" help:"Database backup, restore, and migration commands"`
	Secrets SecretsCmd `cmd:"" help:"Generate application secrets"`
	Health  HealthCmd  `cmd:"" help:"Check application health" aliases:"hc"`
}

// versionVars builds the kong vars for --version.
func versionVars() kong.Vars {
	return kong.Vars{
		"version": fmt.Sprintf("%s %s %s (%s %s)", config.AppName, config.AppVersion, config.Platform, config.BuildHash, config.BuildDate),
	}
}

// globalOverrides maps global flags to config keys.
// --data-dir wins over every other layer.
func globalOverrides(cli *CLI) map[string]any {
	overrides := map[string]any{}
	if cli.DataDir != "" {
		overrides["app.data_dir"] = cli.DataDir
	}
	return overrides
}

// loadConfig loads layered config: global flags (--data-dir,
// --env-file) plus command overrides. Global flags win. With no
// --env-file, .env.local is loaded when present so plain
// `tango <cmd>` sees the same DSNs as the task runner; compose and
// systemd users set real env vars instead.
func loadConfig(cli *CLI, extra map[string]any) (*config.Config, error) {
	envFile := cli.EnvFile
	if envFile == "" {
		if _, err := os.Stat(".env.local"); err == nil {
			envFile = ".env.local"
		}
	}

	overrides := map[string]any{}
	for key, value := range extra {
		overrides[key] = value
	}
	for key, value := range globalOverrides(cli) {
		overrides[key] = value
	}
	cfg, err := config.Load(config.LoadOptions{EnvFile: envFile, Overrides: overrides})
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// stdinReader feeds confirmation prompts; tests override it.
var stdinReader io.Reader = os.Stdin

// stdinIsInteractive is false for pipes and /dev/null.
var stdinIsInteractive = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// confirmDestructive prompts unless --force; refuses in
// non-interactive sessions where nobody can answer.
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

// HealthCmd checks application health.
type HealthCmd struct {
	Addr string `help:"Server health endpoint URL (default: from config)"`
	Live bool   `help:"Check live server via HTTP"`
}

// Run prints binary info, or probes a live server.
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

// checkLive probes the endpoint; unhealthy exits with code 1.
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

// RunCLI parses args and runs the selected command. The db surface
// is wired per-variant (db_migrate_debug/release.go).
func RunCLI(args []string, opts ...kong.Option) error {
	cli := &CLI{}
	base := []kong.Option{
		kong.Name(config.AppName),
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
