// Command corpustool builds and validates the frozen corpus.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/c360studio/semguard/internal/corpus"
)

func newCanary() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "SECRET-" + strings.ToUpper(hex.EncodeToString(b[:])), nil
}

// exitCode maps a validation failure to 1 and anything else (IO) to 2.
func exitCode(err error) int {
	var ve *corpus.ValidationError
	if errors.As(err, &ve) {
		return 1
	}
	return 2
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run returns 0 ok, 1 validation failure, 2 usage or IO error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || (args[0] != "build" && args[0] != "validate") {
		_, _ = fmt.Fprintln(stderr, "usage: corpustool build|validate [-dir corpus]")
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "corpus", "corpus directory")
	partial := fs.Bool("partial", false, "build: allow missing drafts; writes cases.json but no lock")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	switch args[0] {
	case "build":
		res, err := corpus.Build(*dir, newCanary, *partial)
		for _, w := range res.Warnings {
			_, _ = fmt.Fprintln(stderr, "warning:", w)
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return exitCode(err)
		}
		_, _ = fmt.Fprintf(stdout, "corpus: built %d cases, sha256 %s\n", res.Cases, res.SHA256)
	case "validate":
		cases, _, sum, err := corpus.LoadWithHash(*dir)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return exitCode(err)
		}
		_, _ = fmt.Fprintf(stdout, "corpus: ok (%d cases, sha256 %s)\n", len(cases), sum)
	}
	return 0
}
