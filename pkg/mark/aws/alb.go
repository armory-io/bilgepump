package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/elbv2"
)

func (am *AwsMarker) markAlb() error {
	svc := am.getElbV2Session()

	err := svc.DescribeLoadBalancersPages(nil, am.processAlbMarkPages)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}
	return nil
}

func (am *AwsMarker) processAlbMarkPages(page *elbv2.DescribeLoadBalancersOutput, lastPage bool) bool {

	if len(page.LoadBalancers) != 0 {
		for _, lb := range page.LoadBalancers {
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

func (am *AwsMarker) sweepAlb() error {
	svc := am.getElbV2Session()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "alb")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, lb := range toDelete {
				if am.Config.DeleteEnabled {
					input := &elbv2.DeleteLoadBalancerInput{
						LoadBalancerArn: lb,
					}
					_, err := svc.DeleteLoadBalancer(input)
					if serr, ok := err.(awserr.Error); ok {
						if serr.Code() == "Throttling" {
							am.Logger.Warn(err)
							continue
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
