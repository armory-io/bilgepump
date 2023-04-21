package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func (am *AwsMarker) markEbs() error {
	svc := am.getEc2Session()

	err := svc.DescribeVolumesPages(nil, am.processEbsMarkPages)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}
	return nil
}

func (am *AwsMarker) processEbsMarkPages(page *ec2.DescribeVolumesOutput, lastPage bool) bool {

	if len(page.Volumes) != 0 {
		for _, v := range page.Volumes {
			am.FilterAwsObject(am.newAwsFilterable(v).
				WithIgnoreFilter(am.IgnoreConfigFilter).
				WithTypedIgnoreFilter(EbsIgnoreAttachedFilter).
				WithComplianceFilter(NoTagFilter).
				WithComplianceFilter(NoTTLTagFilter).
				WithComplianceFilter(TTLTagExpiredFilter))
		}
	}

	if page.NextToken != nil {
		return true
	}
	return false
}

func (am *AwsMarker) sweepEbs() error {
	svc := am.getEc2Session()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "ebs")
		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, v := range toDelete {
				vol := &ec2.DeleteVolumeInput{
					VolumeId: v,
					DryRun:   aws.Bool(!am.Config.DeleteEnabled),
				}
				_, err := svc.DeleteVolume(vol)
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == "DryRunOperation" {
						am.Logger.Warnf("Would have deleted %s but we're in DryRun", *v)
						continue
					}
					if awsErr.Code() == "Throttling" {
						am.Logger.Warn(err)
						continue
					}
					am.Logger.Error(awsErr)
				}
				err = mark.RemoveCandidates(o, am.Cache, []*string{v})
				if err != nil {
					am.Logger.Error(err)
				}
			}
		}
	}
	return nil
}
