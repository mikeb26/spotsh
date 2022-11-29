/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/mikeb26/spotsh/internal"
	"github.com/mikeb26/spotsh/internal/aws"
)

var subCommandTab = map[string]func(args []string) error{
	"help":      helpMain,
	"info":      infoMain,
	"launch":    launchMain,
	"ssh":       sshMain,
	"terminate": terminateMain,
	"version":   versionMain,
	"upgrade":   upgradeMain,
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
			if lr.LocalKeyFile == "" {
				lr.LocalKeyFile = "<not present>"
			}
			fmt.Printf("\t\tType: %v\n", lr.InstanceType)
			fmt.Printf("\t\tLocalKeyFile: %v\n", lr.LocalKeyFile)
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
	launchArgs := &aws.LaunchEc2SpotArgs{}

	var os string
	var iType string

	f := flag.NewFlagSet("spotsh launch", flag.ContinueOnError)
	f.StringVar(&os, "os", "", "Operating System; e.g. amzn2")
	f.StringVar(&launchArgs.AmiId, "ami", "", "Amazon Machine Image id")
	f.StringVar(&launchArgs.User, "user", "", "username to ssh as")
	f.StringVar(&launchArgs.KeyPair, "key", "", "EC2 keypair")
	f.StringVar(&launchArgs.SecurityGroupId, "sgid", "", "Security Group Id")
	f.StringVar(&launchArgs.AttachRoleName, "role", "", "IAM Role to attach to instance")
	f.StringVar(&launchArgs.InitCmd, "initcmd", "", "Initial command to run in the instance")
	f.StringVar(&iType, "type", "", "Instance type")
	f.StringVar(&launchArgs.MaxSpotPrice, "spotprice", "", "Maximum spot price to pay")
	err := f.Parse(args)
	if err != nil {
		return err
	}

	launchArgs.Os = internal.OsFromString(os)
	launchArgs.InstanceType = types.InstanceType(iType)

	if launchArgs.AmiId != "" {
		if os != "" {
			return fmt.Errorf("--ami and --os are mutually exclusive; choose one but not both flags simultaneously")
		}
		if launchArgs.User == "" {
			return fmt.Errorf("--user must be specified when launching by AMI id so that spotsh knows which user to ssh as in the future")
		}
	} else {
		if launchArgs.User != "" {
			return fmt.Errorf("--user is automatically determined by default or when --os is specified")
		}
	}

	ctx := context.Background()

	launchResult, err := aws.LaunchEc2Spot(ctx, launchArgs)
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

	if selectedResult.LocalKeyFile == "" {
		return fmt.Errorf("Could not find local ssh key for instance w/ id %v",
			sshOpts.instanceId)
	}
	sshArgs := []string{"ssh", "-i", selectedResult.LocalKeyFile, "-o",
		"StrictHostKeyChecking=no",
		selectedResult.User + "@" + selectedResult.PublicIp}
	fmt.Printf("exec %v\n", sshArgs)

	err = syscall.Exec("/usr/bin/ssh", sshArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to ssh: %w\n", err)
	}

	return nil
}

func upgradeMain(args []string) error {
	if versionText == DevVersionText {
		fmt.Fprintf(os.Stderr, "Skipping spotsh upgrade on development version\n")
		return nil
	}
	latestVer, err := getLatestVersion()
	if err != nil {
		return err
	}
	if latestVer == versionText {
		fmt.Printf("spotsh %v is already the latest version\n",
			versionText)
		return nil
	}

	fmt.Printf("A new version of spotsh is available (%v). Upgrade? (Y/N) [Y]: ",
		latestVer)
	shouldUpgrade := "Y"
	fmt.Scanf("%s", &shouldUpgrade)
	shouldUpgrade = strings.ToUpper(strings.TrimSpace(shouldUpgrade))

	if shouldUpgrade[0] != 'Y' {
		return nil
	}

	fmt.Printf("Upgrading spotsh from %v to %v...\n", versionText,
		latestVer)

	return upgradeViaGithub(latestVer)
}

func getLatestVersion() (string, error) {
	const LatestReleaseUrl = "https://api.github.com/repos/mikeb26/spotsh/releases/latest"

	client := http.Client{
		Timeout: time.Second * 30,
	}

	resp, err := client.Get(LatestReleaseUrl)
	if err != nil {
		return "", err
	}

	releaseJsonDoc, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var releaseDoc map[string]any
	err = json.Unmarshal(releaseJsonDoc, &releaseDoc)
	if err != nil {
		return "", err
	}

	latestRelease, ok := releaseDoc["tag_name"].(string)
	if !ok {
		return "", fmt.Errorf("Could not parse %v", LatestReleaseUrl)
	}

	return latestRelease, nil
}

func upgradeViaGithub(latestVer string) error {
	const LatestDownloadFmt = "https://github.com/mikeb26/spotsh/releases/download/%v/spotsh"

	client := http.Client{
		Timeout: time.Second * 30,
	}

	resp, err := client.Get(fmt.Sprintf(LatestDownloadFmt, latestVer))
	if err != nil {
		return fmt.Errorf("Failed to download version %v: %w", versionText, err)

	}

	tmpFile, err := os.CreateTemp("", "spotsh-*")
	if err != nil {
		return fmt.Errorf("Failed to create temp file: %w", err)
	}
	binaryContent, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to download version %v: %w", versionText, err)
	}
	_, err = tmpFile.Write(binaryContent)
	if err != nil {
		return fmt.Errorf("Failed to download version %v: %w", versionText, err)
	}
	err = tmpFile.Chmod(0755)
	if err != nil {
		return fmt.Errorf("Failed to download version %v: %w", versionText, err)
	}
	err = tmpFile.Close()
	if err != nil {
		return fmt.Errorf("Failed to download version %v: %w", versionText, err)
	}
	myBinaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("Could not determine path to spotsh: %w", err)
	}
	myBinaryPath, err = filepath.EvalSymlinks(myBinaryPath)
	if err != nil {
		return fmt.Errorf("Could not determine path to spotsh: %w", err)
	}

	myBinaryPathBak := myBinaryPath + ".bak"
	err = os.Rename(myBinaryPath, myBinaryPathBak)
	if err != nil {
		return fmt.Errorf("Could not replace existing %v; do you need to be root?: %w",
			myBinaryPath, err)
	}
	err = os.Rename(tmpFile.Name(), myBinaryPath)
	if errors.Is(err, syscall.EXDEV) {
		// invalid cross device link occurs when rename() is attempted aross
		// different filesystems; copy instead
		err = ioutil.WriteFile(myBinaryPath, binaryContent, 0755)
		_ = os.Remove(tmpFile.Name())
	}
	if err != nil {
		err := fmt.Errorf("Could not replace existing %v; do you need to be root?: %w",
			myBinaryPath, err)
		_ = os.Rename(myBinaryPathBak, myBinaryPath)
		return err
	}
	_ = os.Remove(myBinaryPathBak)

	fmt.Printf("Upgrade %v to %v complete\n", myBinaryPath, latestVer)

	return nil
}

func checkAndPrintUpgradeWarning() bool {
	if versionText == DevVersionText {
		return false
	}
	latestVer, err := getLatestVersion()
	if err != nil {
		return false
	}
	if latestVer == versionText {
		return false
	}

	fmt.Fprintf(os.Stderr, "*WARN*: A new version of spotsh is available (%v). Please upgrade via 'spotsh upgrade'.\n\n",
		latestVer)

	return true
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

	if subCommandName != "upgrade" {
		checkAndPrintUpgradeWarning()
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
