/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func loadAWSConfigOrSkip(t *testing.T) awssdk.Config {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		t.Skipf("skipping AWS integration test: failed to load AWS config: %v", err)
	}
	if awsCfg.Region == "" {
		t.Skip("skipping AWS integration test: no AWS region configured")
	}
	if _, err := awsCfg.Credentials.Retrieve(ctx); err != nil {
		t.Skipf("skipping AWS integration test: AWS credentials unavailable: %v", err)
	}

	return awsCfg
}
