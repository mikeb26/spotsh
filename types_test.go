/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package spotsh

import (
	"testing"
)

func TestOsToFromString(t *testing.T) {
	if len(osTab) != len(osMap) {
		t.Fatalf("len(osTab)[%v] != len(osMap)[%v]", len(osTab), len(osMap))
	}

	for idx, osStr := range osTab {
		os := OperatingSystem(idx)
		if os.String() != osStr {
			t.Fatalf("osTab os.String(%v) != expected %v", os, osStr)
		}
		if OsFromString(osStr) != os {
			t.Fatalf("osTab OsFromString(%v) != expected %v", osStr, os)
		}
	}

	for osStr, os := range osMap {
		if os.String() != osStr {
			t.Fatalf("osMap os.String(%v) != expected %v", os, osStr)
		}
		if OsFromString(osStr) != os {
			t.Fatalf("osMap OsFromString(%v) != expected %v", osStr, os)
		}
	}

	if OsFromString("deadbeef") != OsInvalid {
		t.Fatalf("OsFromString() invalid test failed")
	}
	if OperatingSystem(0xdeadbeef).String() != "invalid" {
		t.Fatalf("Os.String() invalid test failed")
	}
}
