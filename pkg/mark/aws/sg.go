package aws

import (
	"github.com/armory-io/bilgepump/pkg/mark"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func (am *AwsMarker) markSG() error {
	svc := am.getEc2Session()

	// reset the in-use security groups
	am.sgs = nil
	am.sgs = []map[string]bool{
		am.getEc2InstanceSgList(),
		am.getElbV2SgList(),
		am.getElbSgList(),
	}

	err := svc.DescribeSecurityGroupsPages(nil, am.processSGMarkPages)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			return err
		}
	}
	return nil
}

func (am *AwsMarker) getEc2InstanceSgList() map[string]bool {
	svc := am.getEc2Session()

	result, err := svc.DescribeInstances(nil)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}

	instanceSgs := make(map[string]bool)
	if len(result.Reservations) != 0 {
		for _, r := range result.Reservations {
			if len(r.Instances) != 0 {
				for _, i := range r.Instances {
					for _, sg := range i.SecurityGroups {
						instanceSgs[*sg.GroupId] = true
					}
				}
			}
		}
	}
	return instanceSgs
}

func (am *AwsMarker) getElbV2SgList() map[string]bool {
	// covers ELB and ALB new-gen
	svc := am.getElbV2Session()

	result, err := svc.DescribeLoadBalancers(nil)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}

	elbSgs := make(map[string]bool)
	if len(result.LoadBalancers) != 0 {
		for _, l := range result.LoadBalancers {
			if len(l.SecurityGroups) != 0 {
				for _, sg := range l.SecurityGroups {
					elbSgs[*sg] = true
				}
			}
		}
	}
	return elbSgs
}

func (am *AwsMarker) getElbSgList() map[string]bool {
	// covers classic ELB
	svc := am.getElbSession()

	result, err := svc.DescribeLoadBalancers(nil)
	if serr, ok := err.(awserr.Error); ok {
		if serr.Code() == "Throttling" {
			am.Logger.Warn(err)
		} else {
			am.Logger.Error(err)
			return nil
		}
	}

	elbSgs := make(map[string]bool)
	if len(result.LoadBalancerDescriptions) != 0 {
		for _, lb := range result.LoadBalancerDescriptions {
			if len(lb.SecurityGroups) != 0 {
				for _, sg := range lb.SecurityGroups {
					elbSgs[*sg] = true
				}
			}
		}
	}
	return elbSgs
}

func (am *AwsMarker) processSGMarkPages(page *ec2.DescribeSecurityGroupsOutput, lastPage bool) bool {
	/*
	 *  Security groups have no notion of time, they are always associated with some
	 *  other object.  This marker mostly functions to find and remove sg orphans
	 */

	if len(page.SecurityGroups) != 0 {
		for _, sg := range page.SecurityGroups {
			am.FilterAwsObject(am.newAwsFilterable(sg).
				WithIgnoreFilter(am.IgnoreConfigFilter).
				WithTypedIgnoreFilter(SGIgnoreChild).
				WithTypedIgnoreFilter(am.SGIgnoreInUse).
				WithComplianceFilter(NoTagFilter))
		}
	}

	if page.NextToken != nil {
		return true
	}
	return false
}

func (am *AwsMarker) sweepSG() error {
	svc := am.getEc2Session()

	owners, err := am.Cache.ReadOwners()
	if err != nil {
		return err
	}

	for _, o := range owners {
		toDelete := am.toDelete(o, "sg")

		am.Logger.Debug("DryRun? ", !am.Config.DeleteEnabled)
		if len(toDelete) != 0 {
			for _, sg := range toDelete {
				delSgs := &ec2.DeleteSecurityGroupInput{
					GroupId: sg,
					DryRun:  aws.Bool(!am.Config.DeleteEnabled),
				}
				_, err := svc.DeleteSecurityGroup(delSgs)
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == "DryRunOperation" {
						am.Logger.Warnf("Would have deleted %d instances but we're in DryRun", len(toDelete))
						continue
					}
					if awsErr.Code() == "Throttling" {
						am.Logger.Warn(err)
						continue
					}
					am.Logger.Error(awsErr)
				}
				err = mark.RemoveCandidates(o, am.Cache, []*string{sg})
				if err != nil {
					am.Logger.Error(err)
				}
			}
		}
	}
	return nil
}
