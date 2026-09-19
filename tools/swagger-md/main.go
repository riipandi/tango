package main

// Parses the Pocket ID swagger document into a Markdown reference.
// Run with: go run ./tools/swagger-md <input.yaml> <output.md>

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type swagger struct {
	Swagger     string                    `yaml:"swagger"`
	Info        info                      `yaml:"info"`
	BasePath    string                    `yaml:"basePath"`
	Schemes     []string                  `yaml:"schemes"`
	Paths       map[string]map[string]op  `yaml:"paths"`
	Definitions map[string]map[string]any `yaml:"definitions"`
}

type info struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

type op struct {
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description"`
	Tags        []string            `yaml:"tags"`
	OperationID string              `yaml:"operationId"`
	Consumes    []string            `yaml:"consumes"`
	Produces    []string            `yaml:"produces"`
	Parameters  []parameter         `yaml:"parameters"`
	Responses   map[string]response `yaml:"responses"`
	Security    []map[string][]any  `yaml:"security"`
}

type parameter struct {
	Name        string `yaml:"name"`
	In          string `yaml:"in"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Type        string `yaml:"type"`
	Schema      struct {
		Ref  string `yaml:"$ref"`
		Type string `yaml:"type"`
	} `yaml:"schema"`
}

type response struct {
	Description string `yaml:"description"`
	Schema      struct {
		Ref   string `yaml:"$ref"`
		Type  string `yaml:"type"`
		Items struct {
			Ref string `yaml:"$ref"`
		} `yaml:"items"`
	} `yaml:"schema"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: swagger-md <input.yaml> <output.md>")
		os.Exit(2)
	}
	// The paths are operator-supplied command arguments, not request
	// input; this is a local one-shot generator.
	raw, err := os.ReadFile(os.Args[1]) //nolint:gosec // CLI argument, not tainted input
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var doc swagger
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var b strings.Builder
	b.WriteString("# Pocket ID API Reference\n\n")
	b.WriteString("Upstream contract for the surfaces tango ports. Extracted from\n")
	b.WriteString("<https://pocket-id.org/swagger.yaml> (swagger 2.0, ")
	fmt.Fprintf(&b, "%s v%s", doc.Info.Title, doc.Info.Version)
	b.WriteString(").\n\n")
	b.WriteString("This file is a reference only. tango serves ConnectRPC below `/rpc` for these\n")
	b.WriteString("operations and keeps a small HTTP/REST surface for protocol endpoints; see\n")
	b.WriteString("`docs/api-endpoint.md` for what tango actually mounts.\n\n")

	// Group by tag, then path.
	type entry struct {
		method string
		path   string
		op     op
	}
	byTag := map[string][]entry{}
	for path, methods := range doc.Paths {
		for method, o := range methods {
			tag := "Untagged"
			if len(o.Tags) > 0 {
				tag = o.Tags[0]
			}
			byTag[tag] = append(byTag[tag], entry{strings.ToUpper(method), path, o})
		}
	}

	tags := make([]string, 0, len(byTag))
	for t := range byTag {
		tags = append(tags, t)
	}
	sort.Strings(tags)

	total := 0
	b.WriteString("## Operations\n\n")
	for _, tag := range tags {
		entries := byTag[tag]
		slices.SortFunc(entries, func(a, b entry) int {
			if c := strings.Compare(a.path, b.path); c != 0 {
				return c
			}
			return strings.Compare(a.method, b.method)
		})
		fmt.Fprintf(&b, "### %s\n\n", tag)
		b.WriteString("| Method | Path | Summary | Success |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, e := range entries {
			total++
			success := successCode(e.op)
			fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n",
				e.method, e.path, clean(e.op.Summary), success)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Summary\n\n%d operations across %d paths.\n\n", total, len(doc.Paths))

	// Schema inventory: the request/response models the operations use.
	names := make([]string, 0, len(doc.Definitions))
	for n := range doc.Definitions {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Fprintf(&b, "## Schemas\n\n%d definitions in the upstream document.\n\n", len(names))
	b.WriteString("| Definition | Fields |\n| --- | --- |\n")
	for _, n := range names {
		props, _ := doc.Definitions[n]["properties"].(map[string]any)
		fields := make([]string, 0, len(props))
		for f := range props {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		fmt.Fprintf(&b, "| `%s` | %s |\n", n, strings.Join(fields, ", "))
	}

	if err := os.WriteFile(os.Args[2], []byte(b.String()), 0o600); err != nil { //nolint:gosec // CLI argument, not tainted input
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d operations, %d paths, %d definitions\n", os.Args[2], total, len(doc.Paths), len(names))
}

func successCode(o op) string {
	for _, code := range []string{"200", "201", "204"} {
		if _, ok := o.Responses[code]; ok {
			return code
		}
	}
	return "-"
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "-"
	}
	return s
}
