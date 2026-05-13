/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"net"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestSshPermissionCoversIP(t *testing.T) {
	tests := []struct {
		name string
		perm ec2types.IpPermission
		ip   string
		want bool
	}{
		{
			name: "ipv4 covered by broader ssh cidr",
			perm: ec2types.IpPermission{
				IpProtocol: awssdk.String("tcp"),
				FromPort:   awssdk.Int32(22),
				ToPort:     awssdk.Int32(22),
				IpRanges: []ec2types.IpRange{
					{CidrIp: awssdk.String("203.0.113.0/24")},
				},
			},
			ip:   "203.0.113.42",
			want: true,
		},
		{
			name: "ipv4 not covered by ssh cidr",
			perm: ec2types.IpPermission{
				IpProtocol: awssdk.String("tcp"),
				FromPort:   awssdk.Int32(22),
				ToPort:     awssdk.Int32(22),
				IpRanges: []ec2types.IpRange{
					{CidrIp: awssdk.String("203.0.113.0/24")},
				},
			},
			ip:   "198.51.100.42",
			want: false,
		},
		{
			name: "non-ssh cidr does not count",
			perm: ec2types.IpPermission{
				IpProtocol: awssdk.String("tcp"),
				FromPort:   awssdk.Int32(80),
				ToPort:     awssdk.Int32(80),
				IpRanges: []ec2types.IpRange{
					{CidrIp: awssdk.String("203.0.113.0/24")},
				},
			},
			ip:   "203.0.113.42",
			want: false,
		},
		{
			name: "port range containing ssh counts",
			perm: ec2types.IpPermission{
				IpProtocol: awssdk.String("tcp"),
				FromPort:   awssdk.Int32(20),
				ToPort:     awssdk.Int32(30),
				IpRanges: []ec2types.IpRange{
					{CidrIp: awssdk.String("203.0.113.0/24")},
				},
			},
			ip:   "203.0.113.42",
			want: true,
		},
		{
			name: "all protocols containing cidr counts",
			perm: ec2types.IpPermission{
				IpProtocol: awssdk.String("-1"),
				IpRanges: []ec2types.IpRange{
					{CidrIp: awssdk.String("203.0.113.0/24")},
				},
			},
			ip:   "203.0.113.42",
			want: true,
		},
		{
			name: "ipv6 covered by ssh cidr",
			perm: ec2types.IpPermission{
				IpProtocol: awssdk.String("tcp"),
				FromPort:   awssdk.Int32(22),
				ToPort:     awssdk.Int32(22),
				Ipv6Ranges: []ec2types.Ipv6Range{
					{CidrIpv6: awssdk.String("2001:db8::/64")},
				},
			},
			ip:   "2001:db8::1",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse test IP %q", tt.ip)
			}
			if got := sshPermissionCoversIP(tt.perm, ip); got != tt.want {
				t.Fatalf("sshPermissionCoversIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
