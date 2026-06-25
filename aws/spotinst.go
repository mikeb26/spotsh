/* Copyright © 2022-2024 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/mikeb26/spotsh"
)

const (
	DefaultTagPrefix        = "spotsh"
	UserTagSuffix           = "user"
	OsTagSuffix             = "os"
	VpnTagSuffix            = "vpn"
	DefaultRootVolSizeInGiB = int32(64)
	DefaultMaxSpotPrice     = "0.08"
)

var DefaultInstanceTypes = []types.InstanceType{
	types.InstanceTypeC5Large,
	types.InstanceTypeC5aLarge,
	types.InstanceTypeC6iLarge,
	types.InstanceTypeC6aLarge,
	types.InstanceTypeC7iLarge,
	types.InstanceTypeC7aLarge,
	types.InstanceTypeC7iFlexLarge,
	types.InstanceTypeC8iLarge,
	types.InstanceTypeC8aLarge,
	types.InstanceTypeC8iFlexLarge,
}

var vcpuCounts = map[types.InstanceType]int{
	// "C" families
	types.InstanceTypeC8aMedium: 1,

	types.InstanceTypeC5Large:      2,
	types.InstanceTypeC5aLarge:     2,
	types.InstanceTypeC6iLarge:     2,
	types.InstanceTypeC6aLarge:     2,
	types.InstanceTypeC7iLarge:     2,
	types.InstanceTypeC7aLarge:     2,
	types.InstanceTypeC7iFlexLarge: 2,
	types.InstanceTypeC8iLarge:     2,
	types.InstanceTypeC8aLarge:     2,
	types.InstanceTypeC8iFlexLarge: 2,

	types.InstanceTypeC5Xlarge:      4,
	types.InstanceTypeC5aXlarge:     4,
	types.InstanceTypeC6iXlarge:     4,
	types.InstanceTypeC6aXlarge:     4,
	types.InstanceTypeC7iXlarge:     4,
	types.InstanceTypeC7aXlarge:     4,
	types.InstanceTypeC7iFlexXlarge: 4,
	types.InstanceTypeC8iXlarge:     4,
	types.InstanceTypeC8aXlarge:     4,
	types.InstanceTypeC8iFlexXlarge: 4,

	types.InstanceTypeC52xlarge:      8,
	types.InstanceTypeC5a2xlarge:     8,
	types.InstanceTypeC6i2xlarge:     8,
	types.InstanceTypeC6a2xlarge:     8,
	types.InstanceTypeC7i2xlarge:     8,
	types.InstanceTypeC7a2xlarge:     8,
	types.InstanceTypeC7iFlex2xlarge: 8,
	types.InstanceTypeC8i2xlarge:     8,
	types.InstanceTypeC8a2xlarge:     8,
	types.InstanceTypeC8iFlex2xlarge: 8,

	types.InstanceTypeC54xlarge:      16,
	types.InstanceTypeC5a4xlarge:     16,
	types.InstanceTypeC6i4xlarge:     16,
	types.InstanceTypeC6a4xlarge:     16,
	types.InstanceTypeC7i4xlarge:     16,
	types.InstanceTypeC7a4xlarge:     16,
	types.InstanceTypeC7iFlex4xlarge: 16,
	types.InstanceTypeC8i4xlarge:     16,
	types.InstanceTypeC8a4xlarge:     16,
	types.InstanceTypeC8iFlex4xlarge: 16,

	types.InstanceTypeC5a8xlarge:     32,
	types.InstanceTypeC6i8xlarge:     32,
	types.InstanceTypeC6a8xlarge:     32,
	types.InstanceTypeC7i8xlarge:     32,
	types.InstanceTypeC7a8xlarge:     32,
	types.InstanceTypeC7iFlex8xlarge: 32,
	types.InstanceTypeC8i8xlarge:     32,
	types.InstanceTypeC8a8xlarge:     32,
	types.InstanceTypeC8iFlex8xlarge: 32,

	types.InstanceTypeC59xlarge: 36,

	types.InstanceTypeC512xlarge:      48,
	types.InstanceTypeC5a12xlarge:     48,
	types.InstanceTypeC6i12xlarge:     48,
	types.InstanceTypeC6a12xlarge:     48,
	types.InstanceTypeC7i12xlarge:     48,
	types.InstanceTypeC7a12xlarge:     48,
	types.InstanceTypeC7iFlex12xlarge: 48,
	types.InstanceTypeC8i12xlarge:     48,
	types.InstanceTypeC8a12xlarge:     48,
	types.InstanceTypeC8iFlex12xlarge: 48,

	types.InstanceTypeC5a16xlarge:     64,
	types.InstanceTypeC6i16xlarge:     64,
	types.InstanceTypeC6a16xlarge:     64,
	types.InstanceTypeC7i16xlarge:     64,
	types.InstanceTypeC7a16xlarge:     64,
	types.InstanceTypeC7iFlex16xlarge: 64,
	types.InstanceTypeC8i16xlarge:     64,
	types.InstanceTypeC8a16xlarge:     64,
	types.InstanceTypeC8iFlex16xlarge: 64,

	types.InstanceTypeC518xlarge: 72,

	types.InstanceTypeC524xlarge:  96,
	types.InstanceTypeC5a24xlarge: 96,
	types.InstanceTypeC6i24xlarge: 96,
	types.InstanceTypeC6a24xlarge: 96,
	types.InstanceTypeC7i24xlarge: 96,
	types.InstanceTypeC7a24xlarge: 96,
	types.InstanceTypeC8i24xlarge: 96,
	types.InstanceTypeC8a24xlarge: 96,

	types.InstanceTypeC6i32xlarge: 128,
	types.InstanceTypeC6a32xlarge: 128,
	types.InstanceTypeC7a32xlarge: 128,
	types.InstanceTypeC8i32xlarge: 128,

	types.InstanceTypeC6a48xlarge: 192,
	types.InstanceTypeC7i48xlarge: 192,
	types.InstanceTypeC7a48xlarge: 192,
	types.InstanceTypeC8i48xlarge: 192,
	types.InstanceTypeC8a48xlarge: 192,

	types.InstanceTypeC8i96xlarge: 384,

	// "M" families
	types.InstanceTypeM8aMedium: 1,

	types.InstanceTypeM5Large:      2,
	types.InstanceTypeM5aLarge:     2,
	types.InstanceTypeM6iLarge:     2,
	types.InstanceTypeM6aLarge:     2,
	types.InstanceTypeM7iLarge:     2,
	types.InstanceTypeM7aLarge:     2,
	types.InstanceTypeM7iFlexLarge: 2,
	types.InstanceTypeM8iLarge:     2,
	types.InstanceTypeM8aLarge:     2,
	types.InstanceTypeM8iFlexLarge: 2,

	types.InstanceTypeM5Xlarge:      4,
	types.InstanceTypeM5aXlarge:     4,
	types.InstanceTypeM6iXlarge:     4,
	types.InstanceTypeM6aXlarge:     4,
	types.InstanceTypeM7iXlarge:     4,
	types.InstanceTypeM7aXlarge:     4,
	types.InstanceTypeM7iFlexXlarge: 4,
	types.InstanceTypeM8iXlarge:     4,
	types.InstanceTypeM8aXlarge:     4,
	types.InstanceTypeM8iFlexXlarge: 4,

	types.InstanceTypeM52xlarge:      8,
	types.InstanceTypeM5a2xlarge:     8,
	types.InstanceTypeM6i2xlarge:     8,
	types.InstanceTypeM6a2xlarge:     8,
	types.InstanceTypeM7i2xlarge:     8,
	types.InstanceTypeM7a2xlarge:     8,
	types.InstanceTypeM7iFlex2xlarge: 8,
	types.InstanceTypeM8i2xlarge:     8,
	types.InstanceTypeM8a2xlarge:     8,
	types.InstanceTypeM8iFlex2xlarge: 8,

	types.InstanceTypeM54xlarge:      16,
	types.InstanceTypeM5a4xlarge:     16,
	types.InstanceTypeM6i4xlarge:     16,
	types.InstanceTypeM6a4xlarge:     16,
	types.InstanceTypeM7i4xlarge:     16,
	types.InstanceTypeM7a4xlarge:     16,
	types.InstanceTypeM7iFlex4xlarge: 16,
	types.InstanceTypeM8i4xlarge:     16,
	types.InstanceTypeM8a4xlarge:     16,
	types.InstanceTypeM8iFlex4xlarge: 16,

	types.InstanceTypeM58xlarge:      32,
	types.InstanceTypeM5a8xlarge:     32,
	types.InstanceTypeM6i8xlarge:     32,
	types.InstanceTypeM6a8xlarge:     32,
	types.InstanceTypeM7i8xlarge:     32,
	types.InstanceTypeM7a8xlarge:     32,
	types.InstanceTypeM7iFlex8xlarge: 32,
	types.InstanceTypeM8i8xlarge:     32,
	types.InstanceTypeM8a8xlarge:     32,
	types.InstanceTypeM8iFlex8xlarge: 32,

	types.InstanceTypeM512xlarge:      48,
	types.InstanceTypeM5a12xlarge:     48,
	types.InstanceTypeM6i12xlarge:     48,
	types.InstanceTypeM6a12xlarge:     48,
	types.InstanceTypeM7i12xlarge:     48,
	types.InstanceTypeM7a12xlarge:     48,
	types.InstanceTypeM7iFlex12xlarge: 48,
	types.InstanceTypeM8i12xlarge:     48,
	types.InstanceTypeM8a12xlarge:     48,
	types.InstanceTypeM8iFlex12xlarge: 48,

	types.InstanceTypeM516xlarge:      64,
	types.InstanceTypeM5a16xlarge:     64,
	types.InstanceTypeM6i16xlarge:     64,
	types.InstanceTypeM6a16xlarge:     64,
	types.InstanceTypeM7i16xlarge:     64,
	types.InstanceTypeM7a16xlarge:     64,
	types.InstanceTypeM7iFlex16xlarge: 64,
	types.InstanceTypeM8i16xlarge:     64,
	types.InstanceTypeM8a16xlarge:     64,
	types.InstanceTypeM8iFlex16xlarge: 64,

	types.InstanceTypeM524xlarge:  96,
	types.InstanceTypeM5a24xlarge: 96,
	types.InstanceTypeM6i24xlarge: 96,
	types.InstanceTypeM6a24xlarge: 96,
	types.InstanceTypeM7i24xlarge: 96,
	types.InstanceTypeM7a24xlarge: 96,
	types.InstanceTypeM8i24xlarge: 96,
	types.InstanceTypeM8a24xlarge: 96,

	types.InstanceTypeM6i32xlarge: 128,
	types.InstanceTypeM6a32xlarge: 128,
	types.InstanceTypeM7a32xlarge: 128,
	types.InstanceTypeM8i32xlarge: 128,

	types.InstanceTypeM6a48xlarge: 192,
	types.InstanceTypeM7i48xlarge: 192,
	types.InstanceTypeM7a48xlarge: 192,
	types.InstanceTypeM8i48xlarge: 192,
	types.InstanceTypeM8a48xlarge: 192,

	types.InstanceTypeM8i96xlarge: 384,

	// "R" families
	types.InstanceTypeR8aMedium: 1,

	types.InstanceTypeR5Large:      2,
	types.InstanceTypeR5aLarge:     2,
	types.InstanceTypeR6iLarge:     2,
	types.InstanceTypeR6aLarge:     2,
	types.InstanceTypeR7iLarge:     2,
	types.InstanceTypeR7aLarge:     2,
	types.InstanceTypeR8iLarge:     2,
	types.InstanceTypeR8aLarge:     2,
	types.InstanceTypeR8iFlexLarge: 2,

	types.InstanceTypeR5Xlarge:      4,
	types.InstanceTypeR5aXlarge:     4,
	types.InstanceTypeR6iXlarge:     4,
	types.InstanceTypeR6aXlarge:     4,
	types.InstanceTypeR7iXlarge:     4,
	types.InstanceTypeR7aXlarge:     4,
	types.InstanceTypeR8iXlarge:     4,
	types.InstanceTypeR8aXlarge:     4,
	types.InstanceTypeR8iFlexXlarge: 4,

	types.InstanceTypeR52xlarge:      8,
	types.InstanceTypeR5a2xlarge:     8,
	types.InstanceTypeR6i2xlarge:     8,
	types.InstanceTypeR6a2xlarge:     8,
	types.InstanceTypeR7i2xlarge:     8,
	types.InstanceTypeR7a2xlarge:     8,
	types.InstanceTypeR8i2xlarge:     8,
	types.InstanceTypeR8a2xlarge:     8,
	types.InstanceTypeR8iFlex2xlarge: 8,

	types.InstanceTypeR54xlarge:      16,
	types.InstanceTypeR5a4xlarge:     16,
	types.InstanceTypeR6i4xlarge:     16,
	types.InstanceTypeR6a4xlarge:     16,
	types.InstanceTypeR7i4xlarge:     16,
	types.InstanceTypeR7a4xlarge:     16,
	types.InstanceTypeR8i4xlarge:     16,
	types.InstanceTypeR8a4xlarge:     16,
	types.InstanceTypeR8iFlex4xlarge: 16,

	types.InstanceTypeR58xlarge:      32,
	types.InstanceTypeR5a8xlarge:     32,
	types.InstanceTypeR6i8xlarge:     32,
	types.InstanceTypeR6a8xlarge:     32,
	types.InstanceTypeR7i8xlarge:     32,
	types.InstanceTypeR7a8xlarge:     32,
	types.InstanceTypeR8i8xlarge:     32,
	types.InstanceTypeR8a8xlarge:     32,
	types.InstanceTypeR8iFlex8xlarge: 32,

	types.InstanceTypeR512xlarge:      48,
	types.InstanceTypeR5a12xlarge:     48,
	types.InstanceTypeR6i12xlarge:     48,
	types.InstanceTypeR6a12xlarge:     48,
	types.InstanceTypeR7i12xlarge:     48,
	types.InstanceTypeR7a12xlarge:     48,
	types.InstanceTypeR8i12xlarge:     48,
	types.InstanceTypeR8a12xlarge:     48,
	types.InstanceTypeR8iFlex12xlarge: 48,

	types.InstanceTypeR516xlarge:      64,
	types.InstanceTypeR5a16xlarge:     64,
	types.InstanceTypeR6i16xlarge:     64,
	types.InstanceTypeR6a16xlarge:     64,
	types.InstanceTypeR7i16xlarge:     64,
	types.InstanceTypeR7a16xlarge:     64,
	types.InstanceTypeR8i16xlarge:     64,
	types.InstanceTypeR8a16xlarge:     64,
	types.InstanceTypeR8iFlex16xlarge: 64,

	types.InstanceTypeR524xlarge:  96,
	types.InstanceTypeR5a24xlarge: 96,
	types.InstanceTypeR6i24xlarge: 96,
	types.InstanceTypeR6a24xlarge: 96,
	types.InstanceTypeR7i24xlarge: 96,
	types.InstanceTypeR7a24xlarge: 96,
	types.InstanceTypeR8i24xlarge: 96,
	types.InstanceTypeR8a24xlarge: 96,

	types.InstanceTypeR6i32xlarge: 128,
	types.InstanceTypeR6a32xlarge: 128,
	types.InstanceTypeR7a32xlarge: 128,
	types.InstanceTypeR8i32xlarge: 128,

	types.InstanceTypeR6a48xlarge: 192,
	types.InstanceTypeR7i48xlarge: 192,
	types.InstanceTypeR7a48xlarge: 192,
	types.InstanceTypeR8i48xlarge: 192,
	types.InstanceTypeR8a48xlarge: 192,

	types.InstanceTypeR8i96xlarge: 384,
}

func getVCPUCount(iType types.InstanceType) int {

	count, ok := vcpuCounts[iType]
	if !ok {
		fmt.Fprintf(os.Stderr, "WARN: getVCPUCount() missing vcpu def for %v\n",
			iType)

		return 1
	}

	return count
}

const DefaultOperatingSystem = spotsh.AmazonLinux2023

type LaunchEc2SpotArgs struct {
	Os               spotsh.OperatingSystem // optional; defaults to AmazonLinux2023
	AmiId            string                 // optional; overrides Os; defaults to latest ami for specified Os
	AmiName          string                 // optional; default is ignored in lieu of AmiId
	KeyPair          string                 // optional; defaults to spotinst keypair
	SecurityGroupId  string                 // optional; defaults to default VPC's default SG
	AttachRoleName   string                 // optional; defaults to no attached role
	InitCmd          string                 // optional; defaults to empty
	InstanceTypes    []types.InstanceType   // optional; defaults to DefaultInstanceTypes
	AzNames          []string               // optional; defaults to all AZs in the security group's VPC
	MaxSpotPrice     string                 // optional; defaults to "0.08" (USD$/hour)
	User             string                 // optional; defaults to Os's default user
	RootVolSizeInGiB int32                  // optional; defaults to 64GiB
	TagPrefix        string                 // optional; defaults to 'spotsh'
}

type LaunchEc2SpotResult struct {
	PublicIp     string
	InstanceId   string
	User         string
	LocalKeyFile string
	InstanceType types.InstanceType
	ImageId      string
	CurrentPrice float64
	AzName       string
	DnsName      string
	Os           spotsh.OperatingSystem
	SgId         string
}

func LaunchEc2Spot(ctx context.Context, awsCfg aws.Config,
	launchArgs *LaunchEc2SpotArgs) (LaunchEc2SpotResult, error) {

	if launchArgs == nil {
		launchArgs = &LaunchEc2SpotArgs{}
	}

	var launchResult LaunchEc2SpotResult
	ec2Client := ec2.NewFromConfig(awsCfg)
	templateId, err := createLaunchTemplate(ctx, awsCfg, ec2Client, launchArgs,
		&launchResult)
	if err != nil {
		err = fmt.Errorf("failed to create launch template: %w\n", err)
		return launchResult, err
	}

	err = runInstance(ctx, awsCfg, ec2Client, templateId, launchArgs,
		&launchResult)

	return launchResult, err
}

func createLaunchTemplate(ctx context.Context, awsCfg aws.Config,
	ec2Client *ec2.Client, launchArgs *LaunchEc2SpotArgs,
	launchResult *LaunchEc2SpotResult) (string, error) {

	if launchArgs.TagPrefix == "" {
		launchArgs.TagPrefix = DefaultTagPrefix
	}
	launchTemplateName := launchArgs.TagPrefix + "-lt"
	descInput := &ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateNames: []string{launchTemplateName},
	}
	descOuput, err := ec2Client.DescribeLaunchTemplates(ctx, descInput)
	if err == nil && len(descOuput.LaunchTemplates) > 0 {
		deleteInput := &ec2.DeleteLaunchTemplateInput{
			LaunchTemplateId: aws.String(*descOuput.LaunchTemplates[0].LaunchTemplateId),
		}
		_, err := ec2Client.DeleteLaunchTemplate(ctx, deleteInput)
		if err != nil {
			return "", err
		}
	}

	spotPrice := launchArgs.MaxSpotPrice
	if spotPrice == "" {
		spotPrice = DefaultMaxSpotPrice
	}
	spotOpts := &types.LaunchTemplateSpotMarketOptionsRequest{
		InstanceInterruptionBehavior: types.InstanceInterruptionBehaviorTerminate,
		MaxPrice:                     &spotPrice,
		SpotInstanceType:             types.SpotInstanceTypeOneTime,
	}
	marketOpts := &types.LaunchTemplateInstanceMarketOptionsRequest{
		MarketType:  types.MarketTypeSpot,
		SpotOptions: spotOpts,
	}

	iamOpts := &types.LaunchTemplateIamInstanceProfileSpecificationRequest{}
	if launchArgs.AttachRoleName != "" {
		iamOpts.Name = &launchArgs.AttachRoleName
	} else {
		iamOpts = nil
	}

	var keyName *string
	if launchArgs.KeyPair != "" {
		keyName = &launchArgs.KeyPair
	} else {
		haveDefaultKey, err := haveDefaultKeyPair(ctx, awsCfg)
		if err != nil {
			return "", err
		}
		if !haveDefaultKey {
			err = createDefaultKeyPair(ctx, awsCfg, ec2Client)
			if err != nil {
				return "", err
			}
		}
		keyPair := GetDefaultKeyName(awsCfg)
		keyName = &keyPair
	}
	keysResult, err := LookupKeys(awsCfg)
	if err != nil {
		return "", err
	}
	launchResult.LocalKeyFile = ""
	for _, keyItem := range keysResult.Keys {
		if *keyName == keyItem.Name {
			launchResult.LocalKeyFile = keyItem.LocalKeyFile
			break
		}
	}
	var initCmdEncoded *string
	if launchArgs.InitCmd != "" {
		initCmdEncodedActual :=
			base64.StdEncoding.EncodeToString([]byte(launchArgs.InitCmd))
		initCmdEncoded = &initCmdEncodedActual
	} else {
		initCmdEncoded = nil
	}
	amiId := launchArgs.AmiId
	amiName := launchArgs.AmiName
	if amiName != "" {
		if amiId != "" {
			return "", fmt.Errorf("Ami id and ami name are mutually exclusive; please specify one or the other")
		}
		amiId, err = getAmiIdFromName(awsCfg, ec2Client, amiName)
		if err != nil {
			return "", err
		}
	}
	if amiId == "" {
		if launchArgs.Os == spotsh.OsNone {
			launchArgs.Os = DefaultOperatingSystem
		}
		idx := int(launchArgs.Os)
		launchResult.User = imageIdTab[idx].user
		amiId, err = getLatestAmiId(ctx, awsCfg, launchArgs.Os)
		if err != nil {
			return "", err
		}
	} else if launchArgs.User == "" {
		return "", fmt.Errorf("User must be specified when ami id or ami name are specified")
	} else {
		launchResult.User = launchArgs.User
	}
	sgId := launchArgs.SecurityGroupId
	if sgId == "" {
		sgId, err = getDefaultSecurityGroupId(awsCfg, ec2Client)
		if err != nil {
			return "", err
		}
	}
	launchResult.SgId = sgId
	userTagKey := launchArgs.TagPrefix + "." + UserTagSuffix
	userTagVal := launchResult.User
	userTag := types.Tag{
		Key:   &userTagKey,
		Value: &userTagVal,
	}
	osTagKey := launchArgs.TagPrefix + "." + OsTagSuffix
	osTagVal := launchArgs.Os.String()
	launchResult.Os = launchArgs.Os
	osTag := types.Tag{
		Key:   &osTagKey,
		Value: &osTagVal,
	}
	vpnTagKey := launchArgs.TagPrefix + "." + VpnTagSuffix
	vpnTagVal := "false"
	vpnTag := types.Tag{
		Key:   &vpnTagKey,
		Value: &vpnTagVal,
	}
	tagSpec := types.LaunchTemplateTagSpecificationRequest{
		ResourceType: types.ResourceTypeInstance,
		Tags:         []types.Tag{userTag, osTag, vpnTag},
	}
	rootVolSize := launchArgs.RootVolSizeInGiB
	rootVolName, err := getRootVolName(ctx, ec2Client, amiId)
	if err != nil {
		return "", err
	}
	if rootVolSize == 0 {
		rootVolSize = DefaultRootVolSizeInGiB
	}
	rootBlockMap := types.LaunchTemplateBlockDeviceMappingRequest{
		DeviceName: &rootVolName,
		Ebs: &types.LaunchTemplateEbsBlockDeviceRequest{
			VolumeSize: &rootVolSize,
		},
	}
	if len(launchArgs.InstanceTypes) == 0 {
		launchArgs.InstanceTypes = DefaultInstanceTypes
	}
	createInput := &ec2.CreateLaunchTemplateInput{
		LaunchTemplateData: &types.RequestLaunchTemplateData{
			BlockDeviceMappings:               []types.LaunchTemplateBlockDeviceMappingRequest{rootBlockMap},
			IamInstanceProfile:                iamOpts,
			ImageId:                           aws.String(amiId),
			InstanceInitiatedShutdownBehavior: types.ShutdownBehaviorTerminate,
			InstanceMarketOptions:             marketOpts,
			KeyName:                           keyName,
			SecurityGroupIds:                  []string{sgId},
			TagSpecifications:                 []types.LaunchTemplateTagSpecificationRequest{tagSpec},
			UserData:                          initCmdEncoded,
		},
		LaunchTemplateName: aws.String(launchTemplateName),
	}
	createOutput, err := ec2Client.CreateLaunchTemplate(ctx, createInput)
	if err != nil {
		return "", err
	}

	return *createOutput.LaunchTemplate.LaunchTemplateId, nil
}

func getLaunchTemplateConfigs(templateId string,
	overrides []types.FleetLaunchTemplateOverridesRequest) []types.FleetLaunchTemplateConfigRequest {

	config := types.FleetLaunchTemplateConfigRequest{
		LaunchTemplateSpecification: &types.FleetLaunchTemplateSpecificationRequest{
			LaunchTemplateId: aws.String(templateId),
			Version:          aws.String("$Latest"),
		},
		Overrides: overrides,
	}

	return []types.FleetLaunchTemplateConfigRequest{config}
}

func runInstance(ctx context.Context, awsCfg aws.Config,
	ec2Client *ec2.Client, templateId string, launchArgs *LaunchEc2SpotArgs,
	launchResult *LaunchEc2SpotResult) error {

	spotPrice := launchArgs.MaxSpotPrice
	if spotPrice == "" {
		spotPrice = DefaultMaxSpotPrice
	}
	overrides, err := getFleetLaunchTemplateOverrides(ctx, awsCfg, ec2Client,
		launchArgs, launchResult.SgId)
	if err != nil {
		return err
	}
	input := &ec2.CreateFleetInput{
		LaunchTemplateConfigs: getLaunchTemplateConfigs(templateId, overrides),
		TargetCapacitySpecification: &types.TargetCapacitySpecificationRequest{
			TotalTargetCapacity:       aws.Int32(1),
			DefaultTargetCapacityType: types.DefaultTargetCapacityTypeSpot,
			OnDemandTargetCapacity:    aws.Int32(0),
			SpotTargetCapacity:        aws.Int32(1),
		},
		SpotOptions: &types.SpotOptionsRequest{
			AllocationStrategy:     types.SpotAllocationStrategyCapacityOptimized,
			MaxTotalPrice:          aws.String(spotPrice),
			MinTargetCapacity:      aws.Int32(1),
			SingleAvailabilityZone: aws.Bool(true),
			SingleInstanceType:     aws.Bool(false),
		},
		Type: types.FleetTypeInstant,
	}
	runOutput, err := ec2Client.CreateFleet(ctx, input)
	if err != nil {
		return fmt.Errorf("unable to create EC2 fleet: %w", err)
	}

	if len(runOutput.Instances) != 1 {
		deleteFleet(ctx, ec2Client, runOutput.FleetId)
		return newCreateFleetFailureError(runOutput, spotPrice,
			fmt.Sprintf("EC2 Fleet launched %d instance groups; expected 1",
				len(runOutput.Instances)))
	}
	if len(runOutput.Instances[0].InstanceIds) != 1 {
		deleteFleet(ctx, ec2Client, runOutput.FleetId)
		return newCreateFleetFailureError(runOutput, spotPrice,
			fmt.Sprintf("EC2 Fleet launched %d instances for %s; expected 1",
				len(runOutput.Instances[0].InstanceIds),
				runOutput.Instances[0].InstanceType))
	}

	instanceId := runOutput.Instances[0].InstanceIds[0]
	launchResult.InstanceId = instanceId
	launchResult.InstanceType = runOutput.Instances[0].InstanceType

	for {
		time.Sleep(1 * time.Second)

		describeInput := &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceId},
		}
		descOutput, err := ec2Client.DescribeInstances(ctx, describeInput)
		if err != nil {
			// launched succeeded but we couldn't determine the public ip;
			// treat as success
			break
		}

		if len(descOutput.Reservations) != 1 {
			panic(fmt.Sprintf("Unexpected reservations count: %v",
				len(descOutput.Reservations)))
		}
		if len(descOutput.Reservations[0].Instances) != 1 {
			panic(fmt.Sprintf("Unexpected reservations' instances count: %v",
				len(descOutput.Reservations[0].Instances)))
		}
		if descOutput.Reservations[0].Instances[0].PublicIpAddress != nil {
			launchResult.PublicIp =
				*descOutput.Reservations[0].Instances[0].PublicIpAddress
			break
		}
	}

	return nil
}

type fleetLaunchCandidate struct {
	override          types.FleetLaunchTemplateOverridesRequest
	price             float64
	azName            string
	instanceTypeOrder int
}

func getFleetLaunchTemplateOverrides(ctx context.Context, awsCfg aws.Config,
	ec2Client *ec2.Client, launchArgs *LaunchEc2SpotArgs,
	sgId string) ([]types.FleetLaunchTemplateOverridesRequest, error) {

	spotPriceResult, err := LookupEc2SpotPrices(awsCfg, launchArgs.InstanceTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to look up spot prices for launch candidates: %w",
			err)
	}

	overrides := buildFleetLaunchTemplateOverrides(awsCfg.Region,
		launchArgs.InstanceTypes, launchArgs.AzNames, spotPriceResult)
	if len(overrides) == 0 {
		azConstraint := ""
		if len(launchArgs.AzNames) > 0 {
			azConstraint = fmt.Sprintf(" constrained to availability zones %v",
				launchArgs.AzNames)
		}
		return nil, fmt.Errorf("could not find any launchable spot capacity pools in %v for instance types %v in security group %v's VPC%v; no AZ had current spot price data, current instance type offering, and an available subnet",
			awsCfg.Region, launchArgs.InstanceTypes, sgId, azConstraint)
	}

	return overrides, nil
}

func buildFleetLaunchTemplateOverrides(region string,
	iTypes []types.InstanceType, allowedAzs []string,
	spotPriceResult *LookupEc2SpotPriceResult) []types.FleetLaunchTemplateOverridesRequest {

	instanceTypeOrder := make(map[types.InstanceType]int)
	for idx, iType := range iTypes {
		if _, ok := instanceTypeOrder[iType]; !ok {
			instanceTypeOrder[iType] = idx
		}
	}

	candidates := make([]fleetLaunchCandidate, 0)
	if spotPriceResult == nil {
		return nil
	}
	for _, iType := range iTypes {
		lookupIType := spotPriceResult.InstanceTypes[iType]
		if lookupIType == nil {
			continue
		}
		lookupReg := lookupIType.Regions[region]
		if lookupReg == nil {
			continue
		}

		for azName, lookupAz := range lookupReg.Azs {
			if lookupAz == nil {
				continue
			}
			if len(allowedAzs) > 0 && !slices.Contains(allowedAzs, azName) {
				continue
			}

			candidates = append(candidates, fleetLaunchCandidate{
				override: types.FleetLaunchTemplateOverridesRequest{
					InstanceType:     iType,
					AvailabilityZone: aws.String(azName),
				},
				price:             lookupAz.CurPrice,
				azName:            azName,
				instanceTypeOrder: instanceTypeOrder[iType],
			})
		}
	}

	sort.Slice(candidates, func(i int, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.price != right.price {
			return left.price < right.price
		}
		if left.instanceTypeOrder != right.instanceTypeOrder {
			return left.instanceTypeOrder < right.instanceTypeOrder
		}
		if left.override.InstanceType != right.override.InstanceType {
			return left.override.InstanceType < right.override.InstanceType
		}

		return left.azName < right.azName
	})

	overrides := make([]types.FleetLaunchTemplateOverridesRequest, 0,
		len(candidates))
	for _, candidate := range candidates {
		overrides = append(overrides, candidate.override)
	}

	return overrides
}

func deleteFleet(ctx context.Context, ec2Client *ec2.Client, fleetId *string) {
	if fleetId == nil {
		return
	}

	deleteInput := &ec2.DeleteFleetsInput{
		FleetIds:           []string{*fleetId},
		TerminateInstances: aws.Bool(true),
	}
	_, _ = ec2Client.DeleteFleets(ctx, deleteInput)
}

func newCreateFleetFailureError(runOutput *ec2.CreateFleetOutput,
	spotPrice string, summary string) error {

	msgParts := []string{
		fmt.Sprintf("unable to create instances at max spot price %s", spotPrice),
	}
	if runOutput.FleetId != nil {
		msgParts = append(msgParts, fmt.Sprintf("fleet %s", *runOutput.FleetId))
	}
	if summary != "" {
		msgParts = append(msgParts, summary)
	}

	if len(runOutput.Errors) == 0 {
		msgParts = append(msgParts, "EC2 Fleet did not return launch error details")
	} else {
		msgParts = append(msgParts,
			fmt.Sprintf("EC2 Fleet errors: %s",
				formatCreateFleetErrors(runOutput.Errors)))
	}

	return fmt.Errorf("%s", strings.Join(msgParts, "; "))
}

func formatCreateFleetErrors(fleetErrors []types.CreateFleetError) string {
	formattedErrors := make([]string, 0, len(fleetErrors))
	for _, fleetErr := range fleetErrors {
		details := make([]string, 0)

		if fleetErr.LaunchTemplateAndOverrides != nil &&
			fleetErr.LaunchTemplateAndOverrides.Overrides != nil {
			overrides := fleetErr.LaunchTemplateAndOverrides.Overrides
			if overrides.InstanceType != "" {
				details = append(details,
					fmt.Sprintf("instanceType=%s", overrides.InstanceType))
			}
			if overrides.AvailabilityZone != nil {
				details = append(details,
					fmt.Sprintf("availabilityZone=%s", *overrides.AvailabilityZone))
			}
			if overrides.AvailabilityZoneId != nil {
				details = append(details,
					fmt.Sprintf("availabilityZoneId=%s", *overrides.AvailabilityZoneId))
			}
			if overrides.SubnetId != nil {
				details = append(details,
					fmt.Sprintf("subnetId=%s", *overrides.SubnetId))
			}
		}

		if fleetErr.Lifecycle != "" {
			details = append(details,
				fmt.Sprintf("lifecycle=%s", fleetErr.Lifecycle))
		}

		errorCode := aws.ToString(fleetErr.ErrorCode)
		errorMessage := aws.ToString(fleetErr.ErrorMessage)
		switch {
		case errorCode != "" && errorMessage != "":
			details = append(details,
				fmt.Sprintf("%s: %s", errorCode, errorMessage))
		case errorCode != "":
			details = append(details, errorCode)
		case errorMessage != "":
			details = append(details, errorMessage)
		}

		if len(details) == 0 {
			details = append(details, "unknown EC2 Fleet launch error")
		}

		formattedErrors = append(formattedErrors, strings.Join(details, ", "))
	}

	return strings.Join(formattedErrors, "; ")
}

func TerminateInstance(awsCfg aws.Config, instanceId string) error {
	ec2Client := ec2.NewFromConfig(awsCfg)

	dryRun := false
	termInput := &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceId},
		DryRun:      &dryRun,
	}
	ctx := context.Background()
	_, err := ec2Client.TerminateInstances(ctx, termInput)
	if err != nil {
		return err
	}

	return nil
}

func UpdateTag(awsCfg aws.Config, instanceId string, key string,
	value string) error {

	ec2Client := ec2.NewFromConfig(awsCfg)

	tagInput := &ec2.CreateTagsInput{
		Resources: []string{instanceId},
		Tags: []types.Tag{
			{
				Key:   &key,
				Value: &value,
			},
		},
	}

	_, err := ec2Client.CreateTags(context.Background(), tagInput)
	if err != nil {
		return err
	}

	return nil
}

func GetTagValue(awsCfg aws.Config, instanceId string,
	key string) (string, error) {

	ec2Client := ec2.NewFromConfig(awsCfg)

	resourceId := "resource-id"
	keyName := "key"
	tagInput := &ec2.DescribeTagsInput{
		Filters: []types.Filter{
			{
				Name:   &resourceId,
				Values: []string{instanceId},
			},
			{
				Name:   &keyName,
				Values: []string{key},
			},
		},
	}

	tagOutput, err := ec2Client.DescribeTags(context.Background(), tagInput)
	if err != nil {
		return "", err
	}

	if len(tagOutput.Tags) == 0 {
		return "", nil
	}

	return *tagOutput.Tags[0].Value, nil
}

func LookupEc2Spot(ctx context.Context,
	awsCfgIn aws.Config, tagPrefix string) ([]LaunchEc2SpotResult, error) {

	if tagPrefix == "" {
		tagPrefix = DefaultTagPrefix
	}
	var err error
	var regionList []string
	resultsAllRegions := make([]LaunchEc2SpotResult, 0)

	if awsCfgIn.Region == "all" {
		regionList, err = getRegions()
		if err != nil {
			return nil, err
		}
	} else {
		regionList = []string{awsCfgIn.Region}
	}

	var wg errgroup.Group
	var resultLock sync.Mutex

	for _, curReg := range regionList {
		curReg := curReg // https://golang.org/doc/faq#closures_and_goroutines
		wg.Go(func() error {
			awsCfgTmp, err := config.LoadDefaultConfig(ctx,
				config.WithRegion(curReg))
			if err != nil {
				return err
			}
			resultsOneRegion, err := lookupEc2SpotOneRegion(awsCfgTmp, tagPrefix)
			if err != nil {
				return err
			}
			resultLock.Lock()
			resultsAllRegions = append(resultsAllRegions, resultsOneRegion...)
			resultLock.Unlock()

			return nil
		})
	}

	err = wg.Wait()
	if err != nil {
		return nil, err
	}

	return resultsAllRegions, nil
}

func lookupEc2SpotOneRegion(awsCfg aws.Config,
	tagPrefix string) ([]LaunchEc2SpotResult, error) {

	launchResults := make([]LaunchEc2SpotResult, 0)

	ec2Client := ec2.NewFromConfig(awsCfg)
	dryRun := false
	maxResults := int32(1000)
	describeInput := &ec2.DescribeInstancesInput{
		DryRun:     &dryRun,
		MaxResults: &maxResults,
	}
	ctx := context.Background()
	descOutput, err := ec2Client.DescribeInstances(ctx, describeInput)
	if err != nil {
		return launchResults, err
	}
	keysResult, err := LookupKeys(awsCfg)
	if err != nil {
		return launchResults, err
	}

	azMap := make(map[string]string)
	var iTypes []types.InstanceType

	var foundSpotShTag bool
	var user string
	var os string
	userTagKey := tagPrefix + "." + UserTagSuffix
	osTagKey := tagPrefix + "." + OsTagSuffix
	for _, resv := range descOutput.Reservations {
		for _, inst := range resv.Instances {
			if inst.State.Name != types.InstanceStateNameRunning {
				continue
			}
			foundSpotShTag = false
			for _, tag := range inst.Tags {
				if *tag.Key == userTagKey {
					foundSpotShTag = true
					user = *tag.Value
				} else if *tag.Key == osTagKey {
					os = *tag.Value
				}
			}
			if !foundSpotShTag {
				continue
			}

			localKeyFile := ""
			for _, keyItem := range keysResult.Keys {
				if inst.KeyName != nil && keyItem.Name == *inst.KeyName {
					localKeyFile = keyItem.LocalKeyFile
					break
				}
			}

			azName, err := getAzNameFromSubnetId(ec2Client, azMap,
				*inst.SubnetId)
			if err != nil {
				return launchResults, err
			}
			iTypes = append(iTypes, inst.InstanceType)
			publicIp := ""
			if inst.PublicIpAddress != nil {
				publicIp = *inst.PublicIpAddress
			}
			launchResult := LaunchEc2SpotResult{
				InstanceId:   *inst.InstanceId,
				PublicIp:     publicIp,
				User:         user,
				LocalKeyFile: localKeyFile,
				InstanceType: inst.InstanceType,
				ImageId:      *inst.ImageId,
				AzName:       azName,
				CurrentPrice: 0.00,
				DnsName:      *inst.PublicDnsName,
				Os:           spotsh.OsFromString(os),
				SgId:         *inst.SecurityGroups[0].GroupId,
			}

			launchResults = append(launchResults, launchResult)
		}
	}

	if len(iTypes) == 0 {
		return launchResults, nil
	}

	spotPriceResult, err := LookupEc2SpotPrices(awsCfg, iTypes)
	if err != nil {
		return launchResults, err
	}

	for idx := range launchResults {
		launchResult := &launchResults[idx]
		iType := launchResult.InstanceType
		reg := awsCfg.Region
		azName := launchResult.AzName
		curPrice :=
			spotPriceResult.InstanceTypes[iType].Regions[reg].Azs[azName].CurPrice
		launchResult.CurrentPrice = curPrice
	}

	return launchResults, nil
}
