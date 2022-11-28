/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"syscall"

	"github.com/mikeb26/spotsh/internal/aws"
)

var subCommandTab = map[string]func(args []string) error{
	"help":      helpMain,
	"info":      infoMain,
	"launch":    launchMain,
	"ssh":       sshMain,
	"terminate": terminateMain,
	"version":   versionMain,
}

type cmdOpts struct {
	keyName      string
	keyFile      string
	instanceType string
	spotPrice    float32
	os           string
	amiId        string
	sgId         string
	role         string
	initCmd      string
	instanceId   string
	user         string
}

//go:embed help.txt
var helpText string

func helpMain(args []string) error {
	fmt.Printf(helpText)

	return nil
}

//go:embed version.txt
var versionText string

const DevVersionText = "v0.devbuild"

func versionMain(args []string) error {
	fmt.Printf("spotsh-%v\n", versionText)

	return nil
}

func infoMain(args []string) error {
	ctx := context.Background()
	launchResults, err := aws.LookupEc2Spot(ctx)
	if err != nil {
		return fmt.Errorf("Failed to lookup instance: %w", err)
	}

	if len(launchResults) == 0 {
		fmt.Printf("No spot shell instances running\n")
	} else {
		fmt.Printf("Spot shell instances:\n")
		for idx, lr := range launchResults {
			fmt.Printf("\tInstance[%v]:\n", idx)
			fmt.Printf("\t\tId: %v\n\t\tPublicIp: %v\n\t\tUser: %v\n",
				lr.InstanceId, lr.PublicIp, lr.User)
		}
	}

	vpcSgResults, err := aws.LookupVpcSecurityGroups(ctx)
	if err != nil {
		return fmt.Errorf("Failed to lookup security groups: %w", err)
	}
	fmt.Printf("Vpcs:\n")
	idx := 0
	for vpcId, vpc := range vpcSgResults.Vpcs {
		fmt.Printf("\tVpc[%v]:\n", idx)
		fmt.Printf("\t\tId: %v\n", vpcId)
		fmt.Printf("\t\tDefault: %v\n", vpc.Default)
		fmt.Printf("\t\tSecurityGroups:\n")
		idx2 := 0
		for sgId, sg := range vpc.Sgs {
			fmt.Printf("\t\t\tSG[%v]:\n", idx2)
			fmt.Printf("\t\t\t\tId: %v\n", sgId)
			fmt.Printf("\t\t\t\tName: %v\n", sg.Name)
			idx2++
		}
		idx++
	}

	keyResults, err := aws.LookupKeys(ctx)
	if err != nil {
		return fmt.Errorf("Failed to lookup keys: %w", err)
	}
	fmt.Printf("Keys:\n")
	idx = 0
	for keyId, key := range keyResults.Keys {
		fmt.Printf("\tKey[%v]:\n", idx)
		fmt.Printf("\t\tId: %v\n", keyId)
		fmt.Printf("\t\tName: %v\n", key.Name)
		if key.LocalKeyFile != "" {
			fmt.Printf("\t\tLocal: %v\n", key.LocalKeyFile)
		}
		idx++
	}

	return nil
}

func launchMain(args []string) error {
	ctx := context.Background()

	var launchArgs aws.LaunchEc2SpotArgs

	launchResult, err := aws.LaunchEc2Spot(ctx, &launchArgs)
	if err != nil {
		return err
	}
	fmt.Printf("Launched %v (%v@%v)\n", launchResult.InstanceId,
		launchResult.User, launchResult.PublicIp)

	return nil
}

