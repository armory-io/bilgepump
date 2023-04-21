package aws

import (
	"context"
	"github.com/armory-io/bilgepump/pkg/cache"
	"github.com/armory-io/bilgepump/pkg/config"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var log = logrus.New()

func TestFilters(t *testing.T) {
	testCases := map[string]struct {
		filter  Filter
		matched bool
		tags    []*ec2.Tag
	}{
		"autoscale_filter": {
			filter:  Ec2IgnoreAutoScaleInstanceFilter,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("aws:autoscaling:groupName"),
					Value: aws.String("foo"),
				},
			},
		},
		"no_tag_filter": {
			filter:  NoTagFilter,
			matched: true,
			tags:    nil,
		},
		"no_tag_filter_pass": {
			filter:  NoTagFilter,
			matched: false,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("owner"),
					Value: aws.String("Some Jerk"),
				},
			},
		},
		"no_ttl_tag_filter": {
			filter:  NoTTLTagFilter,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("owner"),
					Value: aws.String("Some Jerk"),
				},
			},
		},
		"no_ttl_tag_filter_pass": {
			filter:  NoTTLTagFilter,
			matched: false,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("ttl"),
					Value: aws.String("0"),
				},
			},
		},
		"ttl_tag_expired": {
			filter:  TTLTagExpiredFilter,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("ttl"),
					Value: aws.String("-1w"),
				},
			},
		},
		"ttl_tag_not_expired": {
			filter:  TTLTagExpiredFilter,
			matched: false,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("ttl"),
					Value: aws.String("1w"),
				},
			},
		},
		"ttl_tag_expired_unlimited": {
			filter:  TTLTagExpiredFilter,
			matched: false,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("ttl"),
					Value: aws.String("0"),
				},
			},
		},
		"ignore_k8s_thing": {
			filter:  IgnoreK8sTagFilter,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/some-cluster/some-jerk"),
					Value: aws.String("owned"),
				},
			},
		},
	}

	for desc, tc := range testCases {
		t.Run(desc, func(t *testing.T) {
			filterResult := tc.filter(aws.String("test_instance"), tc.tags, aws.Time(time.Now()), logrus.NewEntry(log))
			assert.Equal(t, tc.matched, filterResult)
		})
	}
}

func TestTypeFilters(t *testing.T) {
	testEc2Instance := &ec2.Instance{
		InstanceId: aws.String("test_instance"),
		State: &ec2.InstanceState{
			Name: aws.String(ec2.InstanceStateNameTerminated),
		},
	}
	t.Run("ec2_ignore_terminated_instance", func(t *testing.T) {
		filterResult := Ec2IgnoreTerminatedFilter(testEc2Instance, logrus.NewEntry(log))
		assert.Equal(t, true, filterResult)
	})

	testEbsVolume := &ec2.Volume{
		VolumeId: aws.String("test_volume"),
		Attachments: []*ec2.VolumeAttachment{
			{
				InstanceId: aws.String("foo"),
			},
		},
		State: aws.String("in-use"),
	}
	t.Run("ebs_ignore_attached_volume", func(t *testing.T) {
		filterResult := EbsIgnoreAttachedFilter(testEbsVolume, logrus.NewEntry(log))
		assert.Equal(t, true, filterResult)
	})

	testSecurityGroupIngress := &ec2.SecurityGroup{
		GroupId: aws.String("test_sg_ingress"),
		IpPermissions: []*ec2.IpPermission{
			{
				UserIdGroupPairs: []*ec2.UserIdGroupPair{
					{
						UserId: aws.String("foo"),
					},
				},
			},
		},
		IpPermissionsEgress: []*ec2.IpPermission{},
	}
	testSecurityGroupEgress := &ec2.SecurityGroup{
		GroupId: aws.String("test_sg_ingress"),
		IpPermissionsEgress: []*ec2.IpPermission{
			{
				UserIdGroupPairs: []*ec2.UserIdGroupPair{
					{
						UserId: aws.String("foo"),
					},
				},
			},
		},
		IpPermissions: []*ec2.IpPermission{},
	}
	testSecurityGroupOrphan := &ec2.SecurityGroup{}

	t.Run("test_sg_ignore_ingress", func(t *testing.T) {
		filterResult := SGIgnoreChild(testSecurityGroupIngress, logrus.NewEntry(log))
		assert.Equal(t, true, filterResult)
	})
	t.Run("test_sg_ignore_egress", func(t *testing.T) {
		filterResult := SGIgnoreChild(testSecurityGroupEgress, logrus.NewEntry(log))
		assert.Equal(t, true, filterResult)
	})
	t.Run("test_sg_ignore_match", func(t *testing.T) {
		filterResult := SGIgnoreChild(testSecurityGroupOrphan, logrus.NewEntry(log))
		assert.Equal(t, false, filterResult)
	})

	testAsg := &autoscaling.Group{
		AutoScalingGroupName: aws.String("test-asg"),
		DesiredCapacity:      aws.Int64(0),
	}
	t.Run("test_asg_zero_capacity", func(t *testing.T) {
		filterResult := AsgZeroCapacity(testAsg, logrus.NewEntry(log))
		assert.Equal(t, false, filterResult)
	})
	t.Run("test_asg_has_capacity", func(t *testing.T) {
		testAsg.DesiredCapacity = aws.Int64(10)
		filterResult := AsgZeroCapacity(testAsg, logrus.NewEntry(log))
		assert.Equal(t, true, filterResult)
	})

}

