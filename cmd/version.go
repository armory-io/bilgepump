package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	COMMIT string
	BRANCH string
	SEMVER string
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Prints version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("bilgepump %s (%s-%s)\n\r", SEMVER, BRANCH, COMMIT)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