func terminateMain(args []string) error {
	termOpts := struct {
		instanceId string
	}{}

	f := flag.NewFlagSet("spotsh terminate", flag.ContinueOnError)
	f.StringVar(&termOpts.instanceId, "instance-id", "", "EC2 instance id")
	err := f.Parse(args)
	if err != nil {
		return err
	}

	ctx := context.Background()
	launchResults, err := aws.LookupEc2Spot(ctx)
	if err != nil {
		return fmt.Errorf("Failed to lookup instance: %w", err)
	}

	if len(launchResults) > 1 && termOpts.instanceId == "" {
		errStr := "Multiple spotsh instances found; please disambiguate w/ --instance-id:"
		for _, lr := range launchResults {
			errStr = fmt.Sprintf("%v\n\t%v:%v", errStr, lr.InstanceId,
				lr.PublicIp)
		}
		return fmt.Errorf("%v", errStr)
	}

	var selectedResult *aws.LaunchEc2SpotResult
	for idx, lr := range launchResults {
		if termOpts.instanceId == "" || termOpts.instanceId == lr.InstanceId {
			selectedResult = &launchResults[idx]
			break
		}
	}

	if selectedResult == nil {
		if termOpts.instanceId == "" {
			return fmt.Errorf("No spotssh instances running")
		} // else
		return fmt.Errorf("Could not find spotssh instance w/ id %v",
			termOpts.instanceId)
	}

	return aws.TerminateInstance(ctx, selectedResult.InstanceId)
}

func sshMain(args []string) error {
	return sshCommon(false, args)
}

func sshCommon(canLaunch bool, args []string) error {
	sshOpts := struct {
		instanceId string
	}{}

	f := flag.NewFlagSet("spotsh ssh", flag.ContinueOnError)
	f.StringVar(&sshOpts.instanceId, "instance-id", "", "EC2 instance id")
	err := f.Parse(args)
	if err != nil {
		return err
	}

	ctx := context.Background()

	launchResults, err := aws.LookupEc2Spot(ctx)
	if err == nil && len(launchResults) == 0 {
		if canLaunch {
			var launchArgs aws.LaunchEc2SpotArgs
			var newLaunchResult aws.LaunchEc2SpotResult

			newLaunchResult, err = aws.LaunchEc2Spot(ctx, &launchArgs)
			launchResults = append(launchResults, newLaunchResult)
		} else {
			err = fmt.Errorf("No spotssh instances running")
		}
	}
	if err != nil {
		return fmt.Errorf("Failed to lookup/launch instance: %w", err)
	}

	if len(launchResults) > 1 && sshOpts.instanceId == "" {
		errStr := "Multiple spotsh instances found; please disambiguate w/ --instance-id:"
		for _, lr := range launchResults {
			errStr = fmt.Sprintf("%v\n\t%v:%v", errStr, lr.InstanceId,
				lr.PublicIp)
		}
		return fmt.Errorf("%v", errStr)
	}

	var selectedResult *aws.LaunchEc2SpotResult
	for idx, lr := range launchResults {
		if sshOpts.instanceId == "" || sshOpts.instanceId == lr.InstanceId {
			selectedResult = &launchResults[idx]
			break
		}
	}

	if selectedResult == nil {
		return fmt.Errorf("Could not find spotssh instance w/ id %v",
			sshOpts.instanceId)
	}

	keyFile, err := aws.GetLocalDefaultKeyFile(ctx)
	if err != nil {
		return fmt.Errorf("Failed to find local key: %w\n", err)
	}
	sshArgs := []string{"ssh", "-i", keyFile, "-o", "StrictHostKeyChecking no",
		selectedResult.User + "@" + selectedResult.PublicIp}
	fmt.Printf("exec %v\n", sshArgs)

	err = syscall.Exec("/usr/bin/ssh", sshArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to ssh: %w\n", err)
	}

	return nil
}

func main() {
	subCommandName := ""
	if len(os.Args) > 1 {
		subCommandName = os.Args[1]
	}
	exitStatus := 0
	var args []string
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	var err error
	if subCommandName == "" {
		err = sshCommon(true, args)
	} else {
		subCommand, ok := subCommandTab[subCommandName]
		if !ok {
			subCommand = helpMain
			exitStatus = 1
		}
		err = subCommand(args)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitStatus = 1
	}

	os.Exit(exitStatus)

}
