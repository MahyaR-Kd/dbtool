package cmd

// Version is the current build version of dbtool.
// It is overridden at build time via:
//
//	go build -ldflags "-X dbtool/cmd.Version=<tag>" .
var Version = "dev"
