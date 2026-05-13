/* Copyright © 2022-2024 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
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
	spotsh.Debian13: {
		os:       spotsh.Debian13,
		desc:     "Debian GNU/Linux 13",
		ssmParam: "/aws/service/debian/release/13/latest/amd64",
		user:     "admin",
	},
	spotsh.Ubuntu26_04: {
		os:       spotsh.Ubuntu26_04,
		desc:     "Ubuntu 26.04 LTS",
		ssmParam: "/aws/service/canonical/ubuntu/server/26.04/stable/current/amd64/hvm/ebs-gp3/ami-id",
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

func GetAmiUser(awsCfg aws.Config, amiId string, amiName string) (string, error) {
	ec2Client := ec2.NewFromConfig(awsCfg)
	if amiName != "" {
		if amiId != "" {
			return "", fmt.Errorf("Ami id and ami name are mutually exclusive; please specify one or the other")
		}

		var err error
		amiId, err = getAmiIdFromName(awsCfg, ec2Client, amiName)
		if err != nil {
			return "", err
		}
	}
	if amiId == "" {
		return "", nil
	}

	image, err := describeSingleImage(context.Background(), ec2Client, amiId)
	if err != nil {
		return "", err
	}

	return getTagValueFromTags(image.Tags, DefaultTagPrefix+"."+UserTagSuffix)
}

func describeSingleImage(ctx context.Context, ec2Client *ec2.Client,
	amiId string) (*types.Image, error) {

	dryRun := false
	descInput := &ec2.DescribeImagesInput{
		DryRun:   &dryRun,
		ImageIds: []string{amiId},
	}
	descOutput, err := ec2Client.DescribeImages(ctx, descInput)
	if err != nil {
		return nil, err
	}
	if len(descOutput.Images) != 1 {
		return nil, fmt.Errorf("Unexpected image count returned(%v) for %v description",
			len(descOutput.Images), amiId)
	}

	return &descOutput.Images[0], nil
}

func getTagValueFromTags(tags []types.Tag, tagKey string) (string, error) {
	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		if *tag.Key == tagKey {
			return *tag.Value, nil
		}
	}

	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		if strings.HasSuffix(*tag.Key, "."+UserTagSuffix) {
			return *tag.Value, nil
		}
	}

	return "", nil
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
	ctx := context.Background()

	imageTags, err := getImageTagsFromInstance(ctx, ec2Client, instanceId)
	if err != nil {
		return "", err
	}

	input := &ec2.CreateImageInput{
		InstanceId: aws.String(instanceId),
	}
	if len(imageTags) > 0 {
		input.TagSpecifications = []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeImage,
				Tags:         imageTags,
			},
		}
	}
	if name != "" {
		input.Name = aws.String(name)
	}
	if desc != "" {
		input.Description = aws.String(desc)
	}

	result, err := ec2Client.CreateImage(ctx, input)
	if err != nil {
		return "", err
	}

	return *result.ImageId, nil
}

func getImageTagsFromInstance(ctx context.Context, ec2Client *ec2.Client,
	instanceId string) ([]types.Tag, error) {

	dryRun := false
	descInput := &ec2.DescribeInstancesInput{
		DryRun:      &dryRun,
		InstanceIds: []string{instanceId},
	}
	descOutput, err := ec2Client.DescribeInstances(ctx, descInput)
	if err != nil {
		return nil, err
	}
	if len(descOutput.Reservations) != 1 ||
		len(descOutput.Reservations[0].Instances) != 1 {
		return nil, fmt.Errorf("Unexpected instance count returned for %v description",
			instanceId)
	}

	return getSpotshImageTags(descOutput.Reservations[0].Instances[0].Tags), nil
}

func getSpotshImageTags(tags []types.Tag) []types.Tag {
	userTagKey := DefaultTagPrefix + "." + UserTagSuffix
	osTagKey := DefaultTagPrefix + "." + OsTagSuffix
	ret := make([]types.Tag, 0, 2)

	for _, tagKey := range []string{userTagKey, osTagKey} {
		if tagValue, ok := getExactTagValueFromTags(tags, tagKey); ok {
			ret = append(ret, types.Tag{
				Key:   aws.String(tagKey),
				Value: aws.String(tagValue),
			})
		}
	}
	if len(ret) > 0 {
		return ret
	}

	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		if strings.HasSuffix(*tag.Key, "."+UserTagSuffix) ||
			strings.HasSuffix(*tag.Key, "."+OsTagSuffix) {
			ret = append(ret, types.Tag{
				Key:   aws.String(*tag.Key),
				Value: aws.String(*tag.Value),
			})
		}
	}

	return ret
}

func getExactTagValueFromTags(tags []types.Tag, tagKey string) (string, bool) {
	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		if *tag.Key == tagKey {
			return *tag.Value, true
		}
	}

	return "", false
}
