package aws

import (
	"encoding/json"
	"fmt"
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/prometheus/common/model"
	"time"
)

func (am *AwsMarker) markEks() error {
	err := am.peekEc2Instances(am.processEksPeekCallback)
	if err != nil {
		return err
	}
	return nil
}

func (am *AwsMarker) getEksClusters() []*eks.DescribeClusterOutput {
	svc := am.getEksSession()

	var clusters []*string
	cx, err := svc.ListClustersWithContext(am.Ctx, nil)
	if err != nil {
		return nil
	}

	clusters = append(clusters, cx.Clusters...)

	for cx.NextToken != nil {
		in := &eks.ListClustersInput{
			NextToken: cx.NextToken,
		}
		cx, err := svc.ListClustersWithContext(am.Ctx, in)
		if err != nil {
			am.Logger.Error(err)
			continue
		}
		clusters = append(clusters, cx.Clusters...)

	}
	decodedClusters := make([]*eks.DescribeClusterOutput, len(clusters))
	for ix, c := range clusters {
		in := &eks.DescribeClusterInput{
			Name: c,
		}
		cInfo, err := svc.DescribeCluster(in)
		if err != nil {
			am.Logger.Error(err)
			continue
		}
		decodedClusters[ix] = cInfo
	}

	return decodedClusters
}

func (am *AwsMarker) checkEksCluster(i *ec2.Instance, cluster *eks.Cluster) {
	id, tags, created, _ := am.ExtractTags(i)
	tagFilters := []Filter{
		NoTTLTagFilter,
		TTLTagExpiredFilter,
	}
	for _, f := range tagFilters {
		if f(id, tags, created, am.Logger) {
			err := am.eksTtlRejected(i, cluster)
			if err != nil {
				am.Logger.Error(err)
			}
			break
		}
	}
}

func (am *AwsMarker) processEksPeekCallback(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
	clusters := am.getEksClusters()
	if len(clusters) == 0 {
		am.Logger.Warn("No eks clusters to process")
		return false
	}
ClusterList:
	for _, c := range clusters {
		if len(page.Reservations) != 0 {
			for _, r := range page.Reservations {
				if len(r.Instances) != 0 {
				InstanceList:
					for _, i := range r.Instances {
						if Ec2IgnoreTerminatedFilter(i, am.Logger) {
							continue InstanceList
						}
						id, tags, created, _ := am.ExtractTags(i)
						ignoreFilters := []Filter{
							am.IgnoreConfigFilter,
							NoTagFilter,
						}
						for _, f := range ignoreFilters {
							if f(id, tags, created, am.Logger) {
								continue InstanceList
							}
						}
						// we have tag data, see if we match the eks cluster name
						for _, t := range i.Tags {
							if fmt.Sprintf("kubernetes.io/cluster/%s", *c.Cluster.Name) == *t.Key {
								// we matched a cluster, dawg
								am.Logger.Debugf("matched eks cluster: %s", *c.Cluster.Name)
								// process the cluster TTLs
								am.checkEksCluster(i, c.Cluster)
								continue ClusterList
							}
						}
					}
				}
			}
		}
		// if we get here in the ClusterList it isn't to spec, flag it for sweep
		err := am.eksTtlRejected(nil, c.Cluster)
		if err != nil {
			am.Logger.Error(err)
		}
	}
	return page.NextToken != nil
}

func (am *AwsMarker) eksTtlRejected(i *ec2.Instance, cluster *eks.Cluster) error {
	gp, _ := model.ParseDuration(am.Config.GracePeriod) // already checked this in config
	marked := &mark.MarkedCandidate{}
	var owner string
	if i != nil {
		_, tags, _, _ := am.ExtractTags(i)
		owner := tagOrNil("owner", tags)
		extraTags := map[string]string{}
		if len(i.Tags) != 0 {
			for _, t := range i.Tags {
				extraTags[*t.Key] = *t.Value
			}
		}
		marked.MarkerType = mark.AWS
		marked.CandidateType = "eks"
		marked.Id = *cluster.Name
		marked.Owner = owner
		marked.Purpose = tagOrNil("purpose", tags)
		marked.Ttl = tagOrNil("ttl", tags)
	} else {
		marked.MarkerType = mark.AWS
		marked.CandidateType = "eks"
		marked.Id = *cluster.Name
		marked.Owner = ""
	}
	mjson, err := json.Marshal(marked)
	if err != nil {
		return err
	}
	if am.Cache.CandidateExists(owner, string(mjson)) {
		am.Logger.Debugf("EKS Cluster: %s already exists in cache, skip", *cluster.Name)
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
	err = am.Cache.WriteTimer(fmt.Sprintf("bilge:timers:%s", *cluster.Name),
		am.Config.GracePeriod, time.Now().Local().Add(time.Duration(gp)))
	if err != nil {
		return err
	}
	return nil
}

func (am *AwsMarker) sweepEks() error {
	svc := am.getEksSession()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "eks")
		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, c := range toDelete {
				indel := &eks.DeleteClusterInput{
					Name: c,
				}
				if am.Config.DeleteEnabled {
					_, err := svc.DeleteCluster(indel)
					if err != nil {
						am.Logger.Error(err)
						continue
					}
					err = mark.RemoveCandidates(o, am.Cache, []*string{c})
					if err != nil {
						am.Logger.Error(err)
					}
				} else {
					am.Logger.Warnf("would delete %s but we're in DryRun", *c)
					continue
				}
			}
		}

	}
	return nil
}
