package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/prometheus/common/model"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

type AwsMarker struct {
	Config *config.Aws
	Logger *logrus.Entry
	Cache  cache.Cache
	Ctx    context.Context
	creds  *credentials.Credentials // this isn't exported on purpose
	sess   *session.Session         // this isn't exported on purpose
	mux    *sync.Mutex
	sgs    []map[string]bool
}

type AwsCandidateFuncMap map[string]func() error

func NewAwsMarker(ctx context.Context, cfg *config.Aws, logger *logrus.Logger, cache cache.Cache) *AwsMarker {
	sess := session.Must(session.NewSession())
	creds := stscreds.NewCredentials(sess, cfg.IamRole)

	return &AwsMarker{
		Config: cfg,
		Logger: logger.WithFields(logrus.Fields{"class": mark.AWS, "account": cfg.Name, "region": cfg.Region}),
		Cache:  cache,
		Ctx:    ctx,
		creds:  creds,
		sess:   sess,
		mux:    &sync.Mutex{},
	}
}

func (am *AwsMarker) GetMarkSchedule() string {
	return am.Config.MarkSchedule
}

func (am *AwsMarker) GetSweepSchedule() string {
	return am.Config.SweepSchedule
}

func (am *AwsMarker) GetNotifySchedule() string {
	return am.Config.NotifySchedule
}

func (am *AwsMarker) GetName() string {
	return am.Config.Name
}

func (am *AwsMarker) GetType() mark.MarkerType {
	return mark.AWS
}

func (am *AwsMarker) getEc2Session() *ec2.EC2 {
	return ec2.New(am.sess, &aws.Config{Credentials: am.creds})
}

func (am *AwsMarker) getElbSession() *elb.ELB {
	return elb.New(am.sess, &aws.Config{Credentials: am.creds})
}

func (am *AwsMarker) getElbV2Session() *elbv2.ELBV2 {
	return elbv2.New(am.sess, &aws.Config{Credentials: am.creds})
}

func (am *AwsMarker) getEksSession() *eks.EKS {
	return eks.New(am.sess, &aws.Config{Credentials: am.creds})
}

func (am *AwsMarker) getECSession() *elasticache.ElastiCache {
	return elasticache.New(am.sess, &aws.Config{Credentials: am.creds})
}

func (am *AwsMarker) getASGSession() *autoscaling.AutoScaling {
	return autoscaling.New(am.sess, &aws.Config{Credentials: am.creds})
}

//
//func (am *AwsMarker) getOrgSession() *organizations.Organizations {
//	return organizations.New(am.sess, &aws.Config{Credentials: am.creds})
//}

func (am *AwsMarker) getStsSession() *sts.STS {
	return sts.New(am.sess, &aws.Config{Credentials: am.creds})
}

//func (am *AwsMarker) getMasterAccountId() *string {
//	svc := am.getOrgSession()
//
//	result, err := svc.DescribeOrganization(nil)
//	if serr, ok := err.(awserr.Error); ok {
//		if serr.Code() == "Throttling" {
//			am.Logger.Warn(err)
//		} else {
//			am.Logger.Error(err)
//			return nil
//		}
//	}
//
//	return result.Organization.MasterAccountId
//}

func (am *AwsMarker) getAccountId() *string {
	svc := am.getStsSession()

	result, err := svc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}
	return result.Account
}

func (am *AwsMarker) Mark() {
	am.Logger.Debugf("starting %s mark run for %s", mark.AWS, am.Config.Name)

	fm := AwsCandidateFuncMap{
		"ec2": am.markEc2,
		"eks": am.markEks,
		"ebs": am.markEbs,
		"sg":  am.markSG,
		"elb": am.markElb,
		"alb": am.markAlb,
		"ec":  am.markElasticache,
		"asg": am.markAsg,
		"lc":  am.markLaunchConfig,
	}

	am.mux.Lock()
	defer am.mux.Unlock()
	for _, c := range am.Config.Candidates {
		am.Logger = am.Logger.WithFields(logrus.Fields{"type": c, "phase": "mark"})
		err := fm[c]()
		if err != nil {
			am.Logger.Error(err)
		}
	}
}

