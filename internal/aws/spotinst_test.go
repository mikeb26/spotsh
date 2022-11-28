/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/mikeb26/spotsh/internal"
)

func TestInstanceIdTab(t *testing.T) {
	if len(instanceIdTab) < int(internal.OsInvalid) {
		t.Fatalf("instanceIdTab is missing OS entry")
	}

	for idx := 0; idx < int(internal.OsInvalid); idx++ {
		os := internal.OperatingSystem(idx)

		if instanceIdTab[idx].os != os {
			t.Fatalf("instanceIdTab entry mismatch expecting %v have %v", os,
				instanceIdTab[idx].os)
		}
	}
}

func TestSsmParam(t *testing.T) {
	ctx := context.Background()

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("failed to init aws config: %v", err)
	}

	for idx := int(internal.OsNone) + 1; idx < int(internal.OsInvalid); idx++ {
		os := internal.OperatingSystem(idx)

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

	launchResult, err := LaunchEc2Spot(ctx, nil)
	if err != nil {
		t.Fatalf("failed to launch spot instance: %v", err)
	}

	if !strings.Contains(launchResult.InstanceId, "i-") {
		t.Fatalf("launch returned unexpected instance id: %v",
			launchResult.InstanceId)
	}

	defer TerminateInstance(ctx, launchResult.InstanceId)

	if launchResult.PublicIp == "" {
		t.Fatalf("launch failed to return ip addr")
	}
	if launchResult.User != "ec2-user" {
		t.Fatalf("launch returned unexpected user: %v",
			launchResult.User)
	}
}