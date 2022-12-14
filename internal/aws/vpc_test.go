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
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func TestGetDefaultSecurityGroupId(t *testing.T) {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("failed to init aws config: %v", err)
	}

	ec2Client := ec2.NewFromConfig(awsCfg)
	sgId, err := getDefaultSecurityGroupId(awsCfg, ec2Client)
	if err != nil {
		t.Fatalf("failed to get default security group id: %v", err)
	}
	if !strings.Contains(sgId, "sg-") {
		t.Fatalf("get default security group id returned unexpected id: %v",
			sgId)
	}
}

func TestLookupVpcSecurityGroups(t *testing.T) {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("failed to init aws config: %v", err)
	}
	result, err := LookupVpcSecurityGroups(awsCfg)
	if err != nil {
		t.Fatalf("failed to lookup security groups: %v", err)
	}

	for vpcId, vpc := range result.Vpcs {
		if vpcId != vpc.Id {
			t.Errorf("Unexpected VpcId %v vs %v", vpcId, vpc.Id)
		}
		if !strings.Contains(vpcId, "vpc-") {
			t.Fatalf("lookup returned unexpected vpc id: %v", vpcId)
		}
		for sgId, sg := range vpc.Sgs {
			if sgId != sg.Id {
				t.Errorf("vpc %v: Unexpected SgId %v vs %v", vpcId, sgId, sg.Id)
			}
			if !strings.Contains(sgId, "sg-") {
				t.Fatalf("lookup returned unexpected sg id id: %v", sgId)
			}
		}
	}
}
