/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"testing"

	"github.com/mikeb26/spotsh/internal"
)

func TestLookupImagesKeys(t *testing.T) {
	ctx := context.Background()

	imageResults, err := LookupImages(ctx)
	if err != nil {
		t.Fatalf("Failed to lookup images: %v", err)
	}

	var os internal.OperatingSystem

	awsOwnedCount := 0
	for imageId, image := range imageResults.Images {
		if imageId != image.Id {
			t.Errorf("image id inconsistency key %v value %v", imageId,
				image.Id)
		}
		if image.Ownership == "aws" {
			awsOwnedCount++
		}
	}

	if awsOwnedCount != len(os.Values()) {
		t.Errorf("LookupImages returned %v aws images but expecting %v",
			awsOwnedCount, len(os.Values()))
	}
}
