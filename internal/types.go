/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package internal

type OperatingSystem uint64

const (
	OsNone OperatingSystem = iota
	Ubuntu22_04
	AmazonLinux2

	OsInvalid // must be last
)

var osTab = []string{
	OsNone:       "",
	Ubuntu22_04:  "ubuntu22.04",
	AmazonLinux2: "amzn2",

	OsInvalid: "invalid",
}

var osMap = make(map[string]OperatingSystem)

func (os OperatingSystem) String() string {
	idx := int(os)
	if idx < 0 || idx > len(osTab) {
		idx = int(OsInvalid)
	}

	return osTab[idx]
}

func OsFromString(osStr string) OperatingSystem {
	os, ok := osMap[osStr]
	if !ok {
		return OsInvalid
	}

	return os
}

func (os OperatingSystem) Values() []OperatingSystem {
	return []OperatingSystem{
		Ubuntu22_04,
		AmazonLinux2,
	}
}

func init() {
	for idx, osStr := range osTab {
		osMap[osStr] = OperatingSystem(idx)
	}
}
