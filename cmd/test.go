package cmd

import (
	"context"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/armory-io/bilgepump/pkg/mark/aws"
	"github.com/armory-io/bilgepump/pkg/mark/k8s"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Runs a single configuration through a Mark phase test",
	Long: `'test' takes a single marker from configuration and runs it through a mark phase to demonstrate
            the assets that would be identified for later deletion.  You can use this to tune the configuration for
            things like specifying ignore tags.`,
}

var awsCmd = &cobra.Command{
	Use:   "aws",
	Short: "Runs a single marker for aws account name",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, log := loadConfig()
		log.SetLevel(logrus.DebugLevel)
		if cfg.Aws == nil {
			log.Fatal("No AWS configuration present")
		}
		if len(args) <= 0 || len(args) >= 2 {
			log.Fatal("No account specified")
		}
		accounts := map[string]config.Aws{}
		for _, a := range cfg.Aws {
			accounts[a.Name] = a
		}
		if _, ok := accounts[args[0]]; !ok {
			log.Fatalf("Account %s is not in %s", args[0], ConfigLocation)
		}
		log.Infof("Doing a test mark run for %s", accounts[args[0]].Name)
		mc := cache.NewMockCache()
		ctx := context.Background()
		account := accounts[args[0]]
		m := aws.NewAwsMarker(ctx, &account, log, mc)
		m.Mark()
	},
}

var k8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Runs a single marker for k8s account name",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, log := loadConfig()
		log.SetLevel(logrus.DebugLevel)

		if cfg.Kubernetes == nil {
			log.Fatal("No K8S accounts configured")
		}
		if len(args) <= 0 || len(args) >= 2 {
			log.Fatal("No account specified")
		}
		accounts := map[string]config.Kubernetes{}
		for _, k := range cfg.Kubernetes {
			accounts[k.Name] = k
		}
		if _, ok := accounts[args[0]]; !ok {
			log.Fatalf("Account %s is not in %s", args[0], ConfigLocation)
		}
		log.Infof("Doing a test mark run for %s", accounts[args[0]].Name)
		mc := cache.NewMockCache()
		ctx := context.Background()
		account := accounts[args[0]]
		m, err := k8s.NewK8SMarker(ctx, &account, log, mc)
		if err != nil {
			log.Fatal(err)
		}
		m.Mark()
	},
}

func init() {
	rootCmd.AddCommand(testCmd)
	testCmd.AddCommand(awsCmd)
	testCmd.AddCommand(k8sCmd)
}
