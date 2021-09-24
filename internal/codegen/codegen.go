package codegen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iancoleman/strcase"
	"k8s.io/gengo/parser"
	"k8s.io/gengo/types"
)

// Find all top-level types with the tilt:starlark-gen=true tag.
func LoadStarlarkGenTypes(pkg string) (*types.Package, []*types.Type, error) {
	b := parser.New()
	err := b.AddDir(pkg)
	if err != nil {
		return nil, nil, err
	}
	universe, err := b.FindTypes()
	if err != nil {
		return nil, nil, err
	}

	pkgSpec := universe.Package(pkg)
	results := []*types.Type{}
	for _, t := range pkgSpec.Types {
		ok, err := types.ExtractSingleBoolCommentTag("+", "tilt:starlark-gen", false, t.CommentLines)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing tags in %s: %v", t, err)
		}
		if ok {
			results = append(results, t)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name.Name < results[j].Name.Name
	})

	return pkgSpec, results, nil
}

func getSpecMemberType(t *types.Type) *types.Type {
	for _, member := range t.Members {
		if member.Name == "Spec" {
			return member.Type
		}
	}
	return nil
}

// Opens the output file.
func OpenOutputFile(outDir string) (io.Writer, error) {
	out := os.Stdout
	if outDir != "-" {
		outPath := filepath.Join(outDir, "__init__.py")

		var err error
		out, err = os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0555)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Writes the package header.
func WritePreamble(pkg *types.Package, w io.Writer) error {

	_, err := fmt.Fprintf(w, `from typing import Dict, List, Optional

# AUTOGENERATED by github.com/tilt-dev/tilt-starlark-docs-codegen
# DO NOT EDIT MANUALLY
`)
	if err != nil {
		return err
	}
	return nil
}

func argName(m types.Member) string {
	if m.Name == "Labels" {
		return "spec_labels"
	}
	if m.Name == "Annotations" {
		return "spec_anotations"
	}
	return strcase.ToSnake(m.Name)
}

func argSpec(m types.Member) (string, string, string, error) {
	name := argName(m)
	if m.Type.Kind == types.Builtin && m.Type.Name.Name == "string" {
		return name, "str", `""`, nil
	}
	if m.Type.Kind == types.Alias && m.Type.Underlying.Kind == types.Builtin && m.Type.Underlying.Name.Name == "string" {
		return name, "str", `""`, nil
	}
	if m.Type.Kind == types.Pointer && m.Type.Elem.Kind == types.Builtin && m.Type.Elem.Name.Name == "string" {
		return name, "Optional[str]", "None", nil
	}
	if m.Type.Kind == types.Builtin && m.Type.Name.Name == "bool" {
		return name, "bool", `False`, nil
	}
	if m.Type.Kind == types.Builtin && m.Type.Name.Name == "int32" {
		return name, "int", `0`, nil
	}
	if m.Type.Kind == types.Map && m.Type.Elem.Name.Name == "string" && m.Type.Key.Name.Name == "string" {
		return name, "Dict[str, str]", "None", nil
	}
	if m.Type.Kind == types.Slice && m.Type.Elem.Name.Name == "string" {
		return name, "List[str]", "None", nil
	}
	if m.Type.Kind == types.Struct {
		return name, m.Type.Name.Name, "None", nil
	}
	if m.Type.Kind == types.Pointer && m.Type.Elem.Kind == types.Struct {
		return name, fmt.Sprintf("Optional[%s]", m.Type.Elem.Name.Name), "None", nil
	}
	if m.Type.Kind == types.Slice && m.Type.Elem.Kind == types.Struct {
		return name, fmt.Sprintf("List[%s]", m.Type.Elem.Name.Name), "None", nil
	}
	return "", "", "", fmt.Errorf("Unrecognized type of member %s: %s", m.Name, m.Type.Name)
}

func filterCommentTags(lines []string) []string {
	result := []string{}
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "+") {
			continue
		}
		result = append(result, l)
	}
	return result
}

