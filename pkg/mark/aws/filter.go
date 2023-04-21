package aws

import (
	"fmt"
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/sirupsen/logrus"
	"regexp"
	"strings"
	"time"
)

type genericAwsFilter interface {
	Ignore() bool
	Compliant() bool
	GetTypeString() string
	GetTypeInterface() interface{}
}

func (am *AwsMarker) FilterAwsObject(filterable genericAwsFilter) {
	if filterable.Ignore() {
		err := am.filterableUpdate(filterable.GetTypeInterface(), filterable.GetTypeString())
		if err != nil {
			am.Logger.Error(err)
		}
		return
	}
	if !filterable.Compliant() {
		err := am.ttlRejected(filterable.GetTypeInterface(), filterable.GetTypeString())
		if err != nil {
			am.Logger.Error(err)
		}
	}
}

type awsFilterable struct {
	ignoreFilters          []Filter
	complianceFilters      []Filter
	typedIgnoreFilters     []TypedFilter
	typedComplianceFilters []TypedFilter
	id                     *string
	tags                   []*ec2.Tag
	created                *time.Time
	log                    *logrus.Entry
	awsObjectType          string
	object                 interface{}
}

func (am *AwsMarker) newAwsFilterable(i interface{}) *awsFilterable {
	id, tags, created, t := am.ExtractTags(i)
	return &awsFilterable{
		id:            id,
		tags:          tags,
		created:       created,
		log:           am.Logger,
		awsObjectType: t,
		object:        i,
	}
}

func (e *awsFilterable) Ignore() bool {
	for _, f := range e.ignoreFilters {
		if f(e.id, e.tags, e.created, e.log) {
			return true
		}
	}
	for _, f := range e.typedIgnoreFilters {
		if f(e.object, e.log) {
			return true
		}
	}
	return false
}

func (e *awsFilterable) Compliant() bool {
	for _, f := range e.complianceFilters {
		if f(e.id, e.tags, e.created, e.log) {
			return false
		}
	}
	for _, f := range e.typedComplianceFilters {
		if f(e.object, e.log) {
			return false
		}
	}
	return true
}

func (e *awsFilterable) GetTypeString() string {
	return e.awsObjectType
}

func (e *awsFilterable) GetTypeInterface() interface{} {
	return e.object
}

func (e *awsFilterable) WithIgnoreFilter(f Filter) *awsFilterable {
	e.ignoreFilters = append(e.ignoreFilters, f)
	return e
}

func (e *awsFilterable) WithComplianceFilter(f Filter) *awsFilterable {
	e.complianceFilters = append(e.complianceFilters, f)
	return e
}

func (e *awsFilterable) WithTypedIgnoreFilter(f TypedFilter) *awsFilterable {
	e.typedIgnoreFilters = append(e.typedIgnoreFilters, f)
	return e
}

func (e *awsFilterable) WithTypedComplianceFilter(f TypedFilter) *awsFilterable {
	e.typedComplianceFilters = append(e.typedComplianceFilters, f)
	return e
}

/* Normalize ELB tags into EC2 Tags */
func (am *AwsMarker) extractElbTags(e *elb.LoadBalancerDescription) []*ec2.Tag {
	svc := am.getElbSession()

	input := &elb.DescribeTagsInput{
		LoadBalancerNames: []*string{
			e.LoadBalancerName,
		},
	}
	elbtags, err := svc.DescribeTags(input)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}
	// normalize elb tags into ec2 tags.  they're the same format.
	ec2Tags := []*ec2.Tag{}
	if len(elbtags.TagDescriptions) != 0 {
		for _, td := range elbtags.TagDescriptions {
			if len(td.Tags) != 0 {
				for _, tag := range td.Tags {
					et := &ec2.Tag{
						Key:   tag.Key,
						Value: tag.Value,
					}
					ec2Tags = append(ec2Tags, et)
				}
			}
		}
	}
	return ec2Tags
}

/* Normalize ALB tags into EC2 Tags */
func (am *AwsMarker) extractAlbTags(e *elbv2.LoadBalancer) []*ec2.Tag {
	svc := am.getElbV2Session()

	input := &elbv2.DescribeTagsInput{
		ResourceArns: []*string{
			e.LoadBalancerArn,
		},
	}
	elbtags, err := svc.DescribeTags(input)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}
	// normalize elb tags into ec2 tags.  they're the same format.
	ec2Tags := []*ec2.Tag{}
	if len(elbtags.TagDescriptions) != 0 {
		for _, td := range elbtags.TagDescriptions {
			if len(td.Tags) != 0 {
				for _, tag := range td.Tags {
					et := &ec2.Tag{
						Key:   tag.Key,
						Value: tag.Value,
					}
					ec2Tags = append(ec2Tags, et)
				}
			}
		}
	}
	return ec2Tags
}

