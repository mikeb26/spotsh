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
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/mikeb26/spotsh/internal"
)

type imageIdEntry struct {
	os       internal.OperatingSystem
	desc     string
	ssmParam string
	user     string
}

var imageIdTab = []imageIdEntry{
	internal.OsNone: {},
	internal.Ubuntu22_04: {
		os:       internal.Ubuntu22_04,
		desc:     "Ubuntu 22.04 LTS",
		ssmParam: "/aws/service/canonical/ubuntu/server/22.04/stable/current/amd64/hvm/ebs-gp2/ami-id",
		user:     "ubuntu",
	},
	internal.AmazonLinux2: {
		os:       internal.AmazonLinux2,
		desc:     "Amazon Linux 2",
		ssmParam: "/aws/service/ami-amazon-linux-latest/amzn2-ami-hvm-x86_64-gp2",
		user:     "ec2-user",
	},
	internal.AmazonLinux2023: {
		os:       internal.AmazonLinux2023,
		desc:     "Amazon Linux 2023 (standard)",
		ssmParam: "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64",
		user:     "ec2-user",
	},
	internal.AmazonLinux2023Min: {
		os:       internal.AmazonLinux2023Min,
		desc:     "Amazon Linux 2023 (minimal)",
		ssmParam: "/aws/service/ami-amazon-linux-latest/al2023-ami-minimal-kernel-default-x86_64",
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

func getRootVolName(ctx context.Context, ec2Client *ec2.Client,
	amiId string) (string, error) {

	dryRun := false
	descInput := &ec2.DescribeImagesInput{
		DryRun:   &dryRun,
		ImageIds: []string{amiId},
	}

	descOutput, err := ec2Client.DescribeImages(ctx, descInput)
	if err != nil {
		return "", err
	}

	if len(descOutput.Images) != 1 {
		return "", fmt.Errorf("Unexpected image count returned(%v) for %v description",
			len(descOutput.Images), amiId)
	}

	return *descOutput.Images[0].RootDeviceName, nil
}

func getAmiIdFromName(awsCfg aws.Config, ec2Client *ec2.Client,
	amiName string) (string, error) {

	lookupImagesResult, err := lookupImagesCommon(awsCfg, ec2Client)
	if err != nil {
		return "", err
	}

	for _, imgDesc := range lookupImagesResult.Images {
		if imgDesc.Name == amiName {
			return imgDesc.Id, nil
		}
	}

	return "", fmt.Errorf("Could not find ami id for %v", amiName)
}

type LookupImageItem struct {
	Id        string
	Name      string
	Ownership string
}

type LookupImagesResult struct {
	Images map[string]*LookupImageItem
}

func LookupImages(awsCfg aws.Config) (LookupImagesResult, error) {
	ec2Client := ec2.NewFromConfig(awsCfg)

	return lookupImagesCommon(awsCfg, ec2Client)
}

func lookupImagesCommon(awsCfg aws.Config,
	ec2Client *ec2.Client) (LookupImagesResult, error) {

	lookupImagesResult := LookupImagesResult{
		Images: make(map[string]*LookupImageItem),
	}

	dryRun := false
	descInput := &ec2.DescribeImagesInput{
		DryRun: &dryRun,
		Owners: []string{"self"},
	}

	ctx := context.Background()
	descOutput, err := ec2Client.DescribeImages(ctx, descInput)
	if err != nil {
		return lookupImagesResult, err
	}

	for _, imgDesc := range descOutput.Images {
		lookupImageItem := &LookupImageItem{
			Name:      *imgDesc.Name,
			Id:        *imgDesc.ImageId,
			Ownership: "self",
		}

		lookupImagesResult.Images[lookupImageItem.Id] = lookupImageItem
	}

	var osv internal.OperatingSystem
	amiIds := make([]string, 0)

	for _, os := range osv.Values() {
		amiId, err := getLatestAmiId(ctx, awsCfg, os)
		if err != nil {
			return lookupImagesResult, err
		}
		amiIds = append(amiIds, amiId)
	}
	descInput = nil
	descInput = &ec2.DescribeImagesInput{
		DryRun:   &dryRun,
		ImageIds: amiIds,
	}
	descOutput = nil
	descOutput, err = ec2Client.DescribeImages(ctx, descInput)
	if err != nil {
		return lookupImagesResult, err
	}
	for _, imgDesc := range descOutput.Images {
		lookupImageItem := &LookupImageItem{
			Name:      *imgDesc.Name,
			Id:        *imgDesc.ImageId,
			Ownership: "aws",
		}

		lookupImagesResult.Images[lookupImageItem.Id] = lookupImageItem
	}

	return lookupImagesResult, nil
}
