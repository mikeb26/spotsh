/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"crypto"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const defaultTagKey = "spotsh.user"

func getKeyName(awsCfg aws.Config) string {
	return fmt.Sprintf("spotsh.%v", awsCfg.Region)
}

func GetDefaultKeyName(ctx context.Context) (string, error) {

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	return getKeyName(awsCfg), nil
}

func getSshRootDir() (string, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homedir, ".ssh"), nil
}

func createDefaultKeyPair(ctx context.Context, awsCfg aws.Config,
	ec2Client *ec2.Client) error {

	sshRootDir, err := getSshRootDir()
	if err != nil {
		return err
	}
	err = os.MkdirAll(sshRootDir, 0700)
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
	localKeyFile := filepath.Join(sshRootDir, keyName)
	err = ioutil.WriteFile(localKeyFile, []byte(*createKeyOutput.KeyMaterial),
		0400)
	if err != nil {
		return err
	}

	return nil
}

func GetLocalDefaultKeyFile(ctx context.Context) (string, error) {
	sshRootDir, err := getSshRootDir()
	if err != nil {
		return "", err
	}
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	keyName := getKeyName(awsCfg)

	return filepath.Join(sshRootDir, keyName), nil
}

func haveDefaultKeyPair(ctx context.Context, awsCfg aws.Config) (bool, error) {
	sshRootDir, err := getSshRootDir()
	if err != nil {
		return false, err
	}
	keyName := getKeyName(awsCfg)
	localKeyFile := filepath.Join(sshRootDir, keyName)
	_, err = os.Stat(localKeyFile)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	} // else

	return true, err
}

type LookupKeyItem struct {
	Id           string
	Name         string
	PublicKey    string
	Fingerprint  string
	LocalKeyFile string
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
		keyItem.LocalKeyFile, err = findMatchingKeyFile(keyItem)

		lookupKeysResult.Keys[keyItem.Id] = keyItem
	}

	return lookupKeysResult, nil
}

func findMatchingKeyFile(keyItem *LookupKeyItem) (string, error) {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyItem.PublicKey))
	if err != nil {
		return "", fmt.Errorf("Failed to parse pub key for %v: %w",
			keyItem.Name, err)
	}

	sshRootDir, err := getSshRootDir()
	if err != nil {
		return "", err
	}
	entries, err := ioutil.ReadDir(sshRootDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		keyFile := filepath.Join(sshRootDir, entry.Name())
		match, err := isKeypair(keyFile, pubKey)
		if err != nil || !match {
			continue
		} // else

		return keyFile, nil
	}

	return "", nil
}

func isKeypair(privKeyPemFile string, sshPubKey2Test ssh.PublicKey) (bool, error) {
	// for some reason the standard crypto library does not define these
	// interfaces nor provide a helper utility function to enable conversion
	// from the current anonymous interface returned as a private key
	type privateKey interface {
		Public() crypto.PublicKey
		Equal(x crypto.PrivateKey) bool
	}
	type publicKey interface {
		Equal(x crypto.PublicKey) bool
	}

	privKeyData, err := ioutil.ReadFile(privKeyPemFile)
	if err != nil {
		return false, fmt.Errorf("Could not read %v: %w", privKeyPemFile, err)
	}
	rawPrivKey, err := ssh.ParseRawPrivateKey(privKeyData)
	if err != nil {
		return false, fmt.Errorf("Failed to parse %v: %w", privKeyPemFile, err)
	}

	privKey, ok := rawPrivKey.(privateKey)
	if !ok {
		return false, fmt.Errorf("Failed to convert rawPrivKey from %v into privKey",
			privKeyPemFile)
	}
	rawPubKey4PrivKey := privKey.Public()
	pubKey4PrivKey, ok := rawPubKey4PrivKey.(publicKey)
	if !ok {
		return false, fmt.Errorf("Failed to convert rawPubKey4PrivKey from %v into pubKey4PrivKey",
			privKeyPemFile)
	}

	sshCryptoPubKey2Test, ok := sshPubKey2Test.(ssh.CryptoPublicKey)
	if !ok {
		return false, fmt.Errorf("Failed to convert sshPubKey2Test into sshCryptoPubKey2Test")
	}

	cryptoPubKey2Test := sshCryptoPubKey2Test.CryptoPublicKey()
	pubKey2Test, ok := cryptoPubKey2Test.(publicKey)
	if !ok {
		return false, fmt.Errorf("Failed to convert cryptoPubKey2Test to pubKey2Test")
	}

	return pubKey4PrivKey.Equal(pubKey2Test), nil
}
