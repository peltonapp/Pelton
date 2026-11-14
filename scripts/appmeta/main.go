// appmeta renders a packaging template (the macOS Info.plist, the Windows
// version resource and the NSIS defines) from the app metadata in wails.json,
// with the version of the build at hand. It is run by scripts/build.sh.
//
// Usage: go run ./scripts/appmeta [-version v2026.4.7] [-numeric] [-name N]
// [-product P] [-nightly] template > out
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"
)

type protocol struct {
	Scheme      string `json:"scheme"`
	Description string `json:"description"`
	Role        string `json:"role"`
}

type fileAssociation struct {
	Ext         string `json:"ext"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconName    string `json:"iconName"`
	Role        string `json:"role"`
}

type meta struct {
	// Nightly is set for a nightly build, which the templates give their own
	// bundle id and icon.
	Nightly        bool   `json:"-"`
	Name           string `json:"name"`
	OutputFilename string `json:"outputfilename"`
	Info           struct {
		CompanyName      string            `json:"companyName"`
		ProductName      string            `json:"productName"`
		ProductVersion   string            `json:"productVersion"`
		Copyright        string            `json:"copyright"`
		Comments         string            `json:"comments"`
		Protocols        []protocol        `json:"protocols"`
		FileAssociations []fileAssociation `json:"fileAssociations"`
	} `json:"info"`
}

var numericPrefix = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*`)

// productVersion turns a build version (a tag like v2026.4.7, a nightly like
// nightly-2026-11-13+abc, or a describe string) into what goes in the bundle.
// numeric keeps only a dotted x.y.z, which is all Windows accepts.
func productVersion(version string, numeric bool) string {
	v := strings.TrimPrefix(version, "v")
	if !numeric {
		return v
	}
	parts := strings.Split(numericPrefix.FindString(v), ".")
	if parts[0] == "" {
		parts = nil
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	return strings.Join(parts[:3], ".")
}

func main() {
	version := flag.String("version", "", "build version; empty keeps wails.json's")
	numeric := flag.Bool("numeric", false, "reduce the version to a numeric x.y.z")
	config := flag.String("config", "wails.json", "app metadata file")
	name := flag.String("name", "", "executable and project name; empty keeps wails.json's")
	product := flag.String("product", "", "product name; empty keeps wails.json's")
	nightly := flag.Bool("nightly", false, "render for a nightly build")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: appmeta [-version v] [-numeric] template")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*config)
	if err != nil {
		fail(err)
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		fail(fmt.Errorf("%s: %w", *config, err))
	}
	if *version != "" {
		m.Info.ProductVersion = *version
	}
	if *name != "" {
		m.Name, m.OutputFilename = *name, *name
	}
	if *product != "" {
		m.Info.ProductName = *product
	}
	m.Nightly = *nightly
	m.Info.ProductVersion = productVersion(m.Info.ProductVersion, *numeric)

	tmpl, err := template.ParseFiles(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	if err := tmpl.Execute(os.Stdout, m); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "appmeta:", err)
	os.Exit(1)
}
