package launcher

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/spf13/cobra"
)

var (
	healthAddr string
	healthLive bool
)

var healthCmd = &cobra.Command{
	Use:     "health",
	Aliases: []string{"hc"},
	Short:   "Check application health",
	Run: func(cmd *cobra.Command, args []string) {
		addr := healthAddr
		if addr == "" {
			cfg, err := config.Load(config.LoadOptions{EnvFile: argEnvFile})
			if err != nil {
				fmt.Printf("unhealthy: %v\n", err)
				os.Exit(1)
			}
			addr = fmt.Sprintf("http://%s:%d/api/healthz", cfg.Host, cfg.Port)
		}
		if healthLive {
			checkLive(addr)
			return
		}
		checkStatic(cmd)
	},
}

func checkStatic(cmd *cobra.Command) {
	exe, _ := os.Executable()
	info, err := os.Stat(exe)
	var size string
	if err == nil {
		size = formatSize(info.Size())
	}
	cmd.Printf("runtime:   %s\n", runtime.Version())
	cmd.Printf("platform:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
	cmd.Printf("binary:    %s\n", exe)
	cmd.Printf("size:      %s\n", size)
	cmd.Println("status:    healthy")
}

func checkLive(addr string) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(addr)
	if err != nil {
		fmt.Printf("unhealthy: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("unhealthy: status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	fmt.Println("ok")
}

func formatSize(bytes int64) string {
	const mb = 1024 * 1024
	if bytes >= mb {
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(mb))
	}
	const kb = 1024
	return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
}

func init() {
	healthCmd.Flags().StringVar(&healthAddr, "addr", "", "Server health endpoint URL (default: from config)")
	healthCmd.Flags().BoolVar(&healthLive, "live", false, "Check live server via HTTP")
}
