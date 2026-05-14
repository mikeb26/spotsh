/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/mikeb26/spotsh"
)

func TestInstanceIdTab(t *testing.T) {
	if len(imageIdTab) < int(spotsh.OsInvalid) {
		t.Fatalf("imageIdTab is missing OS entry")
	}

	for idx := 0; idx < int(spotsh.OsInvalid); idx++ {
		os := spotsh.OperatingSystem(idx)

		if imageIdTab[idx].os != os {
			t.Fatalf("imageIdTab entry mismatch expecting %v have %v", os,
				imageIdTab[idx].os)
		}
	}
}

func TestSsmParam(t *testing.T) {
	ctx := context.Background()
	awsCfg := loadAWSConfigOrSkip(t)

	for idx := int(spotsh.OsNone) + 1; idx < int(spotsh.OsInvalid); idx++ {
		os := spotsh.OperatingSystem(idx)

		amiId, err := getLatestAmiId(ctx, awsCfg, os)
		if err != nil {
			t.Fatalf("get latest ami for %v failed: %v", os, err)
		}
		if !strings.Contains(amiId, "ami-") {
			t.Fatalf("get latest ami for %v returned unexpected id: %v",
				os, amiId)
		}
	}
}

func TestLaunch(t *testing.T) {
	ctx := context.Background()
	awsCfg := loadAWSConfigOrSkip(t)

	launchResult, err := LaunchEc2Spot(ctx, awsCfg, nil)
	if err != nil {
		t.Fatalf("failed to launch spot instance: %v", err)
	}

	if !strings.Contains(launchResult.InstanceId, "i-") {
		t.Fatalf("launch returned unexpected instance id: %v",
			launchResult.InstanceId)
	}

	defer TerminateInstance(awsCfg, launchResult.InstanceId)

	if launchResult.PublicIp == "" {
		t.Fatalf("launch failed to return ip addr")
	}
	if launchResult.User != "ec2-user" {
		t.Fatalf("launch returned unexpected user: %v",
			launchResult.User)
	}
}

func TestBuildFleetLaunchTemplateOverridesFiltersAllowedAzs(t *testing.T) {
	region := "ap-southeast-5"
	iType := types.InstanceType("c8i.48xlarge")
	spotPriceResult := newTestSpotPriceResult(region, iType,
		map[string]float64{
			"ap-southeast-5b": 0.70,
			"ap-southeast-5c": 0.88,
		})

	overrides := buildFleetLaunchTemplateOverrides(region,
		[]types.InstanceType{iType}, []string{"ap-southeast-5c"}, spotPriceResult)

	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %v", len(overrides))
	}
	if overrides[0].InstanceType != iType {
		t.Fatalf("unexpected instance type: %v", overrides[0].InstanceType)
	}
	if overrides[0].AvailabilityZone == nil || *overrides[0].AvailabilityZone != "ap-southeast-5c" {
		t.Fatalf("unexpected availability zone: %v", overrides[0].AvailabilityZone)
	}
}

func TestBuildFleetLaunchTemplateOverridesSortsBySpotPrice(t *testing.T) {
	region := "ap-southeast-5"
	iType := types.InstanceType("c8i.48xlarge")
	spotPriceResult := newTestSpotPriceResult(region, iType,
		map[string]float64{
			"ap-southeast-5a": 0.90,
			"ap-southeast-5c": 0.88,
		})

	overrides := buildFleetLaunchTemplateOverrides(region,
		[]types.InstanceType{iType}, nil, spotPriceResult)

	if len(overrides) != 2 {
		t.Fatalf("expected 2 overrides, got %v", len(overrides))
	}
	if overrides[0].AvailabilityZone == nil || *overrides[0].AvailabilityZone != "ap-southeast-5c" {
		t.Fatalf("expected cheapest availability zone first, got %v", overrides[0].AvailabilityZone)
	}
}

func TestBuildFleetLaunchTemplateOverridesReturnsEmptyWhenAllowedAzHasNoSpotPrice(t *testing.T) {
	region := "ap-southeast-5"
	iType := types.InstanceType("c8i.48xlarge")
	spotPriceResult := newTestSpotPriceResult(region, iType,
		map[string]float64{"ap-southeast-5a": 0.90})

	overrides := buildFleetLaunchTemplateOverrides(region,
		[]types.InstanceType{iType}, []string{"ap-southeast-5c"}, spotPriceResult)

	if len(overrides) != 0 {
		t.Fatalf("expected no overrides, got %v", len(overrides))
	}
}

func newTestSpotPriceResult(region string, iType types.InstanceType,
	azPrices map[string]float64) *LookupEc2SpotPriceResult {

	lookupReg := &LookupEc2SpotPriceRegion{
		Region: region,
		Azs:    make(map[string]*LookupEc2SpotPriceAz),
	}
	for azName, price := range azPrices {
		lookupReg.Azs[azName] = &LookupEc2SpotPriceAz{
			AzName:   azName,
			CurPrice: price,
		}
	}

	return &LookupEc2SpotPriceResult{
		InstanceTypes: map[types.InstanceType]*LookupEc2SpotPriceIType{
			iType: {
				InstanceType: iType,
				Regions: map[string]*LookupEc2SpotPriceRegion{
					region: lookupReg,
				},
			},
		},
	}
}
