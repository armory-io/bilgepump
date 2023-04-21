package cmd

import (
	"context"
	"fmt"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/armory-io/bilgepump/pkg/mark"
	awsmarker "github.com/armory-io/bilgepump/pkg/mark/aws"
	k8smarker "github.com/armory-io/bilgepump/pkg/mark/k8s"
	"github.com/armory-io/bilgepump/pkg/notify"
	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"syscall"
)

const DEFAULT_FILEPATH = "./config.yml"

var (
	ConfigLocation string
	LogLevel       string
	log            *logrus.Logger
)

var rootCmd = &cobra.Command{
	Use:   "bilgepump",
	Short: "Manage cloud costs by automatically removing unneeded cloud resources",
	Long: `A mark/notify/sweep tool that automatically gathers, notifies and deletes
 			unused cloud resources to control your cloud spend.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, log := loadConfig()

		ctx, cancel := context.WithCancel(context.Background())
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

		go func() {
			<-stop
			cancel()
			log.Warn("Received signal, stopping processes...")
			os.Exit(1)
		}()

		// start a "cache" client
		redisCache, err := cache.NewRedisCache(cfg, log)
		if err != nil {
			log.Fatal(err)
		}

		markers := []mark.Marker{}

		// Each marker interface is added individually because in theory, each account is unique with its own
		// api limits
		if cfg.Aws != nil {
			for _, a := range cfg.Aws {
				aws := a
				m := awsmarker.NewAwsMarker(ctx, &aws, log, redisCache)
				markers = append(markers, m)
			}
		}

		if cfg.Kubernetes != nil {
			for _, k := range cfg.Kubernetes {
				k8s := k
				m, err := k8smarker.NewK8SMarker(ctx, &k8s, log, redisCache)
				if err != nil {
					log.Error(err)
					continue
				}
				markers = append(markers, m)
			}
		}

		if len(markers) == 0 {
			log.Fatal("There are no markers configured")
		}

		var sla *notify.SlackNotifier
		// check to make sure slack works
		if cfg.Slack.Token != "" {
			sla = notify.NewSlackNotifier(ctx, cfg, log, redisCache)
			if !sla.IsValid() {
				log.Fatal("Slack isn't configured with proper default account")
			}
		}
		c := cron.New()
		for _, m := range markers {
			log.Infof("Adding %s marker %s with mark schedule %s, sweep schedule %s, notify schedule %v", m.GetType(),
				m.GetName(), m.GetMarkSchedule(), m.GetSweepSchedule(), m.GetNotifySchedule())
			err = c.AddFunc(m.GetMarkSchedule(), m.Mark) // we don't bother checking for schedule parse because we did it in cfg
			if err != nil {
				log.Fatal(err)
			}

			err = c.AddFunc(m.GetSweepSchedule(), m.Sweep)
			if err != nil {
				log.Fatal(err)
			}

			if sla != nil {
				err = c.AddFunc(m.GetNotifySchedule(), sla.Collect)
				if err != nil {
					log.Fatal(err)
				}

			}
		}
		c.Run()
	},
}

func loadConfig() (*config.Config, *logrus.Logger) {
	log = logrus.New()
	var level logrus.Level
	level, err := logrus.ParseLevel(LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	log.Infof("Loading config from %s", ConfigLocation)
	cfg, err := config.LoadConfig(ConfigLocation)
	if err != nil {
		log.Fatalf(err.Error())
	}

	log.Debugf("Config settings: %+v", cfg)
	return cfg, log
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&LogLevel, "loglevel", "l", "info", "log level")
	rootCmd.PersistentFlags().StringVarP(&ConfigLocation, "config", "c", DEFAULT_FILEPATH, "config location")
}
