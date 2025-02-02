package v2

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	porterv1 "github.com/porter-dev/api-contracts/generated/go/porter/v1"
	"github.com/porter-dev/porter/internal/telemetry"
)

// AppProtoFromYaml converts a Porter YAML file into a PorterApp proto object
func AppProtoFromYaml(ctx context.Context, porterYamlBytes []byte) (*porterv1.PorterApp, error) {
	ctx, span := telemetry.NewSpan(ctx, "v2-app-proto-from-yaml")
	defer span.End()

	if porterYamlBytes == nil {
		return nil, telemetry.Error(ctx, span, nil, "porter yaml is nil")
	}

	porterYaml := &PorterYAML{}
	err := yaml.Unmarshal(porterYamlBytes, porterYaml)
	if err != nil {
		return nil, telemetry.Error(ctx, span, err, "error unmarshaling porter yaml")
	}

	appProto := &porterv1.PorterApp{
		Name: porterYaml.Name,
		Env:  porterYaml.Env,
	}

	if porterYaml.Build != nil {
		appProto.Build = &porterv1.Build{
			Context:    porterYaml.Build.Context,
			Method:     porterYaml.Build.Method,
			Builder:    porterYaml.Build.Builder,
			Buildpacks: porterYaml.Build.Buildpacks,
			Dockerfile: porterYaml.Build.Dockerfile,
		}
	}

	if porterYaml.Image != nil {
		appProto.Image = &porterv1.AppImage{
			Repository: porterYaml.Image.Repository,
			Tag:        porterYaml.Image.Tag,
		}
	}

	if porterYaml.Services == nil {
		return nil, telemetry.Error(ctx, span, nil, "porter yaml is missing services")
	}

	services := make(map[string]*porterv1.Service, 0)
	for name, service := range porterYaml.Services {
		serviceType, err := protoEnumFromType(name, service)
		if err != nil {
			return nil, telemetry.Error(ctx, span, err, "error getting service type")
		}

		serviceProto, err := serviceProtoFromConfig(service, serviceType)
		if err != nil {
			return nil, telemetry.Error(ctx, span, err, "error casting service config")
		}

		services[name] = serviceProto
	}
	appProto.Services = services

	if porterYaml.Predeploy != nil {
		predeployProto, err := serviceProtoFromConfig(*porterYaml.Predeploy, porterv1.ServiceType_SERVICE_TYPE_JOB)
		if err != nil {
			return nil, telemetry.Error(ctx, span, err, "error casting predeploy config")
		}
		appProto.Predeploy = predeployProto
	}

	return appProto, nil
}

// PorterYAML represents all the possible fields in a Porter YAML file
type PorterYAML struct {
	Name     string             `yaml:"name"`
	Services map[string]Service `yaml:"services"`
	Image    *Image             `yaml:"image"`
	Build    *Build             `yaml:"build"`
	Env      map[string]string  `yaml:"env"`

	Predeploy *Service `yaml:"predeploy"`
}

// Build represents the build settings for a Porter app
type Build struct {
	Context    string   `yaml:"context" validate:"dir"`
	Method     string   `yaml:"method" validate:"required,oneof=pack docker registry"`
	Builder    string   `yaml:"builder" validate:"required_if=Method pack"`
	Buildpacks []string `yaml:"buildpacks"`
	Dockerfile string   `yaml:"dockerfile" validate:"required_if=Method docker"`
}

// Service represents a single service in a porter app
type Service struct {
	Run             string       `yaml:"run"`
	Type            string       `yaml:"type" validate:"required, oneof=web worker job"`
	Instances       int          `yaml:"instances"`
	CpuCores        float32      `yaml:"cpuCores"`
	RamMegabytes    int          `yaml:"ramMegabytes"`
	Port            int          `yaml:"port"`
	Autoscaling     *AutoScaling `yaml:"autoscaling,omitempty" validate:"excluded_if=Type job"`
	Domains         []Domains    `yaml:"domains" validate:"excluded_unless=Type web"`
	HealthCheck     *HealthCheck `yaml:"healthCheck,omitempty" validate:"excluded_unless=Type web"`
	AllowConcurrent bool         `yaml:"allowConcurrent" validate:"excluded_unless=Type job"`
	Cron            string       `yaml:"cron" validate:"excluded_unless=Type job"`
}

// AutoScaling represents the autoscaling settings for web services
type AutoScaling struct {
	Enabled                bool `yaml:"enabled"`
	MinInstances           int  `yaml:"minInstances"`
	MaxInstances           int  `yaml:"maxInstances"`
	CpuThresholdPercent    int  `yaml:"cpuThresholdPercent"`
	MemoryThresholdPercent int  `yaml:"memoryThresholdPercent"`
}

