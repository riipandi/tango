package launcher

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"
)

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
