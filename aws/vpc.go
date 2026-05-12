/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func GetDefaultSecurityGroupId(awsCfg aws.Config) (string, error) {
	ec2Client := ec2.NewFromConfig(awsCfg)

	return getDefaultSecurityGroupId(awsCfg, ec2Client)
}

func getExternalIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get external IP: %s", resp.Status)
	}

	ip, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(ip), nil
}

func addSshIngressRule(ctx context.Context, host string, ec2Client *ec2.Client,
	sgId string) error {

	myIp, err := getExternalIP()
	if err != nil {
		return err
	}
	cidrBlock := fmt.Sprintf("%v/32", myIp)
	permissions := []types.IpPermission{
		{
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int32(22),
			ToPort:     aws.Int32(22),
			IpRanges: []types.IpRange{
				{
					CidrIp:      aws.String(cidrBlock),
					Description: aws.String(fmt.Sprintf("allow ssh from %v (added by spotsh)", host)),
				},
			},
		},
	}

	input := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(sgId),
		IpPermissions: permissions,
	}

	_, err = ec2Client.AuthorizeSecurityGroupIngress(ctx, input)
	return err
}

func hasSshIngressRule(ctx context.Context, host string, ec2Client *ec2.Client,
	sgId string) bool {

	input := &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgId},
	}

	resp, err := ec2Client.DescribeSecurityGroups(ctx, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get security groups: %v", err)
		return false
	}

	for _, sg := range resp.SecurityGroups {
		for _, perm := range sg.IpPermissions {
			for _, descr := range perm.IpRanges {
				if strings.Contains(*descr.Description, "ssh") &&
					strings.Contains(*descr.Description, host) {
					return true
				}
			}

			for _, descr := range perm.Ipv6Ranges {
				if strings.Contains(*descr.Description, "ssh") &&
					strings.Contains(*descr.Description, host) {
					return true
				}
			}
		}
	}

	return false
}

func CheckOrAddSshIngressRule(awsCfg aws.Config, sgId string) error {
	ec2Client := ec2.NewFromConfig(awsCfg)
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}

	ctx := context.Background()

	if hasSshIngressRule(ctx, host, ec2Client, sgId) {
		return nil
	}

	return addSshIngressRule(context.Background(), host, ec2Client, sgId)
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

type subnetCandidate struct {
	subnetId                string
	defaultForAz            bool
	mapPublicIpOnLaunch     bool
	availableIpAddressCount int32
}

func getVpcIdFromSecurityGroup(ctx context.Context, ec2Client *ec2.Client,
	sgId string) (string, error) {

	descIn := &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgId},
	}
	descOut, err := ec2Client.DescribeSecurityGroups(ctx, descIn)
	if err != nil {
		return "", err
	}
	if len(descOut.SecurityGroups) != 1 {
		return "", fmt.Errorf("expected 1 security group for %v, got %v",
			sgId, len(descOut.SecurityGroups))
	}

	return aws.ToString(descOut.SecurityGroups[0].VpcId), nil
}

func getSubnetIdsByAzForSecurityGroup(ctx context.Context,
	ec2Client *ec2.Client, sgId string) (map[string]string, error) {

	vpcId, err := getVpcIdFromSecurityGroup(ctx, ec2Client, sgId)
	if err != nil {
		return nil, err
	}
	if vpcId == "" {
		return nil, fmt.Errorf("security group %v is not associated with a VPC",
			sgId)
	}

	dryRun := false
	descIn := &ec2.DescribeSubnetsInput{
		DryRun: &dryRun,
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcId},
			},
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
	}
	descOut, err := ec2Client.DescribeSubnets(ctx, descIn)
	if err != nil {
		return nil, err
	}

	subnetsByAz := make(map[string]subnetCandidate)
	for _, subnet := range descOut.Subnets {
		azName := aws.ToString(subnet.AvailabilityZone)
		subnetId := aws.ToString(subnet.SubnetId)
		if azName == "" || subnetId == "" {
			continue
		}

		candidate := subnetCandidate{
			subnetId:                subnetId,
			defaultForAz:            aws.ToBool(subnet.DefaultForAz),
			mapPublicIpOnLaunch:     aws.ToBool(subnet.MapPublicIpOnLaunch),
			availableIpAddressCount: aws.ToInt32(subnet.AvailableIpAddressCount),
		}
		existing, ok := subnetsByAz[azName]
		if !ok || isBetterSubnetCandidate(candidate, existing) {
			subnetsByAz[azName] = candidate
		}
	}

	if len(subnetsByAz) == 0 {
		return nil, fmt.Errorf("could not find available subnets in VPC %v for security group %v",
			vpcId, sgId)
	}

	subnetIdsByAz := make(map[string]string, len(subnetsByAz))
	for azName, subnet := range subnetsByAz {
		subnetIdsByAz[azName] = subnet.subnetId
	}

	return subnetIdsByAz, nil
}

func isBetterSubnetCandidate(candidate subnetCandidate,
	existing subnetCandidate) bool {

	if candidate.defaultForAz != existing.defaultForAz {
		return candidate.defaultForAz
	}
	if candidate.mapPublicIpOnLaunch != existing.mapPublicIpOnLaunch {
		return candidate.mapPublicIpOnLaunch
	}

	return candidate.availableIpAddressCount > existing.availableIpAddressCount
}

func getAzNameFromSubnetId(ec2Client *ec2.Client, azMap map[string]string,
	subnetId string) (string, error) {

	azName, ok := azMap[subnetId]
	if ok {
		return azName, nil
	}

	dryRun := false
	descIn := &ec2.DescribeSubnetsInput{
		DryRun: &dryRun,
	}
	ctx := context.Background()
	descOut, err := ec2Client.DescribeSubnets(ctx, descIn)
	if err != nil {
		return "", err
	}

	for _, subnet := range descOut.Subnets {
		azMap[*subnet.SubnetId] = *subnet.AvailabilityZone
	}

	return azMap[subnetId], nil
}

func getSubnetIdFromAzName(ec2Client *ec2.Client, azName string) (string, error) {
	dryRun := false
	descIn := &ec2.DescribeSubnetsInput{
		DryRun: &dryRun,
	}
	ctx := context.Background()
	descOut, err := ec2Client.DescribeSubnets(ctx, descIn)
	if err != nil {
		return "", err
	}

	for _, subnet := range descOut.Subnets {
		if azName == *subnet.AvailabilityZone {
			return *subnet.SubnetId, nil
		}
	}

	return "", fmt.Errorf("Could not find subnet for az:%v", azName)
}