// Domains are the custom domains for a web service
type Domains struct {
	Name string `yaml:"name"`
}

// HealthCheck is the health check settings for a web service
type HealthCheck struct {
	Enabled  bool   `yaml:"enabled"`
	HttpPath string `yaml:"httpPath"`
}

// Image is the repository and tag for an app's build image
type Image struct {
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag"`
}

func protoEnumFromType(name string, service Service) (porterv1.ServiceType, error) {
	var serviceType porterv1.ServiceType

	if service.Type != "" {
		if service.Type == "web" {
			return porterv1.ServiceType_SERVICE_TYPE_WEB, nil
		}
		if service.Type == "worker" {
			return porterv1.ServiceType_SERVICE_TYPE_WORKER, nil
		}
		if service.Type == "job" {
			return porterv1.ServiceType_SERVICE_TYPE_JOB, nil
		}

		return serviceType, fmt.Errorf("invalid service type '%s'", service.Type)
	}

	if strings.Contains(name, "web") {
		return porterv1.ServiceType_SERVICE_TYPE_WEB, nil
	}

	if strings.Contains(name, "wkr") {
		return porterv1.ServiceType_SERVICE_TYPE_WORKER, nil
	}

	if strings.Contains(name, "job") {
		return porterv1.ServiceType_SERVICE_TYPE_JOB, nil
	}

	return serviceType, errors.New("no type provided and could not parse service type from name")
}

func serviceProtoFromConfig(service Service, serviceType porterv1.ServiceType) (*porterv1.Service, error) {
	serviceProto := &porterv1.Service{
		Run:          service.Run,
		Type:         serviceType,
		Instances:    int32(service.Instances),
		CpuCores:     service.CpuCores,
		RamMegabytes: int32(service.RamMegabytes),
		Port:         int32(service.Port),
	}

	switch serviceType {
	default:
		return nil, fmt.Errorf("invalid service type '%s'", serviceType)
	case porterv1.ServiceType_SERVICE_TYPE_UNSPECIFIED:
		return nil, errors.New("Service type unspecified")
	case porterv1.ServiceType_SERVICE_TYPE_WEB:
		webConfig := &porterv1.WebServiceConfig{}

		var autoscaling *porterv1.Autoscaling
		if service.Autoscaling != nil {
			autoscaling = &porterv1.Autoscaling{
				Enabled:                service.Autoscaling.Enabled,
				MinInstances:           int32(service.Autoscaling.MinInstances),
				MaxInstances:           int32(service.Autoscaling.MaxInstances),
				CpuThresholdPercent:    int32(service.Autoscaling.CpuThresholdPercent),
				MemoryThresholdPercent: int32(service.Autoscaling.MemoryThresholdPercent),
			}
		}
		webConfig.Autoscaling = autoscaling

		var healthCheck *porterv1.HealthCheck
		if service.HealthCheck != nil {
			healthCheck = &porterv1.HealthCheck{
				Enabled:  service.HealthCheck.Enabled,
				HttpPath: service.HealthCheck.HttpPath,
			}
		}
		webConfig.HealthCheck = healthCheck

		domains := make([]*porterv1.Domain, 0)
		for _, domain := range service.Domains {
			domains = append(domains, &porterv1.Domain{
				Name: domain.Name,
			})
		}
		webConfig.Domains = domains

		serviceProto.Config = &porterv1.Service_WebConfig{
			WebConfig: webConfig,
		}
	case porterv1.ServiceType_SERVICE_TYPE_WORKER:
		workerConfig := &porterv1.WorkerServiceConfig{}

		var autoscaling *porterv1.Autoscaling
		if service.Autoscaling != nil {
			autoscaling = &porterv1.Autoscaling{
				Enabled:                service.Autoscaling.Enabled,
				MinInstances:           int32(service.Autoscaling.MinInstances),
				MaxInstances:           int32(service.Autoscaling.MaxInstances),
				CpuThresholdPercent:    int32(service.Autoscaling.CpuThresholdPercent),
				MemoryThresholdPercent: int32(service.Autoscaling.MemoryThresholdPercent),
			}
		}
		workerConfig.Autoscaling = autoscaling

		serviceProto.Config = &porterv1.Service_WorkerConfig{
			WorkerConfig: workerConfig,
		}
	case porterv1.ServiceType_SERVICE_TYPE_JOB:
		jobConfig := &porterv1.JobServiceConfig{
			AllowConcurrent: service.AllowConcurrent,
			Cron:            service.Cron,
		}

		serviceProto.Config = &porterv1.Service_JobConfig{
			JobConfig: jobConfig,
		}
	}

	return serviceProto, nil
}
