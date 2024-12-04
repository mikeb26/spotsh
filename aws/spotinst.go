/* Copyright © 2022-2024 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"encoding/base64"
	"fmt"
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
	UserTagKey              = "spotsh.user"
	OsTagKey                = "spotsh.os"
	VpnTagKey               = "spotsh.vpn"
	DefaultRootVolSizeInGiB = int32(64)
	DefaultMaxSpotPrice     = "0.08"
	LaunchTemplateName      = "spotsh"
)

var DefaultInstanceTypes = []types.InstanceType{
	types.InstanceTypeC5Large,
	types.InstanceTypeC5aLarge,
	types.InstanceTypeC6iLarge,
	types.InstanceTypeC6aLarge,
	types.InstanceTypeC7iLarge,
	types.InstanceTypeC7aLarge,
	types.InstanceTypeC7iFlexLarge,
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
	InstanceTypes    []types.InstanceType   // optional; defaults to c5a.large
	MaxSpotPrice     string                 // optional; defaults to "0.08" (USD$/hour)
	User             string                 // optional; defaults to Os's default user
	RootVolSizeInGiB int32                  // optional; defaults to 64GiB
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

func LaunchEc2Spot(awsCfg aws.Config,
	launchArgs *LaunchEc2SpotArgs) (LaunchEc2SpotResult, error) {

	if launchArgs == nil {
		launchArgs = &LaunchEc2SpotArgs{}
	}

	var launchResult LaunchEc2SpotResult
	ec2Client := ec2.NewFromConfig(awsCfg)
	ctx := context.Background()
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

	descInput := &ec2.DescribeLaunchTemplatesInput{
		LaunchTemplateNames: []string{LaunchTemplateName},
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
	userTagKey := UserTagKey
	userTagVal := launchResult.User
	userTag := types.Tag{
		Key:   &userTagKey,
		Value: &userTagVal,
	}
	osTagKey := OsTagKey
	osTagVal := launchArgs.Os.String()
	launchResult.Os = launchArgs.Os
	osTag := types.Tag{
		Key:   &osTagKey,
		Value: &osTagVal,
	}
	vpnTagKey := VpnTagKey
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
		LaunchTemplateName: aws.String(LaunchTemplateName),
	}
	createOutput, err := ec2Client.CreateLaunchTemplate(ctx, createInput)
	if err != nil {
		return "", err
	}

	return *createOutput.LaunchTemplate.LaunchTemplateId, nil
}

func getLaunchTemplateConfigs(templateId string,
	launchArgs *LaunchEc2SpotArgs) []types.FleetLaunchTemplateConfigRequest {

	configList := make([]types.FleetLaunchTemplateConfigRequest, 0)
	for _, iType := range launchArgs.InstanceTypes {
		config := types.FleetLaunchTemplateConfigRequest{
			LaunchTemplateSpecification: &types.FleetLaunchTemplateSpecificationRequest{
				LaunchTemplateId: aws.String(templateId),
				Version:          aws.String("$Latest"),
			},
			Overrides: []types.FleetLaunchTemplateOverridesRequest{
				{InstanceType: iType},
			},
		}
		configList = append(configList, config)
	}

	return configList
}

func runInstance(ctx context.Context, awsCfg aws.Config,
	ec2Client *ec2.Client, templateId string, launchArgs *LaunchEc2SpotArgs,
	launchResult *LaunchEc2SpotResult) error {

	spotPrice := launchArgs.MaxSpotPrice
	if spotPrice == "" {
		spotPrice = DefaultMaxSpotPrice
	}
	input := &ec2.CreateFleetInput{
		LaunchTemplateConfigs: getLaunchTemplateConfigs(templateId, launchArgs),
		TargetCapacitySpecification: &types.TargetCapacitySpecificationRequest{
			TotalTargetCapacity:       aws.Int32(1),
			DefaultTargetCapacityType: types.DefaultTargetCapacityTypeSpot,
			OnDemandTargetCapacity:    aws.Int32(0),
			SpotTargetCapacity:        aws.Int32(1),
		},
		SpotOptions: &types.SpotOptionsRequest{
			AllocationStrategy:     types.SpotAllocationStrategyPriceCapacityOptimized,
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
		panic(fmt.Sprintf("Unexpected instance count: %v",
			len(runOutput.Instances)))
	}
	if len(runOutput.Instances[0].InstanceIds) != 1 {
		panic(fmt.Sprintf("Unexpected instanceId count: %v",
			len(runOutput.Instances[0].InstanceIds)))
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
	awsCfgIn aws.Config) ([]LaunchEc2SpotResult, error) {

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
			resultsOneRegion, err := lookupEc2SpotOneRegion(awsCfgTmp)
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

func lookupEc2SpotOneRegion(awsCfg aws.Config) ([]LaunchEc2SpotResult, error) {

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
	for _, resv := range descOutput.Reservations {
		for _, inst := range resv.Instances {
			if inst.State.Name != types.InstanceStateNameRunning {
				continue
			}
			foundSpotShTag = false
			for _, tag := range inst.Tags {
				if *tag.Key == UserTagKey {
					foundSpotShTag = true
					user = *tag.Value
				} else if *tag.Key == OsTagKey {
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
