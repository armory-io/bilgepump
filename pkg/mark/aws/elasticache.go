package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/elasticache"
)

func (am *AwsMarker) markElasticache() error {
	svc := am.getECSession()

	err := svc.DescribeCacheClustersPages(nil, am.processECMarkPages)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}
	return nil
}

func (am *AwsMarker) processECMarkPages(page *elasticache.DescribeCacheClustersOutput, lastPage bool) bool {

	if len(page.CacheClusters) != 0 {
		for _, cc := range page.CacheClusters {
			am.FilterAwsObject(am.newAwsFilterable(cc).
				WithIgnoreFilter(am.IgnoreConfigFilter).
				WithComplianceFilter(NoTagFilter).
				WithComplianceFilter(NoTTLTagFilter).
				WithComplianceFilter(TTLTagExpiredFilter))
		}
	}
	if page.Marker != nil {
		return true
	}
	return false
}

func (am *AwsMarker) sweepElasticache() error {
	svc := am.getECSession()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "ec")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, cc := range toDelete {
				if am.Config.DeleteEnabled {
					input := &elasticache.DeleteCacheClusterInput{
						CacheClusterId: cc,
					}
					_, err := svc.DeleteCacheCluster(input)
					if awsErr, ok := err.(awserr.Error); ok {
						if awsErr.Code() == "Throttling" {
							am.Logger.Warn(err)
						} else {
							am.Logger.Error(err)
							continue
						}
					}
					err = mark.RemoveCandidates(o, am.Cache, []*string{cc})
					if err != nil {
						am.Logger.Error(err)
					}
				}
			}
		}
	}

	return nil
}
