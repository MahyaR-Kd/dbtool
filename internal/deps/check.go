package deps

import (
	"dbtool/internal/interactivelist"
	"dbtool/internal/logger"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// osID returns a string identifying the current OS distribution.
// On macOS it returns "darwin" immediately. On Linux it reads /etc/os-release.
func osID() string {
	if runtime.GOOS == "darwin" {
		return "darwin"
	}

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, `"`)
			return strings.ToLower(id)
		}
	}

	return ""
}

// installCmd returns the shell command to install the given tools based on OS.
func installCmd(tools []string, id string) (string, []string) {
	switch id {
	case "ubuntu", "debian":
		args := append([]string{"apt", "install", "-y"}, tools...)
		return "sudo", args
	case "centos", "rhel", "fedora", "rocky", "almalinux":
		args := append([]string{"yum", "install", "-y"}, tools...)
		return "sudo", args
	case "darwin":
		if _, err := exec.LookPath("brew"); err != nil {
			fmt.Println("Homebrew (brew) is not installed. Visit https://brew.sh to install it, then re-run.")
			return "", nil
		}
		args := append([]string{"install"}, tools...)
		return "brew", args
	default:
		// Generic fallback — just tell the user what to install
		return "", nil
	}
}

// Require checks that each tool in tools is available on PATH.
// If any are missing it lists them, optionally installs them, and exits if they
// remain absent.
func Require(tools ...string) {
	var missing []string

	for _, t := range tools {
		if _, err := exec.LookPath(t); err != nil {
			logger.Warn("required tool not found on PATH: %s", t)
			missing = append(missing, t)
		} else {
			logger.Debug("required tool found: %s", t)
		}
	}

	if len(missing) == 0 {
		return
	}

	fmt.Printf("Missing required tool(s): %s\n", strings.Join(missing, ", "))

	id := osID()
	cmd, args := installCmd(missing, id)

	if cmd == "" {
		logger.Error("cannot install missing tools automatically (OS=%s): %s", id, strings.Join(missing, ", "))
		fmt.Println("Please install the missing tools and try again.")
		os.Exit(1)
	}

	fmt.Printf("Suggested install command: %s %s\n", cmd, strings.Join(args, " "))

	if !interactivelist.Confirm("Install now?", false) {
		logger.Warn("user declined to install missing tools: %s", strings.Join(missing, ", "))
		fmt.Println("Aborted. Please install the required tools and try again.")
		os.Exit(1)
	}

	logger.Info("installing missing tools: %s", strings.Join(missing, ", "))
	installExec := exec.Command(cmd, args...)
	installExec.Stdout = os.Stdout
	installExec.Stderr = os.Stderr

	if err := installExec.Run(); err != nil {
		logger.Error("installation failed for tools %s: %v", strings.Join(missing, ", "), err)
		fmt.Println("Installation failed:", err)
		os.Exit(1)
	}

	// Verify again after installation
	var stillMissing []string
	for _, t := range missing {
		if _, err := exec.LookPath(t); err != nil {
			stillMissing = append(stillMissing, t)
		}
	}

	if len(stillMissing) > 0 {
		logger.Error("tools still missing after install: %s", strings.Join(stillMissing, ", "))
		fmt.Printf("Still missing after install: %s\n", strings.Join(stillMissing, ", "))
		os.Exit(1)
	}

	logger.Info("successfully installed tools: %s", strings.Join(missing, ", "))
}

// Offer checks whether tool is on PATH and, if not, offers to install it
// via the OS package manager — the same detection/install machinery as
// Require, but for a nice-to-have rather than a hard requirement:
// declining, a failed install, or not being able to suggest a command at
// all is never fatal, it just leaves the tool missing. why is a short,
// one-line reason shown alongside the offer so the user knows what
// they'd get from installing it.
func Offer(tool, why string) {
	if _, err := exec.LookPath(tool); err == nil {
		logger.Debug("optional tool found: %s", tool)
		return
	}

	fmt.Printf("Tip: %s is not installed. %s\n", tool, why)

	id := osID()
	cmd, args := installCmd([]string{tool}, id)
	if cmd == "" {
		return
	}

	fmt.Printf("Suggested install command: %s %s\n", cmd, strings.Join(args, " "))
	if !interactivelist.Confirm(fmt.Sprintf("Install %s now?", tool), false) {
		return
	}

	logger.Info("installing optional tool: %s", tool)
	installExec := exec.Command(cmd, args...)
	installExec.Stdout = os.Stdout
	installExec.Stderr = os.Stderr

	if err := installExec.Run(); err != nil {
		logger.Warn("optional tool %s failed to install: %v", tool, err)
		fmt.Println("Installation failed:", err)
		return
	}

	if _, err := exec.LookPath(tool); err != nil {
		logger.Warn("optional tool %s still missing after install", tool)
		fmt.Printf("%s still not found after install.\n", tool)
		return
	}

	logger.Info("successfully installed optional tool: %s", tool)
	fmt.Printf("%s installed.\n", tool)
}
