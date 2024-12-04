/* Copyright © 2022-2024 Mike Brown. All Rights Reserved.
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

	"github.com/mikeb26/spotsh"
)

type imageIdEntry struct {
	os       spotsh.OperatingSystem
	desc     string
	ssmParam string
	user     string
}

var imageIdTab = []imageIdEntry{
	spotsh.OsNone: {},
	spotsh.Ubuntu22_04: {
		os:       spotsh.Ubuntu22_04,
		desc:     "Ubuntu 22.04 LTS",
		ssmParam: "/aws/service/canonical/ubuntu/server/22.04/stable/current/amd64/hvm/ebs-gp2/ami-id",
		user:     "ubuntu",
	},
	spotsh.AmazonLinux2: {
		os:       spotsh.AmazonLinux2,
		desc:     "Amazon Linux 2",
		ssmParam: "/aws/service/ami-amazon-linux-latest/amzn2-ami-hvm-x86_64-gp2",
		user:     "ec2-user",
	},
	spotsh.AmazonLinux2023: {
		os:       spotsh.AmazonLinux2023,
		desc:     "Amazon Linux 2023 (standard)",
		ssmParam: "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64",
		user:     "ec2-user",
	},
	spotsh.AmazonLinux2023Min: {
		os:       spotsh.AmazonLinux2023Min,
		desc:     "Amazon Linux 2023 (minimal)",
		ssmParam: "/aws/service/ami-amazon-linux-latest/al2023-ami-minimal-kernel-default-x86_64",
		user:     "ec2-user",
	},
	spotsh.Debian12: {
		os:       spotsh.Debian12,
		desc:     "Debian GNU/Linux 12",
		ssmParam: "/aws/service/debian/release/12/latest/amd64",
		user:     "admin",
	},
	spotsh.Ubuntu24_04: {
		os:       spotsh.Ubuntu24_04,
		desc:     "Ubuntu 24.04 LTS",
		ssmParam: "/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id",
		user:     "ubuntu",
	},
}

func GetImageDesc(os spotsh.OperatingSystem) string {
	idx := uint64(os)
	if os == spotsh.OsNone || os >= spotsh.OsInvalid {
		idx = uint64(DefaultOperatingSystem)
	}

	return imageIdTab[idx].desc
}

func getLatestAmiId(ctx context.Context, awsCfg aws.Config,
	os spotsh.OperatingSystem) (string, error) {

	if os == spotsh.OsNone {
		return "", fmt.Errorf("Must specify os type to determine latest ami")
	}
	idx := uint64(os)
	if idx >= uint64(spotsh.OsInvalid) {
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

	return lookupImagesResult, nil
}

func CreateImage(awsCfg aws.Config, instanceId string, name string,
	desc string) (string, error) {

	ec2Client := ec2.NewFromConfig(awsCfg)

	input := &ec2.CreateImageInput{
		InstanceId: aws.String(instanceId),
	}
	if name != "" {
		input.Name = aws.String(name)
	}
	if desc != "" {
		input.Description = aws.String(desc)
	}

	result, err := ec2Client.CreateImage(context.Background(), input)
	if err != nil {
		return "", err
	}

	return *result.ImageId, nil
}
