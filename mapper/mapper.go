package mapper

import (
	"encoding/json"
	"fmt"
	"os"
)

type AwsInstance struct {
	Instance         string `json:"instance_id"`
	AMI              string `json:"ami"`
	Type             string `json:"type"`
	AvailabilityZone string `json:"availability_zone"`
}

func FindInstances(path string) ([]AwsInstance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Read File failed: %w", err)
	}

	var instances []AwsInstance //Populates the slice with instances
	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, fmt.Errorf("Unmarshal Error %w", err)
	}
	return instances, nil
}