// Given a gengo Type, create a starlark function that reads that type.
func WriteStarlarkFunction(t *types.Type, pkg *types.Package, w io.Writer) error {
	tName := t.Name.Name
	spec := getSpecMemberType(t)
	if spec == nil {
		return fmt.Errorf("type has no spec: %s", tName)
	}

	// Print the function signature.
	_, err := fmt.Fprintf(w, `
def %s(
  name: str,
  labels: Dict[str, str] = None,
  annotations: Dict[str, str] = None,`,
		strcase.ToSnake(tName))
	if err != nil {
		return err
	}

	// Print parameters for each spec member.
	for _, member := range spec.Members {
		if isTimeMember(member) {
			continue
		}

		argName, argType, argDefault, err := argSpec(member)
		if err != nil {
			return fmt.Errorf("generating type %s: %v", tName, err)
		}

		_, err = fmt.Fprintf(w, `
  %s: %s = %s,`, argName, argType, argDefault)
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, `
):
  """
  %s

  Args:
    name: The name in the Object metadata.
    labels: A set of key/value pairs in the Object metadata for grouping objects.
    annotations: A set of key/value pairs in the Object metadata for attaching data to objects.`,
		strings.Join(filterCommentTags(t.CommentLines), "\n  "))
	if err != nil {
		return err
	}

	// Print the argument docs.
	for _, member := range spec.Members {
		if isTimeMember(member) {
			continue
		}

		doc := strings.Join(filterCommentTags(member.CommentLines), "\n      ")
		if doc == "" {
			doc = "Documentation missing"
		}

		_, err = fmt.Fprintf(w, `
    %s: %s`, argName(member), doc)
		if err != nil {
			return err
		}
	}

	// End the function
	_, err = fmt.Fprintf(w, `
"""
  pass`)
	if err != nil {
		return err
	}
	return nil
}

// Given a gengo member Type, generate the class for that type.
// This needs to appear before any functions that use the class,
// due to how Python type resolution works.
func WriteStarlarkMemberClass(t *types.Type, pkg *types.Package, w io.Writer) error {
	tName := t.Name.Name

	// Print the function signature.
	_, err := fmt.Fprintf(w, `

class %s:
  """%s
"""
  pass

`,
		tName,
		strings.Join(filterCommentTags(t.CommentLines), "\n  "))
	if err != nil {
		return err
	}
	return nil
}

// Given a gengo member Type, create a starlark function that constructs that type
// and returns it.
func WriteStarlarkMemberFunction(t *types.Type, pkg *types.Package, w io.Writer) error {
	tName := t.Name.Name

	// Print the function signature.
	_, err := fmt.Fprintf(w, `

def %s(`,
		strcase.ToSnake(tName))
	if err != nil {
		return err
	}

	// Print parameters for each spec member.
	for _, member := range t.Members {
		if isTimeMember(member) {
			continue
		}

		argName, argType, argDefault, err := argSpec(member)
		if err != nil {
			return fmt.Errorf("generating type %s: %v", tName, err)
		}

		_, err = fmt.Fprintf(w, `
  %s: %s = %s,`, argName, argType, argDefault)
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, `
) -> %s:
  """
  %s

  Args:`,
		tName, strings.Join(filterCommentTags(t.CommentLines), "\n  "))
	if err != nil {
		return err
	}

	// Print the argument docs.
	for _, member := range t.Members {
		if isTimeMember(member) {
			continue
		}

		doc := strings.Join(filterCommentTags(member.CommentLines), "\n      ")
		if doc == "" {
			doc = "Documentation missing"
		}

		_, err = fmt.Fprintf(w, `
    %s: %s`, argName(member), doc)
		if err != nil {
			return err
		}
	}

	// End the function
	_, err = fmt.Fprintf(w, `
"""
  pass`)
	if err != nil {
		return err
	}
	return nil
}
