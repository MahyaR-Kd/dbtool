package ssh

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"dbtool/internal/logger"
)

func getFreePort() string {
	l, _ := net.Listen("tcp", ":0")
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

// StartTunnel opens an SSH port-forward tunnel. It forwards
// localPort (or a random free port if empty) on 127.0.0.1 to
// dbHost:dbPort through the SSH server at sshUser@sshHost:sshPort.
// Returns the tunnel process and the actual local port chosen.
func StartTunnel(sshUser, sshHost, sshPort, dbHost, dbPort, localPort string) (*exec.Cmd, string) {
	if localPort == "" {
		localPort = getFreePort()
	}
	if sshPort == "" {
		sshPort = "22"
	}

	fwd := fmt.Sprintf("%s:%s:%s", localPort, dbHost, dbPort)
	target := fmt.Sprintf("%s@%s", sshUser, sshHost)

	logger.Info("starting SSH tunnel: %s -p %s -> local port %s", target, sshPort, localPort)

	cmd := exec.Command("ssh",
		"-L", fwd,
		"-p", sshPort,
		target,
		"-N",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		logger.Error("SSH tunnel failed to start: %v", err)
		fmt.Println("SSH failed")
		os.Exit(1)
	}

	logger.Debug("SSH tunnel started (PID=%d), waiting 2s for connection", cmd.Process.Pid)
	time.Sleep(2 * time.Second)

	logger.Info("SSH tunnel ready on local port %s", localPort)
	return cmd, localPort
}
