package cli

import (
	"fmt"
	"strings"
)

// reorderKnownFlags moves recognized flags ahead of positional arguments so
// commands can accept common forms like `fascinate machine get hello --json`.
func reorderKnownFlags(args []string, boolFlags map[string]bool, valueFlags map[string]bool) ([]string, error) {
	flagArgs := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i:]...)
			break
		}
		name, hasInlineValue, ok := parseFlagToken(arg)
		if !ok {
			positional = append(positional, arg)
			continue
		}
		if boolFlags[name] {
			flagArgs = append(flagArgs, arg)
			continue
		}
		if valueFlags[name] {
			flagArgs = append(flagArgs, arg)
			if hasInlineValue {
				continue
			}
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag needs an argument: -%s", name)
			}
			i++
			flagArgs = append(flagArgs, args[i])
			continue
		}
		positional = append(positional, arg)
	}
	return append(flagArgs, positional...), nil
}

func parseFlagToken(arg string) (name string, hasInlineValue bool, ok bool) {
	if arg == "-" || !strings.HasPrefix(arg, "-") {
		return "", false, false
	}
	trimmed := strings.TrimLeft(arg, "-")
	if trimmed == "" {
		return "", false, false
	}
	if index := strings.IndexByte(trimmed, '='); index >= 0 {
		return trimmed[:index], true, true
	}
	return trimmed, false, true
}
