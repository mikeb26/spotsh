/* Copyright © 2022 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package aws

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/sync/errgroup"
)

type LookupEc2SpotPriceAz struct {
	AzName   string
	CurPrice float64
}

type LookupEc2SpotPriceRegion struct {
	Region     string
	Azs        map[string]*LookupEc2SpotPriceAz
	CheapestAz *LookupEc2SpotPriceAz
}

type LookupEc2SpotPriceIType struct {
	InstanceType   types.InstanceType
	Regions        map[string]*LookupEc2SpotPriceRegion
	CheapestRegion *LookupEc2SpotPriceRegion
}

type LookupEc2SpotPriceResult struct {
	InstanceTypes map[types.InstanceType]*LookupEc2SpotPriceIType
	CheapestIType *LookupEc2SpotPriceIType

	mutex sync.Locker
}

func LookupEc2SpotPrices(awsCfg aws.Config,
	iTypes []types.InstanceType) (*LookupEc2SpotPriceResult, error) {

	var err error
	var regionList []string

	if len(iTypes) == 0 {
		return nil, fmt.Errorf("Could not fetch spot prices: please specify 1 or more instance types")
	}

	if awsCfg.Region == "all" {
		regionList, err = getRegions()
		if err != nil {
			return nil, err
		}
	} else {
		regionList = []string{awsCfg.Region}
	}

	result := &LookupEc2SpotPriceResult{
		InstanceTypes: make(map[types.InstanceType]*LookupEc2SpotPriceIType),
		mutex:         &sync.Mutex{},
	}
	for _, iType := range iTypes {
		result.InstanceTypes[iType] = &LookupEc2SpotPriceIType{
			InstanceType: iType,
			Regions:      make(map[string]*LookupEc2SpotPriceRegion),
		}

		for _, curReg := range regionList {
			result.InstanceTypes[iType].Regions[curReg] =
				&LookupEc2SpotPriceRegion{
					Region: curReg,
					Azs:    make(map[string]*LookupEc2SpotPriceAz),
				}
		}
	}

	var wg errgroup.Group
	for _, curReg := range regionList {
		curReg := curReg // https://golang.org/doc/faq#closures_and_goroutines
		wg.Go(func() error {
			return lookupEc2SpotPricesOneRegion(curReg, iTypes, result)
		})
	}

	err = wg.Wait()

	return result, err
}

func lookupEc2SpotPricesOneRegion(curReg string, iTypes []types.InstanceType,
	result *LookupEc2SpotPriceResult) error {

	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(curReg))
	if err != nil {
		return err
	}

	ec2Client := ec2.NewFromConfig(awsCfg)
	dryRun := false
	startTime := time.Date(2199, time.January, 1, 0, 0, 0, 0, time.UTC)
	descInput := &ec2.DescribeSpotPriceHistoryInput{
		DryRun:              &dryRun,
		InstanceTypes:       iTypes,
		ProductDescriptions: []string{"Linux/UNIX"},
		StartTime:           &startTime,
	}

	descOutput, err := ec2Client.DescribeSpotPriceHistory(ctx, descInput)
	if err != nil {
		return err
	}

	for _, entry := range descOutput.SpotPriceHistory {
		iType := entry.InstanceType
		azName := *entry.AvailabilityZone
		curPrice, err := strconv.ParseFloat(*entry.SpotPrice, 64)
		if err != nil {
			return fmt.Errorf("Failed to parse float %v for %v:%v:%v: %w",
				entry.SpotPrice, iType, curReg, azName, err)
		}
		lookupAz := &LookupEc2SpotPriceAz{
			AzName:   azName,
			CurPrice: curPrice,
		}

		result.mutex.Lock()

		result.InstanceTypes[iType].Regions[curReg].Azs[azName] = lookupAz
		setCheapest(result, iType, curReg, azName, lookupAz)

		result.mutex.Unlock()
	}

	return nil
}

func setCheapest(result *LookupEc2SpotPriceResult, iType types.InstanceType,
	reg string, azName string, lookupAz *LookupEc2SpotPriceAz) {

	// set cheapest az in region for this iType
	lookupReg := result.InstanceTypes[iType].Regions[reg]
	if lookupReg.CheapestAz == nil {
		lookupReg.CheapestAz = lookupAz
	} else {
		if lookupAz.CurPrice < lookupReg.CheapestAz.CurPrice {
			lookupReg.CheapestAz = lookupAz
		}
	}

	// set cheapest region for this iType
	lookupIType := result.InstanceTypes[iType]
	if lookupIType.CheapestRegion == nil {
		lookupIType.CheapestRegion = lookupReg
	} else {
		if lookupAz.CurPrice < lookupIType.CheapestRegion.CheapestAz.CurPrice {
			lookupIType.CheapestRegion = lookupReg
		}
	}

	// set cheapest iType
	if result.CheapestIType == nil {
		result.CheapestIType = lookupIType
	} else {
		if lookupAz.CurPrice < result.CheapestIType.CheapestRegion.CheapestAz.CurPrice {
			result.CheapestIType = lookupIType
		}
	}
}

func getRegions() ([]string, error) {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-2"))
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.NewFromConfig(awsCfg)

	dryRun := false
	// only include regions that are not disabled
	allReg := false
	descRegInput := &ec2.DescribeRegionsInput{
		AllRegions: &allReg,
		DryRun:     &dryRun,
	}

	descRegOut, err := ec2Client.DescribeRegions(ctx, descRegInput)
	if err != nil {
		return nil, err
	}

	regList := make([]string, 0)
	for _, regOut := range descRegOut.Regions {
		regList = append(regList, *regOut.RegionName)
	}

	return regList, nil
}
