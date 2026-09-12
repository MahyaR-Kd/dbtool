package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install dbtool binary to your PATH so it can be used system-wide",
	Long: `Install copies the dbtool binary into a directory on your PATH.

It first tries /usr/local/bin (system-wide installation).  If that fails due
to insufficient permissions it falls back to ~/bin, creating the directory
when necessary.

After installation you can run dbtool from any directory without specifying
the full path.

Shell completion is set up automatically for bash, zsh, and fish.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		src, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine current executable path: %w", err)
		}

		// Resolve symlinks so we copy the real binary.
		src, err = filepath.EvalSymlinks(src)
		if err != nil {
			return fmt.Errorf("cannot resolve executable path: %w", err)
		}

		dest, err := chooseInstallDest()
		if err != nil {
			return err
		}

		if err := copyExecutable(src, dest); err != nil {
			return fmt.Errorf("failed to install binary to %s: %w", dest, err)
		}

		fmt.Printf("dbtool installed to %s\n", dest)
		fmt.Println("You can now run `dbtool` from anywhere.")

		installCompletion()
		return nil
	},
}

// installCompletion detects the running shell and installs the completion script
// to the appropriate user-local location. Errors are non-fatal: a note is
// printed so the install itself always succeeds.
func installCompletion() {
	shell := filepath.Base(os.Getenv("SHELL"))
	shell = strings.ToLower(shell)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Note: could not determine home directory; skipping completion setup.")
		return
	}

	switch shell {
	case "bash":
		dir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
		dest := filepath.Join(dir, "dbtool")
		generate := func(w io.Writer) error { return rootCmd.GenBashCompletionV2(w, true) }
		if err := writeCompletion(dir, dest, generate); err != nil {
			fmt.Printf("Note: could not install bash completion: %v\n", err)
			return
		}
		fmt.Printf("Bash completion installed to %s\n", dest)

	case "zsh":
		dir, err := zshFpathDir()
		if err != nil {
			fmt.Printf("Note: could not determine zsh fpath: %v\n", err)
			return
		}
		dest := filepath.Join(dir, "_dbtool")
		if err := writeCompletion(dir, dest, rootCmd.GenZshCompletion); err != nil {
			fmt.Printf("Note: could not install zsh completion: %v\n", err)
			return
		}
		fmt.Printf("Zsh completion installed to %s\n", dest)

	case "fish":
		dir := filepath.Join(home, ".config", "fish", "completions")
		dest := filepath.Join(dir, "dbtool.fish")
		generate := func(w io.Writer) error { return rootCmd.GenFishCompletion(w, true) }
		if err := writeCompletion(dir, dest, generate); err != nil {
			fmt.Printf("Note: could not install fish completion: %v\n", err)
			return
		}
		fmt.Printf("Fish completion installed to %s\n", dest)

	default:
		if shell == "" {
			fmt.Println("Note: $SHELL is not set; skipping completion setup.")
		} else {
			fmt.Printf("Note: automatic completion setup is not supported for %q. Supported shells: bash, zsh, fish.\n", shell)
		}
	}
}

// zshFpathDir returns the first writable directory from the zsh fpath by
// running zsh and printing $fpath[1]. This avoids needing to add a custom
// directory to ~/.zshrc.
func zshFpathDir() (string, error) {
	out, err := exec.Command("zsh", "-c", "echo $fpath[1]").Output()
	if err != nil {
		return "", fmt.Errorf("zsh -c `echo $fpath[1]` failed: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("zsh returned an empty fpath[1]")
	}
	return dir, nil
}

// writeCompletion creates dir if needed, generates a completion script via
// generate, and writes it to dest.
func writeCompletion(dir, dest string, generate func(io.Writer) error) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	var buf bytes.Buffer
	if err := generate(&buf); err != nil {
		return fmt.Errorf("cannot generate completion script: %w", err)
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("cannot write %s: %w", dest, err)
	}
	return nil
}

// chooseInstallDest returns the destination path for the binary.
// It prefers /usr/local/bin; falls back to ~/bin when not writable.
func chooseInstallDest() (string, error) {
	systemDir := "/usr/local/bin"
	if isWritable(systemDir) {
		return filepath.Join(systemDir, "dbtool"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	userBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(userBin, 0755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", userBin, err)
	}
	fmt.Printf("Note: /usr/local/bin is not writable; installing to %s instead.\n", userBin)
	fmt.Println("Make sure ~/bin is on your PATH (add it to your shell profile, e.g. PATH=\"$HOME/bin:$PATH\").")
	return filepath.Join(userBin, "dbtool"), nil
}

// isWritable reports whether dir exists and is writable by the current user.
func isWritable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	// Try creating a temp file in the directory.
	tmp, err := os.CreateTemp(dir, ".dbtool_write_test_*")
	if err != nil {
		return false
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return true
}

// copyExecutable copies the file at src to dest, giving it executable permissions.
func copyExecutable(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Write to a temp file next to dest, then rename for atomicity.
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dest)
}

func init() {
	cobra.OnInitialize(setupFlagCompletion)
	if !installedInPath {
		rootCmd.AddCommand(installCmd)
	}
}

// setupFlagCompletion walks the entire command tree and registers a
// ValidArgsFunction on every command that has user-visible flags. This causes
// the shell to suggest flags (e.g. --name, --pass) when the user presses <Tab>
// after a command, not only after a leading dash.
func setupFlagCompletion() {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		// Only attach to leaf-like commands that have non-help flags.
		hasUserFlags := false
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden && f.Name != "help" {
				hasUserFlags = true
			}
		})
		if hasUserFlags && c.ValidArgsFunction == nil {
			c.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				var completions []string
				cmd.Flags().VisitAll(func(f *pflag.Flag) {
					if !f.Hidden {
						completions = append(completions, "--"+f.Name+"\t"+f.Usage)
					}
				})
				return completions, cobra.ShellCompDirectiveNoFileComp
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
