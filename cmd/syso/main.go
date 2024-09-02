package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bitfocus/syso"
	"github.com/bitfocus/syso/pkg/coff"
)

var (
	configFile string
	outFile    string
	arch       string
)

func printErrorAndExit(format string, arg ...interface{}) {
	fmt.Fprintf(os.Stderr, format, arg...)
	os.Exit(1)
}

func init() {
	flag.StringVar(&arch, "arch", "amd64", "architecture (amd64, i386, arm, arm64)")
	flag.StringVar(&configFile, "c", "syso.json", "config file name")
	flag.StringVar(&outFile, "o", "out_[arch].syso", "output file name")
	flag.Parse()
}

func main() {
	outFile = strings.Replace(outFile, "[arch]", arch, -1)

	fcfg, err := os.Open(configFile)
	if err != nil {
		printErrorAndExit("failed to open config file: %v\n", err)
	}
	defer fcfg.Close()

	cfg, err := syso.ParseConfig(fcfg)
	if err != nil {
		printErrorAndExit("failed to parse config: %v\n", err)
	}

	c := coff.New()

	err = c.SetArch(arch)
	if err != nil {
		printErrorAndExit("failed to set architecture: %v\n", err)
	}

	for i, icon := range cfg.Icons {
		if err := syso.EmbedIcon(c, icon); err != nil {
			printErrorAndExit("failed to embed icon #%d: %v\n", i, err)
		}
	}

	if cfg.Manifest != nil {
		if err := syso.EmbedManifest(c, cfg.Manifest); err != nil {
			printErrorAndExit("failed to embed manifest: %v\n", err)
		}
	}

	for i, vi := range cfg.VersionInfos {
		if err := syso.EmbedVersionInfo(c, vi); err != nil {
			printErrorAndExit("failed to embed version info #%d: %v\n", i, err)
		}
	}

	fout, err := os.Create(outFile)
	if err != nil {
		panic(err)
	}
	defer fout.Close()

	if _, err := c.WriteTo(fout); err != nil {
		panic(err)
	}

	fmt.Printf("successfully generated syso file to %s", outFile)
}
