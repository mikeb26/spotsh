/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/mikeb26/spotsh/internal/aws"
)

func main() {
	launchArgs := &aws.LaunchEc2SpotArgs{}
	ctx := context.Background()

	launchResult, err := aws.LookupEc2Spot(ctx)
	if err != nil {
		launchResult, err = aws.LaunchEc2Spot(ctx, launchArgs)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to lookup/launch instance: %v\n", err)
		os.Exit(1)
	}

	keyFile, err := aws.GetLocalKeyFile(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find local key: %v\n", err)
		os.Exit(1)
	}
	sshArgs := []string{"ssh", "-i", keyFile, "-o", "StrictHostKeyChecking no",
		launchResult.User + "@" + launchResult.PublicIp}
	fmt.Printf("exec %v\n", sshArgs)

	err = syscall.Exec("/usr/bin/ssh", sshArgs, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to ssh: %v\n", err)
		os.Exit(1)
	}
}
