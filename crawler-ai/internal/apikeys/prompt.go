package apikeys

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// PromptForKey interactively prompts the user for an API key with masked input.
// It reads from stdin using terminal raw mode for security.
func PromptForKey(provider string) (string, error) {
	return PromptForKeyFromReader(provider, os.Stdin, os.Stdout)
}

// PromptForKeyFromReader prompts using the given reader/writer (for testing).
func PromptForKeyFromReader(provider string, in io.Reader, out io.Writer) (string, error) {
	envVar := EnvVarForProvider(provider)
	fmt.Fprintf(out, "Enter API key for %s (env: %s)\n", provider, envVar)
	fmt.Fprintf(out, "Paste your key (input is hidden): ")

	// Try terminal raw mode for hidden input
	if f, ok := in.(*os.File); ok {
		fd := int(f.Fd())
		if term.IsTerminal(fd) {
			key, err := term.ReadPassword(fd)
			fmt.Fprintln(out) // newline after hidden input
			if err != nil {
				return "", fmt.Errorf("read password: %w", err)
			}
			return strings.TrimSpace(string(key)), nil
		}
	}

	// Fallback for non-terminal (pipes, tests)
	scanner := bufio.NewScanner(in)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return "", fmt.Errorf("no input received")
}

// PromptForProvider asks the user to select a provider from a numbered list.
func PromptForProvider(in io.Reader, out io.Writer) (string, error) {
	providers := SupportedProviders()
	fmt.Fprintln(out, "Select a provider:")
	for i, p := range providers {
		fmt.Fprintf(out, "  %d. %s\n", i+1, p)
	}
	fmt.Fprintf(out, "Enter number (1-%d): ", len(providers))

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", fmt.Errorf("no input received")
	}

	choice := strings.TrimSpace(scanner.Text())
	var idx int
	if _, err := fmt.Sscanf(choice, "%d", &idx); err != nil || idx < 1 || idx > len(providers) {
		return "", fmt.Errorf("invalid selection: %s", choice)
	}

	return providers[idx-1], nil
}

// ConfirmOverwrite asks the user whether to overwrite an existing key.
func ConfirmOverwrite(provider string, in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprintf(out, "A key for %s already exists. Overwrite? [y/N]: ", provider)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}
