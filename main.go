package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tilt-dev/tilt-starlark-docs-codegen/internal/codegen"
)

func main() {
	args := os.Args

	if len(args) != 3 {
		fmt.Fprintf(os.Stderr, `%s: requires exactly 2 arguments.

Usage:
# Sample input and output
tilt-starlark-docs-codegen ./path/to/input ./path/to/output

# In the Tilt codebase
tilt-starlark-docs-codegen ./pkg/apis/core/v1alpha1 ../tilt.build/api/modules/v1alpha1

# Dry run (print to stdout)
tilt-starlark-docs-codegen ./pkg/apis/core/v1alpha1 -
`, filepath.Base(args[0]))
		os.Exit(1)
	}

	pkg, topTypes, err := codegen.LoadStarlarkGenTypes(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	buf := bytes.NewBuffer(nil)
	err = codegen.WritePreamble(pkg, buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	memberTypes, err := codegen.FindStructMembers(topTypes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, t := range memberTypes {
		err := codegen.WriteStarlarkMemberClass(t, pkg, buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	for _, t := range topTypes {
		err := codegen.WriteStarlarkFunction(t, pkg, buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	for _, t := range memberTypes {
		err := codegen.WriteStarlarkMemberFunction(t, pkg, buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	out, err := codegen.OpenOutputFile(args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	_, err = out.Write(buf.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	closer, ok := out.(io.Closer)
	if ok {
		_ = closer.Close()
	}
}
