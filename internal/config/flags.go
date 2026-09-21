package config

import (
	"slices"
	"strings"

	"github.com/knadh/koanf/providers/cliflagv3"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/pkg/envfile"
)

// Flag names the root command declares. They are the plumbing of the config
// layer itself, so they are named here rather than at the call site.
const (
	FlagConfigFile = "config-file"
	FlagEnvFile    = "env-file"
	FlagDataDir    = "data-dir"
)

// flagBindings maps a command-line flag name to the config key it sets. Only a
// flag listed here reaches the configuration: a flag that exists for the
// command's own behavior, such as --dry-run, is not a config key and must not
// become one.
//
// The map is the single place a flag and a config key are tied together, so a
// renamed key is a one-line change and the loader never guesses.
var flagBindings = map[string]string{
	FlagDataDir: "app.data_dir",
	"host":      "server.host",
	"port":      "server.port",
	"base-url":  "server.base_url",
}

// FromCommand builds the options Load merges from a parsed command, so a caller
// gets the full precedence chain without restating it.
func FromCommand(cmd *cli.Command) (Options, error) {
	opts := Options{
		ConfigFile: cmd.String(FlagConfigFile),
		Flags:      flagLayer(cmd),
	}

	if path := cmd.String(FlagEnvFile); path != "" {
		values, err := readEnvFile(path)
		if err != nil {
			return Options{}, err
		}
		opts.EnvFile = values
	}

	if len(opts.Flags) == 0 {
		opts.Flags = nil
	}
	return opts, nil
}

// Resolve loads the configuration for a parsed command. It is the entry point a
// command action calls.
func Resolve(cmd *cli.Command) (Config, error) {
	opts, err := FromCommand(cmd)
	if err != nil {
		return Config{}, err
	}
	return Load(opts)
}

// flagLayer reads the flags the user actually set, at every level of the command
// chain, and maps them to config keys.
//
// Every level is read because a flag belongs to the command that declares it: the
// root command owns --data-dir, and a subcommand owns its own flags such as
// --host. A flag left at its default is not a decision the user made, so it is
// absent from the layer and cannot override a config file with a value nobody
// typed.
func flagLayer(cmd *cli.Command) map[string]any {
	out := make(map[string]any)
	for _, level := range lineage(cmd) {
		for name, value := range readFlags(level) {
			key, ok := flagBindings[name]
			if !ok {
				continue
			}
			out[key] = value
		}
	}
	return out
}

// readFlags returns the flags set on one command, keyed by flag name.
//
// The provider nests its result under the command path, so the keys are stripped
// back to the flag names the bindings are written in. The path is built from the
// same lineage the provider walked, so the two cannot disagree.
func readFlags(cmd *cli.Command) map[string]any {
	flat, err := cliflagv3.Provider(cmd, Delim).Read()
	if err != nil {
		// The provider only fails when its output cannot be unflattened, which
		// would be a bug in this package rather than a user error.
		return nil
	}

	prefix := strings.Join(commandPath(cmd), Delim) + Delim
	out := make(map[string]any, len(flat))
	for key, value := range flattenNested(flat, nil) {
		name, ok := strings.CutPrefix(key, prefix)
		if !ok || name == "" {
			continue
		}
		out[name] = value
	}
	return out
}

// lineage returns the command chain from the root to cmd, the order the provider
// walks it.
func lineage(cmd *cli.Command) []*cli.Command {
	chain := cmd.Lineage()
	out := make([]*cli.Command, 0, len(chain))
	for _, c := range slices.Backward(chain) {
		out = append(out, c)
	}
	return out
}

// commandPath returns the names of a command and its ancestors, root first.
func commandPath(cmd *cli.Command) []string {
	chain := lineage(cmd)
	path := make([]string, 0, len(chain))
	for _, level := range chain {
		path = append(path, level.Name)
	}
	return path
}

// readEnvFile parses a dotenv file into key-value pairs. The file is read with
// pkg/envfile, so quoting and the `export` prefix behave the same here as they
// do in key:generate.
func readEnvFile(path string) (map[string]string, error) {
	file, err := envfile.Load(path)
	if err != nil {
		return nil, err
	}

	values := make(map[string]string, len(file.Keys()))
	for _, key := range file.Keys() {
		if value, ok := file.Get(key); ok {
			values[key] = value
		}
	}
	return values, nil
}
