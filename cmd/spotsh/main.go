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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/mikeb26/spotsh/internal"
	iaws "github.com/mikeb26/spotsh/internal/aws"
)

type Prefs struct {
	Os               string            `json:",omitempty"`
	InstanceType     string            `json:",omitempty"`
	KeyPairs         map[string]string `json:",omitempty"`
	SecurityGroups   map[string]string `json:",omitempty"`
	MaxSpotPrice     string            `json:",omitempty"`
	RootVolSizeInGiB int32             `json:",omitempty"`

	keyPair       string
	securityGroup string
}

var subCommandTab = map[string]func(awsCfg aws.Config, args []string) error{
	"help":      helpMain,
	"info":      infoMain,
	"ls":        infoMain, // alias for info
	"launch":    launchMain,
	"scp":       scpMain,
	"ssh":       sshMain,
	"terminate": terminateMain,
	"version":   versionMain,
	"upgrade":   upgradeMain,
	"config":    configMain,
	"price":     priceMain,
}

//go:embed help.txt
var helpText string

func helpMain(awsCfg aws.Config, args []string) error {
	fmt.Printf(helpText)

	return nil
}

//go:embed version.txt
var versionText string

const DevVersionText = "v0.devbuild"

func versionMain(awsCfg aws.Config, args []string) error {
	fmt.Printf("spotsh-%v\n", versionText)

	return nil
}

func infoMain(awsCfg aws.Config, args []string) error {
	launchResults, err := iaws.LookupEc2Spot(awsCfg)
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

	vpcSgResults, err := iaws.LookupVpcSecurityGroups(awsCfg)
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

	keyResults, err := iaws.LookupKeys(awsCfg)
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

	imageResults, err := iaws.LookupImages(awsCfg)
	if err != nil {
		return fmt.Errorf("Failed to lookup images: %w", err)
	}
	fmt.Printf("Images:\n")
	idx = 0
	for imageId, image := range imageResults.Images {
		fmt.Printf("\tImages[%v]:\n", idx)
		fmt.Printf("\t\tId: %v\n", imageId)
		fmt.Printf("\t\tName: %v\n", image.Name)
		fmt.Printf("\t\tOwnership: %v\n", image.Ownership)
		idx++
	}

	return nil
}

func launchMain(awsCfg aws.Config, args []string) error {
	launchArgs, err := newLaunchArgsFromPrefs(awsCfg)
	if err != nil {
		return err
	}

	var os string
	var iType string

	f := flag.NewFlagSet("spotsh launch", flag.ContinueOnError)
	f.StringVar(&os, "os", "", "Operating System; e.g. amzn2")
	f.StringVar(&launchArgs.AmiId, "ami", launchArgs.AmiId,
		"Amazon Machine Image id")
	f.StringVar(&launchArgs.User, "user", launchArgs.User, "username to ssh as")
	f.StringVar(&launchArgs.KeyPair, "key", launchArgs.KeyPair, "EC2 keypair")
	f.StringVar(&launchArgs.SecurityGroupId, "sgid", launchArgs.SecurityGroupId,
		"Security Group Id")
	f.StringVar(&launchArgs.AttachRoleName, "role", launchArgs.AttachRoleName,
		"IAM Role to attach to instance")
	f.StringVar(&launchArgs.InitCmd, "initcmd", launchArgs.InitCmd,
		"Initial command to run in the instance")
	f.StringVar(&iType, "type", string(launchArgs.InstanceType), "Instance type")
	f.StringVar(&launchArgs.MaxSpotPrice, "spotprice", launchArgs.MaxSpotPrice,
		"Maximum spot price to pay")
	err = f.Parse(args)
	if err != nil {
		return err
	}

	launchArgs.InstanceType = types.InstanceType(iType)

	if launchArgs.AmiId != "" {
		if os != "" {
			return fmt.Errorf("--ami and --os are mutually exclusive; choose one but not both flags simultaneously")
		}
		if launchArgs.User == "" {
			return fmt.Errorf("--user must be specified when launching by AMI id so that spotsh knows which user to ssh as in the future")
		}
	} else {
		if os != "" {
			launchArgs.Os = internal.OsFromString(os)
		}
		if launchArgs.User != "" {
			return fmt.Errorf("--user is automatically determined by default or when --os is specified")
		}
	}

	launchResult, err := iaws.LaunchEc2Spot(awsCfg, launchArgs)
	if err != nil {
		return err
	}
	fmt.Printf("Launched %v (%v@%v)\n", launchResult.InstanceId,
		launchResult.User, launchResult.PublicIp)

	return nil
}

