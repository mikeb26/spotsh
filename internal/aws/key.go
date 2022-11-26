/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const defaultTagKey = "spotsh.user"

func getKeyName(awsCfg aws.Config) string {
	return fmt.Sprintf("spotsh.%v", awsCfg.Region)
}

func createDefaultKeyPair(ctx context.Context, awsCfg aws.Config,
	ec2Client *ec2.Client) error {

	homedir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshdir := filepath.Join(homedir, ".ssh")
	err = os.MkdirAll(sshdir, 0700)
	if err != nil {
		return err
	}

	keyName := getKeyName(awsCfg)
	dryRun := false
	createKeyInput := &ec2.CreateKeyPairInput{
		KeyName:   &keyName,
		DryRun:    &dryRun,
		KeyFormat: types.KeyFormatPem,
		KeyType:   types.KeyTypeEd25519,
	}
	createKeyOutput, err := ec2Client.CreateKeyPair(ctx, createKeyInput)
	if err != nil {
		return err
	}
	localKeyFile := filepath.Join(sshdir, keyName)
	err = ioutil.WriteFile(localKeyFile, []byte(*createKeyOutput.KeyMaterial),
		0400)
	if err != nil {
		return err
	}

	return nil
}

func GetLocalKeyFile(ctx context.Context) (string, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	keyName := getKeyName(awsCfg)

	return filepath.Join(homedir, ".ssh", keyName), nil
}

func haveDefaultKeyPair(ctx context.Context, awsCfg aws.Config) (bool, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	keyName := getKeyName(awsCfg)
	localKeyFile := filepath.Join(homedir, ".ssh", keyName)
	_, err = os.Stat(localKeyFile)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	} // else

	return true, err
}

type LookupKeyItem struct {
	Id          string
	Name        string
	PublicKey   string
	Fingerprint string
}

type LookupKeysResult struct {
	Keys map[string]*LookupKeyItem
}

func LookupKeys(ctx context.Context) (LookupKeysResult, error) {
	lookupKeysResult := LookupKeysResult{
		Keys: make(map[string]*LookupKeyItem),
	}

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return lookupKeysResult, err
	}
	ec2Client := ec2.NewFromConfig(awsCfg)
	dryRun := false
	includePublic := true
	descKeyInput := &ec2.DescribeKeyPairsInput{
		DryRun:           &dryRun,
		IncludePublicKey: &includePublic,
	}
	descKeyOutput, err := ec2Client.DescribeKeyPairs(ctx, descKeyInput)
	if err != nil {
		return lookupKeysResult, err
	}
	for _, keypair := range descKeyOutput.KeyPairs {
		keyItem := &LookupKeyItem{
			Id:          *keypair.KeyPairId,
			Name:        *keypair.KeyName,
			PublicKey:   *keypair.PublicKey,
			Fingerprint: *keypair.KeyFingerprint,
		}

		lookupKeysResult.Keys[keyItem.Id] = keyItem
	}

	return lookupKeysResult, nil
}
