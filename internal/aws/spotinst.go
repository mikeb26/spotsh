/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/mikeb26/spotsh/internal"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const DefaultRootVolSizeInGiB = int32(64)
const DefaultMaxSpotPrice = "0.08"

var DefaultInstanceTypes = []types.InstanceType{types.InstanceTypeC5aLarge,
	types.InstanceTypeC5Large,
	types.InstanceTypeC6iLarge,
	types.InstanceTypeC6aLarge,
}

const DefaultOperatingSystem = internal.AmazonLinux2

type LaunchEc2SpotArgs struct {
	Os               internal.OperatingSystem // optional; defaults to AmazonLinux2
	AmiId            string                   // optional; overrides Os; defaults to latest ami for specified Os
	KeyPair          string                   // optional; defaults to spotinst keypair
	SecurityGroupId  string                   // optional; defaults to default VPC's default SG
	AttachRoleName   string                   // optional; defaults to no attached role
	InitCmd          string                   // optional; defaults to empty
	InstanceTypes    []types.InstanceType     // optional; defaults to c5a.large
	MaxSpotPrice     string                   // optional; defaults to "0.08" (USD$/hour)
	User             string                   // optional; defaults to Os's default user
	RootVolSizeInGiB int32                    // optional; defaults to 64GiB
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
}

func LaunchEc2Spot(awsCfg aws.Config,
	launchArgs *LaunchEc2SpotArgs) (LaunchEc2SpotResult, error) {

	if launchArgs == nil {
		launchArgs = &LaunchEc2SpotArgs{}
	}

	var launchResult LaunchEc2SpotResult
	ec2Client := ec2.NewFromConfig(awsCfg)

	spotPrice := launchArgs.MaxSpotPrice
	if spotPrice == "" {
		spotPrice = DefaultMaxSpotPrice
	}
	spotOpts := &types.SpotMarketOptions{
		InstanceInterruptionBehavior: types.InstanceInterruptionBehaviorTerminate,
		MaxPrice:                     &spotPrice,
		SpotInstanceType:             types.SpotInstanceTypeOneTime,
	}
	marketOpts := &types.InstanceMarketOptionsRequest{
		MarketType:  types.MarketTypeSpot,
		SpotOptions: spotOpts,
	}

	iamOpts := &types.IamInstanceProfileSpecification{}
	if launchArgs.AttachRoleName != "" {
		iamOpts.Name = &launchArgs.AttachRoleName
	} else {
		iamOpts = nil
	}
	maxCount := int32(1)
	minCount := int32(1)

	ctx := context.Background()
	var keyName *string
	if launchArgs.KeyPair != "" {
		keyName = &launchArgs.KeyPair
	} else {
		haveDefaultKey, err := haveDefaultKeyPair(ctx, awsCfg)
		if err != nil {
			return launchResult, err
		}
		if !haveDefaultKey {
			err = createDefaultKeyPair(ctx, awsCfg, ec2Client)
			if err != nil {
				return launchResult, err
			}
		}
		keyPair := GetDefaultKeyName(awsCfg)
		keyName = &keyPair
	}
	keysResult, err := LookupKeys(awsCfg)
	if err != nil {
		return launchResult, err
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
	if amiId == "" {
		if launchArgs.Os == internal.OsNone {
			launchArgs.Os = DefaultOperatingSystem
		}
		idx := int(launchArgs.Os)
		launchResult.User = imageIdTab[idx].user
		amiId, err = getLatestAmiId(ctx, awsCfg, launchArgs.Os)
		if err != nil {
			return launchResult, err
		}
	} else if launchArgs.User == "" {
		return launchResult, fmt.Errorf("User must be specified when ami id is specified")
	} else {
		launchResult.User = launchArgs.User
	}
	sgId := launchArgs.SecurityGroupId
	if sgId == "" {
		sgId, err = getDefaultSecurityGroupId(awsCfg, ec2Client)
		if err != nil {
			return launchResult, err
		}
	}
	tagKey := defaultTagKey
	tagVal := launchResult.User
	tag := types.Tag{
		Key:   &tagKey,
		Value: &tagVal,
	}
	tagSpec := types.TagSpecification{
		ResourceType: types.ResourceTypeInstance,
		Tags:         []types.Tag{tag},
	}
	rootVolSize := launchArgs.RootVolSizeInGiB
	rootVolName, err := getRootVolName(ctx, ec2Client, amiId)
	if err != nil {
		return launchResult, err
	}
	if rootVolSize == 0 {
		rootVolSize = DefaultRootVolSizeInGiB
	}
	rootBlockMap := types.BlockDeviceMapping{
		DeviceName: &rootVolName,
		Ebs: &types.EbsBlockDevice{
			VolumeSize: &rootVolSize,
		},
	}
	if len(launchArgs.InstanceTypes) == 0 {
		launchArgs.InstanceTypes = DefaultInstanceTypes
	}
	spotPriceResult, err := LookupEc2SpotPrices(awsCfg, launchArgs.InstanceTypes)
	if err != nil {
		return launchResult, err
	}
	iType := spotPriceResult.CheapestIType.InstanceType
	launchResult.InstanceType = iType
	cheapestAz := spotPriceResult.CheapestIType.CheapestRegion.CheapestAz.AzName
	subnetId, err := getSubnetIdFromAzName(ec2Client, cheapestAz)
	if err != nil {
		return launchResult, err
	}
	runInput := &ec2.RunInstancesInput{
		MaxCount:                          &maxCount,
		MinCount:                          &minCount,
		IamInstanceProfile:                iamOpts,
		ImageId:                           &amiId,
		InstanceInitiatedShutdownBehavior: types.ShutdownBehaviorTerminate,
		InstanceMarketOptions:             marketOpts,
		InstanceType:                      iType,
		KeyName:                           keyName,
		SecurityGroupIds:                  []string{sgId},
		UserData:                          initCmdEncoded,
		TagSpecifications:                 []types.TagSpecification{tagSpec},
		SubnetId:                          &subnetId,
		BlockDeviceMappings:               []types.BlockDeviceMapping{rootBlockMap},
	}
	runOutput, err := ec2Client.RunInstances(ctx, runInput)
	if err != nil {
		return launchResult, err
	}

	if len(runOutput.Instances) != 1 {
		panic(fmt.Sprintf("Unexpected instance count: %v", len(runOutput.Instances)))
	}

	instanceId := *runOutput.Instances[0].InstanceId
	launchResult.InstanceId = instanceId

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

	return launchResult, nil
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

func LookupEc2Spot(awsCfg aws.Config) ([]LaunchEc2SpotResult, error) {

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

	for _, resv := range descOutput.Reservations {
		for _, inst := range resv.Instances {
			for _, tag := range inst.Tags {
				if inst.State.Name != types.InstanceStateNameRunning ||
					*tag.Key != defaultTagKey {
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
					User:         *tag.Value,
					LocalKeyFile: localKeyFile,
					InstanceType: inst.InstanceType,
					ImageId:      *inst.ImageId,
					AzName:       azName,
					CurrentPrice: 0.00,
				}

				launchResults = append(launchResults, launchResult)
			}
		}
	}

	if len(iTypes) == 0 {
		return launchResults, nil
	}

	spotPriceResult, err := LookupEc2SpotPrices(awsCfg, iTypes)
	if err != nil {
		return launchResults, err
	}

	for idx, _ := range launchResults {
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
