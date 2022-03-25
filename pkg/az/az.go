package az

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/cloudflare/cfssl/log"

	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha5"
	"github.com/weaveworks/eksctl/pkg/utils/strings"
)

var zoneIDsToAvoid = map[string][]string{
	api.RegionCNNorth1: {"cnn1-az4"}, // https://github.com/weaveworks/eksctl/issues/3916
}

func GetAvailabilityZones(ec2API ec2iface.EC2API, region string) ([]string, error) {
	zones, err := getAvailabilityZones(ec2API, region)
	if err != nil {
		return nil, err
	}

	numberOfZones := len(zones)
	if numberOfZones < api.MinRequiredAvailabilityZones {
		return nil, fmt.Errorf("only %d zones discovered %v, at least %d are required", numberOfZones, zones, api.MinRequiredAvailabilityZones)
	}

	if numberOfZones < api.RecommendedAvailabilityZones {
		return zones, nil
	}

	return randomSelectionOfZones(region, zones), nil
}

func randomSelectionOfZones(region string, availableZones []string) []string {
	var zones []string
	desiredNumberOfAZs := api.RecommendedAvailabilityZones
	if region == api.RegionUSEast1 {
		desiredNumberOfAZs = api.MinRequiredAvailabilityZones
	}

	for len(zones) < desiredNumberOfAZs {
		rand := rand.New(rand.NewSource(time.Now().UnixNano()))
		for _, rn := range rand.Perm(len(availableZones)) {
			zones = append(zones, availableZones[rn])
			if len(zones) == desiredNumberOfAZs {
				break
			}
		}
	}

	return zones
}

func getAvailabilityZones(ec2API ec2iface.EC2API, region string) ([]string, error) {
	input := &ec2.DescribeAvailabilityZonesInput{
		Filters: []*ec2.Filter{
			makeFilter("region-name", region),
			makeFilter("state", ec2.AvailabilityZoneStateAvailable),
		},
	}

	output, err := ec2API.DescribeAvailabilityZones(input)
	if err != nil {
		return nil, fmt.Errorf("error getting availability zones for region %s: %w", region, err)
	}

	return filterZones(region, output.AvailabilityZones), nil
}

func filterZones(region string, zones []*ec2.AvailabilityZone) []string {
	filteredZones := []string{}
	azsToAvoid := zoneIDsToAvoid[region]
	for _, z := range zones {
		if !strings.Contains(azsToAvoid, *z.ZoneId) {
			filteredZones = append(filteredZones, *z.ZoneName)
		}
	}

	return filteredZones
}

func makeFilter(name, value string) *ec2.Filter {
	return &ec2.Filter{
		Name:   aws.String(name),
		Values: aws.StringSlice([]string{value}),
	}
}

// SetLocalZones sets the given local zone(s)
func SetLocalZones(spec *api.ClusterConfig, ec2Api ec2iface.EC2API, region string) error {
	if count := len(spec.LocalZones); count == 0 {
		return nil
	}

	if spec.VPC.ID != "" {
		log.Warning("ignoring localZones since existing VPC ID was specified; Local Zones are currently only supported for creating VPCs, not for creating EKS clusters. For more info, see: https://docs.aws.amazon.com/eks/latest/userguide/local-zones.html")
	}

	output, err := ec2Api.DescribeAvailabilityZones(&ec2.DescribeAvailabilityZonesInput{
		ZoneNames: aws.StringSlice(spec.LocalZones),
		Filters: []*ec2.Filter{
			makeFilter("region-name", region),
			makeFilter("zone-type", "local-zone"),
			makeFilter("state", "available"),
		},
	})
	if err != nil {
		return fmt.Errorf("error validating local zone(s) %s: %w", spec.LocalZones, err)
	}

	spec.LocalZones = filterZones(region, output.AvailabilityZones)

	return nil
}