func (am *AwsMarker) Sweep() {
	am.Logger.Debugf("starting %s sweep run for %s", mark.AWS, am.Config.Name)
	fm := AwsCandidateFuncMap{
		"ec2": am.sweepEc2,
		"eks": am.sweepEks,
		"ebs": am.sweepEbs,
		"sg":  am.sweepSG,
		"elb": am.sweepElb,
		"alb": am.sweepAlb,
		"ec":  am.sweepElasticache,
		"asg": am.sweepAsg,
		"lc":  am.sweepLaunchConfig,
	}

	am.mux.Lock()
	defer am.mux.Unlock()
	for _, c := range am.Config.Candidates {
		am.Logger = am.Logger.WithFields(logrus.Fields{"type": c, "phase": "sweep"})
		err := fm[c]()
		if err != nil {
			am.Logger.Error(err)
		}
	}
}

func checkRequiredTags(required string, tags []*ec2.Tag) (int, bool) {
	for ti, k := range tags {
		if *k.Key == required {
			return ti, true
		}
	}
	return 0, false
}

func tagOrNil(tag string, tags []*ec2.Tag) string {
	var tagResult string
	tagIndex, tagExists := checkRequiredTags(tag, tags)
	if tagExists {
		tagResult = *tags[tagIndex].Value
	}
	return tagResult
}

func (am *AwsMarker) filterableUpdate(awsObject interface{}, canType string) error {
	id, tags, _, _ := am.ExtractTags(awsObject)
	owner := tagOrNil("owner", tags)
	err := mark.RemoveCandidates(owner, am.Cache, []*string{id})
	if err != nil {
		if _, ok := err.(*mark.NoCandidatesError); !ok {
			am.Logger.Error(err)
		}
	}
	return nil
}

func (am *AwsMarker) ttlRejected(awsObject interface{}, canType string) error {
	id, tags, _, _ := am.ExtractTags(awsObject)
	gp, _ := model.ParseDuration(am.Config.GracePeriod) // already checked this in config
	owner := tagOrNil("owner", tags)
	extraTags := map[string]string{}
	if len(tags) != 0 {
		for _, t := range tags {
			extraTags[*t.Key] = *t.Value
		}
	}
	extraTags["region"] = am.Config.Region
	marked := &mark.MarkedCandidate{
		MarkerType:    mark.AWS,
		CandidateType: canType,
		Id:            *id,
		Owner:         owner,
		Purpose:       tagOrNil("purpose", tags),
		Ttl:           tagOrNil("ttl", tags),
		Account:       am.Config.Name,
		Tags:          extraTags,
	}
	mjson, err := json.Marshal(marked)
	if err != nil {
		return err
	}
	if am.Cache.CandidateExists(owner, string(mjson)) {
		am.Logger.Debugf("Instance: %s already exists in cache, skip", *id)
		return nil
	}
	// owner index update
	err = am.Cache.Write("bilge:owners", owner)
	if err != nil {
		return err
	}
	// write candidate by owner
	err = am.Cache.Write(fmt.Sprintf("bilge:candidates:%s", owner), string(mjson))
	if err != nil {
		return err
	}
	// write an expiring key with our grace period
	err = am.Cache.WriteTimer(fmt.Sprintf("bilge:timers:%s", *id),
		am.Config.GracePeriod, time.Now().Local().Add(time.Duration(gp)))
	if err != nil {
		return err
	}
	return nil
}

func (am *AwsMarker) toDelete(owner, thing string) []*string {
	toDelete := []*string{}
	mcs, err := mark.BuildCandidates(owner, am.Cache)
	if err != nil {
		return nil
	}
	for _, m := range mcs {
		if !am.Cache.TimerExists(fmt.Sprintf("bilge:timers:%s", m.Id)) {
			if m.CandidateType == thing && m.Account == am.Config.Name {
				am.Logger.Info("Will delete ", m.Id)
				toDelete = append(toDelete, aws.String(m.Id))
			}
		}
	}
	return toDelete
}
