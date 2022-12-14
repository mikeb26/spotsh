/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func GetDefaultSecurityGroupId(awsCfg aws.Config) (string, error) {
	ec2Client := ec2.NewFromConfig(awsCfg)

	return getDefaultSecurityGroupId(awsCfg, ec2Client)
}

func getDefaultSecurityGroupId(awsCfg aws.Config,
	ec2Client *ec2.Client) (string, error) {

	dryRun := false
	maxResults := int32(1000)
	descVpcsInput := &ec2.DescribeVpcsInput{
		DryRun:     &dryRun,
		MaxResults: &maxResults,
	}
	ctx := context.Background()
	descVpcsOutput, err := ec2Client.DescribeVpcs(ctx, descVpcsInput)
	if err != nil {
		return "", err
	}
	var vpcId string
	for _, vpc := range descVpcsOutput.Vpcs {
		if !*vpc.IsDefault {
			continue
		}

		vpcId = *vpc.VpcId
		break
	}
	if vpcId == "" {
		if len(descVpcsOutput.Vpcs) != 1 {
			return "", fmt.Errorf("Could not find default VPC")
		}
		// if there's only 1 VPC then it's the only reasonable choice even
		// if it is not EC2's notion of 'default VPC'
		vpcId = *descVpcsOutput.Vpcs[0].VpcId
	}

	descSgInput := &ec2.DescribeSecurityGroupsInput{
		DryRun:     &dryRun,
		MaxResults: &maxResults,
	}
	descSgOutput, err := ec2Client.DescribeSecurityGroups(ctx, descSgInput)
	if err != nil {
		return "", err
	}

	numSgInVpc := 0
	foundDefaultSg := false
	var sgId string
	for _, sg := range descSgOutput.SecurityGroups {
		if *sg.VpcId != vpcId {
			continue
		}
		numSgInVpc++
		sgId = *sg.GroupId
		if *sg.GroupName == "default" {
			foundDefaultSg = true
			break
		}
	}
	if !foundDefaultSg && numSgInVpc != 1 {
		return "", fmt.Errorf("Could not find default Security Group in vpc %v",
			vpcId)
	}

	return sgId, nil
}

type LookupVpcSgsSg struct {
	Id   string
	Name string
}

type LookupVpcSgsVpc struct {
	Id      string
	Default bool
	Sgs     map[string]*LookupVpcSgsSg
}

type LookupVpcSgsResult struct {
	Vpcs map[string]*LookupVpcSgsVpc
}

func LookupVpcSecurityGroups(awsCfg aws.Config) (LookupVpcSgsResult, error) {

	lookupVpcSgsResult := LookupVpcSgsResult{
		Vpcs: make(map[string]*LookupVpcSgsVpc),
	}

	ec2Client := ec2.NewFromConfig(awsCfg)

	dryRun := false
	maxResults := int32(1000)
	descVpcsInput := &ec2.DescribeVpcsInput{
		DryRun:     &dryRun,
		MaxResults: &maxResults,
	}
	ctx := context.Background()
	descVpcsOutput, err := ec2Client.DescribeVpcs(ctx, descVpcsInput)
	if err != nil {
		return lookupVpcSgsResult, err
	}
	for _, vpc := range descVpcsOutput.Vpcs {
		vpcResult := &LookupVpcSgsVpc{
			Id:      *vpc.VpcId,
			Default: *vpc.IsDefault,
			Sgs:     make(map[string]*LookupVpcSgsSg),
		}
		lookupVpcSgsResult.Vpcs[vpcResult.Id] = vpcResult
	}

	descSgInput := &ec2.DescribeSecurityGroupsInput{
		DryRun:     &dryRun,
		MaxResults: &maxResults,
	}
	descSgOutput, err := ec2Client.DescribeSecurityGroups(ctx, descSgInput)
	if err != nil {
		return lookupVpcSgsResult, err
	}
	for _, sg := range descSgOutput.SecurityGroups {
		vpc, ok := lookupVpcSgsResult.Vpcs[*sg.VpcId]
		if !ok {
			// Vpc must have just been created between DescribeVpcs and
			// DescribeSecurityGroups() calls; skip it
			continue
		}
		sgResult := &LookupVpcSgsSg{
			Id:   *sg.GroupId,
			Name: "",
		}
		if sg.GroupName != nil {
			sgResult.Name = *sg.GroupName
		}
		vpc.Sgs[sgResult.Id] = sgResult
	}

	return lookupVpcSgsResult, nil
}
