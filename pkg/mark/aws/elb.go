package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/elb"
)

func (am *AwsMarker) markElb() error {
	svc := am.getElbSession()

	err := svc.DescribeLoadBalancersPages(nil, am.processElbMarkPages)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}
	return nil
}

func (am *AwsMarker) processElbMarkPages(page *elb.DescribeLoadBalancersOutput, lastPage bool) bool {

	if len(page.LoadBalancerDescriptions) != 0 {
		for _, lb := range page.LoadBalancerDescriptions {
			am.FilterAwsObject(am.newAwsFilterable(lb).
				WithIgnoreFilter(am.IgnoreConfigFilter).
				WithIgnoreFilter(IgnoreK8sTagFilter).
				WithComplianceFilter(NoTagFilter).
				WithComplianceFilter(NoTTLTagFilter).
				WithComplianceFilter(TTLTagExpiredFilter))
		}
	}

	if page.NextMarker != nil {
		return true
	}
	return false
}

func (am *AwsMarker) sweepElb() error {
	svc := am.getElbSession()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "elb")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, lb := range toDelete {
				if am.Config.DeleteEnabled {
					input := &elb.DeleteLoadBalancerInput{
						LoadBalancerName: lb,
					}
					_, err := svc.DeleteLoadBalancer(input)
					if awsErr, ok := err.(awserr.Error); ok {
						if awsErr.Code() == "Throttling" {
							am.Logger.Warn(err)
						} else {
							am.Logger.Error(err)
						}
					}
					err = mark.RemoveCandidates(o, am.Cache, []*string{lb})
					if err != nil {
						am.Logger.Error(err)
					}
				}
			}
		}
	}

	return nil
}
