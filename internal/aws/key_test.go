/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"strings"
	"testing"
)

func TestLookupKeys(t *testing.T) {
	ctx := context.Background()
	keyResults, err := LookupKeys(ctx)
	if err != nil {
		t.Fatalf("failed to lookup keys: %v", err)
	}

	for keyId, key := range keyResults.Keys {
		if keyId != key.Id {
			t.Errorf("Unexpected KeyId %v vs %v", keyId, key.Id)
		}
		if !strings.Contains(keyId, "key-") {
			t.Fatalf("lookup returned unexpected key id: %v", keyId)
		}
	}
}
