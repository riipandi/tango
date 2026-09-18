package launcher

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/riipandi/tango/database/seeders"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
)

// SetupCmd bootstraps the first admin account.
// Only valid on a fresh database; refused otherwise.
type SetupCmd struct {
	AdminEmail    string `help:"Email address for the admin account"`
	AdminPassword string `help:"Password for the admin account (omit to be prompted, hidden input)"`
	Username      string `default:"admin" help:"Username for the admin account"`
}

func (c *SetupCmd) Help() string {
	return fmt.Sprintf("\nExamples:\n"+
		"  %[1]s setup --admin-email admin@example.com --admin-password 'S3cret!'\n"+
		"  %[1]s setup --admin-email admin@example.com   # prompts for the password\n"+
		"  %[1]s setup --admin-email admin@example.com --admin-password 'S3cret!' --username root\n\n"+
		"The command refuses to run when any user already exists.\n", config.AppName)
}

func (c *SetupCmd) Run(cli *CLI) error {
	cfg, err := loadConfig(cli, nil)
	if err != nil {
		return err
	}

	email := strings.TrimSpace(c.AdminEmail)
	if email == "" {
		email, err = promptLine("Admin email: ")
		if err != nil {
			return err
		}
	}
	if c.AdminPassword == "" {
		password, promptErr := promptSecret("Admin password: ")
		if promptErr != nil {
			return promptErr
		}
		confirm, confirmErr := promptSecret("Confirm password: ")
		if confirmErr != nil {
			return confirmErr
		}
		if confirm != password {
			return fmt.Errorf("passwords do not match")
		}
		c.AdminPassword = password
	}
	password := c.AdminPassword

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := datastore.New(ctx, datastore.Options{DSN: cfg.Database.URL})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	id, err := seeders.SetupAdmin(ctx, db, seeders.AdminParams{
		Username: c.Username,
		Email:    email,
		Password: password,
	})
	if err != nil {
		fmt.Printf("%sERROR: %v%s\n", colorRed, err, colorReset)
		return nil
	}

	fmt.Printf("%s✔ admin account created%s (%s)\n", colorGreen, colorReset, id)
	fmt.Printf("  username: %s\n  email:    %s\n", c.Username, email)
	return nil
}

func promptLine(label string) (string, error) {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func promptSecret(label string) (string, error) {
	fmt.Print(label)
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", fmt.Errorf("password prompt requires a terminal; use --admin-password instead")
	}
	data, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(data), nil
}
