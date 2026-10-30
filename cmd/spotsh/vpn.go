/* Copyright © 2022-2024 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/mikeb26/spotsh"
	iaws "github.com/mikeb26/spotsh/aws"
)

const (
	VpnServerWorkingDir     = "vpn" // $HOME/VpnServerWorkingDir
	ClientPrivKeyFile       = "wg.private.key"
	ClientPubKeyFile        = "wg.public.key"
	ServerPubKeyFile        = "vpn.server.key.public"
	SetupVpnServerScript    = "setupVpnServer.sh"
	SetupVpnClientScript    = "setupVpnClient.sh"
	TeardownVpnClientScript = "teardownVpnClient.sh"
	vpnTagTimeout           = 30 * time.Second
)

//go:embed setupVpnServer.sh
var setupVpnServerText string

//go:embed setupVpnClient.sh
var setupVpnClientText string

//go:embed teardownVpnClient.sh
var teardownVpnClientText string

func vpnMain(awsCfg aws.Config, args []string) error {
	instanceID, action, verbose, err := parseVpnArgs(args, os.Stderr)
	if err != nil {
		return err
	}
	// Restore local connectivity before any instance lookup or other AWS call.
	if action == "stop" {
		if err := teardownVpnClient(verbose); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Selecting or launching spot instance...\n")
	selectedResult, err := selectOrLaunch(awsCfg, false, instanceID)
	if err != nil {
		if action == "stop" {
			return fmt.Errorf("VPN client stopped locally, but instance lookup failed (tag not updated): %w", err)
		}
		return err
	}

	if selectedResult.Os != spotsh.AmazonLinux2023 &&
		selectedResult.Os != spotsh.AmazonLinux2023Min {
		return fmt.Errorf("spotsh vpn is only currently supported on Amazon Linux 2023 spot instances")
	}

	if action == "start" {
		fmt.Fprintf(os.Stderr, "Checking or adding WireGuard UDP ingress rule for security group id %v...\n",
			selectedResult.SgId)
		err = iaws.CheckOrAddVpnIngressRule(awsCfg, selectedResult.SgId)
		if err != nil {
			return fmt.Errorf("Failed to add WireGuard ingress rule: %w", err)
		}

		err = startVpnServer(selectedResult, verbose)
		if err != nil {
			return err
		}

		err = startVpnClient(awsCfg, selectedResult, verbose)
		if err != nil {
			return err
		}
	} else {
		err = updateVpnTag(awsCfg, selectedResult, "false", verbose)
		if err != nil {
			return fmt.Errorf("VPN client stopped, but failed to update instance's vpn tag: %w", err)
		}
	}

	return nil
}

// parseVpnArgs accepts flags on either side of the start/stop action.
func parseVpnArgs(args []string, output io.Writer) (instanceID, action string, verbose bool, err error) {
	f := flag.NewFlagSet("spotsh vpn", flag.ContinueOnError)
	f.SetOutput(output)
	f.StringVar(&instanceID, "instance-id", "", "EC2 instance id")
	f.BoolVar(&verbose, "v", false, "Show verbose VPN diagnostics")
	if err = f.Parse(args); err != nil {
		return
	}
	args = f.Args()
	if len(args) == 0 || (!strings.EqualFold(args[0], "start") && !strings.EqualFold(args[0], "stop")) {
		err = fmt.Errorf("spotsh vpn <start|stop> must be specified")
		return
	}
	action = strings.ToLower(args[0])
	if err = f.Parse(args[1:]); err != nil {
		return
	}
	if len(f.Args()) != 0 {
		err = fmt.Errorf("unexpected VPN arguments: %s", strings.Join(f.Args(), " "))
	}
	return
}

func setupVpnClientKey(awsCfg aws.Config, args []string,
	configDir string) error {

	var keyText string
	privKeyFile := filepath.Join(configDir, ClientPrivKeyFile)
	_, err := os.Stat(privKeyFile)
	if err != nil {
		cmdAndArgs := []string{"wg", "genkey"}
		keyText, err := runLocal(cmdAndArgs, nil)
		if err != nil {
			return fmt.Errorf("Failed to generate wg priv key: %w", err)
		}

		err = ioutil.WriteFile(privKeyFile, []byte(keyText), 0400)
		if err != nil {
			return fmt.Errorf("Failed to write wg priv key: %w", err)
		}
	}

	pubKeyFile := filepath.Join(configDir, ClientPubKeyFile)
	_, err = os.Stat(pubKeyFile)
	if err != nil {
		err = nil
		if len(keyText) == 0 {
			var keyTextRaw []byte
			keyTextRaw, err = ioutil.ReadFile(privKeyFile)
			keyText = string(keyTextRaw)
		}
		if err != nil {
			return fmt.Errorf("Failed to read wg priv key: %w", err)
		}
		cmdAndArgs := []string{"wg", "pubkey"}
		keyText, err := runLocal(cmdAndArgs, strings.NewReader(keyText))
		if err != nil {
			return fmt.Errorf("Failed to generate wg pub key: %w", err)
		}

		err = ioutil.WriteFile(pubKeyFile, []byte(keyText), 0444)
		if err != nil {
			return fmt.Errorf("Failed to write wg pub key: %w", err)
		}
	}

	return nil
}

func runRemote(selectedResult *iaws.LaunchEc2SpotResult,
	cmdAndArgs []string, stdinReader io.Reader) (string, error) {

	sshArgs := []string{"-i", selectedResult.LocalKeyFile, "-o",
		"StrictHostKeyChecking=no",
		selectedResult.User + "@" + selectedResult.PublicIp}
	sshArgs = append(sshArgs, cmdAndArgs...)
	cmd := exec.Command("ssh", sshArgs...)
	if stdinReader != nil {
		cmd.Stdin = stdinReader
	}
	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			err = fmt.Errorf("%s", exitError.Stderr)
		}
		return "", err
	}

	return string(output), nil
}

func runLocal(cmdAndArgs []string, stdinReader io.Reader) (string, error) {
	cmd := exec.Command(cmdAndArgs[0], cmdAndArgs[1:]...)
	if stdinReader != nil {
		cmd.Stdin = stdinReader
	}
	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			err = fmt.Errorf("%s", exitError.Stderr)
		}
		return "", err
	}

	return string(output), nil
}

// Keep stderr and stdin live for errors and sudo prompts, even without verbose output.
func runVpnCommand(cmd *exec.Cmd, verbose bool) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.Discard
	if verbose {
		cmd.Stdout = os.Stderr
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func vpnScriptArgs(verbose bool, args ...string) []string {
	if verbose {
		return append([]string{"-v"}, args...)
	}
	return args
}

func readClientPubKey() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	pubKeyPath := filepath.Join(configDir, ClientPubKeyFile)
	fileContentRaw, err := ioutil.ReadFile(pubKeyPath)
	if err != nil {
		return "", err
	}
	fileContent := string(fileContentRaw)
	return strings.Split(fileContent, "\n")[0], nil
}

func readServerPubKey(selectedResult *iaws.LaunchEc2SpotResult) (string, error) {
	serverPubKeyPath := VpnServerWorkingDir + "/" + ServerPubKeyFile
	cmdAndArgs := []string{"cat", serverPubKeyPath}
	serverPubKey, err := runRemote(selectedResult, cmdAndArgs, nil)
	if err != nil {
		return "", fmt.Errorf("Failed to read vpn server public key: %w", err)
	}

	return strings.Split(serverPubKey, "\n")[0], nil
}

func startVpnServer(selectedResult *iaws.LaunchEc2SpotResult, verbose bool) error {
	fmt.Fprintf(os.Stderr, "Copying vpn setup scripts to spot instance...\n")

	cmdAndArgs := []string{"mkdir", "-p", VpnServerWorkingDir}
	_, err := runRemote(selectedResult, cmdAndArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to create vpn working dir: %w", err)
	}
	vpnSetupScriptPath := VpnServerWorkingDir + "/" + SetupVpnServerScript
	cmdAndArgs = []string{"cat", ">" + vpnSetupScriptPath}
	_, err = runRemote(selectedResult, cmdAndArgs,
		strings.NewReader(setupVpnServerText))
	if err != nil {
		return fmt.Errorf("Failed to copy vpn server setup script: %w", err)
	}
	cmdAndArgs = []string{"chmod", "755", vpnSetupScriptPath}
	_, err = runRemote(selectedResult, cmdAndArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to set vpn server setup permissions: %w", err)
	}
	clientPubKey, err := readClientPubKey()
	if err != nil {
		return fmt.Errorf("Failed to read vpn client public key: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Starting vpn server...\n")

	cmdAndArgs = []string{"cd " + VpnServerWorkingDir + ";", "./" + SetupVpnServerScript}
	cmdAndArgs = append(cmdAndArgs, vpnScriptArgs(verbose, clientPubKey, ServerPubKeyFile)...)
	sshArgs := []string{"-i", selectedResult.LocalKeyFile, "-o",
		"StrictHostKeyChecking=no", selectedResult.User + "@" + selectedResult.PublicIp}
	sshArgs = append(sshArgs, cmdAndArgs...)
	err = runVpnCommand(exec.Command("ssh", sshArgs...), verbose)
	if err != nil {
		return fmt.Errorf("Failed to start vpn server: %w", err)
	}

	return nil
}

func startVpnClient(awsCfg aws.Config,
	selectedResult *iaws.LaunchEc2SpotResult, verbose bool) error {

	tempDir, err := ioutil.TempDir("", "spotsh.vpn.*")
	if err != nil {
		return fmt.Errorf("Failed to create tempdir while setting up vpn client:%w",
			err)
	}
	defer os.RemoveAll(tempDir)

	vpnSetupScriptPath := filepath.Join(tempDir, SetupVpnClientScript)
	err = ioutil.WriteFile(vpnSetupScriptPath, []byte(setupVpnClientText), 0755)
	if err != nil {
		return fmt.Errorf("Failed to copy vpn client setup script: %w", err)
	}

	serverPubKey, err := readServerPubKey(selectedResult)
	if err != nil {
		return err
	}
	configDir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("Failed to find vpn client key: %w", err)
	}
	clientPrivKeyFilePath := filepath.Join(configDir, ClientPrivKeyFile)

	fmt.Fprintf(os.Stderr, "Starting vpn client...\n")

	cmdAndArgs := vpnScriptArgs(verbose, serverPubKey, selectedResult.PublicIp, clientPrivKeyFilePath)
	err = runVpnCommand(exec.Command(vpnSetupScriptPath, cmdAndArgs...), verbose)
	if err != nil {
		return fmt.Errorf("Failed to start vpn client: %w", err)
	}

	err = updateVpnTag(awsCfg, selectedResult, "true", verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "VPN tag update failed; rolling back local routing...\n")
		if cleanupErr := teardownVpnClient(verbose); cleanupErr != nil {
			return fmt.Errorf("VPN tag update failed: %w; local cleanup also failed: %v", err, cleanupErr)
		}
		return fmt.Errorf("VPN tag update failed; local VPN stopped (tag state may be uncertain): %w", err)
	}
	if verbose {
		fmt.Fprintf(os.Stderr, "VPN client started and instance tag updated.\n")
	}

	return nil
}

func teardownVpnClient(verbose bool) error {

	tempDir, err := ioutil.TempDir("", "spotsh.vpn.*")
	if err != nil {
		return fmt.Errorf("Failed to create tempdir while setting up vpn client:%w",
			err)
	}
	defer os.RemoveAll(tempDir)

	vpnTeardownScriptPath := filepath.Join(tempDir, TeardownVpnClientScript)
	err = ioutil.WriteFile(vpnTeardownScriptPath, []byte(teardownVpnClientText),
		0755)
	if err != nil {
		return fmt.Errorf("Failed to copy vpn teardown script: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Stopping vpn client...\n")

	err = runVpnCommand(exec.Command(vpnTeardownScriptPath, vpnScriptArgs(verbose)...), verbose)
	if err != nil {
		return fmt.Errorf("Failed to stop vpn client: %w", err)
	}

	return nil
}

func stopVpnClient(awsCfg aws.Config, selectedResult *iaws.LaunchEc2SpotResult) error {
	if err := teardownVpnClient(false); err != nil {
		return err
	}
	if err := updateVpnTag(awsCfg, selectedResult, "false", false); err != nil {
		return fmt.Errorf("VPN client stopped, but failed to update instance's vpn tag: %w", err)
	}
	return nil
}

func updateVpnTag(awsCfg aws.Config, selectedResult *iaws.LaunchEc2SpotResult, value string, verbose bool) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "Updating instance VPN tag to %s (timeout %s)...\n", value, vpnTagTimeout)
	}
	ctx, cancel := context.WithTimeout(context.Background(), vpnTagTimeout)
	defer cancel()
	return iaws.UpdateTagContext(ctx, awsCfg, selectedResult.InstanceId,
		iaws.DefaultTagPrefix+"."+iaws.VpnTagSuffix, value)
}
