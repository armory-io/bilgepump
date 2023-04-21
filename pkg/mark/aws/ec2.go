package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func (am *AwsMarker) peekEc2Instances(fn func(*ec2.DescribeInstancesOutput, bool) bool) error {
	svc := am.getEc2Session()

	err := svc.DescribeInstancesPagesWithContext(am.Ctx, nil, fn)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}

	return nil
}

func (am *AwsMarker) processEc2PagesCallback(page *ec2.DescribeInstancesOutput, lastPage bool) bool {

	if len(page.Reservations) != 0 {
		for _, r := range page.Reservations {
			if len(r.Instances) != 0 {
				for _, i := range r.Instances {
					am.FilterAwsObject(am.newAwsFilterable(i).
						WithIgnoreFilter(am.IgnoreConfigFilter).
						WithIgnoreFilter(Ec2IgnoreAutoScaleInstanceFilter).
						WithTypedIgnoreFilter(Ec2IgnoreTerminatedFilter).
						WithComplianceFilter(NoTagFilter).
						WithComplianceFilter(NoTTLTagFilter).
						WithComplianceFilter(TTLTagExpiredFilter))
				}
			}
		}
	}
	if page.NextToken != nil {
		return true
	}
	return false
}

func (am *AwsMarker) markEc2() error {
	err := am.peekEc2Instances(am.processEc2PagesCallback)
	if err != nil {
		return err
	}
	return nil
}

func (am *AwsMarker) sweepEc2() error {
	// look through all instances in cache and if the grace period key does not exist, delete

	svc := am.getEc2Session()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "ec2")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, i := range toDelete {
				instances := &ec2.TerminateInstancesInput{
					InstanceIds: []*string{i},
					DryRun:      aws.Bool(!am.Config.DeleteEnabled),
				}
				_, err := svc.TerminateInstances(instances)
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == "DryRunOperation" {
						am.Logger.Warnf("Would have deleted %s but we're in DryRun", *i)
						continue
					}
					if awsErr.Code() == "Throttling" {
						am.Logger.Warn(err)
						continue
					}
					am.Logger.Error(awsErr)
				}
				err = mark.RemoveCandidates(o, am.Cache, []*string{i})
				if err != nil {
					am.Logger.Error(err)
				}
			}
		}
	}

	return nil
}
