package config

import (
	"errors"
	"fmt"
	"github.com/prometheus/common/model"
	"github.com/robfig/cron"
	"gopkg.in/validator.v2"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const (
	DEFAULT_REDIS_HOST      = "127.0.0.1"
	DEFAULT_REDIS_PORT      = uint32(6379)
	DEFAULT_SWEEP_SCHEDULE  = "@daily"
	DEFAULT_MARK_SCHEDULE   = "@hourly"
	DEFAULT_NOTIFY_SCHEDULE = "@every 12h"
	DEFAULT_GRACEPERIOD     = "24h"
	DEFAULT_MAX_RETRY       = 10
)

var validAwsCandidates = map[string]bool{
	"elb": true,
	"ec2": true,
	"eks": true,
	"alb": true,
	"ebs": true,
	"sg":  true,
	"ec":  true,
	"asg": true,
	"lc":  true,
}

type Config struct {
	RedisHost  string       `yaml:"redis_host"`
	RedisPort  uint32       `yaml:"redis_port"`
	Aws        []Aws        `yaml:"aws"`
	Kubernetes []Kubernetes `yaml:"kubernetes"`
	Slack      Slack        `yaml:"slack"`
}

type Slack struct {
	Token        string `yaml:"token"`
	DefaultOwner string `yaml:"default_owner"`
	Channel      string `yaml:"channel"`
}

type Aws struct {
	Name           string     `yaml:"name" validate:"nonzero"`
	MaxClientRetry int        `yaml:"max_retries"`
	Candidates     []string   `yaml:"candidates" validate:"isValidAwsCandidate"`
	Region         string     `yaml:"region" validate:"nonzero"`
	MarkSchedule   string     `yaml:"mark_schedule" validate:"isCron"`
	SweepSchedule  string     `yaml:"sweep_schedule" validate:"isCron"`
	NotifySchedule string     `yaml:"notify_schedule" validate:"isCron"`
	Not            []AwsTagKV `yaml:"not_tags"`
	GracePeriod    string     `yaml:"grace_period" validate:"isDuration"`
	DeleteEnabled  bool       `yaml:"delete_enabled"`
	IamRole        string     `yaml:"iamRole" validate:"nonzero"`
}

type Kubernetes struct {
	Name           string   `yaml:"name" validate:"nonzero"`
	KubeConfig     string   `yaml:"kubeconfig" validate:"nonzero"`
	KubeContext    string   `yaml:"kubecontext"`
	MarkSchedule   string   `yaml:"mark_schedule" validate:"isCron"`
	SweepSchedule  string   `yaml:"sweep_schedule" validate:"isCron"`
	NotifySchedule string   `yaml:"notify_schedule" validate:"isCron"`
	DeleteEnabled  bool     `yaml:"delete_enabled"`
	GracePeriod    string   `yaml:"grace_period" validate:"isDuration"`
	Not            []string `yaml:"not_namespaces"`
	NotRegex       []string `yaml:"not_regex" validate:"isRegex"`
}

type AwsTagKV struct {
	Key        string `yaml:"key"`
	Value      string `yaml:"value"`
	KeyRegex   string `yaml:"key_regex" validate:"isRegex"`
	ValueRegex string `yaml:"value_regex" validate:"isRegex"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Errorf("Could not load config: %+v", err)
		return nil, err
	}

	var c Config
	err = yaml.Unmarshal([]byte(data), &c)
	if err != nil {
		log.Errorf("Could not load config: %+v", err)
		return nil, err
	}

	c.setDefaults()

	if err := c.validate(); err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *Config) setDefaults() {
	// set the default debug url if it's not provided
	if c.RedisHost == "" {
		c.RedisHost = DEFAULT_REDIS_HOST
	}

	if c.RedisPort == 0 {
		c.RedisPort = DEFAULT_REDIS_PORT
	}
	if c.Aws != nil || len(c.Aws) != 0 {
		for i, aws := range c.Aws {
			if aws.MaxClientRetry <= 0 {
				c.Aws[i].MaxClientRetry = DEFAULT_MAX_RETRY
			}
			if aws.MarkSchedule == "" {
				c.Aws[i].MarkSchedule = DEFAULT_MARK_SCHEDULE
			}
			if aws.SweepSchedule == "" {
				c.Aws[i].SweepSchedule = DEFAULT_SWEEP_SCHEDULE
			}
			if aws.NotifySchedule == "" {
				c.Aws[i].NotifySchedule = DEFAULT_NOTIFY_SCHEDULE
			}
			if aws.GracePeriod == "" {
				c.Aws[i].GracePeriod = DEFAULT_GRACEPERIOD
			}
		}
	}
	if c.Kubernetes != nil || len(c.Kubernetes) != 0 {
		for i, k8s := range c.Kubernetes {
			if k8s.MarkSchedule == "" {
				c.Kubernetes[i].MarkSchedule = DEFAULT_MARK_SCHEDULE
			}
			if k8s.SweepSchedule == "" {
				c.Kubernetes[i].SweepSchedule = DEFAULT_SWEEP_SCHEDULE
			}
			if k8s.NotifySchedule == "" {
				c.Kubernetes[i].NotifySchedule = DEFAULT_NOTIFY_SCHEDULE
			}
			if k8s.GracePeriod == "" {
				c.Kubernetes[i].GracePeriod = DEFAULT_GRACEPERIOD
			}
			if k8s.KubeConfig == "" {
				home, exists := os.LookupEnv("HOME")
				if !exists {
					log.Fatal("Cannot set default path for kubeconfig")
				}
				c.Kubernetes[i].KubeConfig = home + "/.kube/config"
			}
		}
	}

}

func (c *Config) validate() error {
	awsErrors := []string{}
	if c.Aws != nil {
		for _, a := range c.Aws {
			if a.Candidates == nil || len(a.Candidates) == 0 {
				awsErrors = append(awsErrors, fmt.Sprintf("(%s) must select an aws object to mark", a.Name))
				continue
			}
		}
	}
	if len(awsErrors) != 0 {
		return errors.New(strings.Join(awsErrors, "\n"))
	}
	//nolint - the only error is on nil name
	validator.SetValidationFunc("isuri", isURI)
	//nolint - the only error is on nil name
	validator.SetValidationFunc("isValidAwsCandidate", isAwsCandidate)
	//nolint - the only error is on nil name
	validator.SetValidationFunc("isCron", isCron)
	//nolint - the only error is on nil name
	validator.SetValidationFunc("isDuration", isDuration)
	//nolint - the only error is on nil name
	validator.SetValidationFunc("isRegex", isRegex)
	//nolint - the only error is on nil name
	validator.SetValidationFunc("isPath", isPath)
	if err := validator.Validate(c); err != nil {
		return err
	}

	return nil
}

func isURI(v interface{}, param string) error {
	_, err := url.ParseRequestURI(reflect.ValueOf(v).String())
	if err != nil {
		return errors.New("invalid url")
	}

	return nil
}

func isCron(v interface{}, param string) error {
	s := v.(string)
	_, err := cron.Parse(s)
	if err != nil {
		return err
	}
	return nil
}

func isAwsCandidate(v interface{}, param string) error {
	errs := []string{}
	c := v.([]string)
	for _, i := range c {
		if !validAwsCandidates[i] {
			errs = append(errs, i)
		}
	}
	if len(errs) != 0 {
		return fmt.Errorf(fmt.Sprintf("the following candidates are invalid: %s", strings.Join(errs, ", ")))
	}
	return nil
}

func isDuration(v interface{}, param string) error {
	c := v.(string)
	_, err := model.ParseDuration(c)
	if err != nil {
		return err
	}
	return nil
}

func isRegex(v interface{}, param string) error {
	c, ok := v.(string)
	if !ok {
		c := v.([]string)
		for _, s := range c {
			_, err := regexp.Compile(s)
			if err != nil {
				return err
			}
		}
	}
	// if there isn't anything just ignore
	if c == "" {
		return nil
	}
	_, err := regexp.Compile(c)
	if err != nil {
		return err
	}
	return nil
}

func isPath(v interface{}, param string) error {
	f := v.(string)
	if _, err := os.Stat(f); os.IsNotExist(err) {
		return fmt.Errorf("unable to read file: %s", f)
	}
	return nil
}
