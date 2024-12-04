/* Copyright © 2022-2024 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package main

import (
	_ "embed"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
)

//go:embed setupVpnServer.sh
var setupVpnServerText string

//go:embed setupVpnClient.sh
var setupVpnClientText string

//go:embed teardownVpnClient.sh
var teardownVpnClientText string

func vpnMain(awsCfg aws.Config, args []string) error {
	fmt.Fprintf(os.Stderr, "Selecting or launching spot instance...\n")
	selectedResult, err := selectOrLaunchWithArgs(awsCfg, "spotsh vpn", false,
		&args)
	if err != nil {
		return err
	}

	if selectedResult.Os != spotsh.AmazonLinux2023 &&
		selectedResult.Os != spotsh.AmazonLinux2023Min {
		return fmt.Errorf("spotsh vpn is only currently supported on Amazon Linux 2023 spot instances")
	}

	if len(args) != 1 || (strings.ToLower(args[0]) != "start" &&
		strings.ToLower(args[0]) != "stop") {
		return fmt.Errorf("spotsh vpn <start|stop> must be specified")
	}

	if strings.ToLower(args[0]) == "start" {
		err = startVpnServer(selectedResult)
		if err != nil {
			return err
		}

		err = startVpnClient(awsCfg, selectedResult)
		if err != nil {
			return err
		}
	} else {
		// stop
		err = stopVpnClient(awsCfg, selectedResult)
		if err != nil {
			return err
		}
	}

	return nil
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
			err = fmt.Errorf(string(exitError.Stderr))
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
			err = fmt.Errorf(string(exitError.Stderr))
		}
		return "", err
	}

	return string(output), nil
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

func startVpnServer(selectedResult *iaws.LaunchEc2SpotResult) error {
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

	cmdAndArgs = []string{"cd " + VpnServerWorkingDir + ";",
		"./" + SetupVpnServerScript, clientPubKey, ServerPubKeyFile}
	_, err = runRemote(selectedResult, cmdAndArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to start vpn server: %w", err)
	}

	return nil
}

func startVpnClient(awsCfg aws.Config,
	selectedResult *iaws.LaunchEc2SpotResult) error {

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

	err = iaws.UpdateTag(awsCfg, selectedResult.InstanceId, iaws.VpnTagKey, "true")
	if err != nil {
		return fmt.Errorf("Failed to update instance's vpn tag: %w", err)
	}

	cmdAndArgs := []string{vpnSetupScriptPath, serverPubKey,
		selectedResult.PublicIp, clientPrivKeyFilePath}
	_, err = runLocal(cmdAndArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to start vpn client: %w", err)
	}

	return nil
}

func stopVpnClient(awsCfg aws.Config,
	selectedResult *iaws.LaunchEc2SpotResult) error {

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

	cmdAndArgs := []string{vpnTeardownScriptPath}
	_, err = runLocal(cmdAndArgs, nil)
	if err != nil {
		return fmt.Errorf("Failed to stop vpn client: %w", err)
	}

	err = iaws.UpdateTag(awsCfg, selectedResult.InstanceId, iaws.VpnTagKey, "false")
	if err != nil {
		return fmt.Errorf("Failed to update instance's vpn tag: %w", err)
	}

	return nil
}