func terminateMain(awsCfg aws.Config, args []string) error {
	termOpts := struct {
		instanceId string
	}{}

	f := flag.NewFlagSet("spotsh terminate", flag.ContinueOnError)
	f.StringVar(&termOpts.instanceId, "instance-id", "", "EC2 instance id")
	err := f.Parse(args)
	if err != nil {
		return err
	}

	launchResults, err := iaws.LookupEc2Spot(awsCfg)
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

	var selectedResult *iaws.LaunchEc2SpotResult
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

	return iaws.TerminateInstance(awsCfg, selectedResult.InstanceId)
}

func sshMain(awsCfg aws.Config, args []string) error {
	return sshCommon(awsCfg, false, args)
}

func scpMain(awsCfg aws.Config, args []string) error {
	const SpotHostVar = "{s}"

	selectedResult, err := selectOrLaunchCommon(awsCfg, "spotsh scp", false, &args)
	if err != nil {
		return err
	}

	// replace all instances of {s} in remaining args with user@ip
	userAtPublicIp := selectedResult.User + "@" + selectedResult.PublicIp
	for idx, _ := range args {
		args[idx] = strings.ReplaceAll(args[idx], SpotHostVar, userAtPublicIp)
	}

	scpArgs := []string{"scp", "-i", selectedResult.LocalKeyFile, "-o",
		"StrictHostKeyChecking=no",
	}
	if len(args) > 0 {
		scpArgs = append(scpArgs, args...)
	}
	fmt.Printf("exec %v\n", scpArgs)

	err = syscall.Exec("/usr/bin/scp", scpArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to scp: %w\n", err)
	}

	return nil
}

func selectOrLaunchCommon(awsCfg aws.Config, cmdName string, canLaunch bool,
	args *[]string) (*iaws.LaunchEc2SpotResult, error) {

	sshOpts := struct {
		instanceId string
	}{}

	f := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	f.StringVar(&sshOpts.instanceId, "instance-id", "", "EC2 instance id")
	err := f.Parse(*args)
	if err != nil {
		return nil, err
	}

	launchResults, err := iaws.LookupEc2Spot(awsCfg)
	if err == nil && len(launchResults) == 0 {
		if canLaunch {
			launchArgs, err := newLaunchArgsFromPrefs(awsCfg)
			if err != nil {
				return nil, err
			}
			var newLaunchResult iaws.LaunchEc2SpotResult

			newLaunchResult, err = iaws.LaunchEc2Spot(awsCfg, launchArgs)
			launchResults = append(launchResults, newLaunchResult)
		} else {
			err = fmt.Errorf("No spotssh instances running")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("Failed to lookup/launch instance: %w", err)
	}

	if len(launchResults) > 1 && sshOpts.instanceId == "" {
		errStr := "Multiple spotsh instances found; please disambiguate w/ --instance-id:"
		for _, lr := range launchResults {
			errStr = fmt.Sprintf("%v\n\t%v:%v", errStr, lr.InstanceId,
				lr.PublicIp)
		}
		return nil, fmt.Errorf("%v", errStr)
	}

	var selectedResult *iaws.LaunchEc2SpotResult
	for idx, lr := range launchResults {
		if sshOpts.instanceId == "" || sshOpts.instanceId == lr.InstanceId {
			selectedResult = &launchResults[idx]
			break
		}
	}

	if selectedResult == nil {
		return nil, fmt.Errorf("Could not find spotssh instance w/ id %v",
			sshOpts.instanceId)
	}
	if selectedResult.LocalKeyFile == "" {
		return nil, fmt.Errorf("Could not find local ssh key for instance w/ id %v",
			selectedResult.InstanceId)
	}

	*args = f.Args()
	return selectedResult, nil
}

func sshCommon(awsCfg aws.Config, canLaunch bool, args []string) error {
	selectedResult, err := selectOrLaunchCommon(awsCfg, "spotsh ssh", canLaunch,
		&args)
	if err != nil {
		return err
	}

	sshArgs := []string{"ssh", "-i", selectedResult.LocalKeyFile, "-o",
		"StrictHostKeyChecking=no",
		selectedResult.User + "@" + selectedResult.PublicIp}
	if len(args) > 0 {
		sshArgs = append(sshArgs, args...)
	}
	fmt.Printf("exec %v\n", sshArgs)

	err = syscall.Exec("/usr/bin/ssh", sshArgs, os.Environ())
	if err != nil {
		return fmt.Errorf("Failed to ssh: %w\n", err)
	}

	return nil
}

func upgradeMain(awsCfg aws.Config, args []string) error {
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

func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("Could not find user home directory: %w", err)
	}

	return filepath.Join(homeDir, ".config", "spotsh"), nil
}

func getConfigPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "prefs.json"), nil
}

func loadConfigPrefs(awsCfg aws.Config, configFilePath string, prefs *Prefs) error {
	configContent, err := ioutil.ReadFile(configFilePath)
	if os.IsNotExist(err) {
		// defaults
		return nil
	}

	err = json.Unmarshal(configContent, prefs)
	if err != nil {
		return err
	}

	prefs.keyPair = prefs.KeyPairs[awsCfg.Region]
	prefs.securityGroup = prefs.SecurityGroups[awsCfg.Region]

	return nil
}

func storeConfigPrefs(configFilePath string, prefs *Prefs) error {
	configContent, err := json.Marshal(prefs)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(configFilePath, configContent, 0600)
}

func newPrefs() *Prefs {
	ret := &Prefs{
		KeyPairs:       make(map[string]string),
		SecurityGroups: make(map[string]string),
	}

	return ret
}

func newLaunchArgsFromPrefs(awsCfg aws.Config) (*iaws.LaunchEc2SpotArgs, error) {
	configFilePath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	prefs := newPrefs()
	err = loadConfigPrefs(awsCfg, configFilePath, prefs)
	if err != nil {
		return nil, err
	}

	launchArgs := &iaws.LaunchEc2SpotArgs{
		Os:               internal.OsFromString(prefs.Os),
		KeyPair:          prefs.keyPair,
		SecurityGroupId:  prefs.securityGroup,
		InstanceType:     types.InstanceType(prefs.InstanceType),
		MaxSpotPrice:     prefs.MaxSpotPrice,
		RootVolSizeInGiB: prefs.RootVolSizeInGiB,
	}

	return launchArgs, nil
}