/* Normalize Elasticache tags into EC2 tags */
func (am *AwsMarker) extractECTags(ec *elasticache.CacheCluster) []*ec2.Tag {
	svc := am.getECSession()

	acctId := am.getAccountId()
	if acctId == nil {
		am.Logger.Error("unable to determine account id")
		return nil
	}
	clusterArn := fmt.Sprintf("arn:aws:elasticache:%s:%s:cluster:%s", am.Config.Region, *acctId, *ec.CacheClusterId)
	input := &elasticache.ListTagsForResourceInput{
		ResourceName: aws.String(clusterArn),
	}
	result, err := svc.ListTagsForResource(input)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}
	ec2Tags := []*ec2.Tag{}
	if len(result.TagList) != 0 {
		for _, td := range result.TagList {
			et := &ec2.Tag{
				Key:   td.Key,
				Value: td.Value,
			}
			ec2Tags = append(ec2Tags, et)
		}
	}
	return ec2Tags
}

func (am *AwsMarker) extractLcTags(lc *autoscaling.LaunchConfiguration) []*ec2.Tag {
	ec2Tags := []*ec2.Tag{}
	tagMap := map[int]string{
		0: "owner",
		1: "version",
		2: "date",
		3: "ttl",
	}
	// LaunchConfigs don't support tags.  Use a naming convention instead.
	// format: ${owner}-${version}-${date}-${ttl}
	tags := strings.SplitN(*lc.LaunchConfigurationName, "-", 4)
	for i, t := range tags {
		etag := &ec2.Tag{
			Key:   aws.String(tagMap[i]),
			Value: aws.String(t),
		}
		ec2Tags = append(ec2Tags, etag)
	}
	return ec2Tags
}

func (am *AwsMarker) extractAsgTags(asg *autoscaling.Group) []*ec2.Tag {
	ec2Tags := []*ec2.Tag{}
	if len(asg.Tags) != 0 {
		for _, t := range asg.Tags {
			et := &ec2.Tag{
				Key:   t.Key,
				Value: t.Value,
			}
			ec2Tags = append(ec2Tags, et)
		}
	}
	return ec2Tags
}

func (am *AwsMarker) ExtractTags(awsObject interface{}) (*string, []*ec2.Tag, *time.Time, string) {
	var id *string
	var tags []*ec2.Tag
	var created *time.Time
	var objType string
	switch obj := awsObject.(type) {
	case *ec2.Volume:
		id = obj.VolumeId
		tags = obj.Tags
		created = obj.CreateTime
		objType = "ebs"
	case *ec2.Instance:
		id = obj.InstanceId
		tags = obj.Tags
		created = obj.LaunchTime
		objType = "ec2"
	case *ec2.SecurityGroup:
		id = obj.GroupId
		tags = obj.Tags
		objType = "sg"
	case *elb.LoadBalancerDescription:
		id = obj.LoadBalancerName
		created = obj.CreatedTime
		tags = am.extractElbTags(obj)
		objType = "elb"
	case *elbv2.LoadBalancer:
		id = obj.LoadBalancerArn
		created = obj.CreatedTime
		tags = am.extractAlbTags(obj)
		objType = "alb"
	case *elasticache.CacheCluster:
		id = obj.CacheClusterId
		created = obj.CacheClusterCreateTime
		tags = am.extractECTags(obj)
		objType = "ec"
	case *autoscaling.Group:
		id = obj.AutoScalingGroupName
		created = obj.CreatedTime
		tags = am.extractAsgTags(obj)
		objType = "asg"
	case *autoscaling.LaunchConfiguration:
		id = obj.LaunchConfigurationName
		created = obj.CreatedTime
		tags = am.extractLcTags(obj)
		objType = "lc"
	}
	return id, tags, created, objType
}

