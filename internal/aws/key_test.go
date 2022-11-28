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

func TestLookupKeys(t *testing.T) {
	ctx := context.Background()

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatalf("failed to init aws config: %v", err)
	}
	haveDefaultKey, err := haveDefaultKeyPair(ctx, awsCfg)
	if err != nil {
		t.Fatalf("failed to test for default keypair: %v", err)
	}
	if !haveDefaultKey {
		ec2Client := ec2.NewFromConfig(awsCfg)
		err = createDefaultKeyPair(ctx, awsCfg, ec2Client)
		if err != nil {
			t.Fatalf("failed to create default keypair: %v", err)
		}
	}

	keyResults, err := LookupKeys(ctx)
	if err != nil {
		t.Fatalf("failed to lookup keys: %v", err)
	}

	defaultKeyPath, err := GetLocalDefaultKeyFile(ctx)
	if err != nil {
		t.Fatalf("failed to get default key file: %v", err)
	}
	foundSpotShKeyfile := false
	for keyId, key := range keyResults.Keys {
		if keyId != key.Id {
			t.Errorf("Unexpected KeyId %v vs %v", keyId, key.Id)
		}
		if !strings.Contains(keyId, "key-") {
			t.Fatalf("lookup returned unexpected key id: %v", keyId)
		}

		if key.LocalKeyFile == defaultKeyPath {
			foundSpotShKeyfile = true
		}
	}

	if !foundSpotShKeyfile {
		t.Errorf("Failed to find spotsh key")
	}
}
