/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"testing"
)

func TestLookupImages(t *testing.T) {
	awsCfg := loadAWSConfigOrSkip(t)

	imageResults, err := LookupImages(awsCfg)
	if err != nil {
		t.Fatalf("Failed to lookup images: %v", err)
	}

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

	if awsOwnedCount != 0 {
		t.Errorf("LookupImages returned %v aws images but expecting %v",
			awsOwnedCount, 0)
	}
}