func TestAwsMarkerIgnoreFilters(t *testing.T) {

	notMarker := NewAwsMarker(context.Background(), &config.Aws{
		Not: []config.AwsTagKV{
			{
				Key:   "foo",
				Value: "bar",
			},
		},
	}, log, cache.NewMockCache())

	regexMarker := NewAwsMarker(context.Background(), &config.Aws{
		Not: []config.AwsTagKV{
			{
				KeyRegex:   "^foo.*",
				ValueRegex: "^bar.*",
			},
		},
	}, log, cache.NewMockCache())

	testCases := map[string]struct {
		marker  *AwsMarker
		matched bool
		tags    []*ec2.Tag
		filter  Filter
	}{
		"ignore_config_not": {
			marker:  notMarker,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				},
			},
			filter: notMarker.IgnoreConfigFilter,
		},
		"no_match_config_not": {
			marker:  notMarker,
			matched: false,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("baz"),
					Value: aws.String("bar"),
				},
			},
			filter: notMarker.IgnoreConfigFilter,
		},
		"ignore_regex_key": {
			marker:  regexMarker,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("foofoo"),
					Value: aws.String("baz"),
				},
			},
			filter: regexMarker.IgnoreConfigFilter,
		},
		"ignore_regex_value": {
			marker:  regexMarker,
			matched: true,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("blah"),
					Value: aws.String("barbar"),
				},
			},
			filter: regexMarker.IgnoreConfigFilter,
		},
		"no_regex_match": {
			marker:  regexMarker,
			matched: false,
			tags: []*ec2.Tag{
				{
					Key:   aws.String("blah"),
					Value: aws.String("test??"),
				},
			},
			filter: regexMarker.IgnoreConfigFilter,
		},
	}

	for desc, tc := range testCases {
		t.Run(desc, func(t *testing.T) {
			filterResult := tc.filter(aws.String("test_instance"), tc.tags, aws.Time(time.Now()), logrus.NewEntry(log))
			assert.Equal(t, tc.matched, filterResult)
		})
	}
}

func TestAwsMarkerIngoreTyped(t *testing.T) {
	m := NewAwsMarker(context.Background(), &config.Aws{}, log, cache.NewMockCache())
	m.sgs = []map[string]bool{
		{
			"foo": true,
		},
	}
	sg := &ec2.SecurityGroup{
		GroupId: aws.String("foo"),
	}
	sgUnused := &ec2.SecurityGroup{
		GroupId: aws.String("bar"),
	}
	t.Run("test_ignore_used_sg", func(t *testing.T) {
		filterResult := m.SGIgnoreInUse(sg, logrus.NewEntry(log))
		assert.Equal(t, true, filterResult)
	})
	t.Run("test_match_unused_sg", func(t *testing.T) {
		filterResult := m.SGIgnoreInUse(sgUnused, logrus.NewEntry(log))
		assert.Equal(t, false, filterResult)
	})
}

type mockFilter struct{}

func (mf *mockFilter) Ignore() bool                  { return true }
func (mf *mockFilter) Compliant() bool               { return true }
func (mf *mockFilter) GetTypeString() string         { return "mock" }
func (mf *mockFilter) GetTypeInterface() interface{} { return nil }
func newMockFilterable() *mockFilter                 { return &mockFilter{} }

func TestFilterAwsObject(t *testing.T) {
	m := NewAwsMarker(context.Background(), &config.Aws{}, log, cache.NewMockCache())
	f := newMockFilterable()
	assert.NotPanics(t, func() { m.FilterAwsObject(f) })
}
