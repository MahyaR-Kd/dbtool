package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/logger"
	"dbtool/internal/secureinput"
	"dbtool/internal/settings"

	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage SOCKS5 proxy settings for database connections",
}

var proxySetHost string
var proxySetPort string
var proxySetUser string
var proxySetPassword string

var proxySetCmd = &cobra.Command{
	Use:   "set",
	Short: "Configure the SOCKS5 proxy used for dump/restore connections",
	Long: `Configure a SOCKS5 proxy.

When configured, dump and restore operations will route their database
connections through this proxy instead of connecting directly.

Host and port are required. User and password are optional.

Non-interactive example:
  dbtool proxy set --host 127.0.0.1 --port 1080
  dbtool proxy set --host 127.0.0.1 --port 1080 --user myuser --password secret`,
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()

		hasFlags := cmd.Flags().Changed("host") ||
			cmd.Flags().Changed("port") ||
			cmd.Flags().Changed("user") ||
			cmd.Flags().Changed("password")

		if hasFlags {
			if !cmd.Flags().Changed("host") || !cmd.Flags().Changed("port") {
				fmt.Println("--host and --port are required.")
				os.Exit(1)
			}
			s.Proxy.Host = proxySetHost
			s.Proxy.Port = proxySetPort
			if cmd.Flags().Changed("user") {
				s.Proxy.User = proxySetUser
			}
			if cmd.Flags().Changed("password") {
				s.Proxy.Password = proxySetPassword
			}
		} else {
			reader := bufio.NewReader(os.Stdin)

			readField := func(prompt, cur string) string {
				if cur != "" {
					fmt.Printf("%s [%s]: ", prompt, cur)
				} else {
					fmt.Printf("%s: ", prompt)
				}
				val, _ := reader.ReadString('\n')
				val = strings.TrimSpace(val)
				if val == "" {
					return cur
				}
				return val
			}

			readSecret := func(prompt, cur string) string {
				var label string
				if cur != "" {
					label = fmt.Sprintf("%s [%s]: ", prompt, maskedPlaceholder)
				} else {
					label = fmt.Sprintf("%s (optional): ", prompt)
				}
				val, err := secureinput.ReadPassword(label)
				if err != nil {
					fmt.Println("Error reading input:", err)
					return cur
				}
				val = strings.TrimSpace(val)
				if val == "" || val == maskedPlaceholder {
					return cur
				}
				return val
			}

			s.Proxy.Host = readField("Proxy host", s.Proxy.Host)
			s.Proxy.Port = readField("Proxy port", s.Proxy.Port)
			s.Proxy.User = readField("Proxy user (optional, press Enter to skip)", s.Proxy.User)
			s.Proxy.Password = readSecret("Proxy password", s.Proxy.Password)

			if s.Proxy.Host == "" || s.Proxy.Port == "" {
				fmt.Println("Error: host and port are required.")
				os.Exit(1)
			}
		}

		if err := settings.Save(s); err != nil {
			logger.Error("failed to save proxy settings: %v", err)
			fmt.Println("Error saving settings:", err)
			os.Exit(1)
		}

		logger.Info("proxy settings saved (host=%s port=%s)", s.Proxy.Host, s.Proxy.Port)
		fmt.Printf("Proxy set to %s:%s\n", s.Proxy.Host, s.Proxy.Port)
	},
}

var proxyShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current SOCKS5 proxy configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		if !s.Proxy.Enabled() {
			fmt.Println("No proxy configured.")
			return
		}
		fmt.Printf("Proxy host: %s\n", s.Proxy.Host)
		fmt.Printf("Proxy port: %s\n", s.Proxy.Port)
		if s.Proxy.User != "" {
			fmt.Printf("Proxy user: %s\n", s.Proxy.User)
		}
		if s.Proxy.Password != "" {
			fmt.Printf("Proxy pass: %s\n", maskedPlaceholder)
		}
	},
}

var proxyClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove the SOCKS5 proxy configuration",
	Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		s.Proxy = settings.ProxyConfig{}
		if err := settings.Save(s); err != nil {
			logger.Error("failed to clear proxy settings: %v", err)
			fmt.Println("Error saving settings:", err)
			os.Exit(1)
		}
		logger.Info("proxy settings cleared")
		fmt.Println("Proxy configuration cleared.")
	},
}

func init() {
	proxySetCmd.Flags().StringVar(&proxySetHost, "host", "", "Proxy host (non-interactive)")
	proxySetCmd.Flags().StringVar(&proxySetPort, "port", "", "Proxy port (non-interactive)")
	proxySetCmd.Flags().StringVar(&proxySetUser, "user", "", "Proxy user (optional, non-interactive)")
	proxySetCmd.Flags().StringVar(&proxySetPassword, "password", "", "Proxy password (optional, non-interactive)")
	proxyCmd.AddCommand(proxySetCmd)
	proxyCmd.AddCommand(proxyShowCmd)
	proxyCmd.AddCommand(proxyClearCmd)
	if installedInPath {
		settingCmd.AddCommand(proxyCmd)
	}
}
