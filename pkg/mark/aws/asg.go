package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

func (am *AwsMarker) markAsg() error {
	svc := am.getASGSession()

	err := svc.DescribeAutoScalingGroupsPages(nil, am.processsAsgMarkPages)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}
	return nil
}

func (am *AwsMarker) processsAsgMarkPages(page *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {

	if len(page.AutoScalingGroups) != 0 {
		for _, asg := range page.AutoScalingGroups {
			am.FilterAwsObject(am.newAwsFilterable(asg).
				WithIgnoreFilter(am.IgnoreConfigFilter).
				WithIgnoreFilter(IgnoreK8sTagFilter).
				WithComplianceFilter(NoTagFilter).
				WithComplianceFilter(NoTTLTagFilter).
				WithComplianceFilter(TTLTagExpiredFilter).
				WithTypedComplianceFilter(AsgZeroCapacity))
		}
	}

	if page.NextToken != nil {
		return true
	}
	return false
}

func (am *AwsMarker) sweepAsg() error {
	svc := am.getASGSession()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "asg")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, asg := range toDelete {
				if am.Config.DeleteEnabled {
					input := &autoscaling.DeleteAutoScalingGroupInput{
						AutoScalingGroupName: asg,
						ForceDelete:          aws.Bool(true),
					}
					_, err := svc.DeleteAutoScalingGroup(input)
					if serr, ok := err.(awserr.Error); ok {
						if serr.Code() == "Throttling" {
							am.Logger.Warn(err)
							continue
						} else if serr.Code() == "ValidationError" {
							// the asset has gone missing.  remove it.
							am.Logger.Warn(err)
						} else {
							am.Logger.Error(err)
							continue
						}
					}
					err = mark.RemoveCandidates(o, am.Cache, []*string{asg})
					if err != nil {
						am.Logger.Error(err)
					}
				}
			}
		}
	}

	return nil
}
