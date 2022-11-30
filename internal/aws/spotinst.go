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
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const DefaultMaxSpotPrice = "0.08"
const DefaultInstanceType = types.InstanceTypeC5aLarge
const DefaultOperatingSystem = internal.AmazonLinux2

type imageIdEntry struct {
	os       internal.OperatingSystem
	desc     string
	ssmParam string
	user     string
}

var imageIdTab = []imageIdEntry{
	internal.OsNone: imageIdEntry{},
	internal.Ubuntu22_04: imageIdEntry{
		os:       internal.Ubuntu22_04,
		desc:     "Ubuntu 22.04 LTS",
		ssmParam: "/aws/service/canonical/ubuntu/server/22.04/stable/current/amd64/hvm/ebs-gp2/ami-id",
		user:     "ubuntu",
	},
	internal.AmazonLinux2: imageIdEntry{
		os:       internal.AmazonLinux2,
		desc:     "Amazon Linux 2",
		ssmParam: "/aws/service/ami-amazon-linux-latest/amzn2-ami-hvm-x86_64-gp2",
		user:     "ec2-user",
	},
}

func GetImageDesc(os internal.OperatingSystem) string {
	idx := uint64(os)
	if os == internal.OsNone || os >= internal.OsInvalid {
		idx = uint64(DefaultOperatingSystem)
	}

	return imageIdTab[idx].desc
}

func getLatestAmiId(ctx context.Context, awsCfg aws.Config,
	os internal.OperatingSystem) (string, error) {

	if os == internal.OsNone {
		return "", fmt.Errorf("Must specify os type to determine latest ami")
	}
	idx := uint64(os)
	if idx >= uint64(internal.OsInvalid) {
		return "", fmt.Errorf("No such os index %v", idx)
	}
	idEntry := &imageIdTab[idx]

	ssmClient := ssm.NewFromConfig(awsCfg)
	getParamInput := &ssm.GetParameterInput{
		Name: &idEntry.ssmParam,
	}
	getParamOutput, err := ssmClient.GetParameter(ctx, getParamInput)
	if err != nil {
		return "", err
	}

	return *getParamOutput.Parameter.Value, nil
}

type LaunchEc2SpotArgs struct {
	Os              internal.OperatingSystem // optional; defaults to AmazonLinux2
	AmiId           string                   // optional; overrides Os; defaults to latest ami for specified Os
	KeyPair         string                   // optional; defaults to spotinst keypair
	SecurityGroupId string                   // optional; defaults to default VPC's default SG
	AttachRoleName  string                   // optional; defaults to no attached role
	InitCmd         string                   // optional; defaults to empty
	InstanceType    types.InstanceType       // optional; defaults to c5a.large
	MaxSpotPrice    string                   // optional; defaults to "0.08" (USD$/hour)
	User            string                   // optional; defaults to Os's default user
}

type LaunchEc2SpotResult struct {
	PublicIp     string
	InstanceId   string
	User         string
	LocalKeyFile string
	InstanceType types.InstanceType
}

func LaunchEc2Spot(ctx context.Context,
	launchArgs *LaunchEc2SpotArgs) (LaunchEc2SpotResult, error) {

	if launchArgs == nil {
		launchArgs = &LaunchEc2SpotArgs{}
	}

	var launchResult LaunchEc2SpotResult
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return launchResult, err
	}
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
		keyPair := getKeyName(awsCfg)
		keyName = &keyPair
	}
	keysResult, err := LookupKeys(ctx)
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
		amiId, err = getLatestAmiId(ctx, awsCfg, launchArgs.Os)
		if err != nil {
			return launchResult, err
		}
	} else if launchArgs.User == "" {
		return launchResult, fmt.Errorf("User must be specified when ami id is specified")
	}
	sgId := launchArgs.SecurityGroupId
	if sgId == "" {
		sgId, err = getDefaultSecurityGroupId(ctx, ec2Client)
		if err != nil {
			return launchResult, err
		}
	}
	iType := launchArgs.InstanceType
	if iType == "" {
		iType = DefaultInstanceType
	}
	launchResult.InstanceType = iType
	if launchArgs.Os != internal.OsNone {
		idx := int(launchArgs.Os)
		launchResult.User = imageIdTab[idx].user
	} else {
		launchResult.User = launchArgs.User
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

func TerminateInstance(ctx context.Context, instanceId string) error {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	ec2Client := ec2.NewFromConfig(awsCfg)

	dryRun := false
	termInput := &ec2.TerminateInstancesInput{
		InstanceIds: []string{instanceId},
		DryRun:      &dryRun,
	}
	_, err = ec2Client.TerminateInstances(ctx, termInput)
	if err != nil {
		return err
	}

	return nil
}

func LookupEc2Spot(ctx context.Context) ([]LaunchEc2SpotResult, error) {

	launchResults := make([]LaunchEc2SpotResult, 0)

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return launchResults, err
	}
	ec2Client := ec2.NewFromConfig(awsCfg)
	dryRun := false
	maxResults := int32(1000)
	describeInput := &ec2.DescribeInstancesInput{
		DryRun:     &dryRun,
		MaxResults: &maxResults,
	}
	descOutput, err := ec2Client.DescribeInstances(ctx, describeInput)
	if err != nil {
		return launchResults, err
	}
	keysResult, err := LookupKeys(ctx)
	if err != nil {
		return launchResults, err
	}

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
				}

				launchResults = append(launchResults, launchResult)
			}
		}
	}

	return launchResults, nil
}