/* ----------------- START FILTER ----------------- */
type Filter func(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool
type TypedFilter func(interface{}, *logrus.Entry) bool

func Ec2IgnoreAutoScaleInstanceFilter(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool {
	for _, t := range tags {
		if *t.Key == "aws:autoscaling:groupName" {
			log.Debugf("Ignoring %s. Reason: Managed by ASG", *id)
			return true
		}
	}
	return false
}

func (am *AwsMarker) IgnoreConfigFilter(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool {
	for _, t := range tags {
		// ignore if instance matches our not criteria (must match both key and value)
		for _, ignore := range am.Config.Not {
			if ignore.Key == *t.Key && ignore.Value == *t.Value {
				am.Logger.Debugf("Ignoring %s. Reason: matched ignore rule: %s:%s", *id, ignore.Key, ignore.Value)
				return true
			}
			// MustCompile shouldn't panic because we've already checked it in config parse
			if ignore.KeyRegex != "" && regexp.MustCompile(ignore.KeyRegex).MatchString(*t.Key) {
				am.Logger.Debugf("Ignoring %s. Reason: matched ignore key regex: %s", *id, ignore.KeyRegex)
				return true
			}
			if ignore.ValueRegex != "" && regexp.MustCompile(ignore.ValueRegex).MatchString(*t.Value) {
				am.Logger.Debugf("Ignoring %s. Reason: matched ignore value regex: %s", *id, ignore.ValueRegex)
				return true
			}
		}
	}
	return false
}

func NoTagFilter(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool {
	if len(tags) == 0 {
		log.Infof("Adding AWS candidate: %s, Reason: no tags, Created: %+v", *id, created)
		return true
	}
	return false
}

func NoTTLTagFilter(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool {
	_, tagExists := checkRequiredTags(mark.REQUIRED_TAG, tags)
	if !tagExists {
		log.Infof("Adding AWS candidate: %s, Reason: no ttl tag, Created: %+v", *id, created)
		return true
	}
	return false
}

func TTLTagExpiredFilter(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool {
	tagIndex, _ := checkRequiredTags(mark.REQUIRED_TAG, tags)
	timeToLive := *tags[tagIndex].Value
	if timeToLive == "0" {
		log.Debugf("Ignoring %s. Reason: Unlimited TTL", *id)
		return false
	}
	if !mark.WithinTTLTime(timeToLive, *created) {
		log.Infof("Adding AWS candidate: %s, Reason: ttl expired, Created: %+v", *id, created)
		return true
	}
	return false
}

func IgnoreK8sTagFilter(id *string, tags []*ec2.Tag, created *time.Time, log *logrus.Entry) bool {
	for _, t := range tags {
		split := strings.Split(*t.Key, "/")
		if split[0] == "kubernetes.io" {
			log.Debugf("Ignoring %s. Object is a kubernetes resource", *id)
			return true
		}
	}
	return false
}

func Ec2IgnoreTerminatedFilter(i interface{}, log *logrus.Entry) bool {
	if instance, ok := i.(*ec2.Instance); ok {
		if *instance.State.Name == ec2.InstanceStateNameTerminated {
			log.Debugf("%s is terminated, ignoring", *instance.InstanceId)
			return true
		}
	}
	return false
}

func EbsIgnoreAttachedFilter(v interface{}, log *logrus.Entry) bool {
	if volume, ok := v.(*ec2.Volume); ok {
		if len(volume.Attachments) != 0 && *volume.State == "in-use" {
			log.Debugf("Ignoring %s. Reason: attached to instance", *volume.VolumeId)
			return true
		}
	}
	return false
}

func SGIgnoreChild(secGroup interface{}, log *logrus.Entry) bool {
	if sg, ok := secGroup.(*ec2.SecurityGroup); ok {
		for _, ingressRule := range sg.IpPermissions {
			if len(ingressRule.UserIdGroupPairs) != 0 {
				log.Debugf("Ignoring %s. Reason: SG is referenced by another SG", *sg.GroupId)
				return true
			}
		}
		for _, egressRule := range sg.IpPermissionsEgress {
			if len(egressRule.UserIdGroupPairs) != 0 {
				log.Debugf("Ignoring %s. Reason: SG is referenced by another SG", *sg.GroupId)
				return true
			}
		}
	}
	return false
}

func (am *AwsMarker) SGIgnoreInUse(secGroup interface{}, log *logrus.Entry) bool {

	if sg, ok := secGroup.(*ec2.SecurityGroup); ok {
		for _, m := range am.sgs {
			if m[*sg.GroupId] {
				log.Debugf("Ignoring %s. Reason: attached to object", *sg.GroupId)
				return true
			}
		}
	}
	return false
}

func AsgZeroCapacity(a interface{}, log *logrus.Entry) bool {
	if asg, ok := a.(*autoscaling.Group); ok {
		if *asg.DesiredCapacity == 0 {
			log.Infof("Adding ASG: %s has zero capacity", *asg.AutoScalingGroupName)
			return false
		}

	}
	return true
}

/* ----------------- END FILTER ----------------- */
