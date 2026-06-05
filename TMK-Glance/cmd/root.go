package cmd

import (
	"fmt"
	"log"
	"os"

	"tmk-glance/internal/server"

	"github.com/spf13/cobra"
)

var (
	port     string
	rootCmd  = &cobra.Command{Use: "tmk-glance", Short: "TMK 同声传译服务端"}
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "启动 TMK 服务",
		Run: func(cmd *cobra.Command, args []string) {
			addr := fmt.Sprintf(":%s", port)
			log.Printf("[startup] TMK-Glance server starting on %s", addr)
			if err := server.Start(addr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		},
	}
)

func Execute() {
	startCmd.Flags().StringVar(&port, "port", "8080", "listen port")
	rootCmd.AddCommand(startCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