func configMain(awsCfg aws.Config, args []string) error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}
	err = os.MkdirAll(configDir, 0700)
	if err != nil {
		return fmt.Errorf("Could not create config directory %v: %w",
			configDir, err)
	}
	configFilePath, err := getConfigPath()
	if err != nil {
		return err
	}

	prefs := newPrefs()
	err = loadConfigPrefs(awsCfg, configFilePath, prefs)
	if err != nil {
		return err
	}

	os := iaws.DefaultOperatingSystem
	if prefs.Os != "" {
		os = internal.OsFromString(prefs.Os)
	}

	fmt.Printf("Setting spotsh preferences...\n")
	// set os pref
	fmt.Printf("Default operating system: \"%v\" (%v) Change? (Y/N) [N]: ",
		os, iaws.GetImageDesc(os))
	changePref := "N"
	fmt.Scanf("%s", &changePref)
	changePref = strings.ToUpper(strings.TrimSpace(changePref))
	if changePref[0] == 'Y' {
		fmt.Printf("  Available OS's: \n")
		for _, osTmp := range os.Values() {
			fmt.Printf("    \"%v\" (%v)\n", osTmp, iaws.GetImageDesc(osTmp))
		}
		fmt.Printf("  Enter preferred default operating system: ")
		newOsStr := ""
		fmt.Scanf("%s", &newOsStr)
		newOsStr = strings.TrimSpace(newOsStr)
		newOsStr = strings.Split(newOsStr, " ")[0]
		newOsStr = strings.Trim(newOsStr, "\"")
		os = internal.OsFromString(newOsStr)
		if os == internal.OsInvalid {
			return fmt.Errorf("No such os \"%v\" supported", newOsStr)
		}
		prefs.Os = newOsStr
	}

	// set itype pref
	iType := iaws.DefaultInstanceType
	if prefs.InstanceType != "" {
		iType = types.InstanceType(prefs.InstanceType)
	}
	fmt.Printf("Default instance type: %v Change? (Y/N) [N]: ", iType)
	changePref = "N"
	fmt.Scanf("%s", &changePref)
	changePref = strings.ToUpper(strings.TrimSpace(changePref))
	if changePref[0] == 'Y' {
		fmt.Printf("  see https://aws.amazon.com/ec2/instance-types/ for a complete list\n")
		fmt.Printf("  Enter preferred default instance type: ")
		newItype := ""
		fmt.Scanf("%s", &newItype)
		newItype = strings.TrimSpace(newItype)
		newItype = strings.Split(newItype, " ")[0]
		prefs.InstanceType = newItype
	}

	// set key pref
	keyPair := iaws.GetDefaultKeyName(awsCfg)
	if prefs.KeyPairs[awsCfg.Region] != "" {
		keyPair = prefs.KeyPairs[awsCfg.Region]
	}
	fmt.Printf("Default keypair: %v Change? (Y/N) [N]: ", keyPair)
	changePref = "N"
	fmt.Scanf("%s", &changePref)
	changePref = strings.ToUpper(strings.TrimSpace(changePref))
	if changePref[0] == 'Y' {
		existingKeys, err := iaws.LookupKeys(awsCfg)
		if err != nil {
			return err
		}
		fmt.Printf("  Available keypairs: \n")
		for _, existingKey := range existingKeys.Keys {
			if existingKey.LocalKeyFile == "" {
				existingKey.LocalKeyFile = "<not present>"
			}
			fmt.Printf("    %v (%v)\n", existingKey.Name,
				existingKey.LocalKeyFile)
		}
		fmt.Printf("  Enter preferred default keypair: ")
		newKey := ""
		fmt.Scanf("%s", &newKey)
		newKey = strings.TrimSpace(newKey)
		newKey = strings.Split(newKey, " ")[0]
		prefs.KeyPairs[awsCfg.Region] = newKey
		prefs.keyPair = newKey
	}

	// set security group pref
	sgId, err := iaws.GetDefaultSecurityGroupId(awsCfg)
	if err != nil {
		sgId = "<none>"
	}
	if prefs.SecurityGroups[awsCfg.Region] != "" {
		sgId = prefs.SecurityGroups[awsCfg.Region]
	}
	fmt.Printf("Default security group id: %v Change? (Y/N) [N]: ", sgId)
	changePref = "N"
	fmt.Scanf("%s", &changePref)
	changePref = strings.ToUpper(strings.TrimSpace(changePref))
	if changePref[0] == 'Y' {
		existingSgs, err := iaws.LookupVpcSecurityGroups(awsCfg)
		if err != nil {
			return err
		}
		fmt.Printf("  Available Security Groups: \n")
		for _, vpc := range existingSgs.Vpcs {
			if vpc.Default {
				fmt.Printf("    Vpc %v (default):\n", vpc.Id)
			} else {
				fmt.Printf("    Vpc %v:\n", vpc.Id)
			}
			for _, sg := range vpc.Sgs {
				fmt.Printf("      %v\n", sg.Id)
			}
		}
		fmt.Printf("  Enter preferred default security group: ")
		newSgId := ""
		fmt.Scanf("%s", &newSgId)
		newSgId = strings.TrimSpace(newSgId)
		newSgId = strings.Split(newSgId, " ")[0]
		prefs.SecurityGroups[awsCfg.Region] = newSgId
		prefs.securityGroup = newSgId
	}

	// set max spot price pref
	spotPrice := iaws.DefaultMaxSpotPrice
	if prefs.MaxSpotPrice != "" {
		spotPrice = prefs.MaxSpotPrice
	}
	fmt.Printf("Default max spot price: $%v/hour Change? (Y/N) [N]: ",
		spotPrice)
	changePref = "N"
	fmt.Scanf("%s", &changePref)
	changePref = strings.ToUpper(strings.TrimSpace(changePref))
	if changePref[0] == 'Y' {
		fmt.Printf("  Enter preferred max spot price: ")
		newSpotPrice := ""
		fmt.Scanf("%s", &newSpotPrice)
		newSpotPrice = strings.TrimSpace(newSpotPrice)
		newSpotPrice = strings.Trim(newSpotPrice, "$")
		newSpotPrice = strings.Split(newSpotPrice, " ")[0]
		newSpotPrice = strings.Split(newSpotPrice, "/")[0]
		prefs.MaxSpotPrice = newSpotPrice
	}

	// set root vol size pref
	rootVolSize := iaws.DefaultRootVolSizeInGiB
	if prefs.RootVolSizeInGiB != 0 {
		rootVolSize = prefs.RootVolSizeInGiB
	}
	fmt.Printf("Default root vol size: %v GiB Change? (Y/N) [N]: ",
		rootVolSize)
	changePref = "N"
	fmt.Scanf("%s", &changePref)
	changePref = strings.ToUpper(strings.TrimSpace(changePref))
	if changePref[0] == 'Y' {
		fmt.Printf("  Enter preferred root vol size in GiB: ")
		newRootVolSize := int32(0)
		fmt.Scanf("%d", &newRootVolSize)
		prefs.RootVolSizeInGiB = newRootVolSize
	}

	return storeConfigPrefs(configFilePath, prefs)
}

