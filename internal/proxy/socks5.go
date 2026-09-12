// Package proxy provides a SOCKS5 tunnel that forwards local TCP connections
// through a SOCKS5 proxy server to a target host:port.
package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"

	"dbtool/internal/logger"
)

// dial establishes a SOCKS5 connection through the proxy to the target.
func dial(proxyHost, proxyPort, targetHost, targetPort, user, password string) (net.Conn, error) {
	conn, err := net.Dial("tcp", net.JoinHostPort(proxyHost, proxyPort))
	if err != nil {
		return nil, fmt.Errorf("connect to SOCKS5 proxy %s:%s: %w", proxyHost, proxyPort, err)
	}

	useAuth := user != "" || password != ""

	// Greeting: propose NO_AUTH (0x00) and optionally USERNAME/PASSWORD (0x02).
	if useAuth {
		_, err = conn.Write([]byte{0x05, 0x02, 0x00, 0x02})
	} else {
		_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	}
	if err != nil {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 greeting error: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("SOCKS5 greeting: %w", err)
	}

	// Server selects a method.
	resp := make([]byte, 2)
	if _, err = io.ReadFull(conn, resp); err != nil {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 method response error: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("SOCKS5 method response: %w", err)
	}
	if resp[0] != 0x05 {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after unexpected SOCKS5 version: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("unexpected SOCKS5 version in response: %d", resp[0])
	}

	switch resp[1] {
	case 0x00:
		// No authentication required — proceed.
	case 0x02:
		if !useAuth {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 auth required but no credentials: %v", err)
				return nil, err
			}
			return nil, fmt.Errorf("SOCKS5 proxy requires username/password authentication")
		}
		// Sub-negotiation: VER=0x01, ULEN, UNAME, PLEN, PASSWD.
		msg := []byte{0x01, byte(len(user))}
		msg = append(msg, []byte(user)...)
		msg = append(msg, byte(len(password)))
		msg = append(msg, []byte(password)...)
		if _, err = conn.Write(msg); err != nil {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 auth send error: %v", err)
				return nil, err
			}
			return nil, fmt.Errorf("SOCKS5 auth send: %w", err)
		}
		authResp := make([]byte, 2)
		if _, err = io.ReadFull(conn, authResp); err != nil {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 auth response error: %v", err)
				return nil, err
			}
			return nil, fmt.Errorf("SOCKS5 auth response: %w", err)
		}
		if authResp[1] != 0x00 {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 auth failure: %v", err)
				return nil, err
			}
			return nil, fmt.Errorf("SOCKS5 authentication failed (status %d)", authResp[1])
		}
	case 0xFF:
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 no acceptable methods: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("SOCKS5 proxy rejected all authentication methods")
	default:
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 unsupported auth method: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("unsupported SOCKS5 auth method: %d", resp[1])
	}

	// CONNECT request using domain-name address type (0x03).
	port, err := strconv.Atoi(targetPort)
	if err != nil || port < 1 || port > 65535 {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after invalid target port: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("invalid target port %q: must be 1–65535", targetPort)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))

	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(targetHost))}
	req = append(req, []byte(targetHost)...)
	req = append(req, portBytes...)
	if _, err = conn.Write(req); err != nil {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 CONNECT send error: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("SOCKS5 CONNECT request: %w", err)
	}

	// Read CONNECT response header (4 bytes).
	hdr := make([]byte, 4)
	if _, err = io.ReadFull(conn, hdr); err != nil {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 CONNECT response error: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("SOCKS5 CONNECT response: %w", err)
	}
	if hdr[1] != 0x00 {
		err := conn.Close()
		if err != nil {
			logger.Error("failed to close connection after SOCKS5 CONNECT failure: %v", err)
			return nil, err
		}
		return nil, fmt.Errorf("SOCKS5 CONNECT failed (REP=%d)", hdr[1])
	}

	// Drain the BND.ADDR / BND.PORT fields so the connection is ready for data.
	switch hdr[3] {
	case 0x01: // IPv4
		_, err := io.ReadFull(conn, make([]byte, 4+2))
		if err != nil {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 CONNECT IPv4 drain error: %v", err)
				return nil, err
			}
			return nil, err
		}
	case 0x03: // Domain name
		lb := make([]byte, 1)
		_, err := io.ReadFull(conn, lb)
		if err != nil {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 CONNECT domain length read error: %v", err)
				return nil, err
			}
			return nil, err
		}
		_, err = io.ReadFull(conn, make([]byte, int(lb[0])+2))
		if err != nil {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 CONNECT domain drain error: %v", err)
				return nil, err
			}
			return nil, err
		}
	case 0x04: // IPv6
		_, err := io.ReadFull(conn, make([]byte, 16+2))
		if err != nil {
			err := conn.Close()
			if err != nil {
				logger.Error("failed to close connection after SOCKS5 CONNECT IPv6 drain error: %v", err)
				return nil, err
			}
			return nil, err
		}
	}

	return conn, nil
}

// forward pipes data between the client and the proxy connection bidirectionally.
func forward(client net.Conn, proxyHost, proxyPort, targetHost, targetPort, user, password string) {
	defer func(client net.Conn) {
		err := client.Close()
		if err != nil {
			logger.Error("failed to close client connection: %v", err)
		}
	}(client)
	proxConn, err := dial(proxyHost, proxyPort, targetHost, targetPort, user, password)
	if err != nil {
		logger.Error("SOCKS5 forward dial failed: %v", err)
		return
	}
	defer func(proxConn net.Conn) {
		err := proxConn.Close()
		if err != nil {
			logger.Error("failed to close proxy connection: %v", err)
		}
	}(proxConn)

	done := make(chan struct{}, 2)
	go func() {
		_, err := io.Copy(proxConn, client)
		if err != nil {
			logger.Error("SOCKS5 forward copy to proxy failed: %v", err)
			return
		}
		done <- struct{}{}
	}()
	go func() {
		_, err := io.Copy(client, proxConn)
		if err != nil {
			logger.Error("SOCKS5 forward copy to client failed: %v", err)
			return
		}
		done <- struct{}{}
	}()
	<-done
}

// StartTunnel creates a local TCP listener on 127.0.0.1 and forwards every
// accepted connection through the SOCKS5 proxy to targetHost:targetPort.
// It returns the chosen local port and a stop function that closes the listener.
func StartTunnel(proxyHost, proxyPort, targetHost, targetPort, user, password string) (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("SOCKS5 local listener: %w", err)
	}
	localPort := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	logger.Info("SOCKS5 tunnel: proxy=%s:%s -> target=%s:%s (local port %s)",
		proxyHost, proxyPort, targetHost, targetPort, localPort)

	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return // listener was closed
			}
			go forward(client, proxyHost, proxyPort, targetHost, targetPort, user, password)
		}
	}()

	stop := func() {
		err := ln.Close()
		if err != nil {
			logger.Error("failed to close SOCKS5 listener: %v", err)
			return
		}
	}
	return localPort, stop, nil
}
