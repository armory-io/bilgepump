package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

func (am *AwsMarker) markLaunchConfig() error {
	svc := am.getASGSession()

	err := svc.DescribeLaunchConfigurationsPages(nil, am.processLaunchConfigsCallback)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}

	return nil
}

func (am *AwsMarker) processLaunchConfigsCallback(page *autoscaling.DescribeLaunchConfigurationsOutput, lastPage bool) bool {

	if len(page.LaunchConfigurations) != 0 {
		for _, lc := range page.LaunchConfigurations {
			am.FilterAwsObject(am.newAwsFilterable(lc).
				WithIgnoreFilter(am.IgnoreConfigFilter).
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

func (am *AwsMarker) sweepLaunchConfig() error {
	svc := am.getASGSession()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "lc")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if am.Config.DeleteEnabled {
			if len(toDelete) != 0 {
				for _, lc := range toDelete {
					input := &autoscaling.DeleteLaunchConfigurationInput{
						LaunchConfigurationName: lc,
					}
					_, err := svc.DeleteLaunchConfiguration(input)
					if awsErr, ok := err.(awserr.Error); ok {
						if awsErr.Code() == "Throttling" {
							am.Logger.Warn(err)
							continue
						}
						am.Logger.Error(awsErr.Message())
						continue
					}
					err = mark.RemoveCandidates(o, am.Cache, []*string{lc})
					if err != nil {
						am.Logger.Error(err)
					}
				}

			}
		}

	}
	return nil
}