func priceMain(awsCfg aws.Config, args []string) error {
	launchArgs, err := newLaunchArgsFromPrefs(awsCfg)
	if err != nil {
		return err
	}

	var iTypesIn string

	f := flag.NewFlagSet("spotsh price", flag.ContinueOnError)
	f.StringVar(&iTypesIn, "types", string(launchArgs.InstanceType), "Instance types")
	err = f.Parse(args)
	if err != nil {
		return err
	}

	var iTypes []types.InstanceType
	for _, iTypeIn := range strings.Split(iTypesIn, ",") {
		iTypes = append(iTypes, types.InstanceType(iTypeIn))
	}

	lookupResult, err := iaws.LookupEc2SpotPrices(awsCfg, iTypes)
	if err != nil {
		return err
	}

	for _, lookupInst := range lookupResult.InstanceTypes {
		for _, lookupReg := range lookupInst.Regions {
			if lookupReg.CheapestAz == nil {
				continue
			}

			lookupAz := lookupReg.CheapestAz
			if lookupReg == lookupInst.CheapestRegion &&
				lookupInst == lookupResult.CheapestIType {
				fmt.Printf(" ** ")
			}

			fmt.Printf("%v - %v - %v - $%v/hr\n", lookupInst.InstanceType,
				lookupReg.Region, lookupAz.AzName, lookupAz.CurPrice)
		}
	}

	return nil
}

func main() {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	var region string
	f := flag.NewFlagSet("spotsh", flag.ContinueOnError)
	f.StringVar(&region, "region", awsCfg.Region, "AWS region; e.g. us-east-2")

	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}
	err = f.Parse(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	args = f.Args()

	if region != awsCfg.Region {
		awsCfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	}
	subCommandName := ""
	if len(args) > 0 {
		subCommandName = args[0]
	}
	exitStatus := 0

	if len(args) > 1 {
		args = args[1:]
	}

	if subCommandName != "upgrade" {
		checkAndPrintUpgradeWarning()
	}
	if subCommandName == "" {
		err = sshCommon(awsCfg, true, args)
	} else {
		subCommand, ok := subCommandTab[subCommandName]
		if !ok {
			subCommand = helpMain
			exitStatus = 1
		}
		err = subCommand(awsCfg, args)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitStatus = 1
	}

	os.Exit(exitStatus)

}
