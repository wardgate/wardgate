package exec

import (
	"fmt"
	"strings"

	"github.com/wardgate/wardgate/internal/config"
)

// ShellEscape wraps a value in single quotes, escaping any embedded single
// quotes with the standard '\” idiom. This prevents shell interpretation of
// special characters in argument values.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ExpandTemplate substitutes argument values into a command template.
// Each {argname} placeholder is replaced with the shell-escaped value.
// Returns an error if the number of values doesn't match the declared args,
// or if a declared arg's placeholder is missing from the template.
func ExpandTemplate(template string, args []config.CommandArg, values []string) (string, error) {
	if len(values) != len(args) {
		return "", fmt.Errorf("command expects %d arg(s), got %d", len(args), len(values))
	}

	result := template
	for i, arg := range args {
		placeholder := "{" + arg.Name + "}"
		if !strings.Contains(result, placeholder) {
			return "", fmt.Errorf("placeholder {%s} not found in template", arg.Name)
		}
		result = strings.ReplaceAll(result, placeholder, ShellEscape(values[i]))
	}

	return result, nil
}
