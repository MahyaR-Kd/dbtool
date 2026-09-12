package connection

import (
	"fmt"
	"os"

	"dbtool/internal/logger"
	"dbtool/internal/proxy"
	"dbtool/internal/settings"
	"dbtool/internal/ssh"
	"dbtool/internal/types"
)

// ApplyTunnels inspects the global settings and the config to determine whether
// a SOCKS5 proxy, an SSH tunnel, or both need to be established before
// connecting to the database. It rewrites cfg.Host and cfg.Port to point at the
// local forwarding endpoint and returns a cleanup function that tears down any
// tunnels that were started. The caller must defer the returned cleanup.
func ApplyTunnels(cfg types.Config, s settings.Settings) (types.Config, func()) {
	cleanup := func() {}

	switch {
	case s.Proxy.Enabled() && cfg.SSH:
		// SOCKS5 proxy → SSH tunnel → DB
		sshPort := cfg.SSHPort
		if sshPort == "" {
			sshPort = "22"
		}
		logger.Debug("routing SSH through SOCKS5 proxy (proxy=%s:%s -> SSH=%s:%s)",
			s.Proxy.Host, s.Proxy.Port, cfg.SSHHost, sshPort)
		localSSHPort, stopProxy, err := proxy.StartTunnel(
			s.Proxy.Host, s.Proxy.Port, cfg.SSHHost, sshPort,
			s.Proxy.User, s.Proxy.Password)
		if err != nil {
			logger.Error("failed to start SOCKS5 tunnel for SSH: %v", err)
			fmt.Println("Failed to start SOCKS5 proxy tunnel:", err)
			os.Exit(1)
		}
		tunnel, port := ssh.StartTunnel(cfg.SSHUser, "127.0.0.1", localSSHPort, cfg.Host, cfg.Port, cfg.LocalPort)
		cfg.Host = "127.0.0.1"
		cfg.Port = port
		cleanup = func() {
			stopProxy()
			if err := tunnel.Process.Kill(); err != nil {
				logger.Error("failed to kill SSH tunnel process: %v", err)
				fmt.Println("Error: failed to clean up SSH tunnel process:", err)
				os.Exit(1)
			}
		}

	case cfg.SSH:
		// SSH tunnel only
		sshPort := cfg.SSHPort
		if sshPort == "" {
			sshPort = "22"
		}
		logger.Debug("setting up SSH tunnel (SSH host=%s)", cfg.SSHHost)
		tunnel, port := ssh.StartTunnel(cfg.SSHUser, cfg.SSHHost, sshPort, cfg.Host, cfg.Port, cfg.LocalPort)
		cfg.Host = "127.0.0.1"
		cfg.Port = port
		cleanup = func() {
			if err := tunnel.Process.Kill(); err != nil {
				logger.Error("failed to kill SSH tunnel process: %v", err)
				fmt.Println("Error: failed to clean up SSH tunnel process:", err)
				os.Exit(1)
			}
		}

	case s.Proxy.Enabled():
		// SOCKS5 proxy only
		logger.Debug("setting up SOCKS5 proxy tunnel (proxy=%s:%s)", s.Proxy.Host, s.Proxy.Port)
		localPort, stop, err := proxy.StartTunnel(
			s.Proxy.Host, s.Proxy.Port, cfg.Host, cfg.Port,
			s.Proxy.User, s.Proxy.Password)
		if err != nil {
			logger.Error("failed to start SOCKS5 tunnel: %v", err)
			fmt.Println("Failed to start SOCKS5 proxy tunnel:", err)
			os.Exit(1)
		}
		cfg.Host = "127.0.0.1"
		cfg.Port = localPort
		cleanup = stop
	}

	return cfg, cleanup
}
