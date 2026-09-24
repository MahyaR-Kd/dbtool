package cmd

import (
	"fmt"
	"os"

	"dbtool/internal/logger"
	"dbtool/internal/settings"
	"github.com/spf13/cobra"
)

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func featureToggleCommand(use, short string, update func(*settings.Settings) (bool, string)) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Run: func(cmd *cobra.Command, args []string) {
		s := settings.Load()
		ok, message := update(&s)
		if !ok {
			fmt.Println(message)
			return
		}
		if err := settings.Save(s); err != nil {
			logger.Error("failed to save settings: %v", err)
			fmt.Println("Error saving settings:", err)
			os.Exit(1)
		}
		fmt.Println(message)
	}}
}
